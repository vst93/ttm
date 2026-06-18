package server

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// bytesReader is a tiny io.Reader over a fixed byte slice, used to feed
// deterministic input to readSignificantByte / drain helpers without a real
// terminal. Read returns 0, io.EOF once exhausted.
type bytesReader struct {
	b []byte
	i int
}

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}

// alwaysReadable simulates a terminal that always has the next byte ready
// (the case for atomic OSC/CSI responses). Used to exercise the drain paths
// with a bytesReader, whose data is always "available".
func alwaysReadable(time.Duration) bool { return true }

// neverReadable simulates a terminal with no following byte — used to verify
// that a lone ESC is treated as a standalone Esc (cancel).
func neverReadable(time.Duration) bool { return false }

func withReadable(fn func(time.Duration) bool, body func()) {
	orig := stdinReadable
	stdinReadable = fn
	defer func() { stdinReadable = orig }()
	body()
}

func TestReadSignificantBytePrintable(t *testing.T) {
	r := &bytesReader{b: []byte("hello")}
	for _, want := range []byte("hello") {
		got, ok := readSignificantByte(r)
		if !ok {
			t.Fatal("expected ok")
		}
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

func TestReadSignificantByteDrainsOSC(t *testing.T) {
	// OSC 11 response: ESC ] 11 ; rgb:1e1e/1e1e/1e1e1e BEL  followed by '2'
	osc := []byte{0x1b, ']', '1', '1', ';', 'r', 'g', 'b', ':', 'x', 0x07}
	r := &bytesReader{b: append(osc, '2')}

	withReadable(alwaysReadable, func() {
		// First read: OSC is drained → (0, false)
		got, ok := readSignificantByte(r)
		if ok {
			t.Errorf("OSC should be drained, got byte %q", got)
		}
		// Next read returns the '2'.
		got, ok = readSignificantByte(r)
		if !ok || got != '2' {
			t.Errorf("after OSC drain, got %q ok=%v, want '2'", got, ok)
		}
	})
}

func TestReadSignificantByteDrainsOSCESCBackslash(t *testing.T) {
	// OSC terminated by ESC \ (0x1b 0x5c) instead of BEL.
	osc := []byte{0x1b, ']', '0', ';', 'x', 0x1b, 0x5c}
	r := &bytesReader{b: append(osc, '3')}

	withReadable(alwaysReadable, func() {
		got, ok := readSignificantByte(r)
		if ok {
			t.Errorf("OSC(ESC\\) should be drained, got byte %q", got)
		}
		got, ok = readSignificantByte(r)
		if !ok || got != '3' {
			t.Errorf("after OSC drain, got %q ok=%v, want '3'", got, ok)
		}
	})
}

func TestReadSignificantByteDrainsCSI(t *testing.T) {
	// CSI sequence: ESC [ 1 0 3 ; 5 u  (kitty Ctrl+G — in a dialog it's drained)
	csi := []byte{0x1b, '[', '1', '0', '3', ';', '5', 'u'}
	r := &bytesReader{b: append(csi, '1')}

	withReadable(alwaysReadable, func() {
		got, ok := readSignificantByte(r)
		if ok {
			t.Errorf("CSI should be drained, got byte %q", got)
		}
		got, ok = readSignificantByte(r)
		if !ok || got != '1' {
			t.Errorf("after CSI drain, got %q ok=%v, want '1'", got, ok)
		}
	})
}

func TestReadSignificantByteStandaloneEsc(t *testing.T) {
	// A lone ESC with nothing following should be returned as 0x1b (cancel).
	// With neverReadable, peekByte times out → standalone Esc.
	r := &bytesReader{b: []byte{0x1b}}
	withReadable(neverReadable, func() {
		got, ok := readSignificantByte(r)
		if !ok || got != 0x1b {
			t.Errorf("standalone ESC: got %q ok=%v, want 0x1b", got, ok)
		}
	})
}

func TestReadSignificantByteCtrlC(t *testing.T) {
	r := &bytesReader{b: []byte{0x03}}
	got, ok := readSignificantByte(r)
	if !ok || got != 0x03 {
		t.Errorf("Ctrl+C: got %q ok=%v, want 0x03", got, ok)
	}
}

func TestReadSignificantByteDrainsSS3(t *testing.T) {
	// SS3: ESC O P (F1 on some terminals) followed by a real key.
	r := &bytesReader{b: []byte{0x1b, 'O', 'P', 'y'}}
	withReadable(alwaysReadable, func() {
		got, ok := readSignificantByte(r)
		if ok {
			t.Errorf("SS3 should be drained, got byte %q", got)
		}
		got, ok = readSignificantByte(r)
		if !ok || got != 'y' {
			t.Errorf("after SS3 drain, got %q ok=%v, want 'y'", got, ok)
		}
	})
}

func TestDrainOSCBounded(t *testing.T) {
	// An OSC with no terminator must not loop forever — drainOSC stops at the
	// byte cap / when the reader is exhausted.
	r := &bytesReader{b: []byte{']', '1', '1', ';', 'x'}} // no ST, reader EOFs
	withReadable(alwaysReadable, func() { drainOSC(r, stdinReadable) })
}

func TestDrainCSIBounded(t *testing.T) {
	// CSI with no final byte — drainCSI must return (reader EOF).
	r := &bytesReader{b: []byte{'1', '2', '3'}}
	withReadable(alwaysReadable, func() { drainCSI(r, stdinReadable) })
}

func TestReadByteOrStopStopCloses(t *testing.T) {
	// readByteOrStop must return promptly when stop is closed, even with no
	// stdin data (no deadlock).
	r := &bytesReader{b: nil} // empty, EOFs
	stop := make(chan struct{})
	close(stop)
	if _, ok := readByteOrStop(r, stop); ok {
		t.Error("expected (0,false) when stop closed")
	}
}

func TestParseKittyKey(t *testing.T) {
	cases := []struct {
		seq   string
		wantB byte
		want  bool
	}{
		{"27u", 0x1b, true},   // Esc
		{"27;1u", 0x1b, true}, // Esc (explicit mod 1)
		{"3;5u", 0x03, true},  // Ctrl+C (codepoint 3, control mod)
		{"3u", 0x03, true},    // Ctrl+C (no modifier)
		{"103;5u", 0, false},  // Ctrl+G — not a cancel key in dialogs
		{"97u", 0, false},     // 'a' — not a cancel key
		{"5A", 0, false},      // CSI cuu (arrow up) — not kitty-u
		{"", 0, false},        // empty
		{"27", 0, false},      // no 'u' suffix
	}
	for _, c := range cases {
		gotB, got := parseKittyKey(c.seq)
		if got != c.want || (got && gotB != c.wantB) {
			t.Errorf("parseKittyKey(%q) = (0x%02x,%v), want (0x%02x,%v)", c.seq, gotB, got, c.wantB, c.want)
		}
	}
}

func TestReadSignificantByteKittyEsc(t *testing.T) {
	// kitty-encoded Esc: ESC[27u -> should synthesize 0x1b (cancel), NOT drain.
	data := []byte{0x1b, '[', '2', '7', 'u'}
	r := &bytesReader{b: data}
	withReadable(alwaysReadable, func() {
		got, ok := readSignificantByte(r)
		if !ok || got != 0x1b {
			t.Errorf("kitty Esc: got (0x%02x,%v), want (0x1b,true)", got, ok)
		}
	})
}

func TestReadSignificantByteKittyCtrlC(t *testing.T) {
	// kitty-encoded Ctrl+C: ESC[3;5u -> should synthesize 0x03 (cancel).
	data := []byte{0x1b, '[', '3', ';', '5', 'u'}
	r := &bytesReader{b: append(data, 'x')}
	withReadable(alwaysReadable, func() {
		got, ok := readSignificantByte(r)
		if !ok || got != 0x03 {
			t.Errorf("kitty Ctrl+C: got (0x%02x,%v), want (0x03,true)", got, ok)
		}
		// Following 'x' still readable.
		got, ok = readSignificantByte(r)
		if !ok || got != 'x' {
			t.Errorf("after kitty Ctrl+C, got (0x%02x,%v), want 'x'", got, ok)
		}
	})
}

func TestDirProgressRenderZeroTotal(t *testing.T) {
	// Regression: dirProgress.render() must not panic when total==0
	// (happens on download when remote file count fails). The live ticker
	// calls render() every 150ms, so this would have crashed the transfer.
	var buf bytes.Buffer
	p := &dirProgress{tty: &buf, total: 0, loc: localeEN}
	p.render()       // must not divide-by-zero panic
	p.update("file") // current=1, total=0 — must still not panic
	p.render()
}

func TestDirProgressByteTracking(t *testing.T) {
	// Byte-level progress should advance via addBytes and render with bytes.
	var buf bytes.Buffer
	p := &dirProgress{tty: &buf, total: 2, totalBytes: 1000, loc: localeEN}
	p.addBytes(250) // 25% of bytes
	p.render()
	out := buf.String()
	if !strings.Contains(out, "250 B/1000 B") {
		t.Errorf("expected byte progress in output, got: %q", out)
	}
	// Complete the bytes.
	p.addBytes(750)
	buf.Reset()
	p.render()
	out = buf.String()
	if !strings.Contains(out, "1000 B/1000 B") {
		t.Errorf("expected full byte progress, got: %q", out)
	}
}

func TestCountingReader(t *testing.T) {
	src := &bytesReader{b: []byte("hello world")}
	var total int
	c := &countingReader{r: src, onRead: func(n int) { total += n }}
	buf := make([]byte, 5)
	n, err := c.Read(buf)
	if err != nil || n != 5 {
		t.Fatalf("Read: n=%d err=%v", n, err)
	}
	if total != 5 {
		t.Errorf("after first read, total=%d want 5", total)
	}
}

func TestCountingWriter(t *testing.T) {
	var sink bytes.Buffer
	var total int
	c := &countingWriter{w: &sink, onWrite: func(n int) { total += n }}
	c.Write([]byte("abc"))
	c.Write([]byte("defgh"))
	if total != 8 {
		t.Errorf("total=%d want 8", total)
	}
	if sink.String() != "abcdefgh" {
		t.Errorf("sink=%q want abcdefgh", sink.String())
	}
}

func TestDirProgressByteLevelBar(t *testing.T) {
	// Byte-level progress must render during large-file transfers, not just
	// when a file completes. totalBytes drives the bar fill proportionally.
	var buf bytes.Buffer
	p := &dirProgress{tty: &buf, total: 3, totalBytes: 1000, loc: localeEN}
	p.render()
	if !strings.Contains(buf.String(), "0 B/1000 B") {
		t.Errorf("expected byte total in render, got: %q", buf.String())
	}
	buf.Reset()
	p.addBytes(500) // 50% of bytes
	p.render()
	out := buf.String()
	if !strings.Contains(out, "500 B/1000 B") {
		t.Errorf("expected 500B/1000B, got: %q", out)
	}
	// Bar should be half-filled (10 of 20).
	if filled := strings.Count(out, "█"); filled != 10 {
		t.Errorf("expected 10 filled bars (50%%), got %d in %q", filled, out)
	}
}
