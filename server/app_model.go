package server

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type refreshMsg struct{}

func (r refreshMsg) String() string { return "refresh" }

type initMsg struct{}

func (initMsg) String() string { return "init" }

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

	str := fmt.Sprintf("%d. %s", index+1, i)

	fn := lipgloss.NewStyle().PaddingLeft(4).Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170")).Render("> " + strings.Join(s, " "))
		}
	}
	fmt.Fprint(w, fn(str))
}

type AppModel struct {
	GistConfig
	BookmarkInfo
	list      list.Model
	TipString string
	width     int
	height    int
}

var AM = AppModel{}
var docStyle = lipgloss.NewStyle().Margin(1, 2)

func (am *AppModel) Init() tea.Cmd {
	_, am.GistConfig = InitConfig()
	return func() tea.Msg { return initMsg{} }
}

func createList() {
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
	h, v := docStyle.GetFrameSize()
	AM.list = list.New(items, itemDelegate{}, width-h, height-v)
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
		if msg.String() == "ctrl+c" {
			return am, tea.Quit
		} else if msg.String() == "s" {
			if AM.Token == "" {
				AM.TipString = "请先配置 token"
				return am, nil
			}
			if AM.GistID == "" {
				AM.TipString = "请先配置 gist_id"
				return am, nil
			}
			err := UploadGist()
			if err != nil {
				AM.TipString = "上传失败: " + err.Error()
				return am, nil
			}
			jsonStr, err := json.Marshal(am.BookmarkInfo.List)
			if err != nil {
				AM.TipString = "保存失败: " + err.Error()
				return am, nil
			}
			err = os.WriteFile(APP_DIR+"/bookmarks.json", jsonStr, 0644)
			if err != nil {
				AM.TipString = "写入文件失败: " + err.Error()
				return am, nil
			}
			AM.TipString = "同步成功"
		} else if msg.String() == "enter" {
			item := am.list.SelectedItem()
			if item != nil {
				for _, bookmark := range AM.BookmarkInfo.List {
					theVal := bookmark.Title + "(" + bookmark.Host + ")"
					if theVal == item.FilterValue() {
						sshConfig := &SSHConfig{
							Host:           bookmark.Host,
							User:           bookmark.Username,
							Port:           bookmark.Port,
							PrivateKey:     bookmark.PrivateKey,
							Passphrase:     bookmark.Passphrase,
							Password:       bookmark.Password,
							CallbackShells: nil,
						}
						sshClient, err := genSSHConfig(sshConfig)
						if err != nil {
							AM.TipString = "连接配置失败: " + err.Error()
							return am, nil
						}
						err = sshClient.Login()
						if err != nil {
							AM.TipString = "连接失败: " + err.Error()
							return am, nil
						}
						AM.TipString = "连接成功"
						return am, func() tea.Msg { return refreshMsg{} }
					}
				}
			}
		}
	case tea.WindowSizeMsg:
		AM.width = msg.Width
		AM.height = msg.Height
		if len(AM.BookmarkInfo.List) > 0 {
			// 重新创建列表以确保正确渲染
			h, v := docStyle.GetFrameSize()
			width := msg.Width - h
			height := msg.Height - v
			if width <= 0 {
				width = 20
			}
			if height <= 0 {
				height = 14
			}
			items := make([]list.Item, len(AM.BookmarkInfo.List))
			for i, info := range AM.BookmarkInfo.List {
				items[i] = item(info.Title + "(" + info.Host + ")")
			}
			AM.list = list.New(items, itemDelegate{}, width, height)
		}
	}

	var cmd tea.Cmd
	am.list, cmd = am.list.Update(msg)
	return am, cmd
}

func (am *AppModel) View() string {
	return docStyle.Render(am.list.View())
}
