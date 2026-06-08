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
// (0x07, BEL) to trigger a handler. F12 (ESC [24~) is intentionally NOT
// intercepted because its escape sequence frequently collides with terminal
// protocol responses sent by macOS Terminal.app and other terminals during
// SSH session setup.
//
// Uses a debounce mechanism to distinguish genuine Ctrl+G keypresses from
// terminal protocol noise. When a Ctrl+G byte arrives:
//   - If armed (no recent Ctrl+G): trigger immediately, disarm, start 500ms window
//   - If in debounce window (noise): forward without triggering, reset window
//   - After window expires: re-arm for next genuine keypress
//
// This works because terminal noise sends 0x07 in rapid bursts (sub-50ms),
// keeping the debounce window open continuously. After noise stops, the window
// closes and the user's next Ctrl+G triggers normally.
//
// The handleTrigger callback receives the stdinReader so it can read user
// input directly (e.g. for a dialog). It is called synchronously, blocking
// stdin forwarding while the handler is active.
func startStdinCopyWithIntercept(stdinPipe io.WriteCloser, handleTrigger func(io.Reader)) (<-chan error, func(), error) {
	stdinReader, err := newStdinReader()
	if err != nil {
		return nil, nil, err
	}

	// debounceWindow is the time window after seeing a Ctrl+G byte during
	// which subsequent Ctrl+G bytes are considered noise and suppressed.
	// Terminal protocol noise (macOS Terminal.app) sends 0x07 in rapid
	// bursts (sub-50ms intervals), so the window resets on each 0x07,
	// keeping the "noisy" state active. After noise stops (500ms quiet),
	// the window closes and the next Ctrl+G triggers the upload dialog.
	const debounceWindow = 500 * time.Millisecond

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, readBufSize)
		var lastCtrlG time.Time // time of last forwarded (non-triggering) Ctrl+G
		ctrlGArmed := true      // true = next Ctrl+G triggers; false = in debounce window

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
				for _, b := range data {
					if b == ctrlGByte {
						ctrlGCount++
					}
				}

				if ctrlGCount > 0 {
					now := time.Now()

					if !ctrlGArmed {
						// We're in debounce window — check if it expired.
						if now.Sub(lastCtrlG) >= debounceWindow {
							// Window expired. Re-arm.
							ctrlGArmed = true
						}
					}

					if ctrlGArmed && ctrlGCount == 1 {
						// Single Ctrl+G while armed → user pressed Ctrl+G. Trigger!
						if err := forward(data); err != nil {
							done <- err
							_ = stdinPipe.Close()
							return
						}
						if handleTrigger != nil {
							handleTrigger(stdinReader)
						}
						// Disarm and start debounce window.
						ctrlGArmed = false
						lastCtrlG = now
						goto checkErr
					}

					// Multiple Ctrl+G in one chunk, or single while disarmed → noise.
					// Forward as-is and update debounce timer.
					if err := forward(data); err != nil {
						done <- err
						_ = stdinPipe.Close()
						return
					}
					lastCtrlG = now
					ctrlGArmed = false
					goto checkErr
				}

				// No Ctrl+G in this chunk — forward normally.
				if err := forward(data); err != nil {
					done <- err
					_ = stdinPipe.Close()
					return
				}
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

// chunkResult describes what processChunk did.
type chunkResult struct {
	action chunkAction
}

type chunkAction int

const (
	chunkOK        chunkAction = iota // all bytes forwarded
	chunkTriggered                    // handler was called
	chunkDone                         // error occurred, caller should stop
)

// processChunk scans a chunk of stdin data for Ctrl+G (0x07) and forwards
// all other data to the SSH pipe. F12 escape sequence interception was
// removed because it frequently collides with terminal protocol responses.
func processChunk(data []byte, forward func([]byte) error, stdinReader io.Reader, handleTrigger func(io.Reader)) chunkResult {
	i := 0
	for i < len(data) {
		b := data[i]

		// ── Ctrl+G (0x07): single-press trigger ──
		if b == ctrlGByte {
			if err := forward(data[:i]); err != nil {
				return chunkResult{action: chunkDone}
			}
			if handleTrigger != nil {
				handleTrigger(stdinReader)
			}
			data = data[i+1:]
			i = 0
			continue
		}

		i++
	}

	if err := forward(data); err != nil {
		return chunkResult{action: chunkDone}
	}
	return chunkResult{action: chunkOK}
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
