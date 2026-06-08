//go:build windows

package main

import (
	"golang.org/x/sys/windows"
)

const (
	CP_UTF8      = 65001
	STD_INPUT_HANDLE  = ^uintptr(0) - 9  // -10
	STD_OUTPUT_HANDLE = ^uintptr(0) - 11 // -11
)

// initConsoleUTF8 sets the Windows console to UTF-8 (CP65001) for both
// input and output. It returns the original code pages so they can be
// restored on exit.
func initConsoleUTF8() (oldInputCP, oldOutputCP uint32) {
	stdin := windows.Handle(STD_INPUT_HANDLE)
	stdout := windows.Handle(STD_OUTPUT_HANDLE)
	_ = stdin
	_ = stdout

	windows.GetConsoleCP(&oldInputCP)
	windows.GetConsoleOutputCP(&oldOutputCP)

	windows.SetConsoleCP(CP_UTF8)
	windows.SetConsoleOutputCP(CP_UTF8)

	return oldInputCP, oldOutputCP
}

// restoreConsoleCP restores the original console code pages.
func restoreConsoleCP(inputCP, outputCP uint32) {
	windows.SetConsoleCP(inputCP)
	windows.SetConsoleOutputCP(outputCP)
}
