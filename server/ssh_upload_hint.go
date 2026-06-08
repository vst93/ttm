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
// protocol responses (cursor position queries, capability reports) sent by
// macOS Terminal.app and other terminals during SSH session setup.
//
// Uses buffered reading for performance: data is read in large chunks and
// scanned for trigger bytes.
//
// The handleTrigger callback receives the stdinReader so it can read user
// input directly (e.g. for a dialog). It is called synchronously, blocking
// stdin forwarding while the handler is active.
func startStdinCopyWithIntercept(stdinPipe io.WriteCloser, handleTrigger func(io.Reader)) (<-chan error, func(), error) {
	stdinReader, err := newStdinReader()
	if err != nil {
		return nil, nil, err
	}

	// uploadTriggerGracePeriod is the time after SSH session starts during
	// which Ctrl+G is forwarded without triggering. This prevents terminal
	// protocol sequences sent during SSH setup from accidentally triggering
	// the upload dialog on macOS Terminal.app and similar terminals.
	const uploadTriggerGracePeriod = 3 * time.Second

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, readBufSize)
		sessionStart := time.Now()
		graceActive := true

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
				// Check if we're still in the grace period.
				if graceActive {
					if time.Since(sessionStart) < uploadTriggerGracePeriod {
						// During grace period: forward everything without triggering.
						_ = forward(buf[:n])
					} else {
						graceActive = false
						result := processChunk(buf[:n], forward, stdinReader, handleTrigger)
						if result.action == chunkDone {
							return
						}
					}
				} else {
					result := processChunk(buf[:n], forward, stdinReader, handleTrigger)
					if result.action == chunkDone {
						return
					}
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

		// ── Ctrl+G: trigger ──
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
