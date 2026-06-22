package server

import (
	"io"
	"strconv"
	"strings"
	"time"
)

// ── Dialog input: drain terminal responses, disambiguate Esc ─────────────────
//
// Problem
//   On hosts whose prompt (starship / oh-my-zsh / fish config) sends terminal
//   queries (OSC 11 background-color query, OSC 12 cursor-color query, DEC
//   device-attributes, ...), the local terminal writes the *responses* back to
//   stdin. While stdin is being forwarded to the remote shell this is harmless
//   (the prompt consumes its own responses). But while the upload dialog holds
//   stdin, those responses pollute the dialog's byte-at-a-time read:
//
//     - An OSC 11 response (ESC ] 11 ; rgb:... ST) starts with 0x1b, which the
//       menu read loop mistook for a user pressing Esc → spurious "cancelled".
//     - The rest of the response ("]11;rgb:...") leaked to the remote fish/zsh
//       after the dialog closed → "fish: Unknown command: ']11'".
//
// Fix
//   readSignificantByte reads the next byte that is part of a real keypress,
//   draining (discarding) any terminal-generated escape sequence (OSC/CSI/SS3).
//   A standalone Esc is distinguished from the start of a sequence by polling
//   stdin for a following byte within escPeekTimeout: terminal responses arrive
//   within a few ms (and usually atomically in one chunk), whereas a human
//   pressing Esc and nothing else exceeds that window.

// escPeekTimeout is how long readSignificantByte waits for a byte following an
// ESC before deciding the ESC is a standalone key. Terminal escape-sequence
// bytes follow the ESC within microseconds-to-low-milliseconds; a human double
// keypress is > 30ms. 25ms sits safely above response latency and well below
// human perception, so standalone Esc still feels instant.
const escPeekTimeout = 25 * time.Millisecond

// drainByteTimeout is the per-byte timeout used while draining a known escape
// sequence. Responses arrive atomically, so bytes appear within this window; if
// they don't, we stop draining (the partial sequence is discarded — preferable
// to blocking the dialog on a split/malformed sequence).
const drainByteTimeout = 10 * time.Millisecond

// stdinReadable is the readability probe used by the dialog key reader. It is a
// variable (initialized to the platform stdinReadableWithin) so tests can
// override it to feed deterministic input via a bytesReader.
var stdinReadable = stdinReadableWithin

// readSignificantByte reads the next byte from r that belongs to a real
// keypress, draining terminal escape sequences (OSC/CSI/SS3) first.
//
// Returns (b, true) for a real key byte: printable bytes, CR/LF, Tab, BS
// (0x7f/0x08), Ctrl+C (0x03), Ctrl+U (0x15), and standalone Esc (0x1b).
// Returns (0, false) when an escape sequence was drained (caller continues)
// or on read error/end (caller continues/aborts).
func readSignificantByte(r io.Reader) (byte, bool) {
	var b [1]byte
	n, err := r.Read(b[:])
	if n != 1 || err != nil {
		return 0, false
	}
	if b[0] != 0x1b {
		return b[0], true
	}

	// ESC: peek for a following byte to decide standalone-Esc vs sequence.
	next, ok := peekByte(r, escPeekTimeout, stdinReadable)
	if !ok {
		return 0x1b, true // standalone Esc (cancel)
	}
	switch next {
	case ']': // OSC: ESC ] params ST  (ST = BEL or ESC \)
		drainOSC(r, stdinReadable)
		return 0, false
	case '[': // CSI: ESC [ params <final 0x40-0x7e>
		// Read the full sequence so we can recognize kitty-keyboard-encoded
		// cancel keys (Esc=ESC[27u, Ctrl+C=ESC[3;5u) in case the local kitty
		// disable didn't take effect. Other CSI sequences are drained.
		seq := readCSIReturning(r, stdinReadable)
		if b, ok := parseKittyKey(seq); ok {
			debugf("ctrl-key: synthesized kitty key 0x%02x from CSI%su", b, seq)
			return b, true
		}
		return 0, false
	case 'O': // SS3: ESC O <one byte>
		drainN(r, 1, stdinReadable)
		return 0, false
	case 0x1b: // two ESCs in a row — treat the second as a new standalone Esc
		return 0x1b, true
	default: // Alt+key or other combo — drop the modifier byte
		return 0, false
	}
}

// peekByte returns the next byte if one is available within timeout (per the
// supplied readable probe), without blocking beyond it. Used to disambiguate a
// bare Esc.
func peekByte(r io.Reader, timeout time.Duration, readable func(time.Duration) bool) (byte, bool) {
	if !readable(timeout) {
		return 0, false
	}
	var b [1]byte
	n, err := r.Read(b[:])
	if n == 1 && err == nil {
		return b[0], true
	}
	return 0, false
}

// drainOSC consumes an OSC sequence body (after the leading "ESC ]") up to and
// including its String Terminator (BEL 0x07, or ESC \). Bounded by length and
// per-byte probe so a malformed/split sequence can never block the dialog.
//
// Note: a BEL (0x07) here is the OSC terminator, not a Ctrl+G — Ctrl+G under
// kitty keyboard mode is CSI 103;5u, which never enters this OSC path.
func drainOSC(r io.Reader, readable func(time.Duration) bool) {
	var prev byte
	for i := 0; i < 256; i++ {
		if !readable(drainByteTimeout) {
			return
		}
		var b [1]byte
		n, err := r.Read(b[:])
		if n != 1 || err != nil {
			return
		}
		if b[0] == 0x07 { // BEL terminator
			return
		}
		if prev == 0x1b && b[0] == 0x5c { // ESC \ terminator
			return
		}
		prev = b[0]
	}
}

// drainCSI consumes a CSI sequence body (after the leading "ESC [") up to and
// including its final byte (0x40-0x7e). Bounded by length and per-byte probe.
func drainCSI(r io.Reader, readable func(time.Duration) bool) {
	_ = readCSIReturning(r, readable)
}

// readCSIReturning reads a CSI sequence body (after the leading "ESC [") up to
// and including its final byte (0x40-0x7e), returning the body as a string
// (without the final byte if the reader ends early). Bounded by length and
// per-byte probe so a malformed/split sequence can never block the dialog.
func readCSIReturning(r io.Reader, readable func(time.Duration) bool) string {
	var sb strings.Builder
	for i := 0; i < 64; i++ {
		if !readable(drainByteTimeout) {
			return sb.String()
		}
		var b [1]byte
		n, err := r.Read(b[:])
		if n != 1 || err != nil {
			return sb.String()
		}
		sb.WriteByte(b[0])
		if b[0] >= 0x40 && b[0] <= 0x7e { // final byte
			return sb.String()
		}
	}
	return sb.String()
}

// parseKittyKey inspects a CSI sequence body (the bytes between ESC[ and the
// final byte) for a kitty keyboard protocol key encoding of the form
// "<codepoint>[;<modifier>]u". If it encodes Esc (codepoint 27) or Ctrl+C
// (codepoint 3), it returns the legacy byte (0x1b / 0x03) so the dialog's
// existing cancel handling works even if the local kitty-keyboard disable
// didn't take effect. Returns (0,false) for any other sequence.
func parseKittyKey(seq string) (byte, bool) {
	if !strings.HasSuffix(seq, "u") {
		return 0, false
	}
	body := strings.TrimSuffix(seq, "u")
	// Split optional modifier: "103;5" -> codepoint="103", mod present.
	codepointStr := body
	if i := strings.IndexByte(body, ';'); i >= 0 {
		codepointStr = body[:i]
	}
	cp, err := strconv.Atoi(codepointStr)
	if err != nil {
		return 0, false
	}
	switch cp {
	case 27: // ESC key
		return 0x1b, true
	case 3: // ETX = Ctrl+C
		return 0x03, true
	}
	return 0, false
}

// drainN consumes up to count bytes, each bounded by drainByteTimeout.
func drainN(r io.Reader, count int, readable func(time.Duration) bool) {
	for i := 0; i < count; i++ {
		if !readable(drainByteTimeout) {
			return
		}
		var b [1]byte
		n, _ := r.Read(b[:])
		if n != 1 {
			return
		}
	}
}

// readByteOrStop reads one byte, but polls stdin in short slices so the caller
// can stop it promptly via the stop channel. Returns (b, true) on a byte,
// (0, false) if stop was closed or the reader errored. Used by the upload/
// download cancel listener so it never deadlocks on stop (the previous
// pipe-based listener could block forever on a Read with no input).
func readByteOrStop(r io.Reader, stop <-chan struct{}) (byte, bool) {
	for {
		select {
		case <-stop:
			return 0, false
		default:
		}
		if !stdinReadable(100 * time.Millisecond) {
			continue // no input; loop and re-check stop
		}
		var b [1]byte
		n, err := r.Read(b[:])
		if n == 1 && err == nil {
			return b[0], true
		}
		if err != nil && err != io.EOF {
			// transient read hiccup; keep polling (re-checks stop)
		}
		if err == io.EOF {
			return 0, false
		}
	}
}
