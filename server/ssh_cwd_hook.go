package server

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
)

// ── Remote cwd hook: make bash/zsh announce OSC 7 ─────────────────────────────
//
// Ctrl+G prefills the transfer dialog with the remote shell's working
// directory. That value comes from remoteOutputCwdWriter, which parses OSC 7
// (`ESC ] 7 ; file://HOST/PATH BEL`). fish announces it on its own; bash and
// zsh do not, so there the only source is remoteShellInputTracker, which
// reconstructs the directory from the *keystrokes* of `cd` commands.
//
// Keystroke reconstruction cannot see the shell's line editor. Tab completion
// inserts characters that never reach the terminal, so a completed command is
// recorded as garbage: typing `cd /da<TAB>w<TAB>` is tracked as `cd /daw`, and
// `/daw` is what the dialog then offers. Nothing corrects it, because bash
// never announces the real directory.
//
// Installing a prompt hook once per session makes the announcement happen, so
// every navigation path is tracked exactly — completions, pushd/popd, shell
// functions, z-jump — with no per-trigger round trip.
//
// Presentation matters here, because the hook has to be typed into a live
// shell and the PTY echoes what it receives:
//
//   - The hook body is written to a temporary file through a separate exec
//     channel, which has no terminal, so the body itself is never displayed.
//     Only a short `. <file>` is typed.
//   - Typing waits until the shell has settled. Injecting while the login
//     banner or .bashrc is still printing makes the line discipline echo the
//     text on top of that output, which looks like corruption.
//   - The sourced file erases the one line its own `. <file>` command occupied
//     and deletes itself, so the prompt is all that remains.
//
// The hook is additive: it appends to PROMPT_COMMAND (bash, array-aware) or
// registers through add-zsh-hook (zsh), so existing prompt setup keeps working.
// Shells without a hook are left untouched and keep the previous behaviour.

// cwdHookTempPrefix is the basename prefix of the temporary hook file. The
// file lives directly in /tmp rather than under $TMPDIR so that the path baked
// into the script is the same one the installer wrote, whatever the two shells
// think TMPDIR is.
const cwdHookTempPrefix = ".ttm-osc7-"

// cwdHookSupported reports whether the hook can be installed for this login
// shell. fish is deliberately excluded: it already announces its cwd, and POSIX
// syntax is a syntax error there.
func cwdHookSupported(shell string) bool {
	base := strings.TrimPrefix(path.Base(strings.TrimSpace(shell)), "-") // sshd marks a login shell as "-bash"
	return base == "bash" || base == "zsh"
}

// remoteCwdHookScript is the file sourced by the interactive shell. It is
// shell-agnostic (bash and zsh), defines __ttm_osc7, registers it before every
// prompt, then removes itself and erases the line its `. <file>` command
// occupied.
func remoteCwdHookScript(tempPath string) string {
	return `__ttm_osc7(){ printf '\033]7;file://%s%s\007' "${HOSTNAME:-}${HOST:-}" "$PWD"; }
if [ -n "${BASH_VERSION:-}" ]; then
	case "$(declare -p PROMPT_COMMAND 2>/dev/null)" in
	"declare -a"*) PROMPT_COMMAND+=(__ttm_osc7) ;;
	*) PROMPT_COMMAND="${PROMPT_COMMAND:+$PROMPT_COMMAND;}__ttm_osc7" ;;
	esac
elif [ -n "${ZSH_VERSION:-}" ]; then
	autoload -Uz add-zsh-hook 2>/dev/null && add-zsh-hook precmd __ttm_osc7
fi
rm -f -- ` + shQuote(tempPath) + `
printf '\033[1A\033[2K\r'
`
}

// activityWriter records when the remote last produced output, so session
// bootstrap work can wait for the login shell to settle.
type activityWriter struct {
	dst  io.Writer
	last atomic.Int64 // unix nanoseconds
}

func newActivityWriter(dst io.Writer) *activityWriter {
	return &activityWriter{dst: dst}
}

func (w *activityWriter) Write(p []byte) (int, error) {
	n, err := w.dst.Write(p)
	w.last.Store(time.Now().UnixNano())
	return n, err
}

func (w *activityWriter) lastOutput() time.Time {
	ns := w.last.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

// waitForQuiet blocks until the remote has produced no output for quiet, and
// at most max. It returns false if it gave up waiting for a first byte.
func waitForQuiet(w *activityWriter, quiet, max time.Duration) bool {
	const poll = 25 * time.Millisecond
	deadline := time.Now().Add(max)
	var sawOutput bool
	for {
		if last := w.lastOutput(); !last.IsZero() {
			sawOutput = true
			if time.Since(last) >= quiet {
				return true
			}
		}
		if !time.Now().Before(deadline) {
			return sawOutput
		}
		time.Sleep(poll)
	}
}

// remoteLoginShell reports the shell sshd runs for this account, read from
// $SHELL, which sshd sets from the passwd entry.
func remoteLoginShell(client *ssh.Client) string {
	if client == nil {
		return ""
	}
	out, err := remoteRunSh(client, `printf %s "${SHELL:-}"`, 3*time.Second)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// remoteHookWriteCommand is the POSIX command that creates the hook file. It is
// a pure function so the exact string sent to the remote can be exercised
// locally.
func remoteHookWriteCommand(tempPath, script string) string {
	return "umask 077; printf %s " + shQuote(script) + " > " + shQuote(tempPath)
}

// writeRemoteHookFile writes the script to a private temporary file through an
// exec channel, which has no terminal and therefore no echo. It returns false
// if the file could not be created.
func writeRemoteHookFile(client *ssh.Client, tempPath, script string) bool {
	if _, err := remoteRunSh(client, remoteHookWriteCommand(tempPath, script), 3*time.Second); err != nil {
		return false
	}
	return true
}

func newCwdHookTempPath() string {
	token := make([]byte, 8)
	if _, err := rand.Read(token); err != nil {
		return ""
	}
	return "/tmp/" + cwdHookTempPrefix + hex.EncodeToString(token)
}

// installRemoteCwdHook types `. <file>` into the interactive shell. Failures
// are logged and ignored: the keystroke tracker keeps working as before.
func installRemoteCwdHook(client *ssh.Client, shell string, stdin io.Writer, activity *activityWriter) {
	if client == nil || stdin == nil {
		return
	}
	if !cwdHookSupported(shell) {
		debugf("cwd-hook: no hook for shell=%q, keeping keystroke tracking", shell)
		return
	}
	tempPath := newCwdHookTempPath()
	if tempPath == "" {
		debugf("cwd-hook: could not build a temp path")
		return
	}
	if !writeRemoteHookFile(client, tempPath, remoteCwdHookScript(tempPath)) {
		debugf("cwd-hook: could not write %s", tempPath)
		return
	}

	// Wait for the login banner and .bashrc output to stop. Injecting earlier
	// makes the line discipline echo the command on top of that output.
	if activity != nil {
		waitForQuiet(activity, 300*time.Millisecond, 2*time.Second)
	}

	line := ". " + shQuote(tempPath) + "\r"
	if _, err := io.WriteString(stdin, line); err != nil {
		debugf("cwd-hook: write failed: %v", err)
		return
	}
	debugf("cwd-hook: installed for shell=%q from %s", path.Base(shell), tempPath)
}
