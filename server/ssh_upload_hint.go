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

// f12Sequence is the escape sequence for the F12 key in most terminals.
// Format: ESC [ 2 4 ~   (backup trigger)
var f12Sequence = []byte{0x1b, 0x5b, 0x32, 0x34, 0x7e}

// readBufSize is the buffer size for stdin reads. Matches typical io.Copy
// buffer size for parity with the original unintercepted path.
const readBufSize = 32 * 1024

// startStdinCopyWithIntercept is like startStdinCopy but intercepts Ctrl+G
// (primary) and F12 (backup) to trigger a handler.
//
// Uses buffered reading for performance: data is read in large chunks and
// scanned for trigger bytes, only falling back to byte-level inspection
// when a potential escape sequence is in progress.
//
// The handleTrigger callback receives the stdinReader so it can read user
// input directly (e.g. for a dialog). It is called synchronously, blocking
// stdin forwarding while the handler is active.
func startStdinCopyWithIntercept(stdinPipe io.WriteCloser, handleTrigger func(io.Reader)) (<-chan error, func(), error) {
	stdinReader, err := newStdinReader()
	if err != nil {
		return nil, nil, err
	}

	done := make(chan error, 1)
	go func() {
		buf := make([]byte, readBufSize)
		// leftover holds bytes carried over from the previous read when a
		// potential F12 sequence was split across buffer boundaries.
		var leftover []byte

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
				if len(leftover) > 0 {
					data = append(leftover, data...)
					leftover = nil
				}

				result := processChunk(data, forward, stdinReader, handleTrigger)
				switch result.action {
				case chunkTriggered:
					// Handler returned; continue reading.
				case chunkLeftover:
					leftover = result.remaining
				case chunkDone:
					// Error already forwarded to done channel.
					return
				case chunkOK:
					// All bytes forwarded.
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
	action    chunkAction
	remaining []byte // bytes to carry over (only for chunkLeftover)
}

type chunkAction int

const (
	chunkOK        chunkAction = iota // all bytes forwarded
	chunkTriggered                    // handler was called
	chunkLeftover                     // partial escape sequence at end
	chunkDone                         // error occurred, caller should stop
)

// processChunk scans a chunk of stdin data for trigger bytes and forwards
// non-trigger data to the SSH pipe.
func processChunk(data []byte, forward func([]byte) error, stdinReader io.Reader, handleTrigger func(io.Reader)) chunkResult {
	i := 0
	for i < len(data) {
		b := data[i]

		// ── Ctrl+G: primary trigger ──
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

		// ── ESC: potential F12 sequence ──
		if b == 0x1b {
			remaining := len(data) - i
			if remaining >= len(f12Sequence) {
				if bytes.Equal(data[i:i+len(f12Sequence)], f12Sequence) {
					if err := forward(data[:i]); err != nil {
						return chunkResult{action: chunkDone}
					}
					if handleTrigger != nil {
						handleTrigger(stdinReader)
					}
					data = data[i+len(f12Sequence):]
					i = 0
					continue
				}
				i++
				continue
			}

			if bytes.Equal(data[i:], f12Sequence[:remaining]) {
				if err := forward(data[:i]); err != nil {
					return chunkResult{action: chunkDone}
				}
				rem := make([]byte, len(data)-i)
				copy(rem, data[i:])
				return chunkResult{action: chunkLeftover, remaining: rem}
			}
			i++
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
