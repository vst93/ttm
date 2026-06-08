//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

const (
	CP_UTF8 = 65001
)

// initConsoleUTF8 sets the Windows console to UTF-8 (CP65001) for both
// input and output. It returns the original code pages so they can be
// restored on exit.
func initConsoleUTF8() (oldInputCP, oldOutputCP uint32) {
	oldInputCP, _ = windows.GetConsoleCP()
	oldOutputCP, _ = windows.GetConsoleOutputCP()

	windows.SetConsoleCP(CP_UTF8)
	windows.SetConsoleOutputCP(CP_UTF8)

	return oldInputCP, oldOutputCP
}

// restoreConsoleCP restores the original console code pages.
func restoreConsoleCP(inputCP, outputCP uint32) {
	windows.SetConsoleCP(inputCP)
	windows.SetConsoleOutputCP(outputCP)
}
