package server

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/paginator"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type refreshMsg struct{}

func (r refreshMsg) String() string { return "refresh" }

type initMsg struct{}

func (initMsg) String() string { return "init" }

type connectResultMsg struct {
	Success bool
	Tip     string
}

type probeResultMsg struct {
	Success       bool
	Tip           string
	SSHClient     *defaultClient
	SuccessTip    string
	FailurePrefix string
}

type sshLoginExecCommand struct {
	client *defaultClient
}

func (c sshLoginExecCommand) Run() error {
	return c.client.Login()
}

func (c sshLoginExecCommand) SetStdin(io.Reader) {}

func (c sshLoginExecCommand) SetStdout(io.Writer) {}

func (c sshLoginExecCommand) SetStderr(io.Writer) {}

type clearTipMsg struct {
	Seq int
}

type tipLevel int

type locale int

const (
	tipInfo tipLevel = iota
	tipSuccess
	tipWarn
	tipError
	tipProgress
)

const (
	localeEN locale = iota
	localeZH
)

type item string

func (i item) FilterValue() string { return string(i) }

type itemDelegate struct{}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(item)
	if !ok {
		return
	}

	str := fmt.Sprintf("%02d %s", index+1, i)

	fn := lipgloss.NewStyle().PaddingLeft(1).Foreground(lipgloss.Color("250")).Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return lipgloss.NewStyle().PaddingLeft(0).Foreground(lipgloss.Color("111")).Bold(true).Render("> " + strings.Join(s, " "))
		}
	}
	fmt.Fprint(w, fn(str))
}

type AppModel struct {
	GistConfig
	BookmarkInfo
	list          list.Model
	TipString     string
	tipLevel      tipLevel
	tipSeq        int
	locale        locale
	langToggleKey key.Binding
	isConnecting  bool
	width         int
	height        int
}

var AM = AppModel{}
var docStyle = lipgloss.NewStyle().Margin(0, 1)

func (am *AppModel) Init() tea.Cmd {
	_, am.GistConfig = InitConfig()
	am.locale = localeEN
	am.applyPersistedLocale()
	am.langToggleKey = key.NewBinding(
		key.WithKeys("L"),
		key.WithHelp("L", "toggle language"),
	)
	return func() tea.Msg { return initMsg{} }
}

func (am *AppModel) t(en, zh string) string {
	if am.locale == localeZH {
		return zh
	}
	return en
}

func (am *AppModel) toggleLocale() {
	if am.locale == localeEN {
		am.locale = localeZH
		am.GistConfig.Locale = "zh"
		_ = SaveConfig(am.GistConfig)
		return
	}
	am.locale = localeEN
	am.GistConfig.Locale = "en"
	_ = SaveConfig(am.GistConfig)
}

func (am *AppModel) applyPersistedLocale() {
	if am.GistConfig.Locale == "zh" {
		am.locale = localeZH
		return
	}
	am.locale = localeEN
}

func (am *AppModel) applyListLocale() {
	if am.locale == localeZH {
		am.list.Title = "TTM 书签"
		am.list.KeyMap.CursorUp.SetHelp("↑/k", "上移")
		am.list.KeyMap.CursorDown.SetHelp("↓/j", "下移")
		am.list.KeyMap.PrevPage.SetHelp("←/h/pgup", "上一页")
		am.list.KeyMap.NextPage.SetHelp("→/l/pgdn", "下一页")
		am.list.KeyMap.GoToStart.SetHelp("g/home", "到开头")
		am.list.KeyMap.GoToEnd.SetHelp("G/end", "到结尾")
		am.list.KeyMap.Filter.SetHelp("/", "过滤")
		am.list.KeyMap.ClearFilter.SetHelp("esc", "清除过滤")
		am.list.KeyMap.CancelWhileFiltering.SetHelp("esc", "取消")
		am.list.KeyMap.AcceptWhileFiltering.SetHelp("enter", "应用过滤")
		am.list.KeyMap.ShowFullHelp.SetHelp("?", "更多")
		am.list.KeyMap.CloseFullHelp.SetHelp("?", "关闭帮助")
		am.list.KeyMap.Quit.SetHelp("q", "退出")
		am.list.SetStatusBarItemName("书签", "书签")
		am.list.Paginator.Type = paginator.Arabic
		am.list.Paginator.ArabicFormat = "第%d/%d页"
		am.langToggleKey.SetHelp("L", "切换语言")
		return
	}

	am.list.Title = "TTM Bookmarks"
	am.list.KeyMap.CursorUp.SetHelp("↑/k", "up")
	am.list.KeyMap.CursorDown.SetHelp("↓/j", "down")
	am.list.KeyMap.PrevPage.SetHelp("←/h/pgup", "prev page")
	am.list.KeyMap.NextPage.SetHelp("→/l/pgdn", "next page")
	am.list.KeyMap.GoToStart.SetHelp("g/home", "go to start")
	am.list.KeyMap.GoToEnd.SetHelp("G/end", "go to end")
	am.list.KeyMap.Filter.SetHelp("/", "filter")
	am.list.KeyMap.ClearFilter.SetHelp("esc", "clear filter")
	am.list.KeyMap.CancelWhileFiltering.SetHelp("esc", "cancel")
	am.list.KeyMap.AcceptWhileFiltering.SetHelp("enter", "apply filter")
	am.list.KeyMap.ShowFullHelp.SetHelp("?", "more")
	am.list.KeyMap.CloseFullHelp.SetHelp("?", "close help")
	am.list.KeyMap.Quit.SetHelp("q", "quit")
	am.list.SetStatusBarItemName("bookmark", "bookmarks")
	am.list.Paginator.Type = paginator.Arabic
	am.list.Paginator.ArabicFormat = "Page %d/%d"
	am.langToggleKey.SetHelp("L", "toggle language")
}

func getListSize(width, height int) (int, int) {
	h, v := docStyle.GetFrameSize()
	listWidth := width - h
	listHeight := height - v
	if listWidth <= 0 {
		listWidth = 20
	}
	if listHeight <= 0 {
		listHeight = 14
	}
	return listWidth, listHeight
}

func createListWithSelection(selectedIndex int) {
	items := make([]list.Item, len(AM.BookmarkInfo.List))
	for i, info := range AM.BookmarkInfo.List {
		items[i] = item(info.Title + "(" + info.Host + ")")
	}
	// 使用实际窗口大小，如果没有则使用默认值
	width := AM.width
	height := AM.height
	if width <= 0 {
		width = 40
	}
	if height <= 0 {
		height = 20
	}
	listWidth, listHeight := getListSize(width, height)
	AM.list = list.New(items, itemDelegate{}, listWidth, listHeight)
	AM.list.SetShowTitle(true)
	AM.list.SetShowHelp(true)
	AM.list.SetShowPagination(true)
	AM.list.SetShowStatusBar(true)
	AM.list.StatusMessageLifetime = 2 * time.Second
	AM.list.SetFilteringEnabled(true)
	AM.list.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{AM.langToggleKey}
	}
	AM.list.AdditionalFullHelpKeys = func() []key.Binding {
		return []key.Binding{AM.langToggleKey}
	}
	am := &AM
	am.applyListLocale()

	if len(items) == 0 {
		return
	}

	if selectedIndex < 0 {
		selectedIndex = 0
	}
	if selectedIndex >= len(items) {
		selectedIndex = len(items) - 1
	}
	AM.list.Select(selectedIndex)
}

func createList() {
	createListWithSelection(0)
}

func authModeText(bookmark BookmarkItem) string {
	if strings.TrimSpace(bookmark.PrivateKey) != "" {
		if strings.TrimSpace(bookmark.Passphrase) != "" {
			return AM.t("private-key+passphrase (+ keyboard-interactive fallback)", "私钥+口令(含 keyboard-interactive 回退)")
		}
		return AM.t("private-key (+ keyboard-interactive fallback)", "私钥(含 keyboard-interactive 回退)")
	}
	if strings.TrimSpace(bookmark.Password) != "" {
		return AM.t("password (+ keyboard-interactive fallback)", "密码(含 keyboard-interactive 回退)")
	}
	return "keyboard-interactive"
}

func tipDurationByLevel(level tipLevel) time.Duration {
	switch level {
	case tipProgress:
		return 0
	case tipError:
		return 8 * time.Second
	case tipWarn:
		return 6 * time.Second
	case tipSuccess:
		return 4 * time.Second
	default:
		return 5 * time.Second
	}
}

func setTip(text string, level tipLevel) tea.Cmd {
	AM.TipString = text
	AM.tipLevel = level
	AM.tipSeq++
	seq := AM.tipSeq
	d := tipDurationByLevel(level)
	if d <= 0 {
		return nil
	}
	return tea.Tick(d, func(time.Time) tea.Msg {
		return clearTipMsg{Seq: seq}
	})
}

func buildConnectTarget() (BookmarkItem, int, error) {
	selectedIndex := AM.list.GlobalIndex()
	if selectedIndex < 0 || selectedIndex >= len(AM.BookmarkInfo.List) {
		return BookmarkItem{}, 0, fmt.Errorf("%s", AM.t("selected bookmark not found", "未找到选中的书签"))
	}

	bookmark := AM.BookmarkInfo.List[selectedIndex]
	if !bookmark.EnableSSH {
		return BookmarkItem{}, 0, fmt.Errorf("%s", AM.t("SSH is disabled for selected bookmark", "当前书签未启用 SSH"))
	}

	port := bookmark.Port
	if port <= 0 {
		port = 22
	}

	return bookmark, port, nil
}

func buildSSHClient(bookmark BookmarkItem, port int) (*defaultClient, error) {
	sshConfig := &SSHConfig{
		Host:           bookmark.Host,
		User:           bookmark.Username,
		Port:           port,
		PrivateKey:     bookmark.PrivateKey,
		Passphrase:     bookmark.Passphrase,
		Password:       bookmark.Password,
		CallbackShells: nil,
	}
	return genSSHConfig(sshConfig)
}

func connectExecCmd(sshClient *defaultClient, successTip, failurePrefix string) tea.Cmd {
	return tea.Exec(sshLoginExecCommand{client: sshClient}, func(err error) tea.Msg {
		if err != nil {
			return connectResultMsg{Success: false, Tip: failurePrefix + err.Error()}
		}
		return connectResultMsg{Success: true, Tip: successTip}
	})
}

func probeConnectCmd(sshClient *defaultClient, bookmark BookmarkItem, port int) tea.Cmd {
	return func() tea.Msg {
		authMode := authModeText(bookmark)
		err := sshClient.ProbeConnection(5 * time.Second)
		if err != nil {
			return probeResultMsg{
				Success: false,
				Tip:     fmt.Sprintf(AM.t("connection failed %s@%s:%d (%s): %s", "连接失败 %s@%s:%d (%s): %s"), bookmark.Username, bookmark.Host, port, authMode, err.Error()),
			}
		}

		return probeResultMsg{
			Success:       true,
			Tip:           fmt.Sprintf(AM.t("connected %s@%s:%d (%s)", "已建立连接 %s@%s:%d (%s)"), bookmark.Username, bookmark.Host, port, authMode),
			SSHClient:     sshClient,
			SuccessTip:    fmt.Sprintf(AM.t("session closed %s@%s:%d (%s)", "会话结束 %s@%s:%d (%s)"), bookmark.Username, bookmark.Host, port, authMode),
			FailurePrefix: fmt.Sprintf(AM.t("connection failed %s@%s:%d (%s): ", "连接失败 %s@%s:%d (%s): "), bookmark.Username, bookmark.Host, port, authMode),
		}
	}
}

func blockSize(text string) (int, int) {
	lines := strings.Split(text, "\n")
	width := 0
	for _, line := range lines {
		lineWidth := ansi.StringWidth(line)
		if lineWidth > width {
			width = lineWidth
		}
	}
	return width, len(lines)
}

func overlayAt(base, overlay string, x, y int) string {
	if strings.TrimSpace(overlay) == "" {
		return base
	}

	baseLines := strings.Split(base, "\n")
	overlayLines := strings.Split(overlay, "\n")

	baseWidth, baseHeight := blockSize(base)
	overlayWidth, overlayHeight := blockSize(overlay)
	if baseHeight == 0 || overlayHeight == 0 {
		return base
	}
	if baseWidth <= 0 {
		return base
	}

	if y < 0 {
		y = 0
	}
	if y >= baseHeight {
		return base
	}

	if x < 0 {
		x = 0
	}
	if x > baseWidth {
		x = baseWidth
	}
	if overlayWidth > baseWidth {
		overlayWidth = baseWidth
	}
	if x+overlayWidth > baseWidth {
		x = baseWidth - overlayWidth
		if x < 0 {
			x = 0
		}
	}

	for i := 0; i < overlayHeight && y+i < baseHeight; i++ {
		overlayLine := overlayLines[i]
		overlayLineWidth := ansi.StringWidth(overlayLine)
		if overlayLineWidth > overlayWidth {
			overlayLine = ansi.Truncate(overlayLine, overlayWidth, "")
			overlayLineWidth = ansi.StringWidth(overlayLine)
		}

		baseLine := baseLines[y+i]
		padding := baseWidth - ansi.StringWidth(baseLine)
		if padding > 0 {
			baseLine += strings.Repeat(" ", padding)
		}
		prefix := ansi.Cut(baseLine, 0, x)
		suffixStart := x + overlayLineWidth
		suffix := ""
		if suffixStart < baseWidth {
			suffix = ansi.Cut(baseLine, suffixStart, baseWidth)
		}
		line := prefix + overlayLine + suffix
		lineWidth := ansi.StringWidth(line)
		if lineWidth < baseWidth {
			line += strings.Repeat(" ", baseWidth-lineWidth)
		} else if lineWidth > baseWidth {
			line = ansi.Truncate(line, baseWidth, "")
		}
		baseLines[y+i] = line
	}

	return strings.Join(baseLines, "\n")
}

func overlayTopRight(base, overlay string) string {
	baseWidth, _ := blockSize(base)
	overlayWidth, _ := blockSize(overlay)
	x := baseWidth - overlayWidth
	if x < 0 {
		x = 0
	}
	return overlayAt(base, overlay, x, 0)
}

func overlayCenter(base, overlay string) string {
	baseWidth, baseHeight := blockSize(base)
	overlayWidth, overlayHeight := blockSize(overlay)
	x := (baseWidth - overlayWidth) / 2
	y := (baseHeight - overlayHeight) / 2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return overlayAt(base, overlay, x, y)
}

func buildConnectingOverlay(frameWidth, frameHeight int) string {
	overlayWidth := (frameWidth * 9) / 10
	overlayHeight := (frameHeight * 9) / 10
	if overlayWidth < 24 {
		overlayWidth = frameWidth
	}
	if overlayHeight < 8 {
		overlayHeight = frameHeight
	}
	if overlayWidth > frameWidth {
		overlayWidth = frameWidth
	}
	if overlayHeight > frameHeight {
		overlayHeight = frameHeight
	}

	overlay := lipgloss.NewStyle().
		Width(overlayWidth).
		Height(overlayHeight).
		Background(lipgloss.Color("254")).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("252")).
		Render("")

	badge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Background(lipgloss.Color("255")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("252")).
		Padding(0, 2).
		Render(AM.t("Connecting... please wait", "正在连接，请稍候"))

	return overlayCenter(overlay, badge)
}

func dimBaseForOverlay(base string) string {
	lines := strings.Split(base, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines[i] = lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(line)
	}
	return strings.Join(lines, "\n")
}

func (am *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case initMsg:
		AM.BookmarkInfo.Init()
		fmt.Print("\033[?25h")
		// 立即创建列表，使用默认值
		if len(AM.BookmarkInfo.List) > 0 {
			createList()
		}
		return am, nil
	case refreshMsg:
		AM.BookmarkInfo.Init()
		// 发送 WindowSizeMsg 触发刷新，但不发送额外的 refreshMsg
		if AM.width > 0 && AM.height > 0 {
			return am, func() tea.Msg {
				return tea.WindowSizeMsg{Width: AM.width, Height: AM.height}
			}
		}
		return am, nil
	case tea.KeyMsg:
		if AM.isConnecting && msg.String() != "ctrl+c" {
			return am, nil
		}
		if msg.String() == "ctrl+c" {
			return am, tea.Quit
		} else if msg.String() == "L" {
			am.toggleLocale()
			am.applyListLocale()
			if am.locale == localeZH {
				return am, setTip("语言已切换为中文", tipInfo)
			}
			return am, setTip("language switched to English", tipInfo)
		} else if msg.String() == "s" {
			if AM.Token == "" {
				return am, setTip(am.t("please configure token first", "请先配置 token"), tipWarn)
			}
			if AM.GistID == "" {
				return am, setTip(am.t("please configure gist_id first", "请先配置 gist_id"), tipWarn)
			}
			err := UploadGist()
			if err != nil {
				return am, setTip(am.t("upload failed: ", "上传失败: ")+err.Error(), tipError)
			}
			jsonStr, err := json.Marshal(am.BookmarkInfo.List)
			if err != nil {
				return am, setTip(am.t("save failed: ", "保存失败: ")+err.Error(), tipError)
			}
			err = os.WriteFile(APP_DIR+"/bookmarks.json", jsonStr, 0644)
			if err != nil {
				return am, setTip(am.t("write failed: ", "写入文件失败: ")+err.Error(), tipError)
			}
			return am, setTip(am.t("sync completed", "同步成功"), tipSuccess)
		} else if msg.String() == "enter" {
			bookmark, port, err := buildConnectTarget()
			if err != nil {
				return am, setTip(err.Error(), tipWarn)
			}
			authMode := authModeText(bookmark)

			sshClient, err := buildSSHClient(bookmark, port)
			if err != nil {
				return am, setTip(fmt.Sprintf(am.t("connection config failed %s@%s:%d (%s): %s", "连接配置失败 %s@%s:%d (%s): %s"), bookmark.Username, bookmark.Host, port, authMode, err.Error()), tipError)
			}

			AM.isConnecting = true
			tipCmd := setTip(fmt.Sprintf(am.t("connecting %s@%s:%d (%s)...", "正在连接 %s@%s:%d (%s)..."), bookmark.Username, bookmark.Host, port, authMode), tipProgress)
			return am, tea.Sequence(tipCmd, probeConnectCmd(sshClient, bookmark, port))
		}
	case tea.WindowSizeMsg:
		selectedIndex := am.list.GlobalIndex()
		AM.width = msg.Width
		AM.height = msg.Height
		if len(AM.BookmarkInfo.List) > 0 {
			createListWithSelection(selectedIndex)
		}
	case connectResultMsg:
		AM.isConnecting = false
		var tipCmd tea.Cmd
		if msg.Success {
			tipCmd = setTip(msg.Tip, tipSuccess)
			return am, tea.Batch(tipCmd, func() tea.Msg { return refreshMsg{} })
		}
		tipCmd = setTip(msg.Tip, tipError)
		return am, tipCmd
	case probeResultMsg:
		if !msg.Success {
			AM.isConnecting = false
			return am, setTip(msg.Tip, tipError)
		}
		AM.TipString = msg.Tip
		AM.tipLevel = tipProgress
		AM.tipSeq++
		tipCmd := tea.Cmd(nil)
		execCmd := connectExecCmd(msg.SSHClient, msg.SuccessTip, msg.FailurePrefix)
		return am, tea.Sequence(tipCmd, execCmd)
	case clearTipMsg:
		if msg.Seq == AM.tipSeq {
			AM.TipString = ""
		}
		return am, nil
	}

	var cmd tea.Cmd
	am.list, cmd = am.list.Update(msg)
	return am, cmd
}

func (am *AppModel) View() string {
	am.applyListLocale()
	listView := am.list.View()
	if strings.TrimSpace(am.TipString) == "" {
		return docStyle.Render(listView)
	}
	frameWidth, frameHeight := getListSize(AM.width, AM.height)
	if frameWidth <= 0 {
		frameWidth = lipgloss.Width(listView)
	}
	if frameHeight <= 0 {
		frameHeight = lipgloss.Height(listView)
	}

	maxTipWidth := frameWidth / 2
	if maxTipWidth < 24 {
		maxTipWidth = 24
	}
	if maxTipWidth > frameWidth-8 {
		maxTipWidth = frameWidth - 8
	}
	if maxTipWidth < 16 {
		maxTipWidth = 16
	}
	tipText := ansi.Truncate(AM.TipString, maxTipWidth, "…")

	tipOverlay := lipgloss.NewStyle().
		Foreground(lipgloss.Color("248")).
		Background(lipgloss.Color("238")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("242")).
		Padding(0, 0).
		Render(tipText)

	switch AM.tipLevel {
	case tipSuccess:
		tipOverlay = lipgloss.NewStyle().
			Foreground(lipgloss.Color("84")).
			Background(lipgloss.Color("235")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("70")).
			Padding(0, 0).
			Render(tipText)
	case tipWarn:
		tipOverlay = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Background(lipgloss.Color("235")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("208")).
			Padding(0, 0).
			Render(tipText)
	case tipError:
		tipOverlay = lipgloss.NewStyle().
			Foreground(lipgloss.Color("203")).
			Background(lipgloss.Color("235")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("196")).
			Padding(0, 0).
			Render(tipText)
	case tipProgress:
		tipOverlay = lipgloss.NewStyle().
			Foreground(lipgloss.Color("111")).
			Background(lipgloss.Color("235")).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("110")).
			Padding(0, 0).
			Render("... " + tipText)
	}

	fullFrame := lipgloss.NewStyle().Width(frameWidth).Height(frameHeight).Render(listView)
	view := fullFrame
	if AM.isConnecting {
		dimmed := dimBaseForOverlay(view)
		overlayLayer := buildConnectingOverlay(frameWidth, frameHeight)
		view = overlayCenter(dimmed, overlayLayer)
	}
	view = overlayTopRight(view, tipOverlay)
	return docStyle.Render(view)
}
