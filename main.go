package main

import (
	"fmt"
	"os"
	"ttm/server"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("ttm %s\n", server.Version)
		return
	}

	server.AM.Init()
	p := tea.NewProgram(&server.AM, tea.WithAltScreen())
	if err := p.Start(); err != nil {
		fmt.Println("Oh no, there was an error:", err)
		os.Exit(1)
	}
}
