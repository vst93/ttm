package server

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type configField int

const (
	configFieldPlatform configField = iota
	configFieldToken
	configFieldGistID
)

type configEditor struct {
	focusIndex    int
	scroll        int
	scrollToFocus bool
	inputs        []textinput.Model
	original      GistConfig
	viewport      viewport.Model
}

func newConfigEditor(config GistConfig) *configEditor {
	token := ""
	if strings.TrimSpace(config.Token) != "" {
		token = maskedSecretValue
	}

	editor := &configEditor{
		original: config,
		inputs: []textinput.Model{
			buildEditorInput("", "platform"),
			buildEditorInput(token, "ghp_xxxx..."),
			buildEditorInput(config.GistID, "abc123def456"),
		},
	}
	editor.inputs[configFieldPlatform].Blur()
	editor.setFocus(1)
	return editor
}

func (ce *configEditor) platformText() string {
	return AM.Platform
}

func (ce *configEditor) cyclePlatform() {
	if AM.Platform == "github" {
		AM.Platform = "gitee"
	} else {
		AM.Platform = "github"
	}
}

func (ce *configEditor) focusedField() configField {
	fields := []configField{configFieldPlatform, configFieldToken, configFieldGistID}
	idx := ce.focusIndex
	if idx < 0 {
		idx = 0
	}
	if idx >= len(fields) {
		idx = len(fields) - 1
	}
	return fields[idx]
}

func (ce *configEditor) setFocus(index int) tea.Cmd {
	total := 3
	if index < 0 {
		index = total - 1
	}
	if index >= total {
		index = 0
	}
	ce.focusIndex = index
	ce.scrollToFocus = true

	cmds := make([]tea.Cmd, 0, total+1)
	cmds = append(cmds, textinput.Blink)
	focused := ce.focusedField()
	for i := range ce.inputs {
		if configField(i) == focused && focused != configFieldPlatform {
			cmds = append(cmds, ce.inputs[i].Focus())
			continue
		}
		ce.inputs[i].Blur()
	}
	return tea.Batch(cmds...)
}

func (am *AppModel) configFieldLabel(field configField) string {
	switch field {
	case configFieldPlatform:
		return am.t("Platform", "平台")
	case configFieldToken:
		return am.t("Token", "令牌")
	case configFieldGistID:
		return am.t("Gist ID", "Gist ID")
	default:
		return ""
	}
}

func (am *AppModel) configFieldHint(field configField) string {
	switch field {
	case configFieldPlatform:
		return ""
	case configFieldToken:
		if AM.Platform == "gitee" {
			return am.t(
				"gitee.com/personal_access_tokens → gists",
				"gitee.com/personal_access_tokens → 勾选 gists",
			)
		}
		return am.t(
			"github.com/settings/tokens → gist scope",
			"github.com/settings/tokens → 勾选 gist",
		)
	case configFieldGistID:
		return am.t(
			"from gist URL, or leave empty to auto-create",
			"从 Gist URL 中获取，留空则首次推送自动创建",
		)
	default:
		return ""
	}
}

func (am *AppModel) openConfigEditor() tea.Cmd {
	AM.configEditor = newConfigEditor(am.GistConfig)
	AM.editor = nil
	AM.pendingDelete = nil
	return AM.configEditor.setFocus(1)
}

func (am *AppModel) handleConfigEditorKey(msg tea.KeyMsg) tea.Cmd {
	if AM.configEditor == nil {
		return nil
	}
	switch msg.String() {
	case "esc":
		AM.Platform = AM.configEditor.original.Platform
		AM.configEditor = nil
		return setTip(am.t("config cancelled", "已取消配置"), tipInfo)
	case "tab", "down":
		return AM.configEditor.setFocus(AM.configEditor.focusIndex + 1)
	case "shift+tab", "up":
		return AM.configEditor.setFocus(AM.configEditor.focusIndex - 1)
	case "left", "right":
		if AM.configEditor.focusedField() == configFieldPlatform {
			AM.configEditor.cyclePlatform()
			return AM.configEditor.setFocus(AM.configEditor.focusIndex)
		}
	case "m":
		if AM.configEditor.focusedField() == configFieldPlatform {
			AM.configEditor.cyclePlatform()
			return AM.configEditor.setFocus(AM.configEditor.focusIndex)
		}
	case "ctrl+s":
		return am.saveConfigEditor()
	}

	focused := AM.configEditor.focusedField()
	if focused == configFieldPlatform {
		return nil
	}
	idx := int(focused)
	updated, cmd := AM.configEditor.inputs[idx].Update(msg)
	AM.configEditor.inputs[idx] = updated
	return cmd
}

func (am *AppModel) saveConfigEditor() tea.Cmd {
	if AM.configEditor == nil {
		return nil
	}

	token := resolveSecretValue(
		AM.configEditor.inputs[configFieldToken].Value(),
		AM.configEditor.original.Token,
	)
	gistID := strings.TrimSpace(AM.configEditor.inputs[configFieldGistID].Value())

	am.GistConfig.Token = token
	am.GistConfig.GistID = gistID

	if err := SaveConfig(am.GistConfig); err != nil {
		am.GistConfig = AM.configEditor.original
		AM.configEditor = nil
		return setTip(am.t("save config failed: ", "保存配置失败: ")+err.Error(), tipError)
	}

	AM.configEditor = nil
	return setTip(am.t("config saved", "配置已保存"), tipSuccess)
}

func (am *AppModel) configPlatformView() string {
	current := AM.Platform
	if current == "" {
		current = "github"
	}
	hint := am.t("<- -> / m switch", "<- -> / m 切换")
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	arrowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	return arrowStyle.Render("◂ ") + valueStyle.Render(current) + arrowStyle.Render(" ▸") + "  " + hintStyle.Render(hint)
}

func (am *AppModel) buildConfigEditorOverlay(frameWidth, frameHeight int) string {
	if AM.configEditor == nil {
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
	for i := range AM.configEditor.inputs {
		AM.configEditor.inputs[i].Width = inputWidth
	}

	dimLabelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("246"))
	focusLabelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)
	focusInputStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))
	blurInputStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	fields := []configField{configFieldPlatform, configFieldToken, configFieldGistID}

	body := strings.Builder{}
	for i, field := range fields {
		label := am.configFieldLabel(field)
		isFocused := i == AM.configEditor.focusIndex

		if isFocused {
			label = focusLabelStyle.Render("▸ " + label)
		} else {
			label = dimLabelStyle.Render("  " + label)
		}
		body.WriteString(label)
		body.WriteString("\n")

		if field == configFieldPlatform {
			body.WriteString("  " + am.configPlatformView())
		} else if isFocused {
			body.WriteString("  " + focusInputStyle.Render(AM.configEditor.inputs[int(field)].View()))
		} else {
			body.WriteString("  " + blurInputStyle.Render(AM.configEditor.inputs[int(field)].View()))
		}

		hint := am.configFieldHint(field)
		if hint != "" {
			body.WriteString("\n  " + hintStyle.Render(hint))
		}

		if i != len(fields)-1 {
			body.WriteString("\n\n")
		}
	}
	bodyText := body.String()

	title := am.t("Sync Config", "同步配置")
	help := am.t("tab switch · ^S save · esc cancel",
		"tab 切换 · ^S 保存 · esc 取消")
	sepWidth := inputWidth + 2
	if sepWidth > overlayWidth-2 {
		sepWidth = overlayWidth - 2
	}
	separator := lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(strings.Repeat("─", sepWidth))

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true).Render("⚙ "+title),
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
	AM.configEditor.viewport = viewport.New(viewportWidth, viewportHeight)
	AM.configEditor.viewport.SetContent(content)
	AM.configEditor.viewport.SetYOffset(AM.configEditor.scroll)
	if AM.configEditor.scrollToFocus {
		AM.configEditor.scrollToFocus = false
		focusedIndex := AM.configEditor.focusIndex
		if focusedIndex < 0 {
			focusedIndex = 0
		}
		lineCursor := 3 + focusedIndex*4
		yOffset := AM.configEditor.viewport.YOffset
		if lineCursor+1 >= yOffset+viewportHeight {
			yOffset = lineCursor + 2 - viewportHeight
		}
		if lineCursor < yOffset {
			yOffset = lineCursor
		}
		if yOffset < 0 {
			yOffset = 0
		}
		AM.configEditor.viewport.SetYOffset(yOffset)
	}
	AM.configEditor.scroll = AM.configEditor.viewport.YOffset

	return lipgloss.NewStyle().
		Width(containerWidth).
		Height(containerHeight).
		Padding(0, 0).
		Render(AM.configEditor.viewport.View())
}
