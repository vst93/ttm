//go:build !windows

package server

import (
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// stdinReadableWithin reports whether os.Stdin has data available to read
// within the given timeout, without blocking beyond it.
//
// This is the keystone of the dialog-input robustness fix: it lets us peek
// for a byte following an ESC to disambiguate a standalone Esc key (cancel)
// from the start of a terminal escape sequence (OSC/CSI response), and to
// drain those responses without blocking forever if a sequence is split.
//
// It polls os.Stdin's fd directly. The SSH stdin reader is a cancelreader
// wrapping os.Stdin with no internal pre-buffering, so polling os.Stdin is
// consistent with cancelreader.Read — when poll reports readable, the next
// cancelreader.Read returns immediately with the available byte(s).
func stdinReadableWithin(timeout time.Duration) bool {
	fds := []unix.PollFd{{Fd: int32(os.Stdin.Fd()), Events: unix.POLLIN}}
	msec := int(timeout / time.Millisecond)
	n, err := unix.Poll(fds, msec)
	return err == nil && n > 0 && fds[0].Revents&unix.POLLIN != 0
}
