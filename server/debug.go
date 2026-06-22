package server

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ── Diagnostic trace ──────────────────────────────────────────────────────────
//
// The Ctrl+G double-tap → upload dialog flow touches local keystroke
// interception, the remote shell (via probes), and the local TTY. When it
// silently fails on a specific host, there has been no way to tell which
// stage broke.
//
// debugf writes a timestamped line to <configdir>/ttm/debug.log. It is called
// only on Ctrl+G/SSH-related events, so the file stays tiny. It
// self-truncates at 256 KB so it can never grow unbounded. Safe to leave on.
//
// The very first line written when an SSH session's keystroke interceptor
// starts is "intercept: started ...". If that line is absent, either the new
// binary is not actually running or log creation is failing. If it is present
// but no "ctrl+g:" / "stdin:" lines follow, then Ctrl+G (0x07) is never
// reaching the local stdin reader — i.e. the trigger never fires.

const debugLogMaxSize = 256 * 1024

var debugMu sync.Mutex

func debugLogPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "ttm", "debug.log")
}

// debugf appends a timestamped trace line. It creates the log directory if
// needed and never returns errors — diagnostics must never interfere with the
// interactive session.
func debugf(format string, args ...any) {
	p := debugLogPath()
	if p == "" {
		return
	}

	debugMu.Lock()
	defer debugMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return
	}

	if fi, err := os.Stat(p); err == nil && fi.Size() > debugLogMaxSize {
		_ = os.Truncate(p, 0)
	}

	line := time.Now().Format("15:04:05.000") + " " + fmt.Sprintf(format, args...) + "\n"
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(line)
}
