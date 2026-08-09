package server

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"
)

// uploadAction represents the user's choice in the upload menu.
type uploadAction int

const (
	uploadActionCopy     uploadAction = iota // copy scp command to clipboard
	uploadActionUpload                       // upload file/directory directly
	uploadActionDownload                     // download file/directory from remote
)

// showActionMenu displays a three-option menu and returns the user's choice.
func showActionMenu(tty io.Writer, stdinReader io.Reader, loc locale) (uploadAction, bool) {
	menuTitle := localeT(loc, "─── TTM Transfer ───", "─── TTM 传输 ───")
	opt1 := localeT(loc, "Copy scp command to clipboard", "复制 scp 命令到剪贴板")
	opt2 := localeT(loc, "Upload local file/directory to remote", "上传本地文件/目录到远程")
	opt3 := localeT(loc, "Download remote file/directory to local", "下载远程文件/目录到本地")
	hint := localeT(loc, "1/2/3 to choose, Esc to cancel", "按 1/2/3 选择，Esc 取消")

	fmt.Fprintf(tty, "\r\n\x1b[1;36m%s\x1b[0m\r\n", menuTitle)
	fmt.Fprintf(tty, "  \x1b[33m1)\x1b[0m %s\r\n", opt1)
	fmt.Fprintf(tty, "  \x1b[33m2)\x1b[0m %s\r\n", opt2)
	fmt.Fprintf(tty, "  \x1b[33m3)\x1b[0m %s\r\n", opt3)
	fmt.Fprintf(tty, "\r\n  %s: ", hint)

	for {
		b, ok := readSignificantByte(stdinReader)
		if !ok {
			continue
		}
		switch b {
		case '1':
			fmt.Fprintf(tty, "1\r\n")
			return uploadActionCopy, true
		case '2':
			fmt.Fprintf(tty, "2\r\n")
			return uploadActionUpload, true
		case '3':
			fmt.Fprintf(tty, "3\r\n")
			return uploadActionDownload, true
		case 0x1B, 0x03: // Escape or Ctrl+C (standalone Esc; OSC/CSI responses are drained)
			cancelMsg := localeT(loc, "cancelled", "已取消")
			fmt.Fprintf(tty, "\r\n\x1b[2m%s\x1b[0m\r\n", cancelMsg)
			return 0, false
		}
	}
}

// readInputLine reads a line of input with an optional pre-filled default value.
// Supports full UTF-8 input including Chinese characters.
// When tabComplete is non-nil, Tab triggers path completion.
func readInputLine(tty io.Writer, stdinReader io.Reader, defaultVal string, tabComplete func(string) (string, []string)) string {
	input := []rune(defaultVal)
	modified := false
	if len(input) > 0 {
		fmt.Fprintf(tty, "%s", string(input))
	}

	buf := make([]byte, 1)
	for {
		// First byte via readSignificantByte so terminal escape sequences
		// (OSC/CSI responses from starship/oh-my-zsh) are drained instead
		// of polluting the input field. A standalone Esc cancels.
		b, ok := readSignificantByte(stdinReader)
		if !ok {
			continue
		}

		// Determine UTF-8 sequence length from first byte.
		var seqLen int
		switch {
		case b&0x80 == 0: // 0xxxxxxx — ASCII
			seqLen = 1
		case b&0xE0 == 0xC0: // 110xxxxx — 2 bytes
			seqLen = 2
		case b&0xF0 == 0xE0: // 1110xxxx — 3 bytes (Chinese, Japanese, Korean)
			seqLen = 3
		case b&0xF8 == 0xF0: // 11110xxx — 4 bytes (emoji, rare CJK)
			seqLen = 4
		default:
			continue // invalid UTF-8 start byte
		}

		// Read remaining bytes of the sequence.
		seq := []byte{b}
		for len(seq) < seqLen {
			n, err := stdinReader.Read(buf)
			if n == 1 {
				seq = append(seq, buf[0])
			}
			if err != nil {
				break
			}
		}

		// Decode the rune.
		r, size := utf8.DecodeRune(seq)
		if r == utf8.RuneError || size == 0 {
			continue
		}

		switch r {
		case '\t': // Tab completion (if tabComplete is provided)
			if tabComplete != nil {
				if !modified && len(input) > 0 {
					clearInputLine(tty, input)
					input = input[:0]
					modified = true
				}
				curInput := string(input)
				completed, _ := tabComplete(curInput)
				if completed != curInput && completed != "" {
					clearInputLine(tty, input)
					input = []rune(completed)
					fmt.Fprintf(tty, "%s", completed)
				}
			}

		case '\r', '\n': // Enter
			fmt.Fprintf(tty, "\r\n")
			return strings.TrimSpace(string(input))

		case 0x1B, 0x03: // Escape or Ctrl+C
			fmt.Fprintf(tty, "\r\n")
			return ""

		case 0x08, 0x7F: // Backspace
			if len(input) > 0 {
				last := input[len(input)-1]
				input = input[:len(input)-1]
				// Width: CJK and other wide chars = 2 columns, others = 1.
				w := runeWidth(last)
				fmt.Fprint(tty, strings.Repeat("\b", w)+strings.Repeat(" ", w)+strings.Repeat("\b", w))
			}
			modified = true

		case 0x15: // Ctrl+U — clear line
			for len(input) > 0 {
				last := input[len(input)-1]
				input = input[:len(input)-1]
				w := runeWidth(last)
				fmt.Fprint(tty, strings.Repeat("\b", w)+strings.Repeat(" ", w)+strings.Repeat("\b", w))
			}
			modified = true

		default:
			if !modified && len(input) > 0 {
				// First printable char: clear pre-filled default, start fresh.
				clearInputLine(tty, input)
				input = input[:0]
				modified = true
			}
			input = append(input, r)
			fmt.Fprintf(tty, "%s", string(r))
		}
	}
}

// runeWidth returns the display width of a rune in terminal columns.
// CJK ideographs and fullwidth forms = 2, others = 1.
func runeWidth(r rune) int {
	if r >= 0x1100 && (r <= 0x9FFF ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0x20000 && r <= 0x2A6DF) ||
		(r >= 0xFF01 && r <= 0xFF60) ||
		(r >= 0xFFE0 && r <= 0xFFE6) ||
		(r >= 0xAC00 && r <= 0xD7AF) ||
		(r >= 0x3040 && r <= 0x309F) ||
		(r >= 0x30A0 && r <= 0x30FF) ||
		(r >= 0x31F0 && r <= 0x31FF)) {
		return 2
	}
	return 1
}

// readRemotePath reads a remote path with Tab completion via SSH.
// Single match: auto-complete fully. Multiple matches: auto-complete to first match.
// Results are cached to avoid repeated SSH calls.
func readRemotePath(tty io.Writer, stdinReader io.Reader, client *ssh.Client, defaultVal string) string {
	input := []rune(defaultVal)
	modified := false

	// Cache for Tab completion results.
	var (
		cacheInput    string
		cacheComplete string
		cacheMatches  []string
	)

	if len(input) > 0 {
		fmt.Fprintf(tty, "%s", string(input))
	}

	buf := make([]byte, 1)
	for {
		// First byte via readSignificantByte so terminal escape sequences
		// are drained (Tab/path input is not polluted by OSC/CSI responses).
		b, ok := readSignificantByte(stdinReader)
		if !ok {
			continue
		}

		// Determine UTF-8 sequence length.
		var seqLen int
		switch {
		case b&0x80 == 0:
			seqLen = 1
		case b&0xE0 == 0xC0:
			seqLen = 2
		case b&0xF0 == 0xE0:
			seqLen = 3
		case b&0xF8 == 0xF0:
			seqLen = 4
		default:
			continue
		}

		seq := []byte{b}
		for len(seq) < seqLen {
			n, err := stdinReader.Read(buf)
			if n == 1 {
				seq = append(seq, buf[0])
			}
			if err != nil {
				break
			}
		}

		r, size := utf8.DecodeRune(seq)
		if r == utf8.RuneError || size == 0 {
			continue
		}

		switch r {
		case '\t': // Tab — remote path completion
			if !modified && len(input) > 0 {
				// First Tab: clear pre-filled default, start fresh.
				clearInputLine(tty, input)
				input = input[:0]
				modified = true
			}
			curInput := string(input)
			var completed string

			// Use cache if input hasn't changed.
			if curInput == cacheInput && cacheComplete != "" {
				completed = cacheComplete
			} else {
				var matches []string
				completed, matches = remoteTabComplete(client, curInput)
				// Cache the result.
				cacheInput = curInput
				cacheComplete = completed
				cacheMatches = matches
				_ = cacheMatches
			}

			if completed == curInput {
				// No advancement — do nothing.
				continue
			}
			// Replace input with completed path.
			clearInputLine(tty, input)
			input = []rune(completed)
			fmt.Fprintf(tty, "%s", completed)

		case '\r', '\n': // Enter
			fmt.Fprintf(tty, "\r\n")
			return strings.TrimSpace(string(input))

		case 0x1B, 0x03: // Escape or Ctrl+C
			fmt.Fprintf(tty, "\r\n")
			return ""

		case 0x08, 0x7F: // Backspace
			if len(input) > 0 {
				last := input[len(input)-1]
				input = input[:len(input)-1]
				w := runeWidth(last)
				fmt.Fprint(tty, strings.Repeat("\b", w)+strings.Repeat(" ", w)+strings.Repeat("\b", w))
			}
			// Invalidate cache on input change.
			cacheInput = ""
			modified = true

		case 0x15: // Ctrl+U
			clearInputLine(tty, input)
			input = nil
			cacheInput = ""
			modified = true

		default:
			if !modified && len(input) > 0 {
				// First printable char: clear pre-filled default, start fresh.
				clearInputLine(tty, input)
				input = input[:0]
				modified = true
			}
			input = append(input, r)
			fmt.Fprintf(tty, "%s", string(r))
			// Invalidate cache on input change.
			cacheInput = ""
		}
	}
}

// clearInputLine clears the current input from the terminal line.
func clearInputLine(tty io.Writer, input []rune) {
	for i := len(input) - 1; i >= 0; i-- {
		w := runeWidth(input[i])
		fmt.Fprint(tty, strings.Repeat("\b", w)+strings.Repeat(" ", w)+strings.Repeat("\b", w))
	}
}

func normalizeRemotePathInput(s string) string {
	s = strings.TrimSpace(s)
	s = trimMatchingQuotes(s)
	s = strings.ReplaceAll(s, `\ `, " ")
	s = strings.ReplaceAll(s, `\\`, `\`)
	return strings.TrimRight(s, "/")
}

// localEntry is one path-completion candidate from the local filesystem.
type localEntry struct {
	name  string
	isDir bool
}

// listLocalEntries lists direct children of dir and marks directories.
func listLocalEntries(dir string) ([]localEntry, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var result []localEntry
	for _, e := range entries {
		name := e.Name()
		if name == "" || name == "." || name == ".." {
			continue
		}
		result = append(result, localEntry{
			name:  name,
			isDir: e.IsDir(),
		})
	}
	return result, nil
}

// localTabComplete performs Tab completion for local paths.
// Returns the completed path and a list of matches.
func localTabComplete(input string) (string, []string) {
	// Split input into directory and partial filename.
	dir := filepath.Dir(input)
	if input == "" {
		dir = "."
	}
	prefix := filepath.Base(input)
	if strings.HasSuffix(input, "/") {
		dir = input
		prefix = ""
	}
	// On Windows, also handle backslash suffix.
	if runtime.GOOS == "windows" && strings.HasSuffix(input, `\`) {
		dir = input
		prefix = ""
	}

	entries, err := listLocalEntries(dir)
	if err != nil {
		return input, nil
	}

	var matches []string
	var firstMatch localEntry
	foundFirst := false
	for _, entry := range entries {
		if entry.name == "" || entry.name == "." || entry.name == ".." {
			continue
		}
		if prefix != "" && !strings.HasPrefix(entry.name, prefix) {
			continue
		}
		matches = append(matches, entry.name)
		if !foundFirst {
			firstMatch = entry
			foundFirst = true
		}
	}

	if !foundFirst {
		return input, nil
	}

	var completed string
	if dir == "." {
		completed = firstMatch.name
	} else {
		completed = filepath.Join(dir, firstMatch.name)
	}
	if firstMatch.isDir {
		completed += string(filepath.Separator)
	}

	return completed, matches
}

func normalizeLocalPathInput(s string) string {
	s = strings.TrimSpace(s)
	s = trimMatchingQuotes(s)
	s = strings.ReplaceAll(s, `\ `, " ")
	if runtime.GOOS != "windows" {
		s = strings.ReplaceAll(s, `\\`, `\`)
	}
	if strings.HasPrefix(s, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			s = filepath.Join(home, s[2:])
		}
	}
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(s, `~\`) {
			if home, err := os.UserHomeDir(); err == nil {
				s = filepath.Join(home, s[2:])
			}
		}
		return strings.TrimRight(s, `\/`)
	}
	return strings.TrimRight(s, "/")
}

func trimMatchingQuotes(s string) string {
	if len(s) < 2 {
		return s
	}
	if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
		return s[1 : len(s)-1]
	}
	return s
}

// remoteEntry is one path-completion candidate from the remote filesystem.
type remoteEntry struct {
	name  string
	isDir bool
}

// remoteTabComplete performs Tab completion for remote paths via SSH.
// Returns the completed path and a list of suggestions.
func remoteTabComplete(client *ssh.Client, input string) (string, []string) {
	if client == nil {
		return input, nil
	}

	// Split input into directory and partial filename.
	dir := filepath.Dir(input)
	if input == "" {
		dir = "."
	}
	prefix := filepath.Base(input)
	if strings.HasSuffix(input, "/") {
		dir = input
		prefix = ""
	}

	entries, err := listRemoteEntries(client, dir)
	if err != nil {
		return input, nil
	}

	var matches []string
	var firstMatch remoteEntry
	foundFirst := false
	for _, entry := range entries {
		if entry.name == "" || entry.name == "." || entry.name == ".." {
			continue
		}
		if prefix != "" && !strings.HasPrefix(entry.name, prefix) {
			continue
		}
		matches = append(matches, entry.name)
		if !foundFirst {
			firstMatch = entry
			foundFirst = true
		}
	}

	if !foundFirst {
		return input, nil
	}

	var completed string
	if dir == "." {
		completed = firstMatch.name
	} else {
		completed = filepath.Join(dir, firstMatch.name)
	}
	if firstMatch.isDir {
		completed += "/"
	}

	return completed, matches
}

// listRemoteEntries lists direct children of dir and marks which ones are directories.
func listRemoteEntries(client *ssh.Client, dir string) ([]remoteEntry, error) {
	if client == nil {
		return nil, nil
	}
	// Print one entry per line as: <name><TAB><type>, where type is d or f.
	// Using a single find avoids a second round-trip just to test the first match.
	script := "find " + shQuote(dir) + " -mindepth 1 -maxdepth 1 \\( -type d -printf '%f\\td\\n' -o -printf '%f\\tf\\n' \\) 2>/dev/null"
	out, err := remoteRunSh(client, script, 5*time.Second)
	if err != nil {
		return nil, err
	}
	return parseRemoteEntries(out), nil
}

func parseRemoteEntries(out string) []remoteEntry {
	if strings.TrimSpace(out) == "" {
		return nil
	}
	lines := strings.Split(out, "\n")
	entries := make([]remoteEntry, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		entry := remoteEntry{name: parts[0]}
		if len(parts) == 2 && parts[1] == "d" {
			entry.isDir = true
		}
		entries = append(entries, entry)
	}
	return entries
}

// uniqueLocalPath returns a path that doesn't exist yet by appending a number.
// e.g., file.txt -> file(1).txt -> file(2).txt
func uniqueLocalPath(path string) string {
	if _, err := os.Stat(path); err != nil {
		return path // doesn't exist, use as-is
	}

	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(filepath.Base(path), ext)

	for i := 1; ; i++ {
		newPath := filepath.Join(dir, fmt.Sprintf("%s(%d)%s", base, i, ext))
		if _, err := os.Stat(newPath); err != nil {
			return newPath
		}
	}
}

// showUploadInputs draws the remote dir and local path fields.
func showUploadInputs(tty io.Writer, stdinReader io.Reader, client *ssh.Client, defaultRemoteDir string, dirDetected bool, loc locale) (string, string, bool) {
	remoteLabel := localeT(loc, "Remote", "远程")
	localLabel := localeT(loc, "Local ", "本地 ")

	// Show detection status.
	if dirDetected {
		detectedTag := localeT(loc, "(detected)", "(已检测)")
		fmt.Fprintf(tty, "  %s: \x1b[32m%s\x1b[0m\r\n", remoteLabel, detectedTag)
		fmt.Fprintf(tty, "  \x1b[2m%s\x1b[0m", localeT(loc, "Press Enter to accept, or type a new path (Tab to complete): ", "按 Enter 接受，或输入新路径（Tab 补全）："))
	} else {
		fallbackTag := localeT(loc, "(home, type to override)", "(home 目录，可输入覆盖)")
		fmt.Fprintf(tty, "  %s: \x1b[33m%s\x1b[0m \x1b[2m%s\x1b[0m ", remoteLabel, fallbackTag, localeT(loc, "(Tab to complete)", "(Tab 补全)"))
	}

	remoteDir := normalizeRemotePathInput(readRemotePath(tty, stdinReader, client, defaultRemoteDir))
	if remoteDir == "" {
		cancelMsg := localeT(loc, "cancelled", "已取消")
		fmt.Fprintf(tty, "\x1b[2m%s\x1b[0m\r\n", cancelMsg)
		return "", "", false
	}

	fmt.Fprintf(tty, "  %s: \x1b[2m%s\x1b[0m ", localLabel, localeT(loc, "(Tab to complete)", "(Tab 补全)"))
	localPath := normalizeLocalPathInput(readInputLine(tty, stdinReader, "", localTabComplete))
	if localPath == "" {
		cancelMsg := localeT(loc, "cancelled (empty path)", "已取消（路径为空）")
		fmt.Fprintf(tty, "\x1b[2m%s\x1b[0m\r\n", cancelMsg)
		return "", "", false
	}

	return remoteDir, localPath, true
}

// openDialogTTY opens the controlling terminal for dialog output, falling
// back to stdout if /dev/tty (or CON) is unavailable. Returns a writer and a
// closer that must be called when done. The fallback ensures the menu is
// still shown when there is no controlling terminal.
func openDialogTTY() (io.Writer, func()) {
	if f, err := openLocalTTY(); err == nil {
		return f, func() { f.Close() }
	}
	return os.Stdout, func() {}
}

// uploadWithDialog runs the full interactive flow: menu → action.
// The tty writer is provided by the caller (opened up front for instant
// feedback) so the dialog does not depend on a second openLocalTTY call.
func uploadWithDialog(stdinReader io.Reader, cwdCache *remoteCwdCache, client *ssh.Client, info sshConnInfo, loc locale, tty io.Writer) {
	action, ok := showActionMenu(tty, stdinReader, loc)
	debugf("dialog: menu action=%v ok=%v", action, ok)
	if !ok {
		return
	}

	remoteDir := "~"
	dirDetected := false
	needRemoteDir := action == uploadActionCopy || action == uploadActionUpload || action == uploadActionDownload
	if needRemoteDir {
		remoteDir, dirDetected = queryRemotePwd(cwdCache, client)
		debugf("dialog: cwd=%q detected=%v", remoteDir, dirDetected)
	}

	switch action {
	case uploadActionCopy:
		handleCopyAction(tty, info, remoteDir, loc)
	case uploadActionUpload:
		handleUploadAction(tty, stdinReader, client, info, remoteDir, dirDetected, loc)
	case uploadActionDownload:
		handleDownloadAction(tty, stdinReader, client, info, remoteDir, loc)
	}
}

// handleCopyAction generates the scp command and copies it to clipboard.
func handleCopyAction(tty io.Writer, info sshConnInfo, remoteDir string, loc locale) {
	cmd := buildUploadCmd(info, remoteDir)
	usedOSC52, copyErr := writeTextToClipboard(cmd)

	if copyErr != nil {
		failLabel := localeT(loc, "clipboard copy failed", "剪贴板复制失败")
		fmt.Fprintf(tty, "\x1b[31m✗ %s\x1b[0m\r\n", failLabel)
		fmt.Fprintf(tty, "\x1b[2m  %s\x1b[0m\r\n", cmd)
		printEndBanner(tty, loc)
		return
	}

	label := localeT(loc, "copied to clipboard", "已复制到剪贴板")
	if usedOSC52 {
		label = localeT(loc, "copied (terminal clipboard)", "已复制（终端剪贴板）")
	}
	fmt.Fprintf(tty, "\x1b[32m✓ %s\x1b[0m\r\n", label)
	fmt.Fprintf(tty, "\x1b[2m  %s\x1b[0m\r\n", cmd)
	printEndBanner(tty, loc)
}

// handleUploadAction prompts for remote dir + local path and uploads.
func handleUploadAction(tty io.Writer, stdinReader io.Reader, client *ssh.Client, info sshConnInfo, defaultRemoteDir string, dirDetected bool, loc locale) {
	remoteDir, localPath, ok := showUploadInputs(tty, stdinReader, client, defaultRemoteDir, dirDetected, loc)
	if !ok {
		printEndBanner(tty, loc)
		return
	}

	remoteDir = normalizeRemotePathInput(remoteDir)
	localPath = normalizeLocalPathInput(localPath)

	// Check local path exists.
	fi, err := os.Stat(localPath)
	if err != nil {
		errLabel := localeT(loc, "local path not found", "本地路径未找到")
		fmt.Fprintf(tty, "\x1b[31m✗ %s: %s\x1b[0m\r\n", errLabel, localPath)
		printEndBanner(tty, loc)
		return
	}

	// Set up cancel listener.
	ctx, cancel := context.WithCancel(context.Background())
	stopCancel := startCancelListener(stdinReader, cancel)
	defer func() {
		cancel()
		stopCancel() // close pipe, wait for copy goroutine to exit
	}()

	if fi.IsDir() {
		handleDirUpload(tty, stdinReader, client, info, localPath, remoteDir, ctx, loc)
	} else {
		handleFileUpload(tty, stdinReader, client, info, localPath, remoteDir, fi.Size(), ctx, loc)
	}

	printEndBanner(tty, loc)
}

// confirmTransfer shows a transfer summary and asks for Y/n confirmation.
// Returns true if confirmed, false if cancelled.
func confirmTransfer(tty io.Writer, stdinReader io.Reader, summary string, loc locale) bool {
	confirmHint := localeT(loc, "Continue? [Y/n]: ", "继续? [Y/n]: ")
	cancelMsg := localeT(loc, "cancelled", "已取消")

	fmt.Fprintf(tty, "%s", summary)
	fmt.Fprintf(tty, "  %s", confirmHint)

	for {
		b, ok := readSignificantByte(stdinReader)
		if !ok {
			continue
		}
		switch b {
		case 'y', 'Y', '\r', '\n': // confirm
			fmt.Fprintf(tty, "y\r\n")
			return true
		case 'n', 'N', 0x1B, 0x03: // cancel (standalone Esc; OSC/CSI drained)
			fmt.Fprintf(tty, "n\r\n")
			fmt.Fprintf(tty, "\x1b[2m%s\x1b[0m\r\n", cancelMsg)
			return false
		}
	}
}

// handleFileUpload uploads a single file with progress.
func handleFileUpload(tty io.Writer, stdinReader io.Reader, client *ssh.Client, info sshConnInfo, localPath, remoteDir string, size int64, ctx context.Context, loc locale) {
	// Show confirmation.
	uploadLabel := localeT(loc, "Upload", "上传")
	summary := fmt.Sprintf("\r\n  %s: %s (%s) -> %s:%s/\r\n",
		uploadLabel, filepath.Base(localPath), formatFileSize(size), info.Host, remoteDir)
	if !confirmTransfer(tty, stdinReader, summary, loc) {
		return
	}

	progressLabel := localeT(loc, "uploading", "正在上传")
	sizeLabel := localeT(loc, "size", "大小")
	cancelHint := localeT(loc, "Esc/Ctrl+C to cancel", "Esc/Ctrl+C 取消")
	fmt.Fprintf(tty, "\x1b[36m⋯ %s %s (%s) → %s:%s/ — %s\x1b[0m\r\n",
		progressLabel, filepath.Base(localPath), formatFileSize(size), info.Host, remoteDir, cancelHint)

	progress := &ttyProgress{tty: tty, total: size, loc: loc}
	stopTicker := progress.startLiveRenderer()
	uploadErr := scpUploadFile(ctx, client, remoteDir, localPath, progress)
	stopTicker()
	if uploadErr == nil {
		progress.finish()
	}
	progress.render()
	fmt.Fprintf(tty, "\r\n")

	if uploadErr != nil {
		if ctx.Err() != nil {
			cancelLabel := localeT(loc, "upload cancelled", "上传已取消")
			fmt.Fprintf(tty, "\x1b[33m✗ %s\x1b[0m\r\n", cancelLabel)
		} else {
			errLabel := localeT(loc, "upload failed", "上传失败")
			fmt.Fprintf(tty, "\x1b[31m✗ %s: %s\x1b[0m\r\n", errLabel, uploadErr.Error())
			fallbackLabel := localeT(loc, "scp command copied to clipboard as fallback", "已将 scp 命令复制到剪贴板作为备选")
			cmd := buildScpCommand(info, remoteDir, localPath)
			_, _ = writeTextToClipboard(cmd)
			fmt.Fprintf(tty, "\x1b[2m  %s\x1b[0m\r\n", fallbackLabel)
			fmt.Fprintf(tty, "\x1b[2m  %s\x1b[0m\r\n", cmd)
		}
		return
	}

	successLabel := localeT(loc, "uploaded", "已上传")
	fmt.Fprintf(tty, "\x1b[32m✓ %s %s → %s:%s/\x1b[0m\r\n",
		successLabel, filepath.Base(localPath), info.Host, remoteDir)
	fmt.Fprintf(tty, "\x1b[2m  %s: %s\x1b[0m\r\n", sizeLabel, formatFileSize(size))
}

// handleDirUpload uploads a directory recursively with file count progress.
func handleDirUpload(tty io.Writer, stdinReader io.Reader, client *ssh.Client, info sshConnInfo, localDir, remoteDir string, ctx context.Context, loc locale) {
	// Pre-scan to count files and total size.
	scanLabel := localeT(loc, "scanning directory...", "扫描目录中...")
	fmt.Fprintf(tty, "\x1b[36m⋯ %s\x1b[0m\r", scanLabel)

	totalFiles, totalSize, err := countFiles(localDir)
	if err != nil {
		errLabel := localeT(loc, "failed to scan directory", "扫描目录失败")
		fmt.Fprintf(tty, "\x1b[31m✗ %s: %s\x1b[0m\r\n", errLabel, err.Error())
		return
	}

	if totalFiles == 0 {
		emptyLabel := localeT(loc, "directory is empty", "目录为空")
		fmt.Fprintf(tty, "\x1b[33m! %s\x1b[0m\r\n", emptyLabel)
		return
	}

	dirName := filepath.Base(localDir)
	fileLabel := localeT(loc, "files", "个文件")

	// Show confirmation.
	uploadLabel := localeT(loc, "Upload directory", "上传目录")
	summary := fmt.Sprintf("\r\n  %s: %s/ (%d %s, %s) → %s:%s/\r\n",
		uploadLabel, dirName, totalFiles, fileLabel, formatFileSize(totalSize), info.Host, remoteDir)
	if !confirmTransfer(tty, stdinReader, summary, loc) {
		return
	}

	upLabel := localeT(loc, "uploading directory", "正在上传目录")
	cancelHint := localeT(loc, "Esc/Ctrl+C to cancel", "Esc/Ctrl+C 取消")
	fmt.Fprintf(tty, "\x1b[36m⋯ %s %s/ (%d %s, %s) — %s\x1b[0m\r\n",
		upLabel, dirName, totalFiles, fileLabel, formatFileSize(totalSize), cancelHint)

	// Start recursive upload.
	progress := &dirProgress{tty: tty, total: totalFiles, totalBytes: totalSize, countKnown: true, loc: loc}
	stopTicker := progress.startLiveRenderer()
	uploadErr := scpUploadDir(ctx, client, remoteDir, localDir, progress)
	stopTicker()
	if uploadErr == nil {
		progress.finish()
	}
	progress.render()
	fmt.Fprintf(tty, "\r\n")

	if uploadErr != nil {
		if ctx.Err() != nil {
			cancelLabel := localeT(loc, "upload cancelled", "上传已取消")
			fmt.Fprintf(tty, "\x1b[33m✗ %s (%d/%d)\x1b[0m\r\n",
				cancelLabel, progress.current, totalFiles)
		} else {
			errLabel := localeT(loc, "upload failed", "上传失败")
			fmt.Fprintf(tty, "\x1b[31m✗ %s: %s\x1b[0m\r\n", errLabel, uploadErr.Error())
		}
		return
	}

	successLabel := localeT(loc, "uploaded directory", "已上传目录")
	fmt.Fprintf(tty, "\x1b[32m✓ %s %s/ (%d %s, %s) → %s:%s/\x1b[0m\r\n",
		successLabel, dirName, totalFiles, fileLabel, formatFileSize(totalSize), info.Host, remoteDir)
}

// printEndBanner draws a clear end-of-interaction separator.
func printEndBanner(tty io.Writer, loc locale) {
	endLabel := localeT(loc, "─── TTM Transfer Done ───", "─── TTM 传输结束 ───")
	fmt.Fprintf(tty, "\r\n\x1b[1;36m%s\x1b[0m\r\n", endLabel)
}

// ── Cancel support ────────────────────────────────────────────────────────────

// startCancelListener watches stdin during an upload/download for cancel
// keys (standalone Esc or Ctrl+C). It drains terminal escape sequences
// (OSC/CSI responses) so they neither falsely trigger cancel nor leak to the
// remote shell afterward.
//
// Unlike a plain blocking Read, it polls stdin in short slices so the stop
// function can terminate it promptly — the previous pipe-based version could
// block forever on a Read with no input, deadlocking stop().
func startCancelListener(stdinReader io.Reader, cancel context.CancelFunc) (stop func()) {
	stopCh := make(chan struct{})
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			b, ok := readByteOrStop(stdinReader, stopCh)
			if !ok {
				return // stop closed or reader ended
			}
			if b == 0x1b {
				// Disambiguate standalone Esc (cancel) from an escape sequence.
				next, ok := peekByte(stdinReader, escPeekTimeout, stdinReadable)
				if !ok {
					debugf("cancel: standalone Esc -> cancel")
					cancel()
					return
				}
				switch next {
				case ']':
					drainOSC(stdinReader, stdinReadable)
				case '[':
					// Parse kitty-keyboard CSI-u in case the local kitty disable
					// didn't take effect: Esc=ESC[27u, Ctrl+C=ESC[3;5u.
					seq := readCSIReturning(stdinReader, stdinReadable)
					if kb, kok := parseKittyKey(seq); kok && (kb == 0x1b || kb == 0x03) {
						debugf("cancel: kitty-encoded key 0x%02x -> cancel", kb)
						cancel()
						return
					}
				case 'O':
					drainN(stdinReader, 1, stdinReadable)
				}
				continue
			}
			if b == 0x03 { // Ctrl+C
				debugf("cancel: Ctrl+C -> cancel")
				cancel()
				return
			}
			// Other key bytes (typing) are ignored during transfer.
		}
	}()

	return func() {
		close(stopCh)
		<-done
	}
}

// ── Progress tracking ─────────────────────────────────────────────────────────

// ttyProgress tracks upload progress for a single file.
type ttyProgress struct {
	tty         io.Writer
	total       int64
	written     int64
	lastUpd     time.Time
	lastWritten int64   // bytes at last render, for speed calculation
	speed       float64 // smoothed speed (bytes/sec)
	startTime   time.Time
	done        bool
	loc         locale
	mu          sync.Mutex // guards written/render state (Write vs live ticker)
}

func (p *ttyProgress) Write(data []byte) (int, error) {
	n := len(data)
	p.mu.Lock()
	p.written += int64(n)

	if p.startTime.IsZero() {
		p.startTime = time.Now()
	}

	now := time.Now()
	if now.Sub(p.lastUpd) < 100*time.Millisecond && p.written < p.total {
		p.mu.Unlock()
		return n, nil
	}

	// Calculate instantaneous speed from bytes since last render.
	if !p.lastUpd.IsZero() {
		elapsed := now.Sub(p.lastUpd).Seconds()
		if elapsed > 0 {
			instantSpeed := float64(p.written-p.lastWritten) / elapsed
			// Exponential moving average for smoothness.
			if p.speed == 0 {
				p.speed = instantSpeed
			} else {
				p.speed = 0.3*instantSpeed + 0.7*p.speed
			}
		}
	}
	p.lastWritten = p.written
	p.lastUpd = now
	p.renderLocked()
	p.mu.Unlock()
	return n, nil
}

// render redraws the progress bar (thread-safe).
func (p *ttyProgress) render() {
	p.mu.Lock()
	p.renderLocked()
	p.mu.Unlock()
}

func (p *ttyProgress) renderLocked() {
	shownWritten := p.written
	pct := 0
	if p.total > 0 {
		pct = int(shownWritten * 100 / p.total)
	} else if p.done {
		pct = 100
	}
	if p.done && p.total > 0 && shownWritten < p.total {
		shownWritten = p.total
		pct = 100
	}
	if pct > 100 {
		pct = 100
	}

	barWidth := 20
	filled := pct * barWidth / 100
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	speedLabel := localeT(p.loc, "speed", "速度")
	etaLabel := localeT(p.loc, "eta", "剩余")

	fmt.Fprintf(p.tty, "\r\x1b[K  %s %3d%%  %s/%s",
		bar, pct, formatFileSize(shownWritten), formatFileSize(p.total))

	if !p.done && p.total > 0 && p.speed > 0 && p.written < p.total {
		remaining := float64(p.total-p.written) / p.speed
		fmt.Fprintf(p.tty, "  %s: %s/s  %s: %s",
			speedLabel, formatFileSize(int64(p.speed)),
			etaLabel, formatDuration(time.Duration(remaining*float64(time.Second))))
	}
}

// startLiveRenderer spawns a goroutine that redraws the bar every 150ms so the
// progress stays live even when Write is throttled or blocked on SSH flow
// control (previously the bar could freeze at 0% when stdin.Write stalled).
// Returns a stop func that MUST be called when the transfer ends.
func (p *ttyProgress) finish() {
	p.mu.Lock()
	p.done = true
	if p.total > 0 && p.written < p.total {
		p.written = p.total
	}
	p.mu.Unlock()
}

func (p *ttyProgress) startLiveRenderer() (stop func()) {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(150 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				p.render()
			}
		}
	}()
	return func() { close(done) }
}

// dirProgress tracks upload/download progress for a directory.
//
// It tracks BOTH file count (current/total) and bytes (written/totalBytes) so
// the live ticker can show real-time progress during large-file transfers —
// previously the bar only advanced when a whole file finished, so it appeared
// frozen while a big file streamed in (io.CopyN blocked between updates).
type dirProgress struct {
	tty        io.Writer
	total      int   // total file count (may be 0 if count failed)
	totalBytes int64 // total size of all files (may be 0 if count failed)
	current    int   // files completed
	written    int64 // bytes received so far (across all files)
	lastFile   string
	countKnown bool
	done       bool
	loc        locale
	mu         sync.Mutex
}

// addBytes advances the byte counter (called as file data streams in).
func (p *dirProgress) addBytes(n int) {
	p.mu.Lock()
	p.written += int64(n)
	p.mu.Unlock()
}

func (p *dirProgress) update(filename string) {
	p.mu.Lock()
	p.current++
	p.lastFile = filename
	p.renderLocked()
	p.mu.Unlock()
}

// render redraws the directory progress (thread-safe).
func (p *dirProgress) render() {
	p.mu.Lock()
	p.renderLocked()
	p.mu.Unlock()
}

func (p *dirProgress) renderLocked() {
	barWidth := 20
	var filled int
	// Prefer byte-based fill when a total size is known (smooth during large
	// files); fall back to file-count fill; else empty bar until completion.
	if p.totalBytes > 0 {
		filled = int(p.written * int64(barWidth) / p.totalBytes)
	} else if p.total > 0 {
		filled = p.current * barWidth / p.total
	}
	if p.done && filled == 0 {
		filled = barWidth
	}
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)

	filename := p.lastFile
	// Truncate long filenames by rune (not byte) to avoid breaking UTF-8.
	runes := []rune(filename)
	if len(runes) > 30 {
		filename = "..." + string(runes[len(runes)-27:])
	}

	fileCountLabel := localeT(p.loc, "files", "个文件")

	// Show byte progress when known, plus file count; otherwise show either a
	// known file count or only the completed count when the total is unknown.
	if p.totalBytes > 0 {
		fmt.Fprintf(p.tty, "\r\x1b[K  %s %s/%s  %d/%d  %s",
			bar, formatFileSize(p.written), formatFileSize(p.totalBytes), p.current, p.total, filename)
	} else if p.total > 0 {
		fmt.Fprintf(p.tty, "\r\x1b[K  %s %d/%d  %s",
			bar, p.current, p.total, filename)
	} else if p.countKnown || p.current > 0 {
		fmt.Fprintf(p.tty, "\r\x1b[K  %s %d %s  %s",
			bar, p.current, fileCountLabel, filename)
	} else {
		fmt.Fprintf(p.tty, "\r\x1b[K  %s %s",
			bar, filename)
	}
}

// startLiveRenderer spawns a goroutine that redraws every 150ms so directory
// progress stays live between file completions. Returns a stop func.
func (p *dirProgress) finish() {
	p.mu.Lock()
	p.done = true
	if p.totalBytes > 0 && p.written < p.totalBytes {
		p.written = p.totalBytes
	}
	if p.total > 0 && p.current < p.total {
		p.current = p.total
	}
	p.mu.Unlock()
}

func (p *dirProgress) startLiveRenderer() (stop func()) {
	done := make(chan struct{})
	go func() {
		t := time.NewTicker(150 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				p.render()
			}
		}
	}()
	return func() { close(done) }
}

func formatDuration(d time.Duration) string {
	if d < time.Second {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

// ── File counting ─────────────────────────────────────────────────────────────

// countFiles walks a directory and returns the total number of files and total size.
// Skips files that cannot be accessed (broken symlinks, permission errors, etc.).
func countFiles(dir string) (int, int64, error) {
	var count int
	var totalSize int64
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip inaccessible files
		}
		if !info.IsDir() && info.Mode().IsRegular() {
			count++
			totalSize += info.Size()
		}
		return nil
	})
	return count, totalSize, err
}

// ── SCP protocol ─────────────────────────────────────────────────────────────

// scpUploadFile uploads a single file using the SCP protocol over an existing
// SSH connection. Supports context cancellation — closes session to abort.
func scpUploadFile(ctx context.Context, client *ssh.Client, remoteDir, localPath string, progress io.Writer) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	f, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("open local file: %w", err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat local file: %w", err)
	}

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("open ssh session: %w", err)
	}
	defer session.Close()

	// Close session on context cancellation to abort io.Copy immediately.
	go func() {
		<-ctx.Done()
		session.Close()
	}()

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("open stdin pipe: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open stdout pipe: %w", err)
	}

	filename := filepath.Base(localPath)
	debugf("scp-upload: start path=%s size=%d remote=%s", localPath, stat.Size(), remoteDir)
	if err := session.Start(fmt.Sprintf("scp -t %q", remoteDir)); err != nil {
		debugf("scp-upload: Start err=%v", err)
		return fmt.Errorf("start remote scp: %w", err)
	}

	readResp := func() error {
		var resp [1]byte
		if _, err := stdout.Read(resp[:]); err != nil {
			return fmt.Errorf("read scp response: %w", err)
		}
		if resp[0] != 0 {
			return fmt.Errorf("scp remote error: code 0x%02x", resp[0])
		}
		return nil
	}

	if err := readResp(); err != nil {
		debugf("scp-upload: initial ack err=%v", err)
		return err
	}

	mode := fmt.Sprintf("0%o", stat.Mode().Perm())
	header := fmt.Sprintf("C%s %d %s\n", mode, stat.Size(), filename)
	if _, err := stdin.Write([]byte(header)); err != nil {
		debugf("scp-upload: header write err=%v", err)
		return fmt.Errorf("send scp header: %w", err)
	}

	if err := readResp(); err != nil {
		debugf("scp-upload: header ack err=%v", err)
		return err
	}
	debugf("scp-upload: header acked, sending data")

	var writer io.Writer = stdin
	if progress != nil {
		writer = io.MultiWriter(stdin, progress)
	}
	if _, err := io.Copy(writer, f); err != nil {
		debugf("scp-upload: data copy err=%v", err)
		return fmt.Errorf("send file data: %w", err)
	}
	debugf("scp-upload: data sent, sending end marker")

	if _, err := stdin.Write([]byte{0}); err != nil {
		debugf("scp-upload: end marker err=%v", err)
		return fmt.Errorf("send scp end marker: %w", err)
	}

	if err := readResp(); err != nil {
		debugf("scp-upload: final ack err=%v", err)
		return err
	}
	debugf("scp-upload: done")

	_ = stdin.Close()
	return session.Wait()
}

// scpUploadDir uploads a directory recursively using the SCP protocol.
// Uses a recursive approach to properly handle the D/E directory markers.
func scpUploadDir(ctx context.Context, client *ssh.Client, remoteParent, localDir string, progress *dirProgress) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("open ssh session: %w", err)
	}
	defer session.Close()

	// Close session on context cancellation to abort immediately.
	go func() {
		<-ctx.Done()
		session.Close()
	}()

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("open stdin pipe: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open stdout pipe: %w", err)
	}

	// scp -r -t = recursive receive mode.
	if err := session.Start(fmt.Sprintf("scp -r -t %q", remoteParent)); err != nil {
		return fmt.Errorf("start remote scp: %w", err)
	}

	readResp := func() error {
		var resp [1]byte
		if _, err := stdout.Read(resp[:]); err != nil {
			return fmt.Errorf("read scp response: %w", err)
		}
		if resp[0] != 0 {
			return fmt.Errorf("scp remote error: code 0x%02x", resp[0])
		}
		return nil
	}

	if err := readResp(); err != nil {
		return err
	}

	// Recursively send the directory tree.
	if err := sendDirRecursive(stdin, readResp, localDir, localDir, progress, ctx); err != nil {
		return err
	}

	_ = stdin.Close()
	return session.Wait()
}

// sendDirRecursive recursively sends a directory and its contents via SCP protocol.
func sendDirRecursive(stdin io.Writer, readResp func() error, rootDir, localDir string, progress *dirProgress, ctx context.Context) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	info, err := os.Stat(localDir)
	if err != nil {
		return err
	}

	// Send directory entry.
	if err := sendDirEntry(stdin, info.Name(), info.Mode().Perm()); err != nil {
		return err
	}
	if err := readResp(); err != nil {
		return err
	}

	// Read directory contents.
	entries, err := os.ReadDir(localDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		fullPath := filepath.Join(localDir, entry.Name())

		if entry.IsDir() {
			// Recurse into subdirectory.
			if err := sendDirRecursive(stdin, readResp, rootDir, fullPath, progress, ctx); err != nil {
				return err
			}
			continue
		}

		// Skip non-regular files (symlinks, etc.).
		fi, err := entry.Info()
		if err != nil {
			continue // skip inaccessible files
		}
		if !fi.Mode().IsRegular() {
			continue
		}

		// Send regular file.
		if progress != nil {
			relPath, _ := filepath.Rel(rootDir, fullPath)
			progress.update(relPath)
		}
		if err := sendFileEntry(stdin, fullPath, fi, nil, progress.addBytes); err != nil {
			return err
		}
		if err := readResp(); err != nil {
			return err
		}
	}

	// End of directory.
	return sendDirEnd(stdin)
}

// sendDirEntry sends an SCP directory entry: D<mode> 0 <name>\n
func sendDirEntry(stdin io.Writer, name string, mode os.FileMode) error {
	cmd := fmt.Sprintf("D0%o 0 %s\n", mode.Perm(), name)
	_, err := stdin.Write([]byte(cmd))
	return err
}

// sendDirEnd sends an SCP end-of-directory marker: E\n
func sendDirEnd(stdin io.Writer) error {
	_, err := stdin.Write([]byte("E\n"))
	return err
}

// sendFileEntry sends a complete SCP file entry (header + data + end marker).
func sendFileEntry(stdin io.Writer, localPath string, info os.FileInfo, progress io.Writer, onBytes func(int)) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	// Send file header: C<mode> <size> <name>\n
	mode := fmt.Sprintf("0%o", info.Mode().Perm())
	header := fmt.Sprintf("C%s %d %s\n", mode, info.Size(), info.Name())
	if _, err := stdin.Write([]byte(header)); err != nil {
		return err
	}

	// Send file data.
	var writer io.Writer = stdin
	if progress != nil {
		writer = io.MultiWriter(stdin, progress)
	}
	if onBytes != nil {
		writer = &countingWriter{w: writer, onWrite: onBytes}
	}
	if _, err := io.Copy(writer, f); err != nil {
		return err
	}

	// Send end marker.
	if _, err := stdin.Write([]byte{0}); err != nil {
		return err
	}

	return nil
}

// buildScpCommand generates the scp command string for display/copy fallback.
func buildScpCommand(info sshConnInfo, remoteDir, localPath string) string {
	port := info.Port
	if port <= 0 {
		port = 22
	}
	user := info.User
	if user == "" {
		user = "root"
	}
	return fmt.Sprintf("scp -P %d %s %s@%s:%s/",
		port, localPath, user, info.Host, remoteDir)
}

// formatFileSize returns a human-readable file size string.
func formatFileSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// ── Download support ──────────────────────────────────────────────────────────

// getDownloadsDir returns the user's Downloads directory.
func getDownloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	// Cross-platform Downloads directory.
	downloads := filepath.Join(home, "Downloads")
	if _, err := os.Stat(downloads); err == nil {
		return downloads
	}
	// Fallback: home directory.
	return home
}

// handleDownloadAction prompts for remote path and downloads to local Downloads dir.
func handleDownloadAction(tty io.Writer, stdinReader io.Reader, client *ssh.Client, info sshConnInfo, remoteDir string, loc locale) {
	// Ask for remote path with Tab completion.
	remoteLabel := localeT(loc, "Remote path (Tab to complete)", "远程路径 (Tab 补全)")
	fmt.Fprintf(tty, "\r\n  %s: ", remoteLabel)

	remotePath := normalizeRemotePathInput(readRemotePath(tty, stdinReader, client, remoteDir+"/"))
	if remotePath == "" {
		cancelMsg := localeT(loc, "cancelled", "已取消")
		fmt.Fprintf(tty, "\x1b[2m%s\x1b[0m\r\n", cancelMsg)
		printEndBanner(tty, loc)
		return
	}

	// Ask for local save directory (default: Downloads).
	defaultLocal := getDownloadsDir()
	localLabel := localeT(loc, "Local save dir", "本地保存目录")
	fmt.Fprintf(tty, "  %s: ", localLabel)
	localDir := normalizeLocalPathInput(readInputLine(tty, stdinReader, defaultLocal, localTabComplete))
	if localDir == "" {
		localDir = defaultLocal
	}

	// Set up cancel listener.
	ctx, cancel := context.WithCancel(context.Background())
	stopCancel := startCancelListener(stdinReader, cancel)
	defer func() {
		cancel()
		stopCancel()
	}()

	// Try to stat the remote path to determine if it's a file or directory.
	isDir, size, err := remoteStat(client, remotePath)
	if err != nil {
		errLabel := localeT(loc, "remote path not found", "远程路径未找到")
		fmt.Fprintf(tty, "\x1b[31m✗ %s: %s\x1b[0m\r\n", errLabel, remotePath)
		printEndBanner(tty, loc)
		return
	}

	if isDir {
		handleDirDownload(tty, stdinReader, client, remotePath, localDir, ctx, loc)
	} else {
		handleFileDownload(tty, stdinReader, client, remotePath, localDir, size, ctx, loc)
	}

	printEndBanner(tty, loc)
}

// remoteStat checks if a remote path is a file or directory.
//
// Runs as a single remote probe to avoid the previous two-round-trip flow
// (`test -d`, then `test -f + stat`), which was noticeable on higher-latency
// SSH links and startup-heavy shells.
func remoteStat(client *ssh.Client, remotePath string) (isDir bool, size int64, err error) {
	script := "if test -d " + shQuote(remotePath) + "; then echo d; " +
		"elif test -f " + shQuote(remotePath) + "; then printf 'f '; stat -c %s " + shQuote(remotePath) + "; " +
		"fi"
	out, err := remoteRunSh(client, script, 5*time.Second)
	debugf("remoteStat: path=%q -> out=%q err=%v", remotePath, out, err)
	if err != nil {
		return false, 0, err
	}
	return parseRemoteStatOutput(out, remotePath)
}

func parseRemoteStatOutput(out, remotePath string) (isDir bool, size int64, err error) {
	if out == "d" {
		return true, 0, nil
	}
	if strings.HasPrefix(out, "f ") {
		var s int64
		if _, scanErr := fmt.Sscanf(strings.TrimPrefix(out, "f "), "%d", &s); scanErr != nil {
			return false, 0, fmt.Errorf("parse remote file size: %w", scanErr)
		}
		return false, s, nil
	}
	return false, 0, fmt.Errorf("not a regular file or directory: %s", remotePath)
}

// handleFileDownload downloads a single remote file to local Downloads dir.
func handleFileDownload(tty io.Writer, stdinReader io.Reader, client *ssh.Client, remotePath, localDir string, size int64, ctx context.Context, loc locale) {
	filename := filepath.Base(remotePath)
	localPath := uniqueLocalPath(filepath.Join(localDir, filename))

	// Show confirmation.
	downloadLabel := localeT(loc, "Download", "下载")
	summary := fmt.Sprintf("\r\n  %s: %s (%s) → %s/\r\n",
		downloadLabel, filename, formatFileSize(size), localDir)
	if !confirmTransfer(tty, stdinReader, summary, loc) {
		return
	}

	downLabel := localeT(loc, "downloading", "正在下载")
	cancelHint := localeT(loc, "Esc/Ctrl+C to cancel", "Esc/Ctrl+C 取消")
	fmt.Fprintf(tty, "\x1b[36m⋯ %s %s → %s/ — %s\x1b[0m\r\n",
		downLabel, filename, localDir, cancelHint)

	progress := &ttyProgress{tty: tty, total: size, loc: loc}
	stopTicker := progress.startLiveRenderer()
	err := scpDownloadFile(ctx, client, remotePath, localPath, progress)
	stopTicker()
	if err == nil {
		progress.finish()
	}
	progress.render()
	fmt.Fprintf(tty, "\r\n")

	if err != nil {
		if ctx.Err() != nil {
			cancelLabel := localeT(loc, "download cancelled", "下载已取消")
			fmt.Fprintf(tty, "\x1b[33m✗ %s\x1b[0m\r\n", cancelLabel)
		} else {
			errLabel := localeT(loc, "download failed", "下载失败")
			fmt.Fprintf(tty, "\x1b[31m✗ %s: %s\x1b[0m\r\n", errLabel, err.Error())
		}
		return
	}

	successLabel := localeT(loc, "downloaded", "已下载")
	sizeLabel := localeT(loc, "size", "大小")
	fmt.Fprintf(tty, "\x1b[32m✓ %s %s → %s/\x1b[0m\r\n", successLabel, filename, localDir)
	if size > 0 {
		fmt.Fprintf(tty, "\x1b[2m  %s: %s\x1b[0m\r\n", sizeLabel, formatFileSize(size))
	}
}

// handleDirDownload downloads a remote directory recursively to local Downloads dir.
func handleDirDownload(tty io.Writer, stdinReader io.Reader, client *ssh.Client, remotePath, localDir string, ctx context.Context, loc locale) {
	dirName := filepath.Base(remotePath)

	// Count remote files first.
	scanLabel := localeT(loc, "scanning remote directory...", "扫描远程目录...")
	fmt.Fprintf(tty, "\x1b[36m⋯ %s\x1b[0m\r", scanLabel)

	totalFiles, totalSize, err := countRemoteFiles(client, remotePath)
	if err != nil {
		totalFiles = 0 // proceed without count
		totalSize = 0
	}

	// Show confirmation.
	fileLabel := localeT(loc, "files", "个文件")
	downloadLabel := localeT(loc, "Download directory", "下载目录")
	countStr := ""
	if err == nil {
		countStr = fmt.Sprintf(" (%d %s)", totalFiles, fileLabel)
	}
	summary := fmt.Sprintf("\r\n  %s: %s/%s → %s/\r\n",
		downloadLabel, dirName, countStr, localDir)
	if !confirmTransfer(tty, stdinReader, summary, loc) {
		return
	}

	downLabel := localeT(loc, "downloading directory", "正在下载目录")
	cancelHint := localeT(loc, "Esc/Ctrl+C to cancel", "Esc/Ctrl+C 取消")
	fmt.Fprintf(tty, "\x1b[36m⋯ %s %s/ → %s/ — %s\x1b[0m\r\n",
		downLabel, dirName, localDir, cancelHint)

	progress := &dirProgress{tty: tty, total: totalFiles, totalBytes: totalSize, countKnown: err == nil, loc: loc}
	stopTicker := progress.startLiveRenderer()
	// Pass localDir as parent — recvDirRecursive will create the top-level dir.
	err = scpDownloadDir(ctx, client, remotePath, localDir, progress)
	stopTicker()
	if err == nil {
		progress.finish()
	}
	progress.render()
	fmt.Fprintf(tty, "\r\n")

	if err != nil {
		if ctx.Err() != nil {
			cancelLabel := localeT(loc, "download cancelled", "下载已取消")
			fmt.Fprintf(tty, "\x1b[33m✗ %s (%d/%d)\x1b[0m\r\n",
				cancelLabel, progress.current, totalFiles)
		} else {
			errLabel := localeT(loc, "download failed", "下载失败")
			fmt.Fprintf(tty, "\x1b[31m✗ %s: %s\x1b[0m\r\n", errLabel, err.Error())
		}
		return
	}

	successLabel := localeT(loc, "downloaded directory", "已下载目录")
	fmt.Fprintf(tty, "\x1b[32m✓ %s %s/ (%d %s) → %s/\x1b[0m\r\n",
		successLabel, dirName, progress.current, fileLabel, localDir)
}

// countRemoteFiles counts files in a remote directory and sums their sizes.
// Returns (count, totalSize, error). Both are best-effort; on error callers
// proceed with 0 (progress falls back to file-count-only rendering).
func countRemoteFiles(client *ssh.Client, remotePath string) (int, int64, error) {
	// stat -c %s prints each file size on its own line; awk sums sizes and
	// counts lines. Output: "<count> <totalSize>". Falls back gracefully if
	// stat fails on a file (skips it). Runs under /bin/sh via remoteRunSh.
	script := "find " + shQuote(remotePath) + " -type f -exec stat -c %s {} + 2>/dev/null | awk '{s+=$1;c++} END{print c,s}'"
	out, err := remoteRunSh(client, script, 5*time.Second)
	if err != nil {
		return 0, 0, err
	}
	var count int
	var size int64
	fmt.Sscanf(out, "%d %d", &count, &size)
	return count, size, nil
}

// scpDownloadFile downloads a single file from remote using SCP protocol.
func scpDownloadFile(ctx context.Context, client *ssh.Client, remotePath, localPath string, progress io.Writer) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("open ssh session: %w", err)
	}
	defer session.Close()

	go func() {
		<-ctx.Done()
		session.Close()
	}()

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("open stdin pipe: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open stdout pipe: %w", err)
	}

	if err := session.Start(fmt.Sprintf("scp -f %q", remotePath)); err != nil {
		return fmt.Errorf("start remote scp: %w", err)
	}

	// Signal ready.
	if _, err := stdin.Write([]byte{0}); err != nil {
		return err
	}

	// Read file header: C<mode> <size> <name>
	header, err := readSCPLine(stdout)
	if err != nil {
		return err
	}
	if header[0] != 'C' {
		return fmt.Errorf("unexpected scp response: %q", header)
	}

	var mode string
	var size int64
	var name string
	if _, err := fmt.Sscanf(header, "%s %d %s", &mode, &size, &name); err != nil {
		return fmt.Errorf("parse scp header: %w", err)
	}

	// Signal ready for data.
	if _, err := stdin.Write([]byte{0}); err != nil {
		return err
	}

	// Create local file.
	f, err := os.Create(localPath)
	if err != nil {
		return fmt.Errorf("create local file: %w", err)
	}
	defer f.Close()

	// Read file data.
	var writer io.Writer = f
	if progress != nil {
		writer = io.MultiWriter(f, progress)
	}
	if _, err := io.CopyN(writer, stdout, size); err != nil {
		return fmt.Errorf("read file data: %w", err)
	}

	// Read end marker.
	var endMarker [1]byte
	if _, err := stdout.Read(endMarker[:]); err != nil {
		return err
	}
	if endMarker[0] != 0 {
		return fmt.Errorf("unexpected end marker: 0x%02x", endMarker[0])
	}

	// Acknowledge.
	if _, err := stdin.Write([]byte{0}); err != nil {
		return err
	}

	_ = stdin.Close()
	return session.Wait()
}

// scpDownloadDir downloads a directory recursively from remote using SCP protocol.
func scpDownloadDir(ctx context.Context, client *ssh.Client, remotePath, localDir string, progress *dirProgress) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Create local directory.
	if err := os.MkdirAll(localDir, 0755); err != nil {
		return fmt.Errorf("create local dir: %w", err)
	}

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("open ssh session: %w", err)
	}
	defer session.Close()

	go func() {
		<-ctx.Done()
		session.Close()
	}()

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("open stdin pipe: %w", err)
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return fmt.Errorf("open stdout pipe: %w", err)
	}

	if err := session.Start(fmt.Sprintf("scp -r -f %q", remotePath)); err != nil {
		return fmt.Errorf("start remote scp: %w", err)
	}

	return recvDirRecursive(stdin, stdout, localDir, progress, ctx)
}

// countingReader wraps an io.Reader and reports each Read's byte count to a
// callback. Used to feed byte-level progress during SCP downloads (io.CopyN
// from the remote stdout).
type countingReader struct {
	r      io.Reader
	onRead func(int)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 && c.onRead != nil {
		c.onRead(n)
	}
	return n, err
}

// countingWriter wraps an io.Writer and reports each Write's byte count to a
// callback. Used to feed byte-level progress during SCP uploads.
type countingWriter struct {
	w       io.Writer
	onWrite func(int)
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	if n > 0 && c.onWrite != nil {
		c.onWrite(n)
	}
	return n, err
}

// readSCPLine reads a newline-terminated line from the SCP protocol.
func readSCPLine(r io.Reader) (string, error) {
	var buf []byte
	tmp := make([]byte, 1)
	for {
		n, err := r.Read(tmp)
		if n == 1 {
			if tmp[0] == '\n' {
				return string(buf), nil
			}
			buf = append(buf, tmp[0])
		}
		if err != nil {
			return string(buf), err
		}
	}
}

// recvDirRecursive receives a directory tree via SCP protocol.
//
// SCP download protocol flow:
//  1. Send 0x00 (ready)
//  2. Read entry: C (file), D (directory), E (end)
//  3. If C: send 0x00, read data, read end marker, send 0x00 (ack)
//  4. If D: create dir, send 0x00, recurse, send 0x00 (ack)
//  5. If E: return (no ack needed)
func recvDirRecursive(stdin io.Writer, stdout io.Reader, localDir string, progress *dirProgress, ctx context.Context) error {
	// Initial ready signal.
	if _, err := stdin.Write([]byte{0}); err != nil {
		return err
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Read response line.
		line, err := readSCPLine(stdout)
		if err != nil {
			// EOF after top-level 'E' is normal — session closed.
			if err == io.EOF {
				return nil
			}
			return err
		}

		if len(line) == 0 {
			continue
		}

		switch line[0] {
		case 'E': // End of directory — return, parent will ack.
			return nil

		case '\x01': // SCP error (e.g. socket, device — not transferable).
			// Skip: the remote scp sends this for files it can't read
			// (sockets, pipes, etc.). Log and continue to the next entry.
			debugf("scp-dir: skipped error: %s", line[1:])
			continue

		case 'C': // File
			var mode string
			var size int64
			var name string
			if _, err := fmt.Sscanf(line, "%s %d %s", &mode, &size, &name); err != nil {
				return fmt.Errorf("parse scp header: %w", err)
			}

			// Signal ready for data.
			if _, err := stdin.Write([]byte{0}); err != nil {
				return err
			}

			// Create local file (auto-rename if exists).
			filePath := uniqueLocalPath(filepath.Join(localDir, name))
			f, err := os.Create(filePath)
			if err != nil {
				return err
			}

			// Read file data. Wrap stdout in a counting writer so the byte-level
			// progress advances during large-file transfers (not just when a
			// file finishes), keeping the live ticker meaningful.
			var src io.Reader = stdout
			if progress != nil {
				src = &countingReader{r: stdout, onRead: progress.addBytes}
			}
			if _, err := io.CopyN(f, src, size); err != nil {
				f.Close()
				return err
			}
			f.Close()

			// Read end marker.
			var endMarker [1]byte
			if _, err := stdout.Read(endMarker[:]); err != nil {
				return err
			}

			// Acknowledge file received.
			if _, err := stdin.Write([]byte{0}); err != nil {
				return err
			}

			if progress != nil {
				progress.update(name)
			}

		case 'D': // Directory
			var mode string
			var size int64
			var name string
			if _, err := fmt.Sscanf(line, "%s %d %s", &mode, &size, &name); err != nil {
				return fmt.Errorf("parse scp header: %w", err)
			}

			// Create subdirectory and recurse.
			subDir := filepath.Join(localDir, name)
			if err := os.MkdirAll(subDir, 0755); err != nil {
				return err
			}
			if err := recvDirRecursive(stdin, stdout, subDir, progress, ctx); err != nil {
				return err
			}

			// Acknowledge directory received.
			if _, err := stdin.Write([]byte{0}); err != nil {
				return err
			}

		default:
			return fmt.Errorf("unexpected scp response: %q", line)
		}
	}
}
