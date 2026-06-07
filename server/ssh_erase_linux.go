//go:build linux

package server

import (
	"syscall"
	"unsafe"

	"golang.org/x/term"
)

// getLocalEraseChar reads the current VERASE character from the local
// terminal's termios settings. This should be called before MakeRaw
// overrides the setting, so the value can be forwarded to the remote PTY
// via SSH TerminalModes. Returns 0 if the value cannot be read.
func getLocalEraseChar(fd int) byte {
	oldState, err := term.GetState(fd)
	if err != nil {
		return 0
	}
	// Restore immediately — we only wanted to peek at the state.
	// MakeRaw will be called right after this.
	defer term.Restore(fd, oldState)

	var t syscall.Termios
	if _, _, errno := syscall.Syscall6(
		syscall.SYS_IOCTL,
		uintptr(fd),
		uintptr(syscall.TCGETS),
		uintptr(unsafe.Pointer(&t)),
		0, 0, 0,
	); errno != 0 {
		return 0
	}
	// VERASE is at index 6 in the c_cc array on Linux.
	if 6 < len(t.Cc) && t.Cc[6] != 0 {
		return byte(t.Cc[6])
	}
	return 0
}
