package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"ttm/server"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) > 2 && os.Args[1] == "--import-config" {
		importConfig(os.Args[2])
		return
	}

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

func maskToken(token string) string {
	if len(token) <= 6 {
		return strings.Repeat("*", len(token))
	}
	return token[:4] + strings.Repeat("*", len(token)-7) + token[len(token)-3:]
}

func importConfig(encoded string) {
	jsonBytes, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to decode config: %v\n", err)
		os.Exit(1)
	}

	var config server.GistConfig
	if err := json.Unmarshal(jsonBytes, &config); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to parse config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(strings.Repeat("─", 48))
	fmt.Println("  Config Import")
	fmt.Println(strings.Repeat("─", 48))
	fmt.Printf("  Platform  :  %s\n", config.Platform)
	tokenDisplay := maskToken(config.Token)
	if config.Token == "" {
		tokenDisplay = "(empty)"
	}
	fmt.Printf("  Token     :  %s\n", tokenDisplay)
	gistDisplay := config.GistID
	if gistDisplay == "" {
		gistDisplay = "(auto-create)"
	}
	fmt.Printf("  Gist ID   :  %s\n", gistDisplay)
	fmt.Printf("  Locale    :  %s\n", config.Locale)
	fmt.Println(strings.Repeat("─", 48))

	if config.Token != "" {
		fmt.Println("  ! This config contains a token. Handle with care.")
		fmt.Println()
	}

	fmt.Print("  Import this config? [y/N] ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input != "y" && input != "yes" {
		fmt.Println("  Import cancelled.")
		os.Exit(0)
	}

	if err := server.SaveConfig(config); err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to save config: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("  Config imported successfully!")
	fmt.Println(strings.Repeat("─", 48))
}
