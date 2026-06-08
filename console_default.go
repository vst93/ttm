//go:build !windows

package main

func initConsoleUTF8() (oldInputCP, oldOutputCP uint32) {
	return 0, 0
}

func restoreConsoleCP(inputCP, outputCP uint32) {}
