package server

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestShQuote(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"vim", `'vim'`},
		{"", `''`},
		{"a b", `'a b'`},
		{"/tmp/.ttm_cwd_1_2", `'/tmp/.ttm_cwd_1_2'`},
		{`it"s`, `'it"s'`},
		{`a\b`, `'a\b'`},
		{`$HOME`, `'$HOME'`},
		{"it's", `'it'"'"'s'`},
		{"中文/空 格/`命令`/$(id)", `'中文/空 格/` + "`命令`" + `/$(id)'`},
		{"line1\nline2", "'line1\nline2'"},
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
	if want := `printf '%b' '\0145\0143\0150\0157\0040\0150\0151' | sh`; got != want {
		t.Fatalf("wrapShScript = %q, want %q", got, want)
	}

	for _, dangerous := range []string{"$(touch /tmp/pwned)", "`id`", "中文'\"$\\\n"} {
		got = wrapShScript(dangerous)
		if strings.Contains(got, dangerous) {
			t.Errorf("encoded wrapper exposed script %q: %q", dangerous, got)
		}
	}
}

func TestShPathArgPreservesHomeAndSpecialCharacters(t *testing.T) {
	cases := map[string]string{
		"~":                   `"$HOME"`,
		"~/中文/空 格":            `"$HOME"/'中文/空 格'`,
		"/tmp/`id`/$(whoami)": "'/tmp/`id`/$(whoami)'",
	}
	for input, want := range cases {
		if got := shPathArg(input); got != want {
			t.Errorf("shPathArg(%q) = %q, want %q", input, got, want)
		}
	}
}

// remoteSCPCommand must keep the session's stdin, because the SCP protocol is
// spoken over it. Running it through a real /bin/sh with a stub scp on PATH
// verifies both halves at once: the exact argv scp receives (quoting) and that
// stdin still reaches it (no `printf ... | sh` wrapper).
func runRemoteSCPCommandThroughShell(t *testing.T, command, stdin string, extraEnv ...string) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell semantics are not exercised on Windows")
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no POSIX shell available")
	}

	dir := t.TempDir()
	stub := "#!/bin/sh\nfor a in \"$@\"; do printf 'argv:%s\\n' \"$a\"; done\nprintf 'stdin:'\ncat\n"
	if err := os.WriteFile(filepath.Join(dir, "scp"), []byte(stub), 0700); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(sh, "-c", command)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.Env = append(cmd.Env, extraEnv...)
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run %q: %v", command, err)
	}

	if entries, readErr := os.ReadDir(dir); readErr == nil {
		for _, e := range entries {
			if e.Name() != "scp" {
				t.Fatalf("command had a side effect: created %q", e.Name())
			}
		}
	}
	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
}

func TestRemoteSCPCommandPreservesStdinAndQuotesPath(t *testing.T) {
	remotePath := "/tmp/中文 空格/`touch pwned`/$(touch pwned2)/it's"
	command := remoteSCPCommand(false, false, remotePath)
	if strings.Contains(command, "| sh") || strings.Contains(command, "printf") {
		t.Fatalf("SCP command must not redirect stdin through a shell pipe: %q", command)
	}

	got := runRemoteSCPCommandThroughShell(t, command, "protocol-bytes")
	want := []string{"argv:-f", "argv:--", "argv:" + remotePath, "stdin:protocol-bytes"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scp invocation = %#v, want %#v", got, want)
	}
}

func TestRemoteSCPCommandRecursiveSinkAndTilde(t *testing.T) {
	command := remoteSCPCommand(true, true, "~/上传 目标")
	got := runRemoteSCPCommandThroughShell(t, command, "", "HOME=/home/tester")
	want := []string{"argv:-r", "argv:-t", "argv:--", "argv:/home/tester/上传 目标", "stdin:"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scp invocation = %#v, want %#v", got, want)
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
	if isRemoteIdle(nil) {
		t.Error("isRemoteIdle(nil) should fail closed")
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

func TestCtrlGStreamScannerIgnoresSplitOSC(t *testing.T) {
	s := new(ctrlGStreamScanner)
	if events := s.scan([]byte{0x1b, ']', '1', '1', ';', 'r', 'g', 'b'}); len(events) != 0 {
		t.Fatalf("unexpected event in OSC prefix: %+v", events)
	}
	if events := s.scan([]byte{':', 'f', 'f', 0x07}); len(events) != 0 {
		t.Fatalf("split OSC BEL was treated as Ctrl+G: %+v", events)
	}
	if events := s.scan([]byte{0x07}); len(events) != 1 {
		t.Fatalf("standalone Ctrl+G after OSC not detected: %+v", events)
	}
}

func TestCtrlGStreamScannerDoesNotCarryNonOSCInputAcrossReads(t *testing.T) {
	s := new(ctrlGStreamScanner)
	if events := s.scan([]byte{0x1b}); len(events) != 0 {
		t.Fatalf("unexpected event for ESC prefix: %+v", events)
	}
	if events := s.scan([]byte{'x', 0x07}); len(events) != 1 || events[0].start != 1 {
		t.Fatalf("non-OSC input was lost after split ESC: %+v", events)
	}

	s = new(ctrlGStreamScanner)
	_ = s.scan([]byte{0x1b})
	if events := s.scan([]byte{']', '1', '1', ';', 'x', 0x07}); len(events) != 0 {
		t.Fatalf("split OSC BEL was treated as Ctrl+G: %+v", events)
	}
}

func TestCtrlGStreamScannerIgnoresSplitOSCST(t *testing.T) {
	s := new(ctrlGStreamScanner)
	_ = s.scan([]byte{0x1b, ']', '0', ';', 'x', 0x1b})
	if events := s.scan([]byte{'\\', 0x07}); len(events) != 1 || events[0].start != 1 {
		t.Fatalf("OSC ST split handling failed: %+v", events)
	}
}

func TestCompleteControlSequenceJoinsSplitKittyCtrlG(t *testing.T) {
	r := &bytesReader{b: []byte{'3', ';', '5', 'u'}}
	withReadable(alwaysReadable, func() {
		got := completeControlSequence(r, []byte{0x1b, '[', '1', '0'})
		if !bytes.Equal(got, kittyCtrlGSeq) {
			t.Fatalf("got % x, want % x", got, kittyCtrlGSeq)
		}
	})
}

func TestCompleteControlSequenceJoinsSplitOSCIntroducer(t *testing.T) {
	r := &bytesReader{b: []byte{']'}}
	withReadable(alwaysReadable, func() {
		got := completeControlSequence(r, []byte{0x1b})
		if !bytes.Equal(got, []byte{0x1b, ']'}) {
			t.Fatalf("got % x, want split OSC introducer", got)
		}
		if events := scanCtrlGEvents(append(got, '1', '1', ';', 'x', 0x07)); len(events) != 0 {
			t.Fatalf("joined OSC BEL was treated as Ctrl+G: %+v", events)
		}
	})
}

func TestCompleteControlSequenceDoesNotHoldStandaloneEsc(t *testing.T) {
	r := &bytesReader{}
	withReadable(neverReadable, func() {
		got := completeControlSequence(r, []byte{0x1b})
		if !bytes.Equal(got, []byte{0x1b}) {
			t.Fatalf("standalone Esc changed: % x", got)
		}
	})
}
