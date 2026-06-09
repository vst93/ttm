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

// isRemoteIdle checks if the remote SSH server has any fullscreen/TUI program
// running. Returns true if the remote appears idle (no fullscreen program detected).
func isRemoteIdle(client *ssh.Client) bool {
	if client == nil {
		return true
	}

	session, err := client.NewSession()
	if err != nil {
		return true // assume idle on error
	}
	defer session.Close()

	var buf bytes.Buffer
	session.Stdout = &buf

	// Check each program individually using pgrep.
	// pgrep returns 0 if a process is found, 1 if not.
	// We chain them with || so the first match short-circuits.
	var cmds []string
	for _, prog := range fullscreenPrograms {
		cmds = append(cmds, fmt.Sprintf("pgrep -x %q >/dev/null 2>&1", prog))
	}
	cmd := strings.Join(cmds, " || ") + " && echo found || true"

	if err := session.Run(cmd); err != nil {
		return true // assume idle on error
	}

	return strings.TrimSpace(buf.String()) == ""
}

// queryRemotePwd gets the interactive shell's cwd by injecting pwd into its stdin.
// The result is written to a temp file and read back via a new exec session.
// Returns (dir, detected).
//
// Tradeoff: the user will briefly see a `pwd > /tmp/.ttm_cwd` command on their
// terminal. This is the most reliable way to get the actual cwd.
func queryRemotePwd(stdinPipe io.WriteCloser, client *ssh.Client) (string, bool) {
	if dir := probeShellCwdViaStdin(stdinPipe, client); dir != "" {
		return dir, true
	}

	// Fallback: new session pwd (returns home directory).
	session, err := client.NewSession()
	if err != nil {
		return "~", false
	}
	defer session.Close()

	var buf bytes.Buffer
	session.Stdout = &buf
	if err := session.Run("pwd"); err != nil {
		return "~", false
	}
	dir := strings.TrimSpace(buf.String())
	if dir == "" {
		return "~", false
	}
	return dir, false
}

// probeShellCwdViaStdin injects `pwd` into the interactive shell's stdin,
// captures the output via a temp file, and reads it back.
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
	time.Sleep(300 * time.Millisecond)

	// Read the temp file via a new exec session, then clean up.
	session, err := client.NewSession()
	if err != nil {
		return ""
	}
	defer session.Close()

	var buf bytes.Buffer
	session.Stdout = &buf
	if err := session.Run(fmt.Sprintf("cat %s 2>/dev/null; rm -f %s", tmpFile, tmpFile)); err != nil {
		return ""
	}

	dir := strings.TrimSpace(buf.String())
	if dir == "" || dir == "/" {
		return ""
	}
	return dir
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

// startStdinCopyWithIntercept is like startStdinCopy but intercepts Ctrl+G
// (0x07, BEL) to trigger a handler.
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
func startStdinCopyWithIntercept(stdinPipe io.WriteCloser, handleTrigger func(io.Reader)) (<-chan error, func(), error) {
	stdinReader, err := newStdinReader()
	if err != nil {
		return nil, nil, err
	}

	// doublePressWindow is the max time between two Ctrl+G presses to
	// count as a double-press (trigger upload dialog).
	const doublePressWindow = 400 * time.Millisecond

	// noiseThreshold is the minimum time between two Ctrl+G presses for
	// them to be considered a genuine double-press. Presses closer than
	// this are treated as terminal protocol noise and forwarded as-is.
	const noiseThreshold = 50 * time.Millisecond

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, readBufSize)
		var lastCtrlGTime time.Time // time of last Ctrl+G that was forwarded

		forward := func(data []byte) error {
			if len(data) == 0 {
				return nil
			}
			_, err := stdinPipe.Write(data)
			return err
		}

		for {
			n, readErr := stdinReader.Read(buf)
			if n > 0 {
				data := buf[:n]

				// Count Ctrl+G bytes in this chunk.
				ctrlGCount := 0
				ctrlGIdx := -1
				for i, b := range data {
					if b == ctrlGByte {
						ctrlGCount++
						ctrlGIdx = i
					}
				}

				if ctrlGCount == 2 {
					// Two Ctrl+G in one chunk — likely a double-press.
					// Find the position of the second Ctrl+G.
					secondIdx := -1
					count := 0
					for i, b := range data {
						if b == ctrlGByte {
							count++
							if count == 2 {
								secondIdx = i
								break
							}
						}
					}
					// Forward everything before the second Ctrl+G,
					// skip the second Ctrl+G itself, trigger upload.
					if err := forward(data[:secondIdx]); err != nil {
						done <- err
						_ = stdinPipe.Close()
						return
					}
					if secondIdx+1 < len(data) {
						if err := forward(data[secondIdx+1:]); err != nil {
							done <- err
							_ = stdinPipe.Close()
							return
						}
					}
					if handleTrigger != nil {
						handleTrigger(stdinReader)
					}
					lastCtrlGTime = time.Time{}
					goto checkErr
				}

				if ctrlGCount > 2 {
					// More than two → terminal noise. Forward as-is.
					if err := forward(data); err != nil {
						done <- err
						_ = stdinPipe.Close()
						return
					}
					lastCtrlGTime = time.Time{}
					goto checkErr
				}

				if ctrlGCount == 1 {
					now := time.Now()

					if !lastCtrlGTime.IsZero() && now.Sub(lastCtrlGTime) < doublePressWindow {
						// A previous Ctrl+G was recently forwarded. This could be
						// a double-press or noise.
						if now.Sub(lastCtrlGTime) < noiseThreshold {
							// Too fast — likely terminal noise. Forward as-is.
							if err := forward(data); err != nil {
								done <- err
								_ = stdinPipe.Close()
								return
							}
							lastCtrlGTime = time.Time{}
							goto checkErr
						}

						// Genuine double-press! Trigger upload dialog.
						// Forward bytes before and after the Ctrl+G, but NOT the
						// Ctrl+G itself (so vim doesn't see a second one).
						if err := forward(data[:ctrlGIdx]); err != nil {
							done <- err
							_ = stdinPipe.Close()
							return
						}
						if ctrlGIdx+1 < len(data) {
							if err := forward(data[ctrlGIdx+1:]); err != nil {
								done <- err
								_ = stdinPipe.Close()
								return
							}
						}
						if handleTrigger != nil {
							handleTrigger(stdinReader)
						}
						lastCtrlGTime = time.Time{} // reset
						goto checkErr
					}

					// First Ctrl+G press — forward to remote, arm double-press window.
					if err := forward(data); err != nil {
						done <- err
						_ = stdinPipe.Close()
						return
					}
					lastCtrlGTime = now
					goto checkErr
				}

				// No Ctrl+G in this chunk — forward normally, reset state.
				if err := forward(data); err != nil {
					done <- err
					_ = stdinPipe.Close()
					return
				}
				lastCtrlGTime = time.Time{}
			}
		checkErr:
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
