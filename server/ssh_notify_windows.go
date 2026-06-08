//go:build windows

package server

import (
	"io"
	"os"
)

// openLocalTTY opens the Windows console (CON) for writing directly to the
// user's terminal, bypassing stdout (which is piped to the SSH session).
func openLocalTTY() (io.WriteCloser, error) {
	return os.OpenFile("CON", os.O_WRONLY, 0)
}
