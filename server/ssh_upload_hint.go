package server

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshConnInfo holds the connection details needed to generate upload commands.
type sshConnInfo struct {
	User string
	Host string
	Port int
}

// fullscreenPrograms lists common TUI/fullscreen programs that occupy the
// terminal. When any of these are running, Ctrl+G should not be intercepted
// to avoid conflicting with their keybindings.
var fullscreenPrograms = []string{
	"vim", "vi", "nvim",
	"nano", "pico",
	"less", "more",
}

// buildIdleCheckScript returns a POSIX sh script that prints "found" if any
// known fullscreen/TUI program is running on the remote host, and prints
// nothing otherwise. It is executed via remoteRunSh so it runs under /bin/sh
// regardless of the user's login shell (fish / zsh+oh-my-zsh / bash).
func buildIdleCheckScript() string {
	var cmds []string
	for _, prog := range fullscreenPrograms {
		cmds = append(cmds, "pgrep -x "+shQuote(prog)+" >/dev/null 2>&1")
	}
	return strings.Join(cmds, " || ") + " && echo found || true"
}

// isRemoteIdle checks if the remote SSH server has any fullscreen/TUI program
// running. Returns true if the remote appears idle (no fullscreen program
// detected, or the probe failed/timed out).
//
// The probe is bounded by a 1s timeout so a slow or hung remote shell (e.g.
// a framework-heavy fish/oh-my-zsh invoked for the exec session) can never
// block the Ctrl+G trigger — on timeout we assume idle and proceed.
func isRemoteIdle(client *ssh.Client) bool {
	if client == nil {
		return true
	}

	out, err := remoteRunSh(client, buildIdleCheckScript(), 1*time.Second)
	if err != nil {
		return true // assume idle on error or timeout
	}
	return out != "found"
}

// queryRemotePwd gets the interactive shell's cwd by injecting pwd into its stdin.
// The result is written to a temp file and read back via the middleware exec
// session. Returns (dir, detected).
//
// Tradeoff: the user will briefly see a `pwd > /tmp/.ttm_cwd` command on their
// terminal. This is the most reliable way to get the actual cwd.
func queryRemotePwd(stdinPipe io.WriteCloser, client *ssh.Client) (string, bool) {
	if dir := probeShellCwdViaStdin(stdinPipe, client); dir != "" {
		return dir, true
	}

	// Fallback: new session pwd (returns home directory).
	out, err := remoteRunSh(client, "pwd", 5*time.Second)
	if err != nil || out == "" {
		return "~", false
	}
	return out, false
}

// probeShellCwdViaStdin injects `pwd` into the interactive shell's stdin,
// captures the output via a temp file, and reads it back through the
// shell-agnostic middleware.
//
// The injected `pwd > /tmp/...` is portable across bash/zsh/fish (all support
// `>` redirection and the pwd builtin). The readback runs under /bin/sh via
// remoteRunSh so it never depends on the login shell's syntax.
//
// Note: uses /tmp which assumes a Unix-like remote server. Will silently fail
// on remote Windows servers (falls back to home directory detection).
func probeShellCwdViaStdin(stdinPipe io.WriteCloser, client *ssh.Client) string {
	tmpFile := fmt.Sprintf("/tmp/.ttm_cwd_%d_%d", os.Getpid(), time.Now().UnixNano()%100000)

	// Inject command into the interactive shell.
	// The shell will execute it and write cwd to the temp file.
	injectCmd := fmt.Sprintf("pwd > %s\n", tmpFile)
	if _, err := stdinPipe.Write([]byte(injectCmd)); err != nil {
		return ""
	}

	// Wait for the shell to execute the command.
	time.Sleep(800 * time.Millisecond)

	// Read the temp file via the middleware, then clean up.
	script := "cat " + shQuote(tmpFile) + " 2>/dev/null; rm -f " + shQuote(tmpFile)
	out, err := remoteRunSh(client, script, 5*time.Second)
	if err != nil {
		return ""
	}

	if out == "" || out == "/" {
		return ""
	}
	return out
}

// buildUploadCmd generates an scp command string with a placeholder for the
// local file path.
func buildUploadCmd(info sshConnInfo, remoteDir string) string {
	port := info.Port
	if port <= 0 {
		port = 22
	}
	user := info.User
	if user == "" {
		user = "root"
	}
	return fmt.Sprintf("scp -P %d <local_file> %s@%s:%s/",
		port, user, info.Host, remoteDir)
}

// localeT is a standalone locale helper (works without AppModel).
func localeT(loc locale, en, zh string) string {
	if loc == localeZH {
		return zh
	}
	return en
}

// buildSSHConnInfo extracts connection details from an SSH client and config.
func buildSSHConnInfo(client *ssh.Client, node *SSHConfig) sshConnInfo {
	user := node.User
	if user == "" {
		user = "root"
	}
	port := node.Port
	if port <= 0 {
		port = 22
	}
	return sshConnInfo{
		User: user,
		Host: node.Host,
		Port: port,
	}
}

// ── Keystroke interception (buffered) ─────────────────────────────────────────

// ctrlGByte is the byte value for Ctrl+G (0x07, BEL). Primary trigger.
const ctrlGByte byte = 0x07

// readBufSize is the buffer size for stdin reads. Matches typical io.Copy
// buffer size for parity with the original unintercepted path.
const readBufSize = 32 * 1024

// triggerMode returns the Ctrl+G trigger mode from the TTM_TRIGGER env var.
// "single" = a single Ctrl+G triggers the dialog (diagnostic / convenience);
// "double" (default) = a double-tap within the window triggers it.
func triggerMode() string {
	if strings.ToLower(os.Getenv("TTM_TRIGGER")) == "single" {
		return "single"
	}
	return "double"
}

// debugChunk logs a hex preview of a stdin chunk when it contains a control
// character (Ctrl+G, escape sequences, etc.). Ordinary printable typing is
// not logged, so the trace stays small. Used to confirm whether Ctrl+G
// (0x07) is actually reaching the local stdin reader.
func debugChunk(data []byte) {
	for _, b := range data {
		if b == ctrlGByte || (b < 0x20 && b != '\r' && b != '\n' && b != '\t') {
			n := len(data)
			if n > 32 {
				n = 32
			}
			debugf("stdin: chunk len=%d hex=%x", len(data), data[:n])
			return
		}
	}
}

// ── Kitty keyboard protocol support ───────────────────────────────────────────
//
// When a remote shell or prompt enables the kitty keyboard protocol (CSI > 1 u),
// capable terminals (Ghostty, kitty, WezTerm, ...) stop sending legacy control
// bytes and instead send disambiguated CSI u sequences. Ctrl+G then arrives as
// ESC[103;5u (codepoint 'g'=103, modifier control=5) instead of the raw 0x07.
//
// This is common on hosts running fish + starship or zsh + oh-my-zsh with a
// kitty-keyboard-aware prompt/plugin. Without recognizing this encoding, the
// double-tap trigger never fires on such hosts — the 0x07 byte simply never
// appears. scanCtrlGEvents recognizes BOTH encodings so the trigger works
// regardless of whether kitty keyboard mode is active.

// ctrlGEvent is a detected Ctrl+G keypress within a stdin chunk. start/end are
// byte offsets; the event is either a raw 0x07 byte (len 1) or the kitty
// keyboard sequence ESC[103;5u (len 8).
type ctrlGEvent struct{ start, end int }

// kittyCtrlGSeq is the kitty keyboard protocol encoding of Ctrl+G:
// CSI 103 ; 5 u  (codepoint 'g'=103, modifier control=5).
var kittyCtrlGSeq = []byte{0x1b, 0x5b, '1', '0', '3', ';', '5', 'u'}

// scanCtrlGEvents finds all Ctrl+G events in data, matching both the raw 0x07
// byte and the kitty keyboard sequence ESC[103;5u. Events are returned sorted
// by position. In-chunk matching only: the kitty sequence is delivered
// atomically per keypress by terminals, so cross-read splitting is not handled
// (a split press would simply be forwarded and the user re-presses — no other
// keys are affected, keeping this safe for normal typing/arrows).
func scanCtrlGEvents(data []byte) []ctrlGEvent {
	var events []ctrlGEvent
	i := 0
	for i < len(data) {
		// Skip OSC sequences (ESC] ... BEL/ESC\) — their BEL terminators
		// must not be matched as Ctrl+G. This is the primary source of
		// false positives from terminal color-query responses.
		if data[i] == 0x1b && i+1 < len(data) && data[i+1] == 0x5d {
			// OSC: scan for terminator (BEL 0x07 or ESC\ 0x1b5c).
			i += 2
			for i < len(data) {
				if data[i] == 0x07 { // BEL terminator
					i++
					break
				}
				if data[i] == 0x1b && i+1 < len(data) && data[i+1] == 0x5c { // ESC\ terminator
					i += 2
					break
				}
				i++
			}
			continue
		}
		if data[i] == ctrlGByte {
			events = append(events, ctrlGEvent{i, i + 1})
			i++
			continue
		}
		// Kitty keyboard sequence: ESC[103;5u.
		if data[i] == 0x1b && i+1 < len(data) && data[i+1] == 0x5b &&
			i+len(kittyCtrlGSeq) <= len(data) &&
			bytes.Equal(data[i:i+len(kittyCtrlGSeq)], kittyCtrlGSeq) {
			events = append(events, ctrlGEvent{i, i + len(kittyCtrlGSeq)})
			i += len(kittyCtrlGSeq)
			continue
		}
		i++
	}
	return events
}

// startStdinCopyWithIntercept is like startStdinCopy but intercepts Ctrl+G
// to trigger a handler.
//
// Ctrl+G is recognized in two encodings:
//   - raw 0x07 (BEL), when the terminal is in legacy mode, and
//   - ESC[103;5u, the kitty keyboard protocol encoding, used when a remote
//     shell/prompt (e.g. fish + starship / oh-my-zsh) has enabled kitty
//     keyboard mode. Without this, the trigger never fires on such hosts.
//
// Uses a double-press mechanism to avoid conflicting with vim's Ctrl+G
//
// Ctrl+G is recognized in two encodings:
//   - raw 0x07 (BEL), when the terminal is in legacy mode, and
//   - ESC[103;5u, the kitty keyboard protocol encoding, used when a remote
//     shell/prompt (e.g. fish + starship / oh-my-zsh) has enabled kitty
//     keyboard mode. Without this, the trigger never fires on such hosts.
//
// Uses a double-press mechanism to avoid conflicting with vim's Ctrl+G
// (which shows file info). Behavior:
//   - Single Ctrl+G: forward to remote (vim shows file info), arm double-press window
//   - Double Ctrl+G (within window): first press forwarded, second press triggers handler
//     (NOT forwarded to remote, so vim doesn't see a second Ctrl+G)
//   - Noise suppression: multiple Ctrl+G in one chunk or rapid bursts (< noiseThreshold)
//     are forwarded as-is without triggering
//
// The handleTrigger callback receives the stdinReader so it can read user
// input directly (e.g. for a dialog). It is called synchronously, blocking
// stdin forwarding while the handler is active.
func startStdinCopyWithIntercept(stdinPipe io.WriteCloser, client *ssh.Client, handleTrigger func(io.Reader)) (<-chan error, func(), error) {
	stdinReader, err := newStdinReader()
	if err != nil {
		return nil, nil, err
	}

	// doublePressWindow is the max time between two Ctrl+G presses to
	// count as a double-press (trigger upload dialog).
	const doublePressWindow = 700 * time.Millisecond

	// noiseThreshold is the minimum time between two Ctrl+G presses for
	// them to be considered a genuine double-press. Presses closer than
	// this are treated as terminal protocol noise and forwarded as-is.
	const noiseThreshold = 30 * time.Millisecond

	done := make(chan error, 1)
	go func() {
		// First trace line: proves the new binary is running, the goroutine
		// started, and the log is writable. If this line is missing, the
		// build is stale or log creation failed.
		debugf("intercept: started mode=%s log=%s", triggerMode(), debugLogPath())

		buf := make([]byte, readBufSize)
		var lastCtrlGTime time.Time // time of last Ctrl+G that was forwarded

		forward := func(data []byte) error {
			if len(data) == 0 {
				return nil
			}
			_, err := stdinPipe.Write(data)
			return err
		}

		// triggerEvent forwards all bytes of data except those of the Ctrl+G
		// event e, then invokes the handler. The event bytes (raw 0x07 or the
		// kitty sequence) are deliberately not forwarded so the remote shell
		// does not see a second Ctrl+G.
		triggerEvent := func(data []byte, e ctrlGEvent) error {
			if err := forward(data[:e.start]); err != nil {
				return err
			}
			if e.end < len(data) {
				if err := forward(data[e.end:]); err != nil {
					return err
				}
			}
			if handleTrigger != nil {
				handleTrigger(stdinReader)
			}
			return nil
		}

		for {
			n, readErr := stdinReader.Read(buf)
			if n > 0 {
				data := buf[:n]
				debugChunk(data)

				// Detect Ctrl+G events. A press may arrive as either a raw 0x07
				// byte (kitty keyboard OFF) or the kitty keyboard sequence
				// ESC[103;5u (kitty keyboard ON — enabled by some remote
				// shells/prompts, e.g. fish + starship / oh-my-zsh). Without
				// recognizing the kitty form, Ctrl+G never triggers on such hosts.
				events := scanCtrlGEvents(data)
				ctrlGCount := len(events)

				switch {
				case ctrlGCount == 2:
					// Two Ctrl+G in one chunk — always noise (real double-presses
					// arrive in separate reads). Forward as-is.
					debugf("ctrl+g: 2-in-chunk (noise) -> forward")
					if err := forward(data); err != nil {
						done <- err
						_ = stdinPipe.Close()
						return
					}
					lastCtrlGTime = time.Time{}

				case ctrlGCount > 2:
					// More than two → terminal noise. Forward as-is.
					debugf("ctrl+g: %d-in-chunk (noise) -> forward", ctrlGCount)
					if err := forward(data); err != nil {
						done <- err
						_ = stdinPipe.Close()
						return
					}
					lastCtrlGTime = time.Time{}

				case ctrlGCount == 1:
					now := time.Now()
					e := events[0]

					// Single-press mode (TTM_TRIGGER=single): trigger on the first
					// Ctrl+G without waiting for a second press.
					if triggerMode() == "single" {
						debugf("ctrl+g: single-mode -> trigger")
						if err := triggerEvent(data, e); err != nil {
							done <- err
							_ = stdinPipe.Close()
							return
						}
						lastCtrlGTime = time.Time{}
						break
					}

					if !lastCtrlGTime.IsZero() && now.Sub(lastCtrlGTime) < doublePressWindow {
						// A previous Ctrl+G was recently forwarded.
						if now.Sub(lastCtrlGTime) < noiseThreshold {
							// Too fast — likely terminal noise. Forward as-is.
							debugf("ctrl+g: 2nd too fast (%v) -> noise", now.Sub(lastCtrlGTime))
							if err := forward(data); err != nil {
								done <- err
								_ = stdinPipe.Close()
								return
							}
							lastCtrlGTime = time.Time{}
							break
						}

						// Genuine double-press! Check remote idle before triggering.
						if !isRemoteIdle(client) {
							debugf("ctrl+g: double-press but remote busy -> forward")
							if err := forward(data); err != nil {
								done <- err
								_ = stdinPipe.Close()
								return
							}
							lastCtrlGTime = time.Time{}
							break
						}
						debugf("ctrl+g: double-press (%v) -> trigger", now.Sub(lastCtrlGTime))
						if err := triggerEvent(data, e); err != nil {
							done <- err
							_ = stdinPipe.Close()
							return
						}
						lastCtrlGTime = time.Time{}
						break
					}

					// First Ctrl+G press — forward to remote, arm window.
					// No idle check here to avoid blocking stdin (~180ms per
					// check). The idle check is deferred to the second press.
					debugf("ctrl+g: first press, arming")
					if err := forward(data); err != nil {
						done <- err
						_ = stdinPipe.Close()
						return
					}
					lastCtrlGTime = now

				default:
					// No Ctrl+G in this chunk — forward normally, reset state.
					if err := forward(data); err != nil {
						done <- err
						_ = stdinPipe.Close()
						return
					}
					lastCtrlGTime = time.Time{}
				}
			}

			if readErr != nil {
				if readErr == io.EOF {
					done <- nil
				} else {
					done <- readErr
				}
				_ = stdinPipe.Close()
				return
			}
		}
	}()

	cancel := func() {
		cancelStdinReader(stdinReader)
	}
	return done, cancel, nil
}

// ── Debounce ──────────────────────────────────────────────────────────────────

var uploadHintLastTrigger time.Time

const uploadHintDebounceDuration = 2 * time.Second

func shouldTriggerUploadHint() bool {
	now := time.Now()
	if now.Sub(uploadHintLastTrigger) < uploadHintDebounceDuration {
		return false
	}
	uploadHintLastTrigger = now
	return true
}
