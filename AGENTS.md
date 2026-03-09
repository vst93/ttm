# AGENTS.md

Operational guide for autonomous coding agents working in this repository.

## 1) Project Snapshot

- Language: Go (`go 1.25.0`)
- Module: `ttm`
- Entry point: `main.go`
- Core package: `server/`
- UI stack: Bubble Tea + Bubbles + Lip Gloss
- Key runtime behavior:
  - TUI runs with `tea.WithAltScreen()`
  - Global model singleton: `server.AM`
  - Config stored via `os.UserConfigDir()` + `/ttm/config.json`

## 2) Source Layout

- `main.go` — program bootstrap
- `server/app_model.go` — primary Bubble Tea model, key handling, overlays, locale
- `server/config.go` — config read/write and path setup
- `server/bookmarks.go` — bookmark file loading
- `server/gist.go` — gist sync operations
- `server/ssh.go` — SSH probe/login/session handling
- `server/*_test.go` — tests

## 3) Build / Lint / Test Commands

Run from repo root: `/Users/vst/Code/goProgram/ttm`

### Build

```bash
go build -o ttm .
go build -o /dev/null ./...
```

### Format + Static Checks

```bash
go fmt ./...
go vet ./...
```

### Full Verification Pipeline (preferred before completion)

```bash
go fmt ./... && go test ./... && go vet ./... && go build -o /dev/null ./...
```

### Tests

```bash
# all tests
go test ./...

# package tests
go test ./server

# verbose package tests
go test -v ./server

# single test by exact name
go test ./server -run "^TestLanguageToggleWithLKey$" -count=1

# multiple specific tests
go test ./server -run "TestA|TestB" -count=1
```

Notes:
- Use `-count=1` when validating behavior sensitive to cache.
- This repo currently has tests only under `server/`.

## 4) Coding Style Conventions

### Imports

- Keep Go standard grouping:
  1. stdlib
  2. third-party
  3. local module imports
- Let `go fmt` normalize ordering.

### Formatting

- Always run `go fmt ./...` after edits.
- Avoid manual alignment/spacing tweaks that fight `go fmt`.

### Naming

- Exported identifiers: PascalCase (`AppModel`, `SaveConfig`)
- Unexported identifiers: camelCase (`toggleLocale`, `applyListLocale`)
- Constants/enums: existing style (`localeEN`, `tipInfo`) — follow local pattern.

### Types and State

- Reuse existing typed enums where present (e.g., `type locale int`, `type tipLevel int`).
- Avoid introducing duplicate state sources; update existing model fields.
- Keep `AppModel` as single source of UI state.

### Error Handling

- Prefer explicit handling over silent discard.
- Wrap with context when returning errors:
  - `fmt.Errorf("failed to read config: %w", err)`
- If intentionally ignoring an error, justify with code structure and ensure UX still safe.

### Comments

- Keep comments minimal and only for non-obvious logic.
- Do not add narrative comments for straightforward code.

## 5) TUI / Bubble Tea Patterns

- Follow Bubble Tea loop discipline:
  - state transitions in `Update`
  - rendering in `View`
  - side effects through `tea.Cmd`
- Keep overlays width-safe (ANSI-aware width handling already exists).
- For locale-aware UI text:
  - use existing translation helper `am.t(en, zh)`
  - call locale application hooks that update list title/help/status labels.

### Keyboard Handling

- Existing language toggle is uppercase `L`.
- When adding keybindings:
  - update behavior in `Update`
  - update help via `AdditionalShortHelpKeys` / `AdditionalFullHelpKeys`
  - ensure locale-specific help labels are applied.

### Pagination

- Pagination is enabled and localized via `Paginator.ArabicFormat`.
- Keep explicit page indicators:
  - EN: `Page %d/%d`
  - ZH: `第%d/%d页`

## 6) Config and Persistence

- Config path uses `os.UserConfigDir()`:
  - macOS: `~/Library/Application Support/ttm/config.json`
  - Linux: `~/.config/ttm/config.json` (if XDG default)
- Config struct: `GistConfig`
- Persist changes via `SaveConfig(GistConfig)`.
- Current persisted locale key: `locale` (`"en"` or `"zh"`).

## 7) Testing Expectations for Agents

- Add/adjust tests for behavior changes.
- For locale features, verify:
  - state toggles
  - rendered help/footer text
  - persisted config behavior
- For UI alignment/overlay changes, assert ANSI width consistency (existing tests provide pattern).

## 8) Git/Delivery Expectations

- Do not commit unless explicitly asked.
- Keep edits focused; avoid unrelated refactors.
- Before declaring completion, run full verification pipeline.

## 9) Cursor / Copilot Rules

Searched locations:
- `.cursor/rules/`
- `.cursorrules`
- `.github/copilot-instructions.md`

Result:
- No Cursor/Copilot rule files found in this repository at the time of writing.

If such files are added later, update this section and prioritize repository-local rules.
