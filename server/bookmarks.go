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

type BookmarkInfo struct {
	List []BookmarkItem
}

var (
	itemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
)

type item string

func (i item) FilterValue() string { return string(i) }

const listHeight = 14

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

	fn := itemStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedItemStyle.Render("> " + strings.Join(s, " "))
		}
	}
	fmt.Fprint(w, fn(str))
}

type BookmarkItem struct {
	ID                  string `json:"id"`
	Title               string `json:"title"`
	Host                string `json:"host"`
	Username            string `json:"username"`
	AuthType            string `json:"authType"`
	Password            string `json:"password"`
	Port                int    `json:"port"`
	LoginScriptDelay    int64  `json:"loginScriptDelay"`
	Encode              string `json:"encode"`
	EnableSSH           bool   `json:"enableSsh"`
	EnableSFTP          bool   `json:"enableSftp"`
	EnvLang             string `json:"envLang"`
	Term                string `json:"term"`
	Proxy               string `json:"proxy"`
	PrivateKey          string `json:"privateKey"`
	Passphrase          string `json:"passphrase"`
	StartDirectoryLocal string `json:"startDirectoryLocal"`
	StartDirectory      string `json:"startDirectory"`
}

func (b *BookmarkInfo) Init() error {
	bmFilePath := APP_DIR + "/bookmarks.json"
	// fmt.Println("bmFile path:", bmFilePath)
	AM.TipString = "bmFile:" + bmFilePath
	// 判断文件是否存在，不存在则创建
	if _, err := os.Stat(bmFilePath); err == nil {
		configStr, err := os.ReadFile(bmFilePath)
		if err == nil {
			json.Unmarshal(configStr, &AM.BookmarkInfo.List)
		}
	}
	const defaultWidth = 20
	items := []list.Item{}
	if len(AM.BookmarkInfo.List) > 0 {
		for _, info := range AM.BookmarkInfo.List {
			items = append(items, item(info.Title+"("+info.Host+")"))
		}
	}

	AM.list = list.New(items, itemDelegate{}, defaultWidth, listHeight)

	return nil
}
