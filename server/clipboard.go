package server

import (
	"encoding/base64"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/atotto/clipboard"
)

var clipboardWriteAll = clipboard.WriteAll
var clipboardReadAll = clipboard.ReadAll
var clipboardOSC52Write = writeOSC52Clipboard
var shouldAttemptOSC52FallbackFn = shouldAttemptOSC52Fallback
var shouldPreferOSC52Fn = shouldPreferOSC52

const maxOSC52TextBytes = 70000

func writeTextToClipboard(text string) (bool, error) {
	if shouldPreferOSC52Fn() {
		if len([]byte(text)) > maxOSC52TextBytes {
			return false, fmt.Errorf("terminal clipboard text too long")
		}
		if err := clipboardOSC52Write(text); err == nil {
			return true, nil
		}
		return false, fmt.Errorf("terminal clipboard unavailable")
	}

	err := clipboardWriteAll(text)
	if err == nil {
		return false, nil
	}
	if !shouldAttemptOSC52FallbackFn() {
		return false, err
	}
	if len([]byte(text)) > maxOSC52TextBytes {
		return false, fmt.Errorf("%w; terminal clipboard text too long", err)
	}
	if err := clipboardOSC52Write(text); err == nil {
		return true, nil
	}
	return false, err
}

func shouldPreferOSC52() bool {
	if runtime.GOOS == "windows" || !isInteractiveTerminal() {
		return false
	}
	if isTermuxEnv() {
		return true
	}
	return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" || os.Getenv("TMUX") != ""
}

func shouldAttemptOSC52Fallback() bool {
	if runtime.GOOS == "windows" || !isInteractiveTerminal() {
		return false
	}
	if isTermuxEnv() {
		return true
	}
	return true
}

func isTermuxEnv() bool {
	if os.Getenv("TERMUX_VERSION") != "" {
		return true
	}
	prefix := os.Getenv("PREFIX")
	return strings.Contains(prefix, "com.termux")
}

func isInteractiveTerminal() bool {
	term := strings.ToLower(strings.TrimSpace(os.Getenv("TERM")))
	if term == "" || term == "dumb" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (info.Mode() & os.ModeCharDevice) != 0
}

func writeOSC52Clipboard(text string) error {
	if !isInteractiveTerminal() {
		return fmt.Errorf("osc52 unavailable: non-interactive terminal")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	seq := "\x1b]52;c;" + encoded + "\a"
	if os.Getenv("TMUX") != "" {
		escaped := strings.ReplaceAll(seq, "\x1b", "\x1b\x1b")
		seq = "\x1bPtmux;" + escaped + "\x1b\\"
	}
	_, err := os.Stdout.WriteString(seq)
	return err
}
