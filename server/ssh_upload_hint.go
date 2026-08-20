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
// running. Probe failures are treated as busy so the shortcut never steals
// Ctrl+G from an application when the remote state is unknown.
//
// The probe is bounded by a 1s timeout so a slow or hung remote shell (e.g.
// a framework-heavy fish/oh-my-zsh invoked for the exec session) can never
// block the Ctrl+G trigger — on timeout the shortcut remains disabled.
func isRemoteIdle(client *ssh.Client) bool {
	if client == nil {
		return false
	}

	out, err := remoteRunSh(client, buildIdleCheckScript(), 1*time.Second)
	if err != nil {
		return false // fail closed: do not steal a key from an unknown remote state
	}
	return out != "found"
}

// queryRemotePwd gets the interactive shell's cwd by injecting pwd into its stdin.
// The result is written to a temp file and read back via the middleware exec
// session. Returns (dir, detected).
//
// Tradeoff: the user will briefly see a `pwd > /tmp/.ttm_cwd` command on their
// terminal. This is the most reliable way to get the actual cwd.
func queryRemotePwd(cache *remoteCwdCache, client *ssh.Client) (string, bool) {
	if dir, ok := cache.Get(); ok {
		return dir, true
	}

	// Fallback: a new exec session starts in the login shell's default cwd,
	// which is typically $HOME. Cache it as the session home so later local
	// `cd` tracking can resolve relative paths without probing interactively.
	out, err := remoteRunSh(client, "pwd", 5*time.Second)
	if err != nil || out == "" {
		return "~", false
	}
	cache.SetHome(out)
	return out, false
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
	remoteTarget := user + "@" + info.Host + ":" + strings.TrimRight(remoteDir, "/") + "/"
	return fmt.Sprintf("scp -P %d %s %s",
		port, shQuote("<local_file>"), shQuote(remoteTarget))
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

type ctrlGStreamScanner struct {
	inOSC      bool
	oscEsc     bool
	pendingESC bool
}

// scan finds Ctrl+G while retaining OSC state between reads. BEL terminators
// inside terminal OSC responses are data, not keyboard input.
func (s *ctrlGStreamScanner) scan(data []byte) []ctrlGEvent {
	var events []ctrlGEvent
	i := 0
	if s.pendingESC {
		s.pendingESC = false
		if len(data) > 0 && data[0] == ']' {
			s.inOSC = true
			i = 1
		}
	}
	for i < len(data) {
		if s.inOSC {
			b := data[i]
			i++
			if b == ctrlGByte {
				s.inOSC = false
				s.oscEsc = false
				continue
			}
			if s.oscEsc && b == '\\' {
				s.inOSC = false
				s.oscEsc = false
				continue
			}
			s.oscEsc = b == 0x1b
			continue
		}

		// Skip OSC sequences (ESC] ... BEL/ESC\) — their BEL terminators
		// must not be matched as Ctrl+G. This is the primary source of
		// false positives from terminal color-query responses.
		if data[i] == 0x1b && i+1 < len(data) && data[i+1] == 0x5d {
			s.inOSC = true
			s.oscEsc = false
			i += 2
			continue
		}
		if data[i] == 0x1b && i+1 == len(data) {
			s.pendingESC = true
			i++
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

func scanCtrlGEvents(data []byte) []ctrlGEvent {
	return new(ctrlGStreamScanner).scan(data)
}

// completeControlSequence reads immediately available continuation bytes for
// a Kitty Ctrl+G sequence split at a read boundary. It waits at most one short
// terminal-response interval per byte; a standalone Esc is then forwarded.
func completeControlSequence(r io.Reader, data []byte) []byte {
	for i := 0; i < len(kittyCtrlGSeq); i++ {
		needsMore := false
		maxSuffix := len(kittyCtrlGSeq) - 1
		if len(data) < maxSuffix {
			maxSuffix = len(data)
		}
		for n := 1; n <= maxSuffix; n++ {
			suffix := data[len(data)-n:]
			if bytes.HasPrefix(kittyCtrlGSeq, suffix) || (n == 1 && suffix[0] == 0x1b) {
				needsMore = true
				break
			}
		}
		if !needsMore || !stdinReadable(drainByteTimeout) {
			return data
		}
		var next [1]byte
		if n, err := r.Read(next[:]); n != 1 || err != nil {
			return data
		}
		data = append(data, next[0])
	}
	return data
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
//   - Two adjacent Ctrl+G presses trigger even if the OS coalesces them into
//     one read; ordinary input between presses disarms the shortcut.
//
// The handleTrigger callback receives the stdinReader so it can read user
// input directly (e.g. for a dialog). It is called synchronously, blocking
// stdin forwarding while the handler is active.
func startStdinCopyWithIntercept(stdinPipe io.WriteCloser, client *ssh.Client, cwdCache *remoteCwdCache, handleTrigger func(io.Reader, bool)) (<-chan error, func(), error) {
	stdinReader, err := newStdinReader()
	if err != nil {
		return nil, nil, err
	}

	// doublePressWindow is the max time between two Ctrl+G presses to
	// count as a double-press (trigger upload dialog).
	const doublePressWindow = 700 * time.Millisecond

	done := make(chan error, 1)
	go func() {
		// First trace line: proves the new binary is running, the goroutine
		// started, and the log is writable. If this line is missing, the
		// build is stale or log creation failed.
		debugf("intercept: started mode=%s log=%s", triggerMode(), debugLogPath())

		buf := make([]byte, readBufSize)
		var lastCtrlGTime time.Time // time of last Ctrl+G that was forwarded
		inputTracker := newRemoteShellInputTracker(cwdCache)
		ctrlGScanner := new(ctrlGStreamScanner)

		forward := func(data []byte) error {
			if len(data) == 0 {
				return nil
			}
			inputTracker.Observe(data)
			_, err := stdinPipe.Write(data)
			return err
		}

		for {
			n, readErr := stdinReader.Read(buf)
			if n > 0 {
				data := completeControlSequence(stdinReader, append([]byte(nil), buf[:n]...))
				debugChunk(data)

				// Detect Ctrl+G events. A press may arrive as either a raw 0x07
				// byte (kitty keyboard OFF) or the kitty keyboard sequence
				// ESC[103;5u (kitty keyboard ON — enabled by some remote
				// shells/prompts, e.g. fish + starship / oh-my-zsh). Without
				// recognizing the kitty form, Ctrl+G never triggers on such hosts.
				events := ctrlGScanner.scan(data)
				cursor := 0
				triggered := false
				idleChecked := false
				forwardOrdinary := func(part []byte) error {
					if len(part) > 0 {
						lastCtrlGTime = time.Time{}
					}
					return forward(part)
				}

				for _, e := range events {
					if err := forwardOrdinary(data[cursor:e.start]); err != nil {
						done <- err
						_ = stdinPipe.Close()
						return
					}
					now := time.Now()
					if !triggered && triggerMode() == "single" {
						debugf("ctrl+g: single-mode -> trigger")
						triggered = true
						lastCtrlGTime = time.Time{}
					} else if !triggered && !lastCtrlGTime.IsZero() && now.Sub(lastCtrlGTime) < doublePressWindow {
						if isRemoteIdle(client) {
							debugf("ctrl+g: double-press (%v) -> trigger", now.Sub(lastCtrlGTime))
							triggered = true
							idleChecked = true
						} else {
							debugf("ctrl+g: double-press but remote busy -> forward")
							if err := forward(data[e.start:e.end]); err != nil {
								done <- err
								_ = stdinPipe.Close()
								return
							}
						}
						lastCtrlGTime = time.Time{}
					} else {
						debugf("ctrl+g: first press, arming")
						if err := forward(data[e.start:e.end]); err != nil {
							done <- err
							_ = stdinPipe.Close()
							return
						}
						lastCtrlGTime = now
					}
					cursor = e.end
				}
				if err := forwardOrdinary(data[cursor:]); err != nil {
					done <- err
					_ = stdinPipe.Close()
					return
				}
				if triggered && handleTrigger != nil {
					lastCtrlGTime = time.Time{}
					handleTrigger(stdinReader, idleChecked)
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
