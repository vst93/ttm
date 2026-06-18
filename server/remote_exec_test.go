package server

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestShQuote(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"vim", `"vim"`},
		{"", `""`},
		{"a b", `"a b"`},
		{"/tmp/.ttm_cwd_1_2", `"/tmp/.ttm_cwd_1_2"`},
		// A double quote in the input must be escaped as \" so the shell
		// reconstructs the literal value.
		{`it"s`, `"it\"s"`},
		// Backslash must be escaped.
		{`a\b`, `"a\\b"`},
		// Dollar sign must be escaped to prevent variable expansion.
		{`$HOME`, `"\$HOME"`},
	}
	for _, tt := range tests {
		got := shQuote(tt.in)
		if got != tt.want {
			t.Errorf("shQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWrapShScript(t *testing.T) {
	got := wrapShScript("echo hi")
	if want := `sh -c "echo hi"`; got != want {
		t.Fatalf("wrapShScript = %q, want %q", got, want)
	}

	// Inner double quotes must be escaped so the login shell hands sh a
	// verbatim script body (this is what makes it fish/zsh-agnostic).
	got = wrapShScript(`echo "a"`)
	want := `sh -c "echo \"a\""`
	if got != want {
		t.Errorf("wrapShScript = %q, want %q", got, want)
	}
}

func TestBuildIdleCheckScript(t *testing.T) {
	s := buildIdleCheckScript()
	for _, prog := range fullscreenPrograms {
		needle := "pgrep -x " + shQuote(prog)
		if !strings.Contains(s, needle) {
			t.Errorf("script missing pgrep for %q: %s", prog, s)
		}
	}
	if !strings.Contains(s, "echo found") {
		t.Errorf("script missing `echo found`: %s", s)
	}
	if !strings.Contains(s, " || ") {
		t.Errorf("script should chain pgrep checks with ||: %s", s)
	}
	if !strings.HasSuffix(s, " || true") {
		t.Errorf("script should end with ` || true` so it never fails: %s", s)
	}
}

func TestIsRemoteIdleNilClient(t *testing.T) {
	// No SSH server needed: nil client short-circuits to "idle".
	if !isRemoteIdle(nil) {
		t.Error("isRemoteIdle(nil) should return true (assume idle)")
	}
}

func TestRemoteRunShNilClient(t *testing.T) {
	if _, err := remoteRunSh(nil, "echo hi", time.Second); err == nil {
		t.Error("remoteRunSh(nil, ...) should return an error")
	}
}

func TestTriggerMode(t *testing.T) {
	orig := os.Getenv("TTM_TRIGGER")
	defer os.Setenv("TTM_TRIGGER", orig)

	os.Unsetenv("TTM_TRIGGER")
	if got := triggerMode(); got != "double" {
		t.Errorf("default triggerMode = %q, want double", got)
	}

	os.Setenv("TTM_TRIGGER", "single")
	if got := triggerMode(); got != "single" {
		t.Errorf("single triggerMode = %q, want single", got)
	}

	os.Setenv("TTM_TRIGGER", "SINGLE") // case-insensitive
	if got := triggerMode(); got != "single" {
		t.Errorf("SINGLE triggerMode = %q, want single", got)
	}

	os.Setenv("TTM_TRIGGER", "garbage")
	if got := triggerMode(); got != "double" {
		t.Errorf("garbage triggerMode = %q, want double", got)
	}
}

func TestDebugChunkNoLogging(t *testing.T) {
	// Printable-only chunks must not be considered "control" chunks.
	// We can't easily assert on the log file here, but we can at least
	// confirm the function runs without panic and returns quickly for
	// plain input.
	debugChunk([]byte("hello world"))
	debugChunk([]byte("ls -la\r\n"))   // CR/LF are not control chars here
	debugChunk([]byte{0x07})           // Ctrl+G — should log (no return value)
	debugChunk([]byte{0x1B, '[', 'A'}) // arrow up escape — should log
}

func TestScanCtrlGEventsRaw(t *testing.T) {
	// Raw 0x07 byte: one event of length 1.
	ev := scanCtrlGEvents([]byte{0x07})
	if len(ev) != 1 || ev[0].start != 0 || ev[0].end != 1 {
		t.Fatalf("raw 0x07: got %+v", ev)
	}
}

func TestScanCtrlGEventsKitty(t *testing.T) {
	// kitty keyboard encoding ESC[103;5u = Ctrl+G.
	data := []byte{0x1b, 0x5b, '1', '0', '3', ';', '5', 'u'}
	ev := scanCtrlGEvents(data)
	if len(ev) != 1 || ev[0].start != 0 || ev[0].end != len(data) {
		t.Fatalf("kitty seq: got %+v", ev)
	}
}

func TestScanCtrlGEventsKittyEmbedded(t *testing.T) {
	// Kitty Ctrl+G followed by Enter, preceded by 'x'.
	data := []byte{'x', 0x1b, 0x5b, '1', '0', '3', ';', '5', 'u', 0x0d}
	ev := scanCtrlGEvents(data)
	if len(ev) != 1 || ev[0].start != 1 || ev[0].end != 9 {
		t.Fatalf("embedded kitty: got %+v", ev)
	}
}

func TestScanCtrlGEventsTwoKitty(t *testing.T) {
	// Two kitty Ctrl+G in one chunk (double-tap in same read).
	seq := []byte{0x1b, 0x5b, '1', '0', '3', ';', '5', 'u'}
	data := append(append([]byte{}, seq...), seq...)
	ev := scanCtrlGEvents(data)
	if len(ev) != 2 || ev[0].start != 0 || ev[1].start != 8 {
		t.Fatalf("two kitty: got %+v", ev)
	}
}

func TestScanCtrlGEventsMixed(t *testing.T) {
	// Raw 0x07 then kitty sequence.
	data := append([]byte{0x07}, []byte{0x1b, 0x5b, '1', '0', '3', ';', '5', 'u'}...)
	ev := scanCtrlGEvents(data)
	if len(ev) != 2 {
		t.Fatalf("mixed: got %+v", ev)
	}
}

func TestScanCtrlGEventsIgnoresLookalikes(t *testing.T) {
	// Other CSI sequences must NOT be matched as Ctrl+G.
	cases := [][]byte{
		{0x1b, '[', 'A'},                          // arrow up
		{0x1b, '[', '1', '0', '3', 'm'},           // SGR (not ;5u)
		{0x1b, '[', '1', '0', '3', ';', '6', 'u'}, // Shift+Ctrl+G (modifier 6)
		{0x1b, '[', '1', '0', '4', ';', '5', 'u'}, // Ctrl+H (codepoint 104)
	}
	for i, c := range cases {
		if ev := scanCtrlGEvents(c); len(ev) != 0 {
			t.Errorf("case %d (% x): expected no events, got %+v", i, c, ev)
		}
	}
}

func TestScanCtrlGEventsIgnoresOSCBEL(t *testing.T) {
	// OSC sequences use BEL (0x07) as terminator — must not be matched as Ctrl+G.
	cases := []struct {
		name string
		data []byte
	}{
		{"OSC color query response",
			[]byte{0x1b, ']', '1', '0', ';', 'r', 'g', 'b', ':', 'f', 'f', '/', 'f', 'f', '/', 'f', 'f', 0x07}},
		{"OSC + CSI mixed",
			[]byte{0x1b, ']', '1', '1', ';', 'r', 'g', 'b', ':', 'f', 'f', '/', 'f', 'f', '/', 'f', 'f', 0x1b, '\\',
				0x1b, '[', '5', '3', ';', '1', 'R'}},
		{"two OSC BELs in one chunk",
			[]byte{0x1b, ']', '1', '0', ';', 'x', 0x07, 0x1b, ']', '1', '1', ';', 'y', 0x07}},
	}
	for _, tc := range cases {
		if ev := scanCtrlGEvents(tc.data); len(ev) != 0 {
			t.Errorf("%s: expected no events, got %+v", tc.name, ev)
		}
	}

	// Standalone BEL before OSC IS a real Ctrl+G — the BEL is not inside the OSC.
	ev := scanCtrlGEvents([]byte{0x07, 0x1b, ']', '1', '0', ';', 'x', 0x07})
	if len(ev) != 1 || ev[0].start != 0 {
		t.Errorf("standalone BEL before OSC: expected 1 event at 0, got %+v", ev)
	}
}
