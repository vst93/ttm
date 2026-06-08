//go:build !windows

package server

import (
	"io"
	"os"
)

// openLocalTTY opens /dev/tty for writing directly to the user's terminal,
// bypassing stdout (which is piped to the SSH session).
func openLocalTTY() (io.WriteCloser, error) {
	return os.OpenFile("/dev/tty", os.O_WRONLY, 0)
}
