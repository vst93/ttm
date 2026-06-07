//go:build !linux

package server

// getLocalEraseChar is a stub for non-Linux platforms.
// On non-Linux systems we skip forwarding the VERASE character.
func getLocalEraseChar(fd int) byte {
	return 0
}
