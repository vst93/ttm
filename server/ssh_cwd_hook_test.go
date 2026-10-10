package server

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCwdHookSupported(t *testing.T) {
	tests := []struct {
		shell string
		want  bool
	}{
		{"/bin/bash", true},
		{"/usr/local/bin/bash", true},
		{"-bash", true}, // sshd marks a login shell with a leading dash
		{"/bin/zsh", true},
		{"/usr/bin/fish", false}, // already announces OSC 7; POSIX syntax breaks it
		{"/bin/dash", false},
		{"/bin/tcsh", false},
		{"", false},
		{"   ", false},
	}
	for _, tt := range tests {
		if got := cwdHookSupported(tt.shell); got != tt.want {
			t.Errorf("cwdHookSupported(%q) = %v, want %v", tt.shell, got, tt.want)
		}
	}
}

func TestNewCwdHookTempPath(t *testing.T) {
	a, b := newCwdHookTempPath(), newCwdHookTempPath()
	if a == "" || b == "" {
		t.Fatal("temp path must not be empty")
	}
	if a == b {
		t.Fatal("temp paths must differ, or two sessions would overwrite each other")
	}
	base := filepath.Base(a)
	if !strings.HasPrefix(base, cwdHookTempPrefix) {
		t.Fatalf("base %q does not start with %q", base, cwdHookTempPrefix)
	}
	if filepath.Dir(a) != filepath.Dir("/tmp/x") {
		t.Fatalf("path %q is not directly under /tmp", a)
	}
}

func envWithout(env []string, key string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, key+"=") {
			out = append(out, e)
		}
	}
	return out
}

func runShell(t *testing.T, shell, script, dir string) string {
	t.Helper()
	cmd := exec.Command(shell, "-c", script)
	if dir != "" {
		cmd.Dir = dir
	}
	// PWD would otherwise be inherited from the test process and disagree with
	// the working directory the child actually runs in.
	cmd.Env = envWithout(os.Environ(), "PWD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s -c %q: %v", shell, script, err)
	}
	return string(out)
}

// writeHookScript writes the hook the way the installer does, so sourcing it
// exercises the real file.
func writeHookScript(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "hook.sh")
	if err := os.WriteFile(p, []byte(remoteCwdHookScript(p)), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// Sourcing the hook must leave the shell announcing its cwd, erase the line the
// `. <file>` command occupied, and delete the file.
func TestBashCwdHookSourcing(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	hook := writeHookScript(t, dir)

	out := runShell(t, bash,
		`PROMPT_COMMAND="existing"; . `+shQuote(hook)+
			`; __ttm_osc7; printf '\nPROMPT=%s\nPWD=%s\n' "$PROMPT_COMMAND" "$PWD"`, dir)

	if !strings.Contains(out, `PROMPT=existing;__ttm_osc7`) {
		t.Fatalf("existing PROMPT_COMMAND was not preserved and appended to: %q", out)
	}
	if !strings.Contains(out, "\x1b[1A\x1b[2K\r") {
		t.Fatalf("hook did not erase the line it was sourced from: %q", out)
	}
	if _, err := os.Stat(hook); !os.IsNotExist(err) {
		t.Fatalf("hook file was not removed (err=%v)", err)
	}

	i := strings.LastIndex(out, "PWD=")
	if i < 0 {
		t.Fatalf("no PWD marker in %q", out)
	}
	want := strings.TrimSpace(out[i+4:])

	cache := newRemoteCwdCache()
	w := newRemoteOutputCwdWriter(io.Discard, cache)
	if _, err := w.Write([]byte(out)); err != nil {
		t.Fatal(err)
	}
	got, ok := cache.Get()
	if !ok {
		t.Fatalf("hook output %q produced no OSC 7 directory", out)
	}
	if got != want {
		t.Fatalf("parsed %q, want bash's own %q", got, want)
	}
}

func TestBashCwdHookArray(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	hook := writeHookScript(t, dir)

	out := runShell(t, bash,
		`PROMPT_COMMAND=(a b); . `+shQuote(hook)+`; printf %s "${PROMPT_COMMAND[*]}"`, dir)
	if !strings.Contains(out, "a b __ttm_osc7") {
		t.Fatalf("array PROMPT_COMMAND lost entries: %q", out)
	}
}

func TestZshCwdHookSourcing(t *testing.T) {
	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skip("zsh not available")
	}
	dir := t.TempDir()
	hook := writeHookScript(t, dir)

	out := runShell(t, zsh, `. `+shQuote(hook)+`; print -r -- ${precmd_functions[*]}`, dir)
	if !strings.Contains(out, "__ttm_osc7") {
		t.Fatalf("precmd_functions = %q, want it to contain __ttm_osc7", out)
	}
	if _, err := os.Stat(hook); !os.IsNotExist(err) {
		t.Fatalf("hook file was not removed (err=%v)", err)
	}
}

func TestRemoteCwdHookScriptIsValidSyntax(t *testing.T) {
	script := remoteCwdHookScript("/tmp/.ttm-osc7-test")
	if bash, err := exec.LookPath("bash"); err == nil {
		if out, err := exec.Command(bash, "-n", "-c", script).CombinedOutput(); err != nil {
			t.Fatalf("bash rejected the hook: %v\n%s", err, out)
		}
	}
	if zsh, err := exec.LookPath("zsh"); err == nil {
		if out, err := exec.Command(zsh, "-n", "-c", script).CombinedOutput(); err != nil {
			t.Fatalf("zsh rejected the hook: %v\n%s", err, out)
		}
	}
}

func TestWaitForQuietWithoutOutput(t *testing.T) {
	w := newActivityWriter(io.Discard)
	start := time.Now()
	if waitForQuiet(w, 20*time.Millisecond, 80*time.Millisecond) {
		t.Fatal("a writer that never received output must not report quiet")
	}
	if elapsed := time.Since(start); elapsed < 60*time.Millisecond {
		t.Fatalf("returned after %v, expected to wait out the budget", elapsed)
	}
}

func TestWaitForQuietAfterOutputStops(t *testing.T) {
	w := newActivityWriter(io.Discard)
	if _, err := w.Write([]byte("login banner\n")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)

	start := time.Now()
	if !waitForQuiet(w, 20*time.Millisecond, 2*time.Second) {
		t.Fatal("expected quiet once output stopped")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("took %v to observe quiet", elapsed)
	}
}

func TestActivityWriterForwardsWrites(t *testing.T) {
	var buf strings.Builder
	w := newActivityWriter(&buf)
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "hello" {
		t.Fatalf("destination got %q", buf.String())
	}
	if w.lastOutput().IsZero() {
		t.Fatal("write did not record activity")
	}
}

// The file is created by a command sent to the remote, so the quoting has to
// survive that trip. Run the exact command locally and then source the result.
func TestRemoteHookWriteCommandProducesSourceableFile(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "hook.sh")

	cmd := exec.Command("/bin/sh", "-c", remoteHookWriteCommand(target, remoteCwdHookScript(target)))
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("write command failed: %v\n%s", err, out)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("hook file was not created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("hook file mode = %o, want 600", perm)
	}

	out := runShell(t, bash, `. `+shQuote(target)+`; __ttm_osc7; printf '\nPWD=%s\n' "$PWD"`, dir)
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("sourcing the generated file did not remove it (err=%v)", err)
	}

	i := strings.LastIndex(out, "PWD=")
	if i < 0 {
		t.Fatalf("no PWD marker in %q", out)
	}
	want := strings.TrimSpace(out[i+4:])

	cache := newRemoteCwdCache()
	w := newRemoteOutputCwdWriter(io.Discard, cache)
	if _, err := w.Write([]byte(out)); err != nil {
		t.Fatal(err)
	}
	got, ok := cache.Get()
	if !ok || got != want {
		t.Fatalf("parsed %q (ok=%v), want %q", got, ok, want)
	}
}
