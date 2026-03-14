# AGENTS.md

Operational guide for autonomous coding agents working in this repository.

## 1) Project Snapshot
- Language: Go (`go 1.25.0`)
- Module: `ttm`
- Entry point: `main.go`
- Main application package: `server/`
- UI stack: Bubble Tea + Bubbles + Lip Gloss
- Runtime model pattern:
  - Program starts with `tea.NewProgram(&server.AM, tea.WithAltScreen())`
  - Global singleton model: `server.AM`
  - Config root from `os.UserConfigDir()` with `/ttm/` subdirectory

## 2) Repository Layout
- `main.go` - bootstraps CLI flags and starts Bubble Tea program
- `server/app_model.go` - `AppModel`, `Init/Update/View`, keybindings, overlays, locale
- `server/bookmark_crud.go` - add/edit/delete editor flows and form behavior
- `server/bookmarks.go` - bookmark structs and persistence loading
- `server/config.go` - config initialization and save/load logic
- `server/config_editor.go` - in-app config editor UI and field handling
- `server/gist.go` - GitHub/Gitee gist sync operations
- `server/ssh.go` - probe/login/session and terminal handling
- `server/*_test.go` - package tests (currently all tests are in `server/`)

## 3) Build, Lint, and Test Commands
Run commands from repo root: `/home/v/document/project/ttm`

### Build
```bash
# Make target (uses trimpath + ldflags for Version)
make build

# Direct build
go build -o ttm .

# Compile all packages without writing binary output
go build -o /dev/null ./...
```

### Format and Static Checks
```bash
go fmt ./...
go vet ./...
```

### Tests
```bash
# all packages
go test ./...

# server package only
go test ./server

# verbose server tests
go test -v ./server

# single test by exact function name
go test ./server -run "^TestLanguageToggleWithLKey$" -count=1

# single test by prefix/pattern
go test ./server -run "^TestEditor" -count=1

# multiple named tests
go test ./server -run "TestA|TestB" -count=1
```

### Full verification pipeline
```bash
go fmt ./... && go test ./... && go vet ./... && go build -o /dev/null ./...
```

Command notes:
- Use `-count=1` when validating behavior that could be affected by test cache.
- If you changed one function, run its exact test first, then run `go test ./...`.

## 4) Go Style Guidelines

### Imports
- Keep standard Go import grouping:
  1. standard library
  2. third-party dependencies
  3. local module imports (`ttm/...`)
- Let `go fmt` manage import ordering and spacing.

### Formatting
- Run `go fmt ./...` after edits.
- Do not hand-tune spacing/alignment that `go fmt` will rewrite.

### Naming
- Exported identifiers: PascalCase (`AppModel`, `SaveConfig`, `InitConfig`).
- Unexported identifiers: camelCase (`toggleLocale`, `renderDetail`).
- Typed enum constants follow local style (`localeEN`, `localeZH`, `tipInfo`).
- Test names follow `TestXxx` and are behavior-descriptive.

### Error Handling
- Return errors explicitly; do not swallow failures silently.
- Wrap errors with context (for example `fmt.Errorf("failed to read config: %w", err)`).
- In CLI entrypoints, fail fast with clear output and non-zero exit code.

## 5) Bubble Tea / Bubbles Conventions
- Respect MVU flow:
  - `Init` initializes model and startup command
  - `Update` handles all messages/state transitions
  - `View` renders complete UI output
- Keep side effects in commands (`tea.Cmd`) and async messages.
- Keybindings use `key.NewBinding(key.WithKeys(...), key.WithHelp(...))`.
- Help extensions use `AdditionalShortHelpKeys` / `AdditionalFullHelpKeys`.
- Keep locale behavior centralized:
  - use `am.t(en, zh)` for user-facing strings
  - call locale apply helpers so list/help/status text stay in sync
- Preserve ANSI-width-safe overlay behavior when editing UI layers.

### Keyboard and Pagination
- Existing locale toggle key is uppercase `L`; preserve unless intentionally changing UX.
- If adding a new key:
  - handle in `Update`
  - include in short/full help
  - localize help labels for EN/ZH
- Maintain explicit localized paginator strings:
  - EN: `Page %d/%d`
  - ZH: `第%d/%d页`

## 6) Config and Persistence
- Config directory is derived from `os.UserConfigDir()` + `/ttm`.
- Key files:
  - `config.json` for platform/token/gist_id/locale
  - `bookmarks.json` for bookmark entries
- `GistConfig` is the persisted config struct.
- Use `SaveConfig(GistConfig)` for config writes.
- Locale persistence key is `locale` with values `"en"` or `"zh"`.

## 7) Testing Expectations for Agents
- Add or update tests for behavior changes in `server/`.
- For locale changes, verify all three:
  - state toggle behavior
  - rendered help/footer text
  - persisted locale read/write behavior
- For overlay/UI changes, validate ANSI width and stable layout behavior.
- Prefer targeted single-test runs during iteration, then run full suite.

## 8) Delivery and Git Hygiene
- Keep edits scoped to the requested task.
- Avoid unrelated refactors in the same change.
- Do not create commits unless explicitly requested.
- Before declaring completion, run the full verification pipeline.

## 9) Cursor / Copilot Rule Files
Checked locations:
- `.cursor/rules/`
- `.cursorrules`
- `.github/copilot-instructions.md`

Current result:
- No Cursor or Copilot instruction files are present in this repository.

If rule files are added later, treat repository-local rule files as higher-priority guidance than this document.
