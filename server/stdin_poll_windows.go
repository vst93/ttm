//go:build windows

package server

import (
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// stdinReadableWithin reports whether os.Stdin has data available to read
// within the given timeout, without blocking beyond it. See the Unix build
// for the rationale; here it uses WaitForSingleObject on the console handle.
func stdinReadableWithin(timeout time.Duration) bool {
	var msec uint32 = windows.INFINITE
	if timeout >= 0 {
		msec = uint32(timeout / time.Millisecond)
	}
	h := windows.Handle(os.Stdin.Fd())
	s, _ := windows.WaitForSingleObject(h, msec)
	return s == windows.WAIT_OBJECT_0
}
