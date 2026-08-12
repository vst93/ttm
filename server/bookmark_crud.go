package server

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const maxPrivateKeyFileBytes int64 = 256 * 1024

type bookmarkEditorMode int

const (
	editorModeAdd bookmarkEditorMode = iota
	editorModeEdit
)

type editorField int

const (
	editorFieldTitle editorField = iota
	editorFieldHost
	editorFieldUsername
	editorFieldPort
	editorFieldAuthType
	editorFieldPassword
	editorFieldPrivateKey
	editorFieldPassphrase
	editorFieldTmuxScroll
)

const maskedSecretValue = "••••••"

type authTypeMode int

const (
	authTypeKeyboardInteractive authTypeMode = iota
	authTypePassword
	authTypePrivateKey
)

type bookmarkEditor struct {
	mode          bookmarkEditorMode
	editIndex     int
	focusIndex    int
	authType      authTypeMode
	authDirty     bool
	tmuxScroll    string
	showSecrets   bool
	secretValues  map[editorField]string
	secretCleared map[editorField]bool
	scroll        int
	scrollToFocus bool
	inputs        []textinput.Model
	original      BookmarkItem
	viewport      viewport.Model
}

func isNewlineKey(msg tea.KeyMsg) bool {
	if msg.Type == tea.KeyRunes {
		if len(msg.Runes) == 1 && (msg.Runes[0] == '\n' || msg.Runes[0] == '\r') {
			return true
		}
	}
	if msg.Type == tea.KeyEnter || msg.Type == tea.KeyCtrlM || msg.Type == tea.KeyCtrlJ {
		return true
	}
	key := msg.String()
	return key == "enter" || key == "ctrl+m" || key == "ctrl+j"
}

func restorePrivateKeyNewlinesIfCollapsed(value string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	normalized = strings.ReplaceAll(normalized, "\\r\\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\\r", "\n")
	return normalized
}

func encodePrivateKeyForDisplay(value string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	return strings.ReplaceAll(normalized, "\n", "\\n")
}

func decodePrivateKeyFromDisplay(value string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
	normalized = strings.ReplaceAll(normalized, "\\r\\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\\r", "\n")
	return normalized
}

func isLikelyPathText(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || strings.Contains(trimmed, "\n") || strings.Contains(trimmed, "\r") {
		return false
	}
	if filepath.IsAbs(trimmed) || strings.HasPrefix(trimmed, "./") || strings.HasPrefix(trimmed, "../") || strings.HasPrefix(trimmed, ".\\") || strings.HasPrefix(trimmed, "..\\") || strings.HasPrefix(trimmed, "~/") || strings.HasPrefix(trimmed, "~\\") {
		return true
	}
	if len(trimmed) >= 3 {
		c0 := trimmed[0]
		if ((c0 >= 'a' && c0 <= 'z') || (c0 >= 'A' && c0 <= 'Z')) && trimmed[1] == ':' && (trimmed[2] == '\\' || trimmed[2] == '/') {
			return true
		}
	}
	return strings.ContainsAny(trimmed, `/\\`)
}

func expandHomePath(pathText string) string {
	if pathText == "~" {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			return home
		}
		return pathText
	}
	if strings.HasPrefix(pathText, "~/") || strings.HasPrefix(pathText, "~\\") {
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			return filepath.Join(home, pathText[2:])
		}
	}
	return pathText
}

func resolvePrivateKeyMaybePath(input string) (string, string) {
	normalizedInput := restorePrivateKeyNewlinesIfCollapsed(decodePrivateKeyFromDisplay(input))
	if !isLikelyPathText(normalizedInput) {
		return normalizedInput, ""
	}
	pathText := expandHomePath(strings.TrimSpace(normalizedInput))
	info, err := os.Stat(pathText)
	if err != nil {
		return normalizedInput, "path_unreadable"
	}
	if info.IsDir() {
		return normalizedInput, "path_unreadable"
	}
	if info.Size() > maxPrivateKeyFileBytes {
		return normalizedInput, "path_too_large"
	}
	content, err := os.ReadFile(pathText)
	if err != nil {
		return normalizedInput, "path_unreadable"
	}
	resolved := strings.ReplaceAll(strings.ReplaceAll(string(content), "\r\n", "\n"), "\r", "\n")
	return resolved, "path_loaded"
}

func isClearFieldKey(msg tea.KeyMsg) bool {
	if msg.Type == tea.KeyCtrlU || msg.String() == "ctrl+u" || msg.Type == tea.KeyCtrlK || msg.String() == "ctrl+k" {
		return true
	}
	if len(msg.Runes) == 1 {
		return msg.Runes[0] == 0x15 || msg.Runes[0] == 0x0b
	}
	return false
}

func syncPrivateKeyInputSingleLine() {
	value := AM.editor.secretValues[editorFieldPrivateKey]
	display := encodePrivateKeyForDisplay(value)
	AM.editor.inputs[int(editorFieldPrivateKey)].SetValue(display)
	AM.editor.inputs[int(editorFieldPrivateKey)].SetCursor(len([]rune(display)))
}

func insertIntoPrivateKeyInput(displayText string) {
	input := &AM.editor.inputs[int(editorFieldPrivateKey)]
	current := []rune(input.Value())
	insert := []rune(displayText)
	pos := input.Position()
	if pos < 0 {
		pos = 0
	}
	if pos > len(current) {
		pos = len(current)
	}
	updated := string(current[:pos]) + string(insert) + string(current[pos:])
	input.SetValue(updated)
	input.SetCursor(pos + len(insert))
	decoded := restorePrivateKeyNewlinesIfCollapsed(decodePrivateKeyFromDisplay(updated))
	AM.editor.secretValues[editorFieldPrivateKey] = decoded
	AM.editor.secretCleared[editorFieldPrivateKey] = strings.TrimSpace(decoded) == ""
}

type deleteConfirmState struct {
	index     int
	label     string
	selectYes bool
}

func saveBookmarksToDisk(bookmarks []BookmarkItem) error {
	if APP_DIR == "" {
		if err, _ := InitConfig(); err != nil {
			return err
		}
	}
	data, err := json.Marshal(bookmarks)
	if err != nil {
		return fmt.Errorf("failed to encode bookmarks: %w", err)
	}
	if err := os.WriteFile(APP_DIR+"/bookmarks.json", data, 0644); err != nil {
		return fmt.Errorf("failed to write bookmarks: %w", err)
	}
	return nil
}

func buildEditorInput(value, placeholder string) textinput.Model {
	ti := textinput.New()
	ti.CharLimit = 4096
	ti.Prompt = ""
	ti.SetValue(value)
	ti.Placeholder = placeholder
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("69"))
	ti.PromptStyle = lipgloss.NewStyle()
	ti.TextStyle = lipgloss.NewStyle()
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	return ti
}

func newBookmarkEditor(mode bookmarkEditorMode, index int, existing BookmarkItem) *bookmarkEditor {
	password := existing.Password
	privateKey := existing.PrivateKey
	passphrase := existing.Passphrase

	port := ""
	if existing.Port > 0 {
		port = strconv.Itoa(existing.Port)
	}

	authType := authTypeKeyboardInteractive
	switch strings.TrimSpace(existing.AuthType) {
	case "password":
		authType = authTypePassword
	case "private-key":
		authType = authTypePrivateKey
	default:
		if strings.TrimSpace(existing.PrivateKey) != "" {
			authType = authTypePrivateKey
		} else if strings.TrimSpace(existing.Password) != "" {
			authType = authTypePassword
		}
	}

	editor := &bookmarkEditor{
		mode:        mode,
		editIndex:   index,
		authType:    authType,
		tmuxScroll:  existing.TmuxScroll,
		showSecrets: false,
		secretValues: map[editorField]string{
			editorFieldPassword:   existing.Password,
			editorFieldPrivateKey: existing.PrivateKey,
			editorFieldPassphrase: existing.Passphrase,
		},
		secretCleared: map[editorField]bool{
			editorFieldPassword:   false,
			editorFieldPrivateKey: false,
			editorFieldPassphrase: false,
		},
		original: existing,
		inputs: []textinput.Model{
			buildEditorInput(existing.Title, "server-prod"),
			buildEditorInput(existing.Host, "10.0.0.8"),
			buildEditorInput(existing.Username, "root"),
			buildEditorInput(port, "22"),
			buildEditorInput("", "auth type"),
			buildEditorInput(password, "password"),
			buildEditorInput(privateKey, "private key or file path"),
			buildEditorInput(passphrase, "passphrase"),
			buildEditorInput("", "tmux scroll"),
		},
	}
	editor.inputs[editorFieldAuthType].Blur()
	editor.inputs[editorFieldTmuxScroll].Blur()
	editor.setFocus(0)
	editor.applySecretVisibility()
	return editor
}

func isSecretEditorField(field editorField) bool {
	return field == editorFieldPassword || field == editorFieldPrivateKey || field == editorFieldPassphrase
}

func (be *bookmarkEditor) applySecretVisibility() {
	secretFields := []editorField{editorFieldPassword, editorFieldPrivateKey, editorFieldPassphrase}
	if be.showSecrets {
		for _, field := range secretFields {
			if field == editorFieldPrivateKey {
				display := encodePrivateKeyForDisplay(be.secretValues[field])
				be.inputs[int(field)].SetValue(display)
				be.inputs[int(field)].SetCursor(len([]rune(display)))
				continue
			}
			be.inputs[int(field)].SetValue(be.secretValues[field])
			be.inputs[int(field)].CursorStart()
		}
		return
	}
	for _, field := range secretFields {
		if strings.TrimSpace(be.secretValues[field]) == "" {
			be.inputs[int(field)].SetValue("")
			be.inputs[int(field)].CursorStart()
			continue
		}
		be.inputs[int(field)].SetValue(maskedSecretValue)
		be.inputs[int(field)].CursorStart()
	}
}

func (be *bookmarkEditor) toggleSecretVisibility() {
	be.showSecrets = !be.showSecrets
	be.applySecretVisibility()
}

func (be *bookmarkEditor) captureSecretInput(field editorField) {
	if !isSecretEditorField(field) || !be.showSecrets {
		return
	}
	value := be.inputs[int(field)].Value()
	previous := be.secretValues[field]
	if strings.TrimSpace(value) == maskedSecretValue && strings.TrimSpace(be.secretValues[field]) != "" {
		return
	}
	if field == editorFieldPrivateKey {
		value = decodePrivateKeyFromDisplay(value)
		value = restorePrivateKeyNewlinesIfCollapsed(value)
	}
	be.secretCleared[field] = strings.TrimSpace(value) == ""
	if field == editorFieldPrivateKey && strings.Contains(previous, "\n") {
		normalizedPrevious := strings.Join(strings.Fields(strings.ReplaceAll(strings.ReplaceAll(previous, "\r\n", "\n"), "\n", " ")), " ")
		normalizedValue := strings.Join(strings.Fields(value), " ")
		if normalizedPrevious == normalizedValue {
			return
		}
	}
	be.secretValues[field] = value
}

func (be *bookmarkEditor) fieldValueForCopy(field editorField) string {
	if isSecretEditorField(field) {
		if be.secretCleared[field] {
			return ""
		}
		if strings.TrimSpace(be.secretValues[field]) != "" {
			return be.secretValues[field]
		}
		inputValue := be.inputs[int(field)].Value()
		if strings.TrimSpace(inputValue) != "" && strings.TrimSpace(inputValue) != maskedSecretValue {
			return inputValue
		}
		switch field {
		case editorFieldPassword:
			return be.original.Password
		case editorFieldPrivateKey:
			return be.original.PrivateKey
		case editorFieldPassphrase:
			return be.original.Passphrase
		}
		return ""
	}
	if field == editorFieldAuthType {
		return be.authTypeText(AM.locale)
	}
	if field == editorFieldTmuxScroll {
		return be.tmuxScrollText(AM.locale)
	}
	return be.inputs[int(field)].Value()
}

func (be *bookmarkEditor) modeHint(am *AppModel) string {
	base := lipgloss.NewStyle().Bold(true).Padding(0, 1)
	if be.showSecrets {
		return base.Foreground(lipgloss.Color("16")).Background(lipgloss.Color("78")).Render(am.t("SHOWN", "显示"))
	}
	return base.Foreground(lipgloss.Color("16")).Background(lipgloss.Color("220")).Render(am.t("HIDDEN", "隐藏"))
}

func (be *bookmarkEditor) activeFields() []editorField {
	base := []editorField{editorFieldTitle, editorFieldHost, editorFieldUsername, editorFieldPort, editorFieldAuthType, editorFieldTmuxScroll}
	switch be.authType {
	case authTypePassword:
		return append(base, editorFieldPassword)
	case authTypePrivateKey:
		return append(base, editorFieldPrivateKey, editorFieldPassphrase)
	default:
		return base
	}
}

func (be *bookmarkEditor) hasField(field editorField) bool {
	for _, current := range be.activeFields() {
		if current == field {
			return true
		}
	}
	return false
}

func (be *bookmarkEditor) focusedField() editorField {
	fields := be.activeFields()
	if len(fields) == 0 {
		return editorFieldTitle
	}
	if be.focusIndex < 0 {
		be.focusIndex = 0
	}
	if be.focusIndex >= len(fields) {
		be.focusIndex = len(fields) - 1
	}
	return fields[be.focusIndex]
}

func (be *bookmarkEditor) authTypeText(locale locale) string {
	var text string
	switch be.authType {
	case authTypePassword:
		text = "password"
	case authTypePrivateKey:
		text = "private-key"
	default:
		text = "keyboard-interactive"
	}
	if locale == localeZH {
		switch be.authType {
		case authTypePassword:
			text = "密码"
		case authTypePrivateKey:
			text = "私钥"
		default:
			text = "键盘交互 (keyboard-interactive)"
		}
	}
	return text
}

func (be *bookmarkEditor) cycleAuthType() {
	be.authType++
	if be.authType > authTypePrivateKey {
		be.authType = authTypeKeyboardInteractive
	}
	be.authDirty = true
}

func (be *bookmarkEditor) tmuxScrollText(locale locale) string {
	var text string
	switch be.tmuxScroll {
	case "on":
		text = "on"
	case "off":
		text = "off"
	default:
		text = "default (no change)"
	}
	if locale == localeZH {
		switch be.tmuxScroll {
		case "on":
			text = "开启"
		case "off":
			text = "关闭"
		default:
			text = "默认（不干预）"
		}
	}
	return text
}

func (be *bookmarkEditor) cycleTmuxScroll() {
	switch be.tmuxScroll {
	case "on":
		be.tmuxScroll = "off"
	case "off":
		be.tmuxScroll = ""
	default:
		be.tmuxScroll = "on"
	}
}

func (be *bookmarkEditor) setFocus(index int) tea.Cmd {
	fields := be.activeFields()
	if len(fields) == 0 {
		return nil
	}
	if index < 0 {
		index = len(fields) - 1
	}
	if index >= len(fields) {
		index = 0
	}
	be.focusIndex = index
	be.scrollToFocus = true
	focusedField := be.focusedField()
	cmds := make([]tea.Cmd, 0, len(fields)+1)
	cmds = append(cmds, textinput.Blink)
	for i := range be.inputs {
		if editorField(i) == focusedField && editorField(i) != editorFieldAuthType && editorField(i) != editorFieldTmuxScroll {
			cmds = append(cmds, be.inputs[i].Focus())
			continue
		}
		be.inputs[i].Blur()
	}
	return tea.Batch(cmds...)
}

func (be *bookmarkEditor) selectedIndex(max int) int {
	if max <= 0 {
		return -1
	}
	index := be.editIndex
	if index < 0 {
		index = 0
	}
	if index >= max {
		index = max - 1
	}
	return index
}

func (am *AppModel) bookmarkLabel(bm BookmarkItem) string {
	title := strings.TrimSpace(bm.Title)
	host := strings.TrimSpace(bm.Host)
	if title == "" {
		title = host
	}
	if host == "" {
		host = am.t("unknown", "未知")
	}
	user := strings.TrimSpace(bm.Username)
	if user == "" {
		user = "root"
	}
	port := bm.Port
	if port <= 0 {
		port = 22
	}
	return fmt.Sprintf("%s (%s@%s:%d)", title, user, host, port)
}

func (am *AppModel) selectedBookmarkIndex() int {
	if len(AM.BookmarkInfo.List) == 0 {
		return -1
	}
	selected := AM.list.GlobalIndex()
	if selected < 0 {
		selected = 0
	}
	if selected >= len(AM.BookmarkInfo.List) {
		selected = len(AM.BookmarkInfo.List) - 1
	}
	return selected
}

func (am *AppModel) openAddBookmarkEditor() tea.Cmd {
	editor := newBookmarkEditor(editorModeAdd, len(AM.BookmarkInfo.List), BookmarkItem{Port: 22, EnableSSH: true})
	AM.editor = editor
	AM.pendingDelete = nil
	AM.pendingSync = nil
	return editor.setFocus(0)
}

func (am *AppModel) openEditBookmarkEditor() tea.Cmd {
	selected := am.selectedBookmarkIndex()
	if selected < 0 {
		return setTip(am.t("no bookmark to edit", "没有可编辑的书签"), tipWarn)
	}
	editor := newBookmarkEditor(editorModeEdit, selected, AM.BookmarkInfo.List[selected])
	AM.editor = editor
	AM.pendingDelete = nil
	AM.pendingSync = nil
	return editor.setFocus(0)
}

func (am *AppModel) openDeleteBookmarkConfirm() tea.Cmd {
	selected := am.selectedBookmarkIndex()
	if selected < 0 {
		return setTip(am.t("no bookmark to delete", "没有可删除的书签"), tipWarn)
	}
	AM.pendingDelete = &deleteConfirmState{
		index:     selected,
		label:     am.bookmarkLabel(AM.BookmarkInfo.List[selected]),
		selectYes: false,
	}
	AM.pendingSync = nil
	AM.editor = nil
	return nil
}

func (am *AppModel) handleDeleteConfirmKey(msg tea.KeyMsg) tea.Cmd {
	if AM.pendingDelete == nil {
		return nil
	}
	switch msg.String() {
	case "esc", "n":
		AM.pendingDelete = nil
		return setTip(am.t("delete cancelled", "已取消删除"), tipInfo)
	case "left", "h", "right", "l", "tab", "shift+tab":
		AM.pendingDelete.selectYes = !AM.pendingDelete.selectYes
		return nil
	case "y":
		deleteIndex := AM.pendingDelete.index
		AM.pendingDelete = nil
		if deleteIndex < 0 || deleteIndex >= len(AM.BookmarkInfo.List) {
			return setTip(am.t("selected bookmark not found", "未找到选中的书签"), tipWarn)
		}
		previous := append([]BookmarkItem(nil), AM.BookmarkInfo.List...)
		AM.BookmarkInfo.List = append(AM.BookmarkInfo.List[:deleteIndex], AM.BookmarkInfo.List[deleteIndex+1:]...)
		if err := saveBookmarksToDisk(AM.BookmarkInfo.List); err != nil {
			AM.BookmarkInfo.List = previous
			createListWithSelection(deleteIndex)
			return setTip(am.t("delete failed: ", "删除失败: ")+err.Error(), tipError)
		}
		createListWithSelection(deleteIndex)
		return setTip(am.t("bookmark deleted", "书签已删除"), tipSuccess)
	case "enter":
		if !AM.pendingDelete.selectYes {
			AM.pendingDelete = nil
			return setTip(am.t("delete cancelled", "已取消删除"), tipInfo)
		}
		deleteIndex := AM.pendingDelete.index
		AM.pendingDelete = nil
		if deleteIndex < 0 || deleteIndex >= len(AM.BookmarkInfo.List) {
			return setTip(am.t("selected bookmark not found", "未找到选中的书签"), tipWarn)
		}
		previous := append([]BookmarkItem(nil), AM.BookmarkInfo.List...)
		AM.BookmarkInfo.List = append(AM.BookmarkInfo.List[:deleteIndex], AM.BookmarkInfo.List[deleteIndex+1:]...)
		if err := saveBookmarksToDisk(AM.BookmarkInfo.List); err != nil {
			AM.BookmarkInfo.List = previous
			createListWithSelection(deleteIndex)
			return setTip(am.t("delete failed: ", "删除失败: ")+err.Error(), tipError)
		}
		createListWithSelection(deleteIndex)
		return setTip(am.t("bookmark deleted", "书签已删除"), tipSuccess)
	default:
		return nil
	}
}

func (am *AppModel) handleEditorKey(msg tea.KeyMsg) tea.Cmd {
	if AM.editor == nil {
		return nil
	}
	if isClearFieldKey(msg) {
		field := AM.editor.focusedField()
		if field == editorFieldAuthType || field == editorFieldTmuxScroll {
			return nil
		}
		if isSecretEditorField(field) {
			AM.editor.secretValues[field] = ""
			AM.editor.secretCleared[field] = true
			switch field {
			case editorFieldPassword:
				AM.editor.original.Password = ""
			case editorFieldPrivateKey:
				AM.editor.original.PrivateKey = ""
			case editorFieldPassphrase:
				AM.editor.original.Passphrase = ""
			}
			if AM.editor.showSecrets {
				AM.editor.inputs[int(field)].SetValue("")
				AM.editor.inputs[int(field)].CursorStart()
			} else {
				AM.editor.applySecretVisibility()
			}
			return setTip(am.t("field cleared", "字段已清空"), tipInfo)
		}
		AM.editor.inputs[int(field)].SetValue("")
		AM.editor.inputs[int(field)].CursorStart()
		return setTip(am.t("field cleared", "字段已清空"), tipInfo)
	}
	switch msg.String() {
	case "esc":
		AM.editor = nil
		return setTip(am.t("edit cancelled", "已取消编辑"), tipInfo)
	case "tab", "down":
		return AM.editor.setFocus(AM.editor.focusIndex + 1)
	case "shift+tab", "up":
		return AM.editor.setFocus(AM.editor.focusIndex - 1)
	case "enter":
		// 在私钥字段且显示密钥时，Enter 用于插入换行
		if AM.editor.focusedField() == editorFieldPrivateKey && AM.editor.showSecrets {
			insertIntoPrivateKeyInput("\\n")
			return nil
		}
		// 聚焦 tmux 滚动字段时切换其值，否则切换认证类型
		if AM.editor.focusedField() == editorFieldTmuxScroll {
			AM.editor.cycleTmuxScroll()
			return AM.editor.setFocus(AM.editor.focusIndex)
		}
		// 否则切换认证类型
		AM.editor.cycleAuthType()
		return AM.editor.setFocus(AM.editor.focusIndex)
	case "ctrl+r":
		AM.editor.toggleSecretVisibility()
		return nil
	case "ctrl+y":
		field := AM.editor.focusedField()
		value := AM.editor.fieldValueForCopy(field)
		if strings.TrimSpace(value) == "" {
			return setTip(am.t("nothing to copy", "没有可复制内容"), tipInfo)
		}
		usedOSC52Only, err := writeTextToClipboard(value)
		if err != nil {
			return setTip(am.t("copy failed: ", "复制失败: ")+err.Error(), tipError)
		}
		if usedOSC52Only {
			return setTip(fmt.Sprintf(am.t("copied %d chars (terminal clipboard)", "已复制 %d 个字符（终端剪贴板）"), len([]rune(value))), tipSuccess)
		}
		return setTip(fmt.Sprintf(am.t("copied %d chars", "已复制 %d 个字符"), len([]rune(value))), tipSuccess)
	case "ctrl+v":
		if AM.editor.focusedField() == editorFieldPrivateKey {
			if !AM.editor.showSecrets {
				if strings.TrimSpace(AM.editor.fieldValueForCopy(editorFieldPrivateKey)) == "" {
					AM.editor.showSecrets = true
					AM.editor.applySecretVisibility()
				} else {
					return setTip(am.t("press ^R to reveal secret before editing", "编辑前请按 ^R 显示密钥"), tipInfo)
				}
			}
			text, err := clipboardReadAll()
			if err != nil {
				return setTip(am.t("paste failed: ", "粘贴失败: ")+err.Error(), tipError)
			}
			if text == "" {
				return nil
			}
			normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
			insertIntoPrivateKeyInput(encodePrivateKeyForDisplay(normalized))
			return nil
		}
	case "left", "right":
		if AM.editor.focusedField() == editorFieldAuthType {
			AM.editor.cycleAuthType()
			return AM.editor.setFocus(AM.editor.focusIndex)
		}
		if AM.editor.focusedField() == editorFieldTmuxScroll {
			AM.editor.cycleTmuxScroll()
			return AM.editor.setFocus(AM.editor.focusIndex)
		}
	case "pgdown":
		if AM.editor.viewport.Height > 0 {
			AM.editor.viewport.LineDown(3)
			AM.editor.scroll = AM.editor.viewport.YOffset
		}
		return nil
	case "pgup":
		if AM.editor.viewport.Height > 0 {
			AM.editor.viewport.LineUp(3)
			AM.editor.scroll = AM.editor.viewport.YOffset
		}
		return nil
	case "home":
		AM.editor.scroll = 0
		return nil
	case "end":
		if AM.editor.viewport.Height > 0 {
			AM.editor.viewport.GotoBottom()
			AM.editor.scroll = AM.editor.viewport.YOffset
		}
		return nil
	case "ctrl+s":
		return am.saveEditor()
	}
	focusField := AM.editor.focusedField()
	if focusField == editorFieldAuthType || focusField == editorFieldTmuxScroll {
		return nil
	}
	if isSecretEditorField(focusField) && !AM.editor.showSecrets {
		if strings.TrimSpace(AM.editor.fieldValueForCopy(focusField)) == "" {
			AM.editor.showSecrets = true
			AM.editor.applySecretVisibility()
		} else {
			return setTip(am.t("press ^R to reveal secret before editing", "编辑前请按 ^R 显示密钥"), tipInfo)
		}
	}
	focus := int(focusField)
	if focusField == editorFieldPrivateKey && AM.editor.showSecrets {
		if isNewlineKey(msg) {
			insertIntoPrivateKeyInput("\\n")
			return nil
		}
		if msg.Type == tea.KeyRunes {
			runesText := string(msg.Runes)
			if strings.Contains(runesText, "\n") || strings.Contains(runesText, "\r") {
				normalized := strings.ReplaceAll(strings.ReplaceAll(runesText, "\r\n", "\n"), "\r", "\n")
				insertIntoPrivateKeyInput(encodePrivateKeyForDisplay(normalized))
				return nil
			}
		}
	}
	updated, cmd := AM.editor.inputs[focus].Update(msg)
	AM.editor.inputs[focus] = updated
	AM.editor.captureSecretInput(focusField)
	return cmd
}

func (am *AppModel) saveEditor() tea.Cmd {
	if AM.editor == nil {
		return nil
	}
	privateKeyPathStatus := ""

	if AM.editor.showSecrets {
		AM.editor.secretValues[editorFieldPassword] = AM.editor.inputs[editorFieldPassword].Value()
		privateKeyInput := AM.editor.inputs[editorFieldPrivateKey].Value()
		decodedPrivateKey := restorePrivateKeyNewlinesIfCollapsed(decodePrivateKeyFromDisplay(privateKeyInput))
		currentPrivateKey := AM.editor.secretValues[editorFieldPrivateKey]
		if strings.Contains(currentPrivateKey, "\n") {
			normalizedCurrent := strings.Join(strings.Fields(strings.ReplaceAll(strings.ReplaceAll(currentPrivateKey, "\r\n", "\n"), "\n", " ")), " ")
			normalizedDecoded := strings.Join(strings.Fields(decodedPrivateKey), " ")
			if normalizedCurrent == normalizedDecoded {
				decodedPrivateKey = currentPrivateKey
			}
		}
		AM.editor.secretValues[editorFieldPrivateKey] = decodedPrivateKey
		AM.editor.secretValues[editorFieldPassphrase] = AM.editor.inputs[editorFieldPassphrase].Value()
		AM.editor.secretCleared[editorFieldPassword] = strings.TrimSpace(AM.editor.secretValues[editorFieldPassword]) == ""
		AM.editor.secretCleared[editorFieldPrivateKey] = strings.TrimSpace(AM.editor.secretValues[editorFieldPrivateKey]) == ""
		AM.editor.secretCleared[editorFieldPassphrase] = strings.TrimSpace(AM.editor.secretValues[editorFieldPassphrase]) == ""
	}

	host := strings.TrimSpace(AM.editor.inputs[editorFieldHost].Value())
	if host == "" {
		return setTip(am.t("host is required", "主机不能为空"), tipWarn)
	}

	portText := strings.TrimSpace(AM.editor.inputs[editorFieldPort].Value())
	port := AM.editor.original.Port
	if port <= 0 {
		port = 22
	}
	if portText != "" {
		parsed, err := strconv.Atoi(portText)
		if err != nil || parsed < 1 || parsed > 65535 {
			return setTip(am.t("port must be between 1 and 65535", "端口必须是 1 到 65535"), tipWarn)
		}
		port = parsed
	}

	bookmark := AM.editor.original
	bookmark.PrivateKey = restorePrivateKeyNewlinesIfCollapsed(decodePrivateKeyFromDisplay(bookmark.PrivateKey))
	title := strings.TrimSpace(AM.editor.inputs[editorFieldTitle].Value())
	if title == "" {
		title = host
	}
	bookmark.Title = title
	bookmark.Host = host
	bookmark.Username = strings.TrimSpace(AM.editor.inputs[editorFieldUsername].Value())
	bookmark.Port = port
	bookmark.AuthType = "keyboard-interactive"
	bookmark.TmuxScroll = AM.editor.tmuxScroll
	strictApply := AM.editor.mode == editorModeAdd || AM.editor.authDirty
	switch AM.editor.authType {
	case authTypePassword:
		bookmark.AuthType = "password"
		bookmark.Password = AM.editor.secretValues[editorFieldPassword]
		if strictApply {
			bookmark.PrivateKey = ""
			bookmark.Passphrase = ""
		}
	case authTypePrivateKey:
		bookmark.AuthType = "private-key"
		bookmark.PrivateKey, privateKeyPathStatus = resolvePrivateKeyMaybePath(AM.editor.secretValues[editorFieldPrivateKey])
		bookmark.Passphrase = AM.editor.secretValues[editorFieldPassphrase]
		if strictApply {
			bookmark.Password = ""
		}
	default:
		if strictApply {
			bookmark.Password = ""
			bookmark.PrivateKey = ""
			bookmark.Passphrase = ""
		}
	}
	if AM.editor.mode == editorModeAdd {
		bookmark.EnableSSH = true
	}

	selected := AM.editor.selectedIndex(len(AM.BookmarkInfo.List))
	if selected < 0 {
		selected = 0
	}
	if AM.editor.mode == editorModeAdd {
		selected = len(AM.BookmarkInfo.List)
	}

	previous := append([]BookmarkItem(nil), AM.BookmarkInfo.List...)
	if AM.editor.mode == editorModeAdd {
		AM.BookmarkInfo.List = append(AM.BookmarkInfo.List, bookmark)
	} else {
		editIndex := AM.editor.selectedIndex(len(AM.BookmarkInfo.List))
		if editIndex < 0 {
			AM.editor = nil
			return setTip(am.t("selected bookmark not found", "未找到选中的书签"), tipWarn)
		}
		AM.BookmarkInfo.List[editIndex] = bookmark
		selected = editIndex
	}

	if err := saveBookmarksToDisk(AM.BookmarkInfo.List); err != nil {
		AM.BookmarkInfo.List = previous
		createListWithSelection(selected)
		return setTip(am.t("save failed: ", "保存失败: ")+err.Error(), tipError)
	}

	AM.editor = nil
	createListWithSelection(selected)
	if AM.editor == nil {
		if len(AM.BookmarkInfo.List) == 0 {
			createList()
		}
	}
	if selected >= 0 {
		AM.list.Select(selected)
	}
	if AM.editor == nil && AM.pendingDelete == nil {
		if selected >= 0 {
			AM.list.Select(selected)
		}
	}
	if len(previous) == len(AM.BookmarkInfo.List) {
		if privateKeyPathStatus == "path_loaded" {
			return setTip(am.t("bookmark updated; private key loaded from file path", "书签已更新；私钥已从文件路径读取"), tipSuccess)
		}
		if privateKeyPathStatus == "path_too_large" {
			return setTip(am.t("bookmark updated; private key path kept as text because file is too large", "书签已更新；私钥文件过大，已保留路径文本"), tipWarn)
		}
		if privateKeyPathStatus == "path_unreadable" {
			return setTip(am.t("bookmark updated; private key path unreadable, kept as text", "书签已更新；私钥路径无法读取，已保留路径文本"), tipWarn)
		}
		return setTip(am.t("bookmark updated", "书签已更新"), tipSuccess)
	}
	if privateKeyPathStatus == "path_loaded" {
		return setTip(am.t("bookmark added; private key loaded from file path", "书签已新增；私钥已从文件路径读取"), tipSuccess)
	}
	if privateKeyPathStatus == "path_too_large" {
		return setTip(am.t("bookmark added; private key path kept as text because file is too large", "书签已新增；私钥文件过大，已保留路径文本"), tipWarn)
	}
	if privateKeyPathStatus == "path_unreadable" {
		return setTip(am.t("bookmark added; private key path unreadable, kept as text", "书签已新增；私钥路径无法读取，已保留路径文本"), tipWarn)
	}
	return setTip(am.t("bookmark added", "书签已新增"), tipSuccess)
}

func (am *AppModel) toggleStar() tea.Cmd {
	selected := am.selectedBookmarkIndex()
	if selected < 0 {
		return setTip(am.t("no bookmark to star", "没有可标星的书签"), tipWarn)
	}
	AM.BookmarkInfo.List[selected].Starred = !AM.BookmarkInfo.List[selected].Starred
	starred := AM.BookmarkInfo.List[selected].Starred

	// Track the bookmark to follow it after sorting
	bm := AM.BookmarkInfo.List[selected]
	sortBookmarksByStarred(AM.BookmarkInfo.List)

	// Find new position
	newIndex := 0
	for i, b := range AM.BookmarkInfo.List {
		if b.Host == bm.Host && b.Title == bm.Title && b.Username == bm.Username && b.Port == bm.Port {
			newIndex = i
			break
		}
	}

	if err := saveBookmarksToDisk(AM.BookmarkInfo.List); err != nil {
		// Revert
		AM.BookmarkInfo.List[selected].Starred = !AM.BookmarkInfo.List[selected].Starred
		sortBookmarksByStarred(AM.BookmarkInfo.List)
		createListWithSelection(selected)
		return setTip(am.t("star failed: ", "标星失败: ")+err.Error(), tipError)
	}

	createListWithSelection(newIndex)
	if starred {
		return setTip(am.t("bookmark starred", "书签已标星"), tipSuccess)
	}
	return setTip(am.t("bookmark unstarred", "书签已取消标星"), tipSuccess)
}

func (am *AppModel) editorFieldLabel(field editorField) string {
	switch field {
	case editorFieldTitle:
		return am.t("Title", "标题")
	case editorFieldHost:
		return am.t("Host", "主机")
	case editorFieldUsername:
		return am.t("Username", "用户名")
	case editorFieldPort:
		return am.t("Port", "端口")
	case editorFieldAuthType:
		return am.t("Auth Type", "认证方式")
	case editorFieldPassword:
		return am.t("Password", "密码")
	case editorFieldPrivateKey:
		return am.t("Private Key", "私钥")
	case editorFieldPassphrase:
		return am.t("Passphrase", "口令")
	case editorFieldTmuxScroll:
		return am.t("Tmux Scroll", "Tmux 滚动")
	default:
		return ""
	}
}

func (am *AppModel) editorAuthTypeView() string {
	current := AM.editor.authTypeText(am.locale)
	hint := am.t("← → / m switch", "← → / m 切换")
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	arrowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	return arrowStyle.Render("◂ ") + valueStyle.Render(current) + arrowStyle.Render(" ▸") + "  " + hintStyle.Render(hint)
}

func (am *AppModel) editorTmuxScrollView() string {
	current := AM.editor.tmuxScrollText(am.locale)
	hint := am.t("← → switch", "← → 切换")
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	arrowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	return arrowStyle.Render("◂ ") + valueStyle.Render(current) + arrowStyle.Render(" ▸") + "  " + hintStyle.Render(hint)
}

func (am *AppModel) buildEditorOverlay(frameWidth, frameHeight int) string {
	if AM.editor == nil {
		return ""
	}
	overlayWidth := frameWidth
	if overlayWidth < 20 {
		overlayWidth = 20
	}
	overlayHeight := frameHeight
	if overlayHeight < 6 {
		overlayHeight = 6
	}

	inputWidth := overlayWidth - 6
	if inputWidth < 8 {
		inputWidth = 8
	}
	for i := range AM.editor.inputs {
		AM.editor.inputs[i].Width = inputWidth
	}

	dimLabelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
	focusLabelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	focusInputStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	blurInputStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	body := strings.Builder{}
	fields := AM.editor.activeFields()
	for i := range fields {
		field := fields[i]
		label := am.editorFieldLabel(field)
		isFocused := i == AM.editor.focusIndex
		if isFocused {
			label = focusLabelStyle.Render("▸ " + label)
		} else {
			label = dimLabelStyle.Render("  " + label)
		}
		body.WriteString(label)
		body.WriteString("\n")
		if field == editorFieldAuthType {
			body.WriteString("  " + am.editorAuthTypeView())
		} else if field == editorFieldTmuxScroll {
			body.WriteString("  " + am.editorTmuxScrollView())
		} else if isFocused {
			body.WriteString("  " + focusInputStyle.Render(AM.editor.inputs[int(field)].View()))
		} else {
			body.WriteString("  " + blurInputStyle.Render(AM.editor.inputs[int(field)].View()))
		}
		if field == editorFieldPrivateKey {
			hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
			body.WriteString("\n  " + hintStyle.Render(am.t("Paste key text or enter a key file path (loaded on save)", "可粘贴私钥文本或填写私钥文件路径（保存时读取）")))
		}
		if i != len(fields)-1 {
			body.WriteString("\n\n")
		}
	}
	bodyText := body.String()

	titleIcon := "✎ "
	title := am.t("Edit Bookmark", "编辑书签")
	if AM.editor.mode == editorModeAdd {
		titleIcon = "+ "
		title = am.t("Add Bookmark", "新增书签")
	}
	help := fmt.Sprintf(am.t("tab switch · ^M auth · ^S save · ^R reveal/hide %s · ^Y copy all · ^U clear field · esc",
		"tab 切换 · ^M 认证 · ^S 保存 · ^R 显示/隐藏 %s · ^Y 全部复制 · ^U 清空字段 · esc"), AM.editor.modeHint(am))
	sepWidth := inputWidth + 2
	if sepWidth > overlayWidth-2 {
		sepWidth = overlayWidth - 2
	}
	separator := lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(strings.Repeat("─", sepWidth))

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true).Render(titleIcon+title),
		lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(help),
		separator,
		bodyText,
	)
	containerWidth := overlayWidth
	if containerWidth > frameWidth {
		containerWidth = frameWidth
	}
	if containerWidth < 12 {
		containerWidth = 12
	}
	containerHeight := overlayHeight
	if containerHeight > frameHeight {
		containerHeight = frameHeight
	}
	if containerHeight < 4 {
		containerHeight = 4
	}

	viewportWidth := containerWidth
	if viewportWidth < 1 {
		viewportWidth = 1
	}
	viewportHeight := containerHeight
	if viewportHeight < 1 {
		viewportHeight = 1
	}
	AM.editor.viewport = viewport.New(viewportWidth, viewportHeight)
	AM.editor.viewport.SetContent(content)
	AM.editor.viewport.SetYOffset(AM.editor.scroll)
	if AM.editor.scrollToFocus {
		AM.editor.scrollToFocus = false
		focusedIndex := AM.editor.focusIndex
		if focusedIndex < 0 {
			focusedIndex = 0
		}
		// 计算聚焦字段标签行的准确位置：头部 3 行 + 各字段块的累计高度
		lineCursor := 3
		fields := AM.editor.activeFields()
		if focusedIndex >= len(fields) {
			focusedIndex = len(fields) - 1
		}
		for i := 0; i < focusedIndex; i++ {
			if fields[i] == editorFieldPrivateKey {
				lineCursor += 4 // 标签+输入+提示+段间空行
			} else {
				lineCursor += 3 // 标签+输入+段间空行
			}
		}
		yOffset := AM.editor.viewport.YOffset
		if lineCursor+1 >= yOffset+viewportHeight {
			yOffset = lineCursor + 2 - viewportHeight
		}
		if lineCursor < yOffset {
			yOffset = lineCursor
		}
		if yOffset < 0 {
			yOffset = 0
		}
		AM.editor.viewport.SetYOffset(yOffset)
	}
	AM.editor.scroll = AM.editor.viewport.YOffset

	return lipgloss.NewStyle().
		Width(containerWidth).
		Height(containerHeight).
		Padding(0, 0).
		Render(AM.editor.viewport.View())
}

func (am *AppModel) buildDeleteConfirmOverlay(frameWidth int) string {
	if AM.pendingDelete == nil {
		return ""
	}
	overlayWidth := (frameWidth * 3) / 5
	if overlayWidth < 42 {
		overlayWidth = 42
	}
	if overlayWidth > frameWidth {
		overlayWidth = frameWidth
	}
	title := lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true).Render("✗ " + am.t("Delete bookmark?", "删除书签？"))
	entry := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render("  " + AM.pendingDelete.label)
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(strings.Repeat("─", overlayWidth-4))
	noStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Padding(0, 1)
	yesStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Padding(0, 1)
	noLabel := "○ " + am.t("No", "否")
	yesLabel := "○ " + am.t("Yes", "是")
	if AM.pendingDelete.selectYes {
		yesStyle = yesStyle.Foreground(lipgloss.Color("16")).Background(lipgloss.Color("78")).Bold(true)
		yesLabel = "◉ " + am.t("Yes", "是")
	} else {
		noStyle = noStyle.Foreground(lipgloss.Color("16")).Background(lipgloss.Color("220")).Bold(true)
		noLabel = "◉ " + am.t("No", "否")
	}
	options := noStyle.Render("["+noLabel+"]") + "  " + yesStyle.Render("["+yesLabel+"]")
	help := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(am.t("left/right switch · enter confirm (default No) · y quick yes · esc cancel", "left/right 切换 · enter 确认(默认否) · y 快速确认 · esc 取消"))
	content := lipgloss.JoinVertical(lipgloss.Left, title, sep, entry, "", options, "", help)
	return lipgloss.NewStyle().
		Width(overlayWidth).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("203")).
		Render(content)
}
