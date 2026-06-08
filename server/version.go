package server

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
)

var Version = "dev"

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

type updateCheckResult struct {
	Available   bool
	Latest      string
	Current     string
	Err         error
	Unreachable bool
	InstallHint string // short hint for the tip bar
	DownloadURL string // direct download URL for current platform
	Method      installMethod
}

type updateCheckMsg struct {
	Result updateCheckResult
}

type updateBrewResultMsg struct {
	Success bool
	Output  string
}

type updateDownloadResultMsg struct {
	Success bool
	Output  string
}

type installMethod int

const (
	installMethodUnknown installMethod = iota
	installMethodBrew
	installMethodScript
	installMethodSource
)

type dlModel int

const (
	dlModelSelect dlModel = iota
	dlModelDownloading
	dlModelDone
	dlModelFailed
	dlModelCancelled
	dlModelNeedSudo
	dlModelSudoDone
	dlModelSudoFailed
)

// updatePromptState holds the interactive update confirmation state.
type updatePromptState struct {
	Result        updateCheckResult
	confirmed     bool
	selected      int // 0 = default action, 1 = cancel
	dlStatus      dlModel
	dlError       string
	dlProgress    float64 // 0.0 ~ 1.0
	sudoPassword  string
	sudoOutput    string
	sudoInput     textinput.Model
	needSudoFocus bool // true when password input is focused
}

type updateSudoResultMsg struct {
	Success bool
	Output  string
}

type updateProgressMsg struct {
	Progress float64
}

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

	brewPrefixes := []string{
		"/usr/local/Cellar",
		"/opt/homebrew/Cellar",
		"/home/linuxbrew/.linuxbrew/Cellar",
		"/linuxbrew/.linuxbrew/Cellar",
	}
	for _, prefix := range brewPrefixes {
		if strings.Contains(exe, prefix) {
			return installMethodBrew
		}
	}

	if strings.Contains(exe, "/go/bin/") || strings.Contains(exe, "go-build") {
		return installMethodSource
	}

	scriptDirs := []string{".local/bin", "bin"}
	home, _ := os.UserHomeDir()
	for _, dir := range scriptDirs {
		if strings.HasPrefix(exe, filepath.Join(home, dir)) {
			return installMethodScript
		}
	}

	return installMethodSource
}

// assetNameForPlatform returns the expected release asset name for the
// current OS and architecture (e.g. "ttm-linux-amd64.zip").
func assetNameForPlatform() string {
	goos := runtime.GOOS
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "amd64"
	} else if arch == "arm64" {
		arch = "arm64"
	}
	return fmt.Sprintf("ttm-%s-%s.zip", goos, arch)
}

func hintForMethod(method installMethod, latest string) string {
	switch method {
	case installMethodBrew:
		return "brew upgrade ttm"
	case installMethodScript:
		return "re-run install script"
	case installMethodSource:
		return "download & replace binary"
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
	if l == c {
		return false
	}
	// Semver comparison: compare numeric parts of major.minor.patch
	cParts := parseSemver(c)
	lParts := parseSemver(l)
	for i := 0; i < 3; i++ {
		if lParts[i] != cParts[i] {
			return lParts[i] > cParts[i]
		}
	}
	return false
}

// parseSemver parses "1.2.3" into [3]int{1, 2, 3}.
// Missing parts default to 0.
func parseSemver(v string) [3]int {
	var parts [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		fmt.Sscanf(p, "%d", &parts[i])
	}
	return parts
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
	method := detectInstallMethod()
	result := updateCheckResult{
		Current:     Version,
		Latest:      latest,
		Method:      method,
		InstallHint: hintForMethod(method, latest),
	}

	// Find the download URL for the current platform.
	wantAsset := assetNameForPlatform()
	for _, a := range release.Assets {
		if a.Name == wantAsset {
			result.DownloadURL = a.BrowserDownloadURL
			break
		}
	}

	if !isNewer(Version, latest) {
		result.Available = false
		return result
	}
	result.Available = true
	return result
}

// performUpdate downloads the release zip for the current platform,
// extracts it, and replaces the running binary.
// Returns (success, message, needSudo, extractedPath, exePath).
// onProgress is called with a value between 0.0 and 1.0 during download.
func performUpdate(downloadURL, latest string, onProgress func(float64)) (bool, string, bool, string, string) {
	if downloadURL == "" {
		return false, "no download URL for this platform", false, "", ""
	}

	exePath, err := os.Executable()
	if err != nil {
		return false, fmt.Sprintf("cannot locate binary: %v", err), false, "", ""
	}
	exePath, _ = filepath.EvalSymlinks(exePath)

	// Download to a temp file.
	tmpDir := os.TempDir()
	zipPath := filepath.Join(tmpDir, fmt.Sprintf("ttm-%s.zip", latest))

	out, err := os.Create(zipPath)
	if err != nil {
		return false, fmt.Sprintf("cannot create temp file: %v", err), false, "", ""
	}

	httpClient := &http.Client{Timeout: 60 * time.Second}
	resp, err := httpClient.Get(downloadURL)
	if err != nil {
		out.Close()
		os.Remove(zipPath)
		return false, fmt.Sprintf("download failed: %v", err), false, "", ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		out.Close()
		os.Remove(zipPath)
		return false, fmt.Sprintf("download returned %d", resp.StatusCode), false, "", ""
	}

	totalSize := resp.ContentLength
	progressReader := &progressReadCloser{
		ReadCloser: resp.Body,
		total:      totalSize,
		onProgress: onProgress,
	}
	if _, err := io.Copy(out, progressReader); err != nil {
		out.Close()
		os.Remove(zipPath)
		return false, fmt.Sprintf("download interrupted: %v", err), false, "", ""
	}
	out.Close()

	// Extract the zip.
	reader, err := zip.OpenReader(zipPath)
	os.Remove(zipPath)
	if err != nil {
		return false, fmt.Sprintf("cannot open zip: %v", err), false, "", ""
	}
	defer reader.Close()

	var extractedPath string
	for _, f := range reader.File {
		if strings.HasPrefix(f.Name, ".") {
			continue
		}
		info := f.FileInfo()
		if info.IsDir() {
			continue
		}
		// We expect a single file named "ttm" (or "ttm.exe").
		rc, err := f.Open()
		if err != nil {
			return false, fmt.Sprintf("cannot extract: %v", err), false, "", ""
		}
		extractedPath = filepath.Join(tmpDir, f.Name)
		extracted, err := os.OpenFile(extractedPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
		if err != nil {
			rc.Close()
			return false, fmt.Sprintf("cannot write temp binary: %v", err), false, "", ""
		}
		_, err = io.Copy(extracted, rc)
		extracted.Close()
		rc.Close()
		if err != nil {
			os.Remove(extractedPath)
			return false, fmt.Sprintf("extraction interrupted: %v", err), false, "", ""
		}
		break // only need the first (and likely only) file
	}

	if extractedPath == "" {
		return false, "no binary found in archive", false, "", ""
	}

	// Replace the running binary.
	// On Unix we can overwrite an executing binary; on we rename the old one first.
	oldPath := exePath + ".old"
	_ = os.Remove(oldPath)

	// Attempt a direct rename-based swap:
	//   ttm.current -> ttm.old
	//   ttm.new     -> ttm.current
	if err := os.Rename(exePath, oldPath); err != nil {
		// Check for permission denied.
		if isPermissionError(err) {
			if runtime.GOOS == "windows" {
				return false, windowsPermissionMsg(exePath), false, "", ""
			}
			return false, permissionErrorMsg(extractedPath, exePath), true, extractedPath, exePath
		}
		// Fallback: try direct copy if rename fails (e.g. permissions).
		if err := copyFile(extractedPath, exePath); err != nil {
			if isPermissionError(err) {
				if runtime.GOOS == "windows" {
					return false, windowsPermissionMsg(exePath), false, "", ""
				}
				return false, permissionErrorMsg(extractedPath, exePath), true, extractedPath, exePath
			}
			os.Remove(extractedPath)
			return false, fmt.Sprintf("cannot replace binary: %v", err), false, "", ""
		}
	} else {
		if err := os.Rename(extractedPath, exePath); err != nil {
			// Check for permission denied.
			if isPermissionError(err) {
				// Rollback the first rename.
				os.Rename(oldPath, exePath)
				if runtime.GOOS == "windows" {
					return false, windowsPermissionMsg(exePath), false, "", ""
				}
				return false, permissionErrorMsg(extractedPath, exePath), true, extractedPath, exePath
			}
			// Rollback.
			os.Rename(oldPath, exePath)
			return false, fmt.Sprintf("cannot install new binary: %v", err), false, "", ""
		}
		_ = os.Remove(oldPath)
	}

	return true, "restart ttm to use the new version", false, "", ""
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

// isPermissionError checks if an error is a permission denied error.
func isPermissionError(err error) bool {
	return errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) ||
		strings.Contains(err.Error(), "permission denied")
}

// permissionErrorMsg returns a friendly bilingual message telling the user
// how to manually complete the update when the binary directory is not writable.
// extractedPath is the path to the downloaded new binary.
// exePath is the path to the currently running binary.
func permissionErrorMsg(extractedPath, exePath string) string {
	method := detectInstallMethod()
	var fixCmd string
	switch method {
	case installMethodBrew:
		fixCmd = "EN: brew upgrade ttm\nCN: brew upgrade ttm\n  (brew will update automatically / brew 会自动完成更新)"
	default:
		fixCmd = fmt.Sprintf("EN: sudo mv %s %s\nCN: sudo mv %s %s\n  (replace binary with sudo / 用 sudo 权限手动替换)", extractedPath, exePath, extractedPath, exePath)
	}
	return fmt.Sprintf(
		"⚠ permission denied: cannot replace %s\n"+
			"  EN: Admin permission required to update system directory.\n"+
			"  CN: 需要管理员权限才能更新到系统目录。\n"+
			"\n"+
			"  Fix / 解决方法:\n%s",
		exePath, fixCmd,
	)
}

// execCommandExists checks if a command exists in PATH.
func execCommandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// brewUpgrade runs "brew upgrade ttm" and returns (success, outputOrError).
func brewUpgrade() (bool, string) {
	cmd := exec.Command("brew", "upgrade", "ttm")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Sprintf("brew upgrade failed: %s", strings.TrimSpace(string(out)))
	}
	return true, strings.TrimSpace(string(out))
}

// progressReadCloser wraps an io.ReadCloser to report read progress.
type progressReadCloser struct {
	io.ReadCloser
	total      int64
	read       int64
	onProgress func(float64)
	lastReport float64
}

func (p *progressReadCloser) Read(b []byte) (int, error) {
	n, err := p.ReadCloser.Read(b)
	p.read += int64(n)
	if p.onProgress != nil && p.total > 0 {
		progress := float64(p.read) / float64(p.total)
		// Report at 1% increments to avoid flooding
		if progress-p.lastReport >= 0.01 || progress >= 1.0 {
			p.lastReport = progress
			p.onProgress(progress)
		}
	}
	return n, err
}

// windowsPermissionMsg returns a friendly message for Windows users when
// the update fails due to permission denied. Windows has no sudo, so we
// guide the user to run as admin or manually replace.
func windowsPermissionMsg(exePath string) string {
	return fmt.Sprintf(
		"⚠ permission denied: cannot replace %s\n"+
			"  EN: The binary is in a protected directory. Please run ttm as Administrator and press U again.\n"+
			"  CN: 二进制文件位于受保护目录。请以管理员身份运行 ttm 后按 U 更新。\n"+
			"\n"+
			"  Fix / 解决方法:\n"+
			"  1. Right-click ttm → 'Run as administrator', then press U\n"+
			"     右键 ttm → '以管理员身份运行'，然后按 U 更新",
		exePath,
	)
}
