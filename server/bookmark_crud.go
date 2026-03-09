package server

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
)

const maskedSecretValue = "••••••"

type authTypeMode int

const (
	authTypeKeyboardInteractive authTypeMode = iota
	authTypePassword
	authTypePrivateKey
)

type bookmarkEditor struct {
	mode       bookmarkEditorMode
	editIndex  int
	focusIndex int
	authType   authTypeMode
	authDirty  bool
	scroll     int
	inputs     []textinput.Model
	original   BookmarkItem
	viewport   viewport.Model
}

type deleteConfirmState struct {
	index int
	label string
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
	password := ""
	privateKey := ""
	passphrase := ""
	if strings.TrimSpace(existing.Password) != "" {
		password = maskedSecretValue
	}
	if strings.TrimSpace(existing.PrivateKey) != "" {
		privateKey = maskedSecretValue
	}
	if strings.TrimSpace(existing.Passphrase) != "" {
		passphrase = maskedSecretValue
	}

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
		mode:      mode,
		editIndex: index,
		authType:  authType,
		original:  existing,
		inputs: []textinput.Model{
			buildEditorInput(existing.Title, "server-prod"),
			buildEditorInput(existing.Host, "10.0.0.8"),
			buildEditorInput(existing.Username, "root"),
			buildEditorInput(port, "22"),
			buildEditorInput("", "auth type"),
			buildEditorInput(password, "password"),
			buildEditorInput(privateKey, "private key"),
			buildEditorInput(passphrase, "passphrase"),
		},
	}
	editor.inputs[editorFieldAuthType].Blur()
	editor.setFocus(0)
	return editor
}

func (be *bookmarkEditor) activeFields() []editorField {
	base := []editorField{editorFieldTitle, editorFieldHost, editorFieldUsername, editorFieldPort, editorFieldAuthType}
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
	focusedField := be.focusedField()
	cmds := make([]tea.Cmd, 0, len(fields)+1)
	cmds = append(cmds, textinput.Blink)
	for i := range be.inputs {
		if editorField(i) == focusedField && editorField(i) != editorFieldAuthType {
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

func (am *AppModel) bookmarkLabel(item BookmarkItem) string {
	title := strings.TrimSpace(item.Title)
	host := strings.TrimSpace(item.Host)
	if title == "" {
		title = host
	}
	if host == "" {
		host = am.t("unknown", "未知")
	}
	return title + "(" + host + ")"
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
	return editor.setFocus(0)
}

func (am *AppModel) openDeleteBookmarkConfirm() tea.Cmd {
	selected := am.selectedBookmarkIndex()
	if selected < 0 {
		return setTip(am.t("no bookmark to delete", "没有可删除的书签"), tipWarn)
	}
	AM.pendingDelete = &deleteConfirmState{
		index: selected,
		label: am.bookmarkLabel(AM.BookmarkInfo.List[selected]),
	}
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
	case "y", "enter":
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
	switch msg.String() {
	case "esc":
		AM.editor = nil
		return setTip(am.t("edit cancelled", "已取消编辑"), tipInfo)
	case "tab", "down":
		return AM.editor.setFocus(AM.editor.focusIndex + 1)
	case "shift+tab", "up":
		return AM.editor.setFocus(AM.editor.focusIndex - 1)
	case "m":
		AM.editor.cycleAuthType()
		return AM.editor.setFocus(AM.editor.focusIndex)
	case "left", "right":
		if AM.editor.focusedField() == editorFieldAuthType {
			AM.editor.cycleAuthType()
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
	if focusField == editorFieldAuthType {
		return nil
	}
	focus := int(focusField)
	updated, cmd := AM.editor.inputs[focus].Update(msg)
	AM.editor.inputs[focus] = updated
	return cmd
}

func resolveSecretValue(newValue, previousValue string) string {
	trimmed := strings.TrimSpace(newValue)
	if trimmed == maskedSecretValue {
		return previousValue
	}
	return newValue
}

func (am *AppModel) saveEditor() tea.Cmd {
	if AM.editor == nil {
		return nil
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
	title := strings.TrimSpace(AM.editor.inputs[editorFieldTitle].Value())
	if title == "" {
		title = host
	}
	bookmark.Title = title
	bookmark.Host = host
	bookmark.Username = strings.TrimSpace(AM.editor.inputs[editorFieldUsername].Value())
	bookmark.Port = port
	bookmark.AuthType = "keyboard-interactive"
	strictApply := AM.editor.mode == editorModeAdd || AM.editor.authDirty
	switch AM.editor.authType {
	case authTypePassword:
		bookmark.AuthType = "password"
		bookmark.Password = resolveSecretValue(AM.editor.inputs[editorFieldPassword].Value(), AM.editor.original.Password)
		if strictApply {
			bookmark.PrivateKey = ""
			bookmark.Passphrase = ""
		}
	case authTypePrivateKey:
		bookmark.AuthType = "private-key"
		bookmark.PrivateKey = resolveSecretValue(AM.editor.inputs[editorFieldPrivateKey].Value(), AM.editor.original.PrivateKey)
		bookmark.Passphrase = resolveSecretValue(AM.editor.inputs[editorFieldPassphrase].Value(), AM.editor.original.Passphrase)
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
		return setTip(am.t("bookmark updated", "书签已更新"), tipSuccess)
	}
	return setTip(am.t("bookmark added", "书签已新增"), tipSuccess)
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
	default:
		return ""
	}
}

func (am *AppModel) editorAuthTypeView() string {
	current := AM.editor.authTypeText(am.locale)
	hint := am.t("left/right or m to switch", "left/right 或 m 切换")
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	return style.Render(current) + " " + lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(hint)
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

	inputWidth := overlayWidth - 4
	if inputWidth < 8 {
		inputWidth = 8
	}
	for i := range AM.editor.inputs {
		AM.editor.inputs[i].Width = inputWidth
	}

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	focusLabelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("69")).Bold(true)
	inputLineStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	body := strings.Builder{}
	fields := AM.editor.activeFields()
	for i := range fields {
		field := fields[i]
		label := am.editorFieldLabel(field)
		if i == AM.editor.focusIndex {
			label = focusLabelStyle.Render("● " + label)
		} else {
			label = labelStyle.Render("○ " + label)
		}
		body.WriteString(label)
		body.WriteString("\n")
		if field == editorFieldAuthType {
			body.WriteString(am.editorAuthTypeView())
		} else {
			body.WriteString(inputLineStyle.Render(AM.editor.inputs[int(field)].View()))
		}
		if i != len(fields)-1 {
			body.WriteString("\n\n")
		}
	}
	bodyText := body.String()

	title := am.t("SSH Bookmark Editor", "SSH 书签编辑")
	if AM.editor.mode == editorModeAdd {
		title = am.t("Add SSH Bookmark", "新增 SSH 书签")
	}
	help := am.t("tab/shift+tab switch • m auth type • ctrl+s save • esc cancel", "tab/shift+tab 切换 • m 认证方式 • ctrl+s 保存 • esc 取消")

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.NewStyle().Foreground(lipgloss.Color("69")).Bold(true).Render(title),
		lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(help),
		"",
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
	if AM.editor.scroll > 0 {
		AM.editor.viewport.SetYOffset(AM.editor.scroll)
	}
	if AM.editor.scroll == 0 {
		focusedIndex := AM.editor.focusIndex
		if focusedIndex < 0 {
			focusedIndex = 0
		}
		lineCursor := focusedIndex * 3
		if lineCursor > AM.editor.viewport.YOffset+AM.editor.viewport.Height-2 {
			AM.editor.viewport.SetYOffset(lineCursor - (AM.editor.viewport.Height - 2))
		}
		if lineCursor < AM.editor.viewport.YOffset {
			AM.editor.viewport.SetYOffset(lineCursor)
		}
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
	title := lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true).Render(am.t("Delete bookmark?", "删除书签？"))
	entry := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render(AM.pendingDelete.label)
	help := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(am.t("y/enter confirm • n/esc cancel", "y/enter 确认 • n/esc 取消"))
	content := lipgloss.JoinVertical(lipgloss.Left, title, "", entry, "", help)
	return lipgloss.NewStyle().
		Width(overlayWidth).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("203")).
		Background(lipgloss.Color("236")).
		Render(content)
}
