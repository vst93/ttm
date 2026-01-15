package main

// These imports will be used later on the tutorial. If you save the file
// now, Go might complain they are unused, but that's fine.
// You may also need to run `go mod tidy` to download bubbletea and its
// dependencies.
import (
	"fmt"
	"os"
	"ttm/server"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	server.AM.Init()
	p := tea.NewProgram(&server.AM, tea.WithAltScreen())
	if err := p.Start(); err != nil {
		fmt.Println("Oh no, there was an error:", err)
		os.Exit(1)
	}
}
