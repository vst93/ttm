package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

var Version = "dev"

type githubRelease struct {
	TagName string `json:"tag_name"`
}

type updateCheckResult struct {
	Available   bool
	Latest      string
	Current     string
	Err         error
	Unreachable bool
	UpdateHint  string
}

type updateCheckMsg struct {
	Result updateCheckResult
}

type installMethod int

const (
	installMethodUnknown installMethod = iota
	installMethodBrew
	installMethodScript
	installMethodSource
)

func detectInstallMethod() installMethod {
	exe, err := os.Executable()
	if err != nil {
		return installMethodUnknown
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		exe, _ = os.Executable()
	}
	exe = strings.ToLower(exe)

	// Homebrew prefix detection
	brewPrefixes := []string{
		"/usr/local/Cellar",              // macOS Intel
		"/opt/homebrew/Cellar",           // macOS ARM
		"/home/linuxbrew/.linuxbrew/Cellar", // Linuxbrew
		"/linuxbrew/.linuxbrew/Cellar",
	}
	for _, prefix := range brewPrefixes {
		if strings.Contains(exe, prefix) {
			return installMethodBrew
		}
	}

	// Check if running from a go install / source build location
	if strings.Contains(exe, "/go/bin/") || strings.Contains(exe, "go-build") {
		return installMethodSource
	}

	// Common script install locations
	scriptDirs := []string{
		".local/bin",
		"bin",
	}
	home, _ := os.UserHomeDir()
	for _, dir := range scriptDirs {
		if strings.HasPrefix(exe, filepath.Join(home, dir)) {
			return installMethodScript
		}
	}

	return installMethodSource
}

func updateHint(method installMethod, latest string) string {
	switch method {
	case installMethodBrew:
		return fmt.Sprintf("brew upgrade ttm  (or: brew update && brew upgrade ttm)")
	case installMethodScript:
		return fmt.Sprintf("curl -fsSL -o install.sh https://raw.githubusercontent.com/vst93/ttm/refs/heads/main/cmd/install.sh && bash install.sh")
	case installMethodSource:
		if runtime.GOOS == "windows" {
			return fmt.Sprintf("Download from: https://github.com/vst93/ttm/releases/tag/%s", latest)
		}
		return fmt.Sprintf("go install github.com/vst93/ttm@%s   (or download from https://github.com/vst93/ttm/releases)", latest)
	default:
		return fmt.Sprintf("https://github.com/vst93/ttm/releases/tag/%s", latest)
	}
}

func cleanVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}

func isNewer(current, latest string) bool {
	c := cleanVersion(current)
	l := cleanVersion(latest)
	if c == "dev" || c == "" {
		return l != ""
	}
	return l != c
}

func checkUpdate() updateCheckResult {
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/vst93/ttm/releases/latest")
	if err != nil {
		return updateCheckResult{Err: err, Unreachable: true}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return updateCheckResult{
			Err: fmt.Errorf("GitHub API returned %d", resp.StatusCode),
		}
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return updateCheckResult{Err: fmt.Errorf("failed to parse release: %w", err)}
	}

	latest := release.TagName
	result := updateCheckResult{
		Current: Version,
		Latest:  latest,
	}
	if !isNewer(Version, latest) {
		result.Available = false
		return result
	}
	result.Available = true
	result.UpdateHint = updateHint(detectInstallMethod(), latest)
	return result
}

// execCommandExists checks if a command exists in PATH.
func execCommandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
