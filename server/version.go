package server

import (
	"archive/zip"
	"context"
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
	tea "github.com/charmbracelet/bubbletea"
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
	ID      uint64
	Success bool
	Output  string
}

type updateDownloadResultMsg struct {
	ID            uint64
	Success       bool
	Output        string
	NeedSudo      bool
	ExtractedPath string
	ExePath       string
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
	id            uint64
	Result        updateCheckResult
	confirmed     bool
	selected      int // 0 = default action, 1 = cancel
	dlStatus      dlModel
	dlError       string
	dlProgress    float64 // 0.0 ~ 1.0
	dlDownloaded  int64
	dlTotal       int64
	sudoPassword  string
	sudoOutput    string
	sudoInput     textinput.Model
	needSudoFocus bool // true when password input is focused
	extractedPath string
	exePath       string
	updateEvents  <-chan tea.Msg
	cancelUpdate  context.CancelFunc
}

type updateSudoResultMsg struct {
	ID      uint64
	Success bool
	Output  string
}

type updateProgressMsg struct {
	ID         uint64
	Progress   float64
	Downloaded int64
	Total      int64
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

// performUpdate downloads the release archive, extracts the platform binary,
// and installs it. Progress is reported through messages so the Bubble Tea
// event loop remains the only writer of UI state.
func performUpdate(ctx context.Context, downloadURL, latest string, onProgress func(updateProgressMsg)) (bool, string, bool, string, string) {
	if downloadURL == "" {
		return false, "no download URL for this platform", false, "", ""
	}

	exePath, err := os.Executable()
	if err != nil {
		return false, fmt.Sprintf("cannot locate binary: %v", err), false, "", ""
	}
	if resolved, resolveErr := filepath.EvalSymlinks(exePath); resolveErr == nil {
		exePath = resolved
	}

	tmpDir, err := os.MkdirTemp("", "ttm-update-*")
	if err != nil {
		return false, fmt.Sprintf("cannot create temp directory: %v", err), false, "", ""
	}
	keepTemp := false
	defer func() {
		if !keepTemp {
			_ = os.RemoveAll(tmpDir)
		}
	}()

	zipPath := filepath.Join(tmpDir, fmt.Sprintf("ttm-%s.zip", cleanVersion(latest)))

	out, err := os.Create(zipPath)
	if err != nil {
		return false, fmt.Sprintf("cannot create temp file: %v", err), false, "", ""
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		out.Close()
		return false, fmt.Sprintf("cannot create download request: %v", err), false, "", ""
	}
	httpClient := &http.Client{Timeout: 5 * time.Minute}
	resp, err := httpClient.Do(req)
	if err != nil {
		out.Close()
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return false, "update cancelled", false, "", ""
		}
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
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return false, "update cancelled", false, "", ""
		}
		return false, fmt.Sprintf("download interrupted: %v", err), false, "", ""
	}
	if err := out.Close(); err != nil {
		return false, fmt.Sprintf("cannot finish download: %v", err), false, "", ""
	}

	// Extract the zip.
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return false, fmt.Sprintf("cannot open zip: %v", err), false, "", ""
	}
	defer reader.Close()

	var extractedPath string
	wantBinary := "ttm"
	if runtime.GOOS == "windows" {
		wantBinary += ".exe"
	}
	for _, f := range reader.File {
		info := f.FileInfo()
		if info.IsDir() || filepath.Base(f.Name) != wantBinary {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return false, fmt.Sprintf("cannot extract: %v", err), false, "", ""
		}
		extractedPath = filepath.Join(tmpDir, wantBinary)
		extracted, err := os.OpenFile(extractedPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
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
	if runtime.GOOS != "windows" {
		if err := os.Chmod(extractedPath, 0755); err != nil {
			return false, fmt.Sprintf("cannot make update executable: %v", err), false, "", ""
		}
	}

	if runtime.GOOS == "windows" {
		if err := checkInstallDirectory(exePath); err != nil {
			if isPermissionError(err) {
				return false, windowsPermissionMsg(exePath), false, "", ""
			}
			return false, fmt.Sprintf("cannot prepare update directory: %v", err), false, "", ""
		}
		if err := scheduleWindowsUpdate(extractedPath, exePath); err != nil {
			if isPermissionError(err) {
				return false, windowsPermissionMsg(exePath), false, extractedPath, exePath
			}
			return false, fmt.Sprintf("cannot schedule update: %v", err), false, "", ""
		}
		keepTemp = true
		return true, "close ttm to finish installing the update, then reopen it", false, "", ""
	}

	if err := installUnixBinary(extractedPath, exePath); err != nil {
		if isPermissionError(err) {
			needSudo := runtime.GOOS != "android" && execCommandExists("sudo")
			keepTemp = needSudo
			return false, permissionErrorMsg(extractedPath, exePath, needSudo), needSudo, extractedPath, exePath
		}
		return false, fmt.Sprintf("cannot replace binary: %v", err), false, "", ""
	}

	return true, "restart ttm to use the new version", false, "", ""
}

func checkInstallDirectory(exePath string) error {
	probe, err := os.CreateTemp(filepath.Dir(exePath), ".ttm-write-check-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

func installUnixBinary(src, dst string) error {
	staged, err := os.CreateTemp(filepath.Dir(dst), ".ttm-update-*")
	if err != nil {
		return err
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)

	in, err := os.Open(src)
	if err != nil {
		staged.Close()
		return err
	}
	_, copyErr := io.Copy(staged, in)
	closeInErr := in.Close()
	syncErr := staged.Sync()
	chmodErr := staged.Chmod(0755)
	closeOutErr := staged.Close()
	for _, candidate := range []error{copyErr, closeInErr, syncErr, chmodErr, closeOutErr} {
		if candidate != nil {
			return candidate
		}
	}
	return os.Rename(stagedPath, dst)
}

func scheduleWindowsUpdate(src, dst string) error {
	helperPath := filepath.Join(filepath.Dir(src), "finish-update.cmd")
	script := fmt.Sprintf("@echo off\r\n:retry\r\nmove /Y %s %s >nul 2>&1\r\nif errorlevel 1 (\r\n  timeout /t 1 /nobreak >nul\r\n  goto retry\r\n)\r\ndel /Q \"%%~dp0*.zip\" >nul 2>&1\r\ndel /Q \"%%~f0\"\r\nrmdir \"%%~dp0.\" >nul 2>&1\r\n", windowsQuote(src), windowsQuote(dst))
	if err := os.WriteFile(helperPath, []byte(script), 0600); err != nil {
		return err
	}
	cmd := exec.Command("cmd.exe", "/C", "start", "", "/B", helperPath)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func windowsQuote(path string) string {
	return `"` + strings.ReplaceAll(path, `"`, `""`) + `"`
}

// isPermissionError checks if an error is a permission denied error.
func isPermissionError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) ||
		strings.Contains(message, "permission denied") || strings.Contains(message, "access is denied") ||
		strings.Contains(message, "access denied")
}

// permissionErrorMsg returns a friendly bilingual message telling the user
// how to manually complete the update when the binary directory is not writable.
// extractedPath is the path to the downloaded new binary.
// exePath is the path to the currently running binary.
func permissionErrorMsg(extractedPath, exePath string, canSudo bool) string {
	method := detectInstallMethod()
	var fixCmd string
	switch method {
	case installMethodBrew:
		fixCmd = "EN: brew upgrade ttm\nCN: brew upgrade ttm\n  (brew will update automatically / brew 会自动完成更新)"
	case installMethodScript, installMethodSource:
		if canSudo {
			fixCmd = fmt.Sprintf("EN: grant permission in this dialog, or run:\n  sudo mv %q %q\nCN: 可在当前窗口授权，或运行上面的命令", extractedPath, exePath)
		} else if runtime.GOOS == "android" {
			fixCmd = "EN: Termux has no sudo. Reinstall ttm into $PREFIX/bin.\nCN: Termux 不使用 sudo，请将 ttm 重新安装到 $PREFIX/bin。"
		} else {
			fixCmd = "EN: sudo is unavailable. Reinstall ttm into a user-writable bin directory.\nCN: 当前没有 sudo，请将 ttm 重新安装到用户可写的 bin 目录。"
		}
	default:
		fixCmd = "EN: reinstall ttm into a writable directory\nCN: 请将 ttm 重新安装到可写目录"
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
func brewUpgrade(ctx context.Context) (bool, string) {
	cmd := exec.CommandContext(ctx, "brew", "upgrade", "ttm")
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
	onProgress func(updateProgressMsg)
	lastReport float64
	lastBytes  int64
}

func (p *progressReadCloser) Read(b []byte) (int, error) {
	n, err := p.ReadCloser.Read(b)
	p.read += int64(n)
	if p.onProgress == nil {
		return n, err
	}
	progress := float64(0)
	shouldReport := p.lastBytes == 0 || p.read-p.lastBytes >= 256*1024 || err == io.EOF
	if p.total > 0 {
		progress = float64(p.read) / float64(p.total)
		if progress > 1 {
			progress = 1
		}
		shouldReport = shouldReport || progress-p.lastReport >= 0.01 || progress >= 1.0
	}
	if shouldReport && (n > 0 || p.read != p.lastBytes) {
		p.lastReport = progress
		p.lastBytes = p.read
		p.onProgress(updateProgressMsg{Progress: progress, Downloaded: p.read, Total: p.total})
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
