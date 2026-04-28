package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"ttm/server"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("ttm %s\n", server.Version)
		return
	}

	if runtime.GOOS == "windows" && os.Getenv("WT_SESSION") == "" {
		exePath, err := os.Executable()
		if err == nil {
			if wtPath, err := exec.LookPath("wt.exe"); err == nil {
				args := append([]string{"new-tab", "cmd", "/k", exePath}, os.Args[1:]...)
				cmd := exec.Command(wtPath, args...)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				cmd.Stdin = os.Stdin
				if err := cmd.Start(); err == nil {
					os.Exit(0)
				}
			}
		}

		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, strings.TrimSpace(`
For the best experience, run ttm in Windows Terminal.
CMD may show layout glitches or misaligned rendering in the TUI.
You can install Windows Terminal from: https://aka.ms/windowsterminal
`))
		fmt.Fprintln(os.Stderr)
	}

	server.AM.Init()
	p := tea.NewProgram(&server.AM, tea.WithAltScreen())
	if err := p.Start(); err != nil {
		fmt.Println("Oh no, there was an error:", err)
		os.Exit(1)
	}
}
