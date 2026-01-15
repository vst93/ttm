# AGENTS.md - ttm Project Guidelines

## Build, Lint, and Test Commands

### Building
```bash
# Standard build
go build -o ttm .

# Release build with optimizations (used in CI)
go build -o ttm -trimpath -ldflags "-w -s" .

# Cross-platform builds
GOOS=darwin GOARCH=amd64 go build -o ttm-darwin-amd64 .
GOOS=darwin GOARCH=arm64 go build -o ttm-darwin-arm64 .
GOOS=linux GOARCH=amd64 go build -o ttm-linux-amd64 .
GOOS=linux GOARCH=arm64 go build -o ttm-linux-arm64 .
```

### Linting and Formatting
```bash
# Format code (required before committing)
go fmt ./...

# Run static analysis
go vet ./...

# Check for issues
go vet ./... 2>&1

# Verify build without output
go build -o /dev/null ./...
```

### Testing
```bash
# Run all tests (no tests exist currently)
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests for specific package
go test -v ./server/

# Run tests matching pattern
go test -run TestName ./...

# Check test coverage
go test -cover ./...
```

### Dependency Management
```bash
# Download dependencies
go mod download

# Tidy dependencies
go mod tidy

# Verify dependencies
go mod verify
```

## Code Style Guidelines

### Import Organization
- Group imports in this order:
  1. Standard library packages
  2. External/third-party packages
  3. Blank imports (if any)
- Use named imports (not `import .`)

```go
import (
    "encoding/json"
    "fmt"
    "os"

    "github.com/charmbracelet/bubbletea"
    "golang.org/x/crypto/ssh"
)
```

### Naming Conventions
- **Packages**: lowercase, short, descriptive (e.g., `server`, `main`)
- **Types**: PascalCase (e.g., `AppModel`, `SSHConfig`, `BookmarkItem`)
- **Variables**: camelCase (e.g., `appModel`, `sshConfig`)
- **Constants**: camelCase or UPPER_SNAKE_CASE for exported constants
- **Functions**: PascalCase for exported, camelCase for unexported
- **Interfaces**: typically one-method interfaces named with `-er` suffix (e.g., `Reader`, `Writer`)
- **Receiver parameters**: short 1-2 letter names (e.g., `am`, `c`, `b`)

### Error Handling
- **Never ignore errors**: Always check error returns explicitly
- **Early returns**: Return errors immediately, avoid nested conditionals
- **Error messages**: Use lowercase, no punctuation, descriptive
- **Wrap errors**: Use `fmt.Errorf("context: %w", err)` for adding context
- **No empty catch blocks**: Never use `if err != nil { /* empty */ }`

```go
// CORRECT
if err != nil {
    return err
}
return nil

// CORRECT
if err != nil {
    return fmt.Errorf("failed to read config: %w", err)
}

// WRONG - never do this
if err != nil {
    // empty
}
```

### Struct Design
- Use struct tags for JSON/YAML serialization
- Group related fields together
- Use pointer receivers for methods that modify the struct
- Embed structs for composition

```go
type AppModel struct {
    GistConfig
    BookmarkInfo
    list      list.Model
    TipString string
}

type BookmarkItem struct {
    ID     string `json:"id"`
    Title  string `json:"title"`
    Host   string `json:"host"`
    Port   int    `json:"port"`
}
```

### Global Variables
- Minimize global state where possible
- Use package-level variables when necessary (follows existing pattern)
- Document exported global variables

```go
var APP_DIR string
var AM = AppModel{}  // Global app model instance
```

### TUI Patterns (bubbletea)
- Follow MVC pattern: Model, Update, View
- Use `tea.Cmd` for side effects
- Handle messages in Update method with type switches
- Keep View methods pure and fast

```go
func (am *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        if msg.String() == "ctrl+c" {
            return am, tea.Quit
        }
    case tea.WindowSizeMsg:
        // Handle resize
    }
    return am, nil
}
```

### Comments
- Use comments for exported identifiers (godoc style)
- Chinese comments are acceptable (project uses mixed Chinese/English)
- Comment non-obvious code logic
- Keep comments concise and meaningful

### File Organization
- One package per directory (except `main` package)
- Split related functionality into separate files
- Use `_test.go` suffix for test files
- Keep files focused and manageable size

### Code Quality Rules
- **Never suppress errors**: No `// @ts-ignore` equivalents in Go
- **No type assertions without checks**: Use comma-ok idiom
- **No unused imports**: Run `go mod tidy` before committing
- **Always defer resource cleanup**: Files, connections, sessions

### CI/CD Pipeline
- GitHub Actions for releases on tag creation
- Builds for: linux (amd64, arm64), darwin (amd64, arm64), windows (amd64)
- Uses `trimpath` and `-ldflags "-w -s"` for release builds
- Auto-updates Homebrew formula on release

### Project Structure
```
ttm/
├── main.go          # Entry point
├── server/          # Core TUI logic
│   ├── app_model.go
│   ├── bookmarks.go
│   ├── config.go
│   ├── gist.go
│   └── ssh.go
├── cmd/
│   └── install.sh   # Installation script
└── go.mod           # Go module definition
```

### Module Information
- Module name: `ttm`
- Go version: 1.24.3
- Key dependencies: bubbletea, lipgloss, bubbles, x/crypto/ssh

### Common Patterns
- SSH connection management with callbacks
- JSON configuration file storage in `~/.config/ttm/`
- Gist-based bookmark synchronization
- Terminal-specific escape sequences for screen management

### Important Notes for Agents
1. **Verify before committing**: Run `go fmt ./... && go vet ./... && go build .`
2. **No breaking changes**: Don't modify public APIs without good reason
3. **Cross-platform awareness**: Some code uses terminal-specific features (xterm, escape sequences)
4. **Authentication**: Code handles SSH passwords and private keys - keep secure
5. **Testing**: No test suite exists; write tests for new functionality
