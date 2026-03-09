# TTM

TUI Terminal Manager - SSH bookmark manager with Gist sync.

## Features

- TUI interface built with Bubble Tea
- SSH connections to bookmarked servers
- Multiple auth methods: password, private key, keyboard-interactive
- Gist sync (GitHub/Gitee)
- Cross-platform: macOS, Linux, Windows

## Install

```bash
# Homebrew
brew install vst93/tap/ttm

# Shell script
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh)"
```

## Usage

| Key | Action |
|-----|--------|
| `Enter` | Connect to server |
| `s` | Sync with Gist |
| `j/k` or `↑/↓` | Navigate |
| `Ctrl+C` | Quit |

## Config

Config stored in `~/.config/ttm/`:

```
~/.config/ttm/
├── config.json     # { "platform": "github", "token": "...", "gist_id": "..." }
└── bookmarks.json  # SSH bookmark entries
```

## Build

```bash
git clone https://github.com/vst93/ttm.git
cd ttm
go build -o ttm .
./ttm
```

## Dependencies

- [bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [lipgloss](https://github.com/charmbracelet/lipgloss) - Styling
- [x/crypto/ssh](https://pkg.go.dev/golang.org/x/crypto/ssh) - SSH client

MIT License
