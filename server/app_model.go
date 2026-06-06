package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/paginator"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/crypto/ssh"
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

type syncUploadMsg struct {
	Err    error
	GistID string
}

type syncDownloadMsg struct {
	Err       error
	Bookmarks []BookmarkItem
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

	text := string(i)
	num := fmt.Sprintf("%2d", index+1)
	title := text
	detail := ""
	if idx := strings.Index(text, " · "); idx >= 0 {
		title = text[:idx]
		detail = text[idx+len(" · "):]
	}

	starred := false
	if after, found := strings.CutPrefix(title, "★ "); found {
		title = after
		starred = true
	}

	selected := index == m.Index()
	detailRendered := ""
	if detail != "" {
		detailRendered = renderDetail(detail, selected)
	}

	dimSep := lipgloss.NewStyle().Foreground(lipgloss.Color("238")).Render("│")

	starBadge := ""
	if starred {
		if selected {
			starBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Render("★") + " "
		} else {
			starBadge = lipgloss.NewStyle().Foreground(lipgloss.Color("172")).Render("★") + " "
		}
	}

	if selected {
		indicator := lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true).Render("▸ ")
		numStr := lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(num)
		titleStr := lipgloss.NewStyle().Foreground(lipgloss.Color("255")).Bold(true).Render(title)
		line := indicator + numStr + " " + starBadge + titleStr
		if detailRendered != "" {
			line += " " + dimSep + " " + detailRendered
		}
		fmt.Fprint(w, line)
	} else {
		numStr := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(num)
		titleStr := lipgloss.NewStyle().Foreground(lipgloss.Color("250")).Render(title)
		line := "  " + numStr + " " + starBadge + titleStr
		if detailRendered != "" {
			line += " " + dimSep + " " + detailRendered
		}
		fmt.Fprint(w, line)
	}
}

func renderDetail(detail string, selected bool) string {
	user, rest := detail, ""
	if at := strings.LastIndex(detail, "@"); at >= 0 {
		user = detail[:at]
		rest = detail[at+1:]
	} else {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(detail)
	}
	host, port := rest, ""
	if colon := strings.LastIndex(rest, ":"); colon >= 0 {
		host = rest[:colon]
		port = rest[colon+1:]
	}
	if selected {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("109")).Render(user) +
			lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render("@") +
			lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Render(host) +
			lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(":") +
			lipgloss.NewStyle().Foreground(lipgloss.Color("109")).Render(port)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(user) +
		lipgloss.NewStyle().Foreground(lipgloss.Color("239")).Render("@") +
		lipgloss.NewStyle().Foreground(lipgloss.Color("247")).Render(host) +
		lipgloss.NewStyle().Foreground(lipgloss.Color("239")).Render(":") +
		lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(port)
}

type AppModel struct {
	GistConfig
	BookmarkInfo
	list                list.Model
	TipString           string
	tipLevel            tipLevel
	tipSeq              int
	locale              locale
	connectKey          key.Binding
	syncKey             key.Binding
	addKey              key.Binding
	editKey             key.Binding
	deleteKey           key.Binding
	starKey             key.Binding
	configKey           key.Binding
	updateKey           key.Binding
	langToggleKey       key.Binding
	editor              *bookmarkEditor
	configEditor        *configEditor
	pendingDelete       *deleteConfirmState
	pendingSync         *syncConfirmState
	pendingConfigExport *configExportState
	isConnecting        bool
	isUpdating          bool
	isSyncing           bool
	enterKeyAt          time.Time
	width               int
	height              int
}

var AM = AppModel{}
var docStyle = lipgloss.NewStyle().Margin(0, 1)

func (am *AppModel) Init() tea.Cmd {
	_, am.GistConfig = InitConfig()
	am.locale = localeEN
	am.applyPersistedLocale()
	am.connectKey = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("⏎", "connect"),
	)
	am.syncKey = key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s/S", "push/pull"),
	)
	am.addKey = key.NewBinding(
		key.WithKeys("a"),
		key.WithHelp("a", "add"),
	)
	am.editKey = key.NewBinding(
		key.WithKeys("e"),
		key.WithHelp("e", "edit"),
	)
	am.deleteKey = key.NewBinding(
		key.WithKeys("d"),
		key.WithHelp("d", "delete"),
	)
	am.starKey = key.NewBinding(
		key.WithKeys("*"),
		key.WithHelp("*", "star"),
	)
	am.configKey = key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "config"),
	)
	am.updateKey = key.NewBinding(
		key.WithKeys("u"),
		key.WithHelp("u", "update"),
	)
	am.langToggleKey = key.NewBinding(
		key.WithKeys("L"),
		key.WithHelp("L", "lang"),
	)
	return func() tea.Msg { return initMsg{} }
}

func (am *AppModel) t(en, zh string) string {
	if am.locale == localeZH {
		return zh
	}
	return en
}

func (am *AppModel) checkSyncConfig(requireGistID bool) tea.Cmd {
	if AM.Token == "" {
		return setTip(am.t(
			"token not configured, press c to open config",
			"未配置 token，按 c 打开配置",
		), tipWarn)
	}
	if requireGistID && AM.GistID == "" {
		return setTip(am.t(
			"gist_id not configured, press c to open config",
			"未配置 gist_id，按 c 打开配置",
		), tipWarn)
	}
	return nil
}

func (am *AppModel) refreshList() {
	createList()
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
		am.list.Title = "TTM " + lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(Version)
		am.list.KeyMap.CursorUp.SetHelp("↑/k", "上移")
		am.list.KeyMap.CursorDown.SetHelp("↓/j", "下移")
		am.list.KeyMap.PrevPage.SetHelp("←/h", "上一页")
		am.list.KeyMap.NextPage.SetHelp("→/l", "下一页")
		am.list.KeyMap.GoToStart.SetHelp("g", "到开头")
		am.list.KeyMap.GoToEnd.SetHelp("G", "到结尾")
		am.list.KeyMap.Filter.SetHelp("/", "过滤")
		am.list.KeyMap.ClearFilter.SetHelp("esc", "清除过滤")
		am.list.KeyMap.CancelWhileFiltering.SetHelp("esc", "取消")
		am.list.KeyMap.AcceptWhileFiltering.SetHelp("enter", "应用过滤")
		am.list.KeyMap.ShowFullHelp.SetHelp("?", "更多")
		am.list.KeyMap.CloseFullHelp.SetHelp("?", "收起")
		am.list.KeyMap.Quit.SetHelp("q", "退出")
		am.list.SetStatusBarItemName("书签", "书签")
		am.list.Paginator.Type = paginator.Arabic
		am.list.Paginator.ArabicFormat = "第%d/%d页"
		am.connectKey.SetHelp("⏎", "连接")
		am.syncKey.SetHelp("s/S", "推送/拉取")
		am.addKey.SetHelp("a", "新增")
		am.editKey.SetHelp("e", "编辑")
		am.deleteKey.SetHelp("d", "删除")
		am.starKey.SetHelp("*", "标星")
		am.configKey.SetHelp("c", "配置")
		am.updateKey.SetHelp("u", "更新")
		am.langToggleKey.SetHelp("L", "语言")
		return
	}

	am.list.Title = "TTM " + lipgloss.NewStyle().Foreground(lipgloss.Color("243")).Render(Version)
	am.list.KeyMap.CursorUp.SetHelp("↑/k", "up")
	am.list.KeyMap.CursorDown.SetHelp("↓/j", "down")
	am.list.KeyMap.PrevPage.SetHelp("←/h", "prev page")
	am.list.KeyMap.NextPage.SetHelp("→/l", "next page")
	am.list.KeyMap.GoToStart.SetHelp("g", "start")
	am.list.KeyMap.GoToEnd.SetHelp("G", "end")
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
	am.connectKey.SetHelp("⏎", "connect")
	am.syncKey.SetHelp("s/S", "push/pull")
	am.addKey.SetHelp("a", "add")
	am.editKey.SetHelp("e", "edit")
	am.deleteKey.SetHelp("d", "delete")
	am.starKey.SetHelp("*", "star")
	am.configKey.SetHelp("c", "config")
	am.updateKey.SetHelp("u", "update")
	am.langToggleKey.SetHelp("L", "lang")
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
		port := info.Port
		if port <= 0 {
			port = 22
		}
		user := info.Username
		if user == "" {
			user = "root"
		}
		star := ""
		if info.Starred {
			star = "★ "
		}
		title := info.Title
		if title == "" {
			title = info.Host
		}
		items[i] = item(fmt.Sprintf("%s%s · %s@%s:%d", star, title, user, info.Host, port))
	}
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
	AM.list.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 0, 2)
	AM.list.Styles.Title = lipgloss.NewStyle().
		Foreground(lipgloss.Color("75")).
		Bold(true)
	AM.list.Styles.StatusBar = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Padding(0, 0, 1, 2)
	AM.list.Styles.StatusBarActiveFilter = lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))
	AM.list.Styles.StatusBarFilterCount = lipgloss.NewStyle().
		Foreground(lipgloss.Color("75"))
	AM.list.Styles.NoItems = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Padding(0, 0, 0, 2)
	AM.list.Styles.ArabicPagination = lipgloss.NewStyle().
		Foreground(lipgloss.Color("243"))
	AM.list.Styles.ActivePaginationDot = lipgloss.NewStyle().
		Foreground(lipgloss.Color("75")).
		SetString("●")
	AM.list.Styles.InactivePaginationDot = lipgloss.NewStyle().
		Foreground(lipgloss.Color("238")).
		SetString("○")
	AM.list.Styles.PaginationStyle = lipgloss.NewStyle().
		PaddingLeft(2)
	AM.list.Styles.HelpStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Padding(0, 0, 0, 2)
	AM.list.Styles.DividerDot = lipgloss.NewStyle().
		Foreground(lipgloss.Color("238")).
		SetString(" · ")
	AM.list.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{AM.connectKey, AM.syncKey, AM.addKey, AM.starKey, AM.configKey, AM.langToggleKey}
	}
	AM.list.AdditionalFullHelpKeys = func() []key.Binding {
		return []key.Binding{AM.connectKey, AM.syncKey, AM.addKey, AM.editKey, AM.deleteKey, AM.starKey, AM.configKey, AM.updateKey, AM.langToggleKey}
	}
	// Re-trigger pagination calc after styles changed (padding differs from defaults)
	AM.list.SetSize(listWidth, listHeight)
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
	authType := strings.TrimSpace(bookmark.AuthType)
	if authType == "keyboard-interactive" {
		return AM.t("interactive login (keyboard-interactive)", "交互登录 (keyboard-interactive)")
	}
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

func describeConnectError(err error) string {
	if err == nil {
		return ""
	}

	detail := err.Error()
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return AM.t("Reason: network timeout while connecting to host\nCheck: host, port, VPN, firewall, or server reachability\nDetail: ", "原因：连接主机网络超时\n建议：检查主机、端口、VPN、防火墙或服务器可达性\n详情：") + detail
	}

	msg := strings.ToLower(detail)
	switch {
	case strings.Contains(msg, "connection refused"):
		return AM.t("Reason: SSH port refused the connection\nCheck: port number and sshd service status on the server\nDetail: ", "原因：SSH 端口拒绝连接\n建议：检查端口号以及服务器上的 sshd 服务状态\n详情：") + detail
	case strings.Contains(msg, "no route to host") || strings.Contains(msg, "network is unreachable"):
		return AM.t("Reason: host is unreachable from this network\nCheck: network route, VPN, firewall, or server security group\nDetail: ", "原因：当前网络无法到达主机\n建议：检查网络路由、VPN、防火墙或服务器安全组\n详情：") + detail
	case strings.Contains(msg, "no such host") || strings.Contains(msg, "server misbehaving"):
		return AM.t("Reason: host name cannot be resolved\nCheck: host address spelling and DNS configuration\nDetail: ", "原因：主机名无法解析\n建议：检查主机地址拼写和 DNS 配置\n详情：") + detail
	case strings.Contains(msg, "unable to authenticate") || strings.Contains(msg, "no supported methods remain") || strings.Contains(msg, "permission denied"):
		return AM.t("Reason: authentication failed\nCheck: username, password, private key, passphrase, and auth type\nDetail: ", "原因：认证失败\n建议：检查用户名、密码、私钥、密钥密码和认证方式\n详情：") + detail
	case strings.Contains(msg, "knownhosts") || strings.Contains(msg, "host key"):
		return AM.t("Reason: host key verification failed\nCheck: server fingerprint or known_hosts entry\nDetail: ", "原因：主机密钥校验失败\n建议：检查服务器指纹或 known_hosts 记录\n详情：") + detail
	case strings.Contains(msg, "request remote pty") || strings.Contains(msg, "pty"):
		return AM.t("Reason: remote PTY allocation failed\nCheck: whether the server allows PTY and whether the terminal type is supported\nDetail: ", "原因：远端 PTY 分配失败\n建议：检查服务器是否允许 PTY，以及终端类型是否受支持\n详情：") + detail
	case strings.Contains(msg, "remote shell exited"):
		// Check for ExitMissingError — server closed the session channel
		// without sending an exit-status or exit-signal. This usually
		// means the remote shell process never properly started or was
		// killed immediately.
		var exitMissingErr *ssh.ExitMissingError
		if errors.As(err, &exitMissingErr) {
			return AM.t("Reason: remote shell exited — no exit status received from the server\nCheck: the remote user's default shell (e.g., /etc/passwd); the shell may need to be reinstalled or reconfigured\nDetail: ", "原因：远端 shell 退出 — 未从服务器收到退出状态\n建议：检查远程用户的默认 shell（如 /etc/passwd）；可能需要重新安装或配置 shell\n详情：") + detail
		}
		return AM.t("Reason: remote shell exited with an error\nCheck: the command output above; remote TUI programs may need TERM and UTF-8 locale support\nDetail: ", "原因：远端 shell 异常退出\n建议：查看上方命令输出；远端 TUI 程序可能需要 TERM 和 UTF-8 locale 支持\n详情：") + detail
	}

	return AM.t("Reason: SSH connection failed\nCheck: the detail below and the remote command output if any\nDetail: ", "原因：SSH 连接失败\n建议：查看下方详情以及可能存在的远端命令输出\n详情：") + detail
}

func connectFailureTip(prefix string, err error) string {
	return prefix + "\n" + describeConnectError(err)
}

func connectEnterRepeated(now time.Time, last time.Time) bool {
	return !last.IsZero() && now.Sub(last) < 500*time.Millisecond
}

func connectExecCmd(sshClient *defaultClient, successTip, failurePrefix string) tea.Cmd {
	return tea.Exec(sshLoginExecCommand{client: sshClient}, func(err error) tea.Msg {
		if err != nil {
			return connectResultMsg{Success: false, Tip: connectFailureTip(failurePrefix, err)}
		}
		return connectResultMsg{Success: true, Tip: successTip}
	})
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

func wrapTipLine(line string, maxWidth int) []string {
	if line == "" {
		return []string{""}
	}

	var lines []string
	var current strings.Builder
	currentWidth := 0
	for _, r := range line {
		part := string(r)
		partWidth := ansi.StringWidth(part)
		if currentWidth > 0 && currentWidth+partWidth > maxWidth {
			lines = append(lines, current.String())
			current.Reset()
			currentWidth = 0
		}
		current.WriteString(part)
		currentWidth += partWidth
	}
	if current.Len() > 0 {
		lines = append(lines, current.String())
	}
	return lines
}

func wrapTipText(text string, maxWidth int) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		lines = append(lines, wrapTipLine(line, maxWidth)...)
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func buildTipText(text string, level tipLevel, maxWidth, maxHeight int) string {
	if maxWidth < 16 {
		maxWidth = 16
	}
	if maxHeight < 1 {
		maxHeight = 1
	}

	prefix := ""
	switch level {
	case tipProgress:
		prefix = "⋯ "
	case tipSuccess:
		prefix = "✓ "
	case tipWarn:
		prefix = "! "
	case tipError:
		prefix = "✗ "
	}

	lines := wrapTipText(text, maxWidth)
	lines[0] = prefix + lines[0]
	if ansi.StringWidth(lines[0]) > maxWidth {
		lines[0] = ansi.Truncate(lines[0], maxWidth, "…")
	}
	if len(lines) > maxHeight {
		lines = lines[:maxHeight]
		lines[maxHeight-1] = ansi.Truncate(lines[maxHeight-1], maxWidth, "…")
	}
	return strings.Join(lines, "\n")
}

func tipStyle(level tipLevel) lipgloss.Style {
	base := lipgloss.NewStyle().
		Background(lipgloss.Color("235")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("238")).
		Padding(0, 1)
	switch level {
	case tipSuccess:
		return base.Foreground(lipgloss.Color("78"))
	case tipWarn:
		return base.Foreground(lipgloss.Color("178"))
	case tipError:
		return base.Foreground(lipgloss.Color("203"))
	case tipProgress:
		return base.Foreground(lipgloss.Color("75"))
	default:
		return base.Foreground(lipgloss.Color("252"))
	}
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
		Background(lipgloss.Color("236")).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		Render("")

	badge := lipgloss.NewStyle().
		Foreground(lipgloss.Color("75")).
		Background(lipgloss.Color("237")).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 2).
		Render("⋯ " + AM.t("Connecting", "正在连接"))

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
		createList()
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
		}

		if AM.pendingConfigExport != nil {
			return am, am.handleConfigExportKey(msg)
		}

		if AM.editor != nil {
			return am, am.handleEditorKey(msg)
		}

		if AM.configEditor != nil {
			return am, am.handleConfigEditorKey(msg)
		}

		if AM.pendingDelete != nil {
			return am, am.handleDeleteConfirmKey(msg)
		}

		if AM.pendingSync != nil {
			return am, am.handleSyncConfirmKey(msg)
		}

		isFiltering := am.list.FilterState() == list.Filtering
		if !isFiltering && msg.String() == "L" {
			am.toggleLocale()
			am.applyListLocale()
			if am.locale == localeZH {
				return am, setTip("语言已切换为中文", tipInfo)
			}
			return am, setTip("language switched to English", tipInfo)
		} else if !isFiltering && msg.String() == "a" {
			return am, am.openAddBookmarkEditor()
		} else if !isFiltering && msg.String() == "e" {
			return am, am.openEditBookmarkEditor()
		} else if !isFiltering && msg.String() == "d" {
			return am, am.openDeleteBookmarkConfirm()
		} else if !isFiltering && msg.String() == "*" {
			return am, am.toggleStar()
		} else if !isFiltering && msg.String() == "c" {
			return am, am.openConfigEditor()
		} else if !isFiltering && msg.String() == "u" {
			if AM.isUpdating {
				return am, nil
			}
			AM.isUpdating = true
			tipCmd := setTip(am.t("checking for updates...", "正在检查更新..."), tipProgress)
			return am, tea.Batch(tipCmd, func() tea.Msg {
				return updateCheckMsg{Result: checkUpdate()}
			})
		} else if !isFiltering && (msg.String() == "s" || msg.String() == "S") {
			requireGistID := msg.String() == "S"
			if tip := am.checkSyncConfig(requireGistID); tip != nil {
				return am, tip
			}
			if AM.isSyncing {
				return am, nil
			}
			if msg.String() == "s" {
				return am, am.openSyncConfirm(syncActionPush)
			}
			return am, am.openSyncConfirm(syncActionPull)
		} else if msg.String() == "enter" {
			now := time.Now()
			if connectEnterRepeated(now, AM.enterKeyAt) {
				return am, nil
			}
			AM.enterKeyAt = now

			bookmark, port, err := buildConnectTarget()
			if err != nil {
				return am, setTip(err.Error(), tipWarn)
			}
			authMode := authModeText(bookmark)

			sshClient, err := buildSSHClient(bookmark, port)
			if err != nil {
				return am, setTip(connectFailureTip(fmt.Sprintf(am.t("connection config failed %s@%s:%d (%s): ", "连接配置失败 %s@%s:%d (%s): "), bookmark.Username, bookmark.Host, port, authMode), err), tipError)
			}

			AM.isConnecting = true
			tipCmd := setTip(fmt.Sprintf(am.t("connecting %s@%s:%d (%s)...", "正在连接 %s@%s:%d (%s)..."), bookmark.Username, bookmark.Host, port, authMode), tipProgress)
			successTip := fmt.Sprintf(am.t("session closed %s@%s:%d (%s)", "会话结束 %s@%s:%d (%s)"), bookmark.Username, bookmark.Host, port, authMode)
			failurePrefix := fmt.Sprintf(am.t("connection failed %s@%s:%d (%s): ", "连接失败 %s@%s:%d (%s): "), bookmark.Username, bookmark.Host, port, authMode)
			return am, tea.Sequence(tipCmd, connectExecCmd(sshClient, successTip, failurePrefix))
		}
		// Navigation wrapping
		if am.list.FilterState() == list.Unfiltered {
			totalItems := len(AM.BookmarkInfo.List)
			currentIndex := am.list.GlobalIndex()
			if totalItems > 1 {
				switch msg.String() {
				case "up", "k":
					if currentIndex <= 0 {
						am.list.Select(totalItems - 1)
						if am.list.Paginator.PerPage > 0 {
							am.list.Paginator.Page = (totalItems - 1) / am.list.Paginator.PerPage
						}
						return am, nil
					}
				case "down", "j":
					if currentIndex >= totalItems-1 {
						am.list.Select(0)
						am.list.Paginator.Page = 0
						return am, nil
					}
				}
			}
			if am.list.Paginator.TotalPages > 1 {
				switch msg.String() {
				case "left", "h", "pgup":
					if am.list.Paginator.Page <= 0 {
						am.list.Paginator.Page = am.list.Paginator.TotalPages - 1
						lastPageStart := am.list.Paginator.PerPage * (am.list.Paginator.TotalPages - 1)
						if lastPageStart >= totalItems {
							lastPageStart = totalItems - 1
						}
						am.list.Select(lastPageStart)
						return am, nil
					}
				case "right", "l", "pgdn":
					if am.list.Paginator.Page >= am.list.Paginator.TotalPages-1 {
						am.list.Paginator.Page = 0
						am.list.Select(0)
						return am, nil
					}
				}
			}
		}
	case tea.WindowSizeMsg:
		selectedIndex := am.list.GlobalIndex()
		AM.width = msg.Width
		AM.height = msg.Height
		createListWithSelection(selectedIndex)
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
	case updateCheckMsg:
		AM.isUpdating = false
		r := msg.Result
		if r.Err != nil {
			if r.Unreachable {
				return am, setTip(am.t("cannot reach GitHub, check network", "无法连接 GitHub，请检查网络"), tipWarn)
			}
			return am, setTip(am.t("update check failed: ", "检查更新失败: ")+r.Err.Error(), tipError)
		}
		if !r.Available {
			return am, setTip(am.t("already latest "+r.Latest, "已是最新版本 "+r.Latest), tipSuccess)
		}
		hint := r.UpdateHint
		if hint == "" {
			hint = fmt.Sprintf("https://github.com/vst93/ttm/releases/tag/%s", r.Latest)
		}
		return am, setTip(fmt.Sprintf(am.t("new version %s available: %s", "发现新版本 %s: %s"), r.Latest, hint), tipInfo)
	case syncUploadMsg:
		AM.isSyncing = false
		if msg.Err != nil {
			return am, setTip(am.t("push failed: ", "推送失败: ")+msg.Err.Error(), tipError)
		}
		if msg.GistID != "" {
			AM.GistID = msg.GistID
			am.GistConfig.GistID = msg.GistID
			if err := SaveConfig(am.GistConfig); err != nil {
				return am, setTip(am.t("write config failed: ", "写入配置失败: ")+err.Error(), tipError)
			}
		}
		jsonStr, err := json.MarshalIndent(am.BookmarkInfo.List, "", "  ")
		if err != nil {
			return am, setTip(am.t("save failed: ", "保存失败: ")+err.Error(), tipError)
		}
		if err := os.WriteFile(APP_DIR+"/bookmarks.json", jsonStr, 0600); err != nil {
			return am, setTip(am.t("write failed: ", "写入文件失败: ")+err.Error(), tipError)
		}
		return am, setTip(am.t("pushed to gist", "已推送到 Gist"), tipSuccess)
	case syncDownloadMsg:
		AM.isSyncing = false
		if msg.Err != nil {
			return am, setTip(am.t("pull failed: ", "拉取失败: ")+msg.Err.Error(), tipError)
		}
		AM.BookmarkInfo.List = msg.Bookmarks
		sortBookmarksByStarred(AM.BookmarkInfo.List)
		jsonStr, err := json.MarshalIndent(AM.BookmarkInfo.List, "", "  ")
		if err != nil {
			return am, setTip(am.t("save failed: ", "保存失败: ")+err.Error(), tipError)
		}
		if err := os.WriteFile(APP_DIR+"/bookmarks.json", jsonStr, 0600); err != nil {
			return am, setTip(am.t("write failed: ", "写入文件失败: ")+err.Error(), tipError)
		}
		am.refreshList()
		return am, setTip(fmt.Sprintf(am.t("pulled %d bookmarks from gist", "已从 Gist 拉取 %d 条书签"), len(msg.Bookmarks)), tipSuccess)
	case clearTipMsg:
		if msg.Seq == AM.tipSeq {
			AM.TipString = ""
		}
		return am, nil
	}

	if AM.configEditor != nil {
		focused := AM.configEditor.focusedField()
		if focused != configFieldPlatform {
			idx := int(focused)
			updated, cmd := AM.configEditor.inputs[idx].Update(msg)
			AM.configEditor.inputs[idx] = updated
			return am, cmd
		}
		return am, nil
	}

	if AM.editor != nil {
		focusField := AM.editor.focusedField()
		if focusField != editorFieldAuthType {
			idx := int(focusField)
			updated, cmd := AM.editor.inputs[idx].Update(msg)
			AM.editor.inputs[idx] = updated
			return am, cmd
		}
		return am, nil
	}

	var cmd tea.Cmd
	am.list, cmd = am.list.Update(msg)
	return am, cmd
}

// compactListView rearranges the bubbles list output so that:
//   - items stay at the top (no trailing blank padding)
//   - pagination + help bar are pinned together at the bottom
//   - the natural gap between them fills the remaining height
func compactListView(s string, totalHeight int) string {
	lines := strings.Split(s, "\n")

	// The bubbles list View renders sections in order:
	//   title, status, content(Height=availHeight), pagination, help
	// The content block has blank-line padding after items.
	// We strip that padding and let the gap push footer to the bottom.

	// Count footer lines from the bottom (pagination + help, always last).
	// These are contiguous non-blank lines at the tail of the output.
	end := len(lines)
	for end > 0 && strings.TrimSpace(ansi.Strip(lines[end-1])) == "" {
		end--
	}
	footerEnd := end
	footerStart := footerEnd
	for footerStart > 0 && strings.TrimSpace(ansi.Strip(lines[footerStart-1])) != "" {
		footerStart--
	}
	footer := lines[footerStart:footerEnd]

	// Content: everything above footer, trim trailing blanks (the padding).
	contentEnd := footerStart
	for contentEnd > 0 && strings.TrimSpace(ansi.Strip(lines[contentEnd-1])) == "" {
		contentEnd--
	}
	content := lines[:contentEnd]

	gap := totalHeight - len(content) - len(footer)
	if gap < 1 {
		gap = 1
	}

	result := make([]string, 0, totalHeight)
	result = append(result, content...)
	for i := 0; i < gap; i++ {
		result = append(result, "")
	}
	result = append(result, footer...)
	return strings.Join(result, "\n")
}

func (am *AppModel) View() string {
	am.applyListLocale()
	frameWidth, frameHeight := getListSize(AM.width, AM.height)
	rawView := am.list.View()
	if frameWidth <= 0 {
		frameWidth = lipgloss.Width(rawView)
	}
	if frameHeight <= 0 {
		frameHeight = lipgloss.Height(rawView)
	}
	listView := compactListView(rawView, frameHeight)
	hasTip := strings.TrimSpace(am.TipString) != ""
	if !hasTip && !AM.isConnecting && AM.editor == nil && AM.configEditor == nil && AM.pendingDelete == nil && AM.pendingSync == nil && AM.pendingConfigExport == nil {
		return docStyle.Render(listView)
	}

	tipOverlay := ""
	if hasTip {
		maxTipWidth := (frameWidth * 2) / 3
		if maxTipWidth < 32 {
			maxTipWidth = 32
		}
		if maxTipWidth > frameWidth-8 {
			maxTipWidth = frameWidth - 8
		}
		maxTipHeight := frameHeight / 3
		if maxTipHeight < 4 {
			maxTipHeight = 4
		}
		if maxTipHeight > frameHeight-4 {
			maxTipHeight = frameHeight - 4
		}
		if maxTipHeight < 1 {
			maxTipHeight = 1
		}
		tipText := buildTipText(AM.TipString, AM.tipLevel, maxTipWidth, maxTipHeight)
		tipOverlay = tipStyle(AM.tipLevel).Render(tipText)
	}

	if AM.editor != nil {
		view := am.buildEditorOverlay(frameWidth, frameHeight)
		if tipOverlay != "" {
			view = overlayTopRight(view, tipOverlay)
		}
		return docStyle.Render(view)
	}

	if AM.configEditor != nil {
		view := am.buildConfigEditorOverlay(frameWidth, frameHeight)
		if AM.pendingConfigExport != nil {
			dimmed := dimBaseForOverlay(view)
			overlayLayer := am.buildConfigExportOverlay(frameWidth)
			view = overlayCenter(dimmed, overlayLayer)
		} else if tipOverlay != "" {
			view = overlayTopRight(view, tipOverlay)
		}
		return docStyle.Render(view)
	}

	fullFrame := lipgloss.NewStyle().Width(frameWidth).Height(frameHeight).Render(listView)
	view := fullFrame
	if AM.isConnecting {
		dimmed := dimBaseForOverlay(view)
		overlayLayer := buildConnectingOverlay(frameWidth, frameHeight)
		view = overlayCenter(dimmed, overlayLayer)
	}
	if AM.pendingDelete != nil {
		dimmed := dimBaseForOverlay(view)
		overlayLayer := am.buildDeleteConfirmOverlay(frameWidth)
		view = overlayCenter(dimmed, overlayLayer)
	}
	if AM.pendingSync != nil {
		dimmed := dimBaseForOverlay(view)
		overlayLayer := am.buildSyncConfirmOverlay(frameWidth)
		view = overlayCenter(dimmed, overlayLayer)
	}
	if AM.pendingConfigExport != nil {
		dimmed := dimBaseForOverlay(view)
		overlayLayer := am.buildConfigExportOverlay(frameWidth)
		view = overlayCenter(dimmed, overlayLayer)
	}
	if tipOverlay != "" {
		view = overlayTopRight(view, tipOverlay)
	}
	return docStyle.Render(view)
}
