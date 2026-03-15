package server

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type syncAction int

const (
	syncActionPush syncAction = iota
	syncActionPull
)

type syncConfirmState struct {
	action    syncAction
	selectYes bool
}

func (am *AppModel) openSyncConfirm(action syncAction) tea.Cmd {
	AM.pendingSync = &syncConfirmState{action: action, selectYes: false}
	AM.pendingDelete = nil
	AM.editor = nil
	AM.configEditor = nil
	return nil
}

func (am *AppModel) startSync(action syncAction) tea.Cmd {
	if AM.isSyncing {
		return nil
	}
	AM.isSyncing = true
	if action == syncActionPush {
		tipCmd := setTip(am.t("pushing to gist...", "正在推送到 Gist..."), tipProgress)
		return tea.Batch(tipCmd, func() tea.Msg {
			err := UploadGist()
			return syncUploadMsg{Err: err, GistID: AM.GistID}
		})
	}
	tipCmd := setTip(am.t("pulling from gist...", "正在从 Gist 拉取..."), tipProgress)
	return tea.Batch(tipCmd, func() tea.Msg {
		err := GetGist()
		if err != nil {
			return syncDownloadMsg{Err: err}
		}
		return syncDownloadMsg{Bookmarks: AM.BookmarkInfo.List}
	})
}

func (am *AppModel) handleSyncConfirmKey(msg tea.KeyMsg) tea.Cmd {
	if AM.pendingSync == nil {
		return nil
	}
	switch msg.String() {
	case "esc", "n":
		AM.pendingSync = nil
		return setTip(am.t("sync cancelled", "已取消同步"), tipInfo)
	case "left", "h", "right", "l", "tab", "shift+tab":
		AM.pendingSync.selectYes = !AM.pendingSync.selectYes
		return nil
	case "y":
		action := AM.pendingSync.action
		AM.pendingSync = nil
		return am.startSync(action)
	case "enter":
		if !AM.pendingSync.selectYes {
			AM.pendingSync = nil
			return setTip(am.t("sync cancelled", "已取消同步"), tipInfo)
		}
		action := AM.pendingSync.action
		AM.pendingSync = nil
		return am.startSync(action)
	default:
		return nil
	}
}

func (am *AppModel) buildSyncConfirmOverlay(frameWidth int) string {
	if AM.pendingSync == nil {
		return ""
	}
	overlayWidth := (frameWidth * 2) / 3
	if overlayWidth < 56 {
		overlayWidth = 56
	}
	if overlayWidth > frameWidth {
		overlayWidth = frameWidth
	}
	operation := am.t("Push to remote Gist", "推送到远端 Gist")
	impact := am.t("Upload local bookmarks to remote Gist.", "将本地书签上传到远端 Gist。")
	if AM.pendingSync.action == syncActionPull {
		operation = am.t("Pull from remote Gist", "从远端 Gist 拉取")
		impact = am.t("Download from remote Gist and overwrite local bookmarks.", "从远端 Gist 下载并覆盖本地书签。")
	}

	warning := lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true).Render("! " + am.t("WARNING", "警告"))
	title := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true).Render(operation)
	desc := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(impact)

	noStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Padding(0, 1)
	yesStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Padding(0, 1)
	noLabel := "○ " + am.t("No", "否")
	yesLabel := "○ " + am.t("Yes", "是")
	if AM.pendingSync.selectYes {
		yesStyle = yesStyle.Foreground(lipgloss.Color("16")).Background(lipgloss.Color("78")).Bold(true)
		yesLabel = "◉ " + am.t("Yes", "是")
	} else {
		noStyle = noStyle.Foreground(lipgloss.Color("16")).Background(lipgloss.Color("220")).Bold(true)
		noLabel = "◉ " + am.t("No", "否")
	}
	options := noStyle.Render("["+noLabel+"]") + "  " + yesStyle.Render("["+yesLabel+"]")
	help := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render(am.t("left/right switch · enter confirm (default No) · y quick yes · esc cancel", "left/right 切换 · enter 确认(默认否) · y 快速确认 · esc 取消"))
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render(strings.Repeat("─", overlayWidth-4))

	content := lipgloss.JoinVertical(lipgloss.Left, warning, title, desc, sep, options, "", help)
	return lipgloss.NewStyle().
		Width(overlayWidth).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("203")).
		Render(content)
}
