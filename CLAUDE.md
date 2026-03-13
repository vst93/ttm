# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

TTM (TUI Terminal Manager) is an SSH bookmark manager with a Bubble Tea TUI interface. It supports password, private key, and keyboard-interactive auth, with bookmark sync via GitHub/Gitee Gists. Bilingual UI (English/Chinese) with persisted locale preference.

## Build & Test Commands

```bash
# Build
go build -o ttm .

# Format & lint
go fmt ./...
go vet ./...

# Run all tests
go test ./...

# Run a single test
go test ./server -run "^TestName$" -count=1

# Full verification pipeline (run before completing work)
go fmt ./... && go test ./... && go vet ./... && go build -o /dev/null ./...
```

Tests only exist under `server/`. Use `-count=1` to bypass test cache when verifying behavior changes.

## Architecture

- **`main.go`** — Bootstrap: initializes the global `server.AM` model, starts Bubble Tea with alt screen.
- **`server/`** — All application logic lives here as a single package.

### Core Files

- **`app_model.go`** — `AppModel` struct (the Bubble Tea model), `Update`/`View` loop, overlay system (tip notifications, connecting modal), locale toggle (`L` key), and list rendering. Contains the global singleton `AM`.
- **`config.go`** — `GistConfig` struct, `InitConfig`/`SaveConfig`. Config path: `os.UserConfigDir()/ttm/config.json`.
- **`bookmarks.go`** — `BookmarkInfo`/`BookmarkItem` types, loading from `bookmarks.json`.
- **`bookmark_crud.go`** — Add/edit/delete bookmark editor overlay with `bookmarkEditor` state machine. Save via `Ctrl+S`, auth type cycling via `m` key.
- **`gist.go`** — GitHub/Gitee Gist API for bookmark sync (GET/PATCH/POST).
- **`ssh.go`** — SSH client (`defaultClient`), connection probe, interactive login with PTY, terminal resize, keepalive.

### Key Patterns

- **Global singleton**: `server.AM` is the single `AppModel` instance, mutated directly throughout.
- **Overlay system**: `overlayAt`/`overlayCenter`/`overlayTopRight` compose layered views using ANSI-aware width calculations (`ansi.StringWidth`). All output lines must maintain identical visual width.
- **Locale**: `am.t(en, zh)` helper for bilingual text. Locale persisted in config as `"en"`/`"zh"`. `applyListLocale()` updates all help keys, pagination format, and status bar labels.
- **Bubble Tea discipline**: State transitions in `Update`, rendering in `View`, side effects via `tea.Cmd`. SSH connections use `tea.Exec` (interactive) or probe-then-exec (non-interactive).
- **Auth types**: `keyboard-interactive` (default), `password`, `private-key`. Editor masks secrets with `"••••••"` and preserves originals when unchanged.

## Coding Conventions

- Follow existing import grouping: stdlib, third-party, local (`ttm/server`).
- Always run `go fmt ./...` after edits.
- Exported: PascalCase. Unexported: camelCase. Enums follow local pattern (`localeEN`, `tipInfo`).
- Wrap errors with context: `fmt.Errorf("failed to X: %w", err)`.
- Keep `AppModel` as the single source of UI state — avoid duplicate state.
- When adding keybindings: update `Update`, help keys (`AdditionalShortHelpKeys`/`AdditionalFullHelpKeys`), and locale-specific labels in `applyListLocale`.
