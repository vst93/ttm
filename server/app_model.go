package server

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type AppModel struct {
	GistConfig
	BookmarkInfo
	list      list.Model
	TipString string
}

var AM = AppModel{}
var docStyle = lipgloss.NewStyle().Margin(1, 2)

func (am *AppModel) Init() tea.Cmd {
	_, am.GistConfig = InitConfig()
	return nil
}

func (am *AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return am, tea.Quit
		} else if msg.String() == "s" {
			// 同步 gist
			err := GetGist()
			if err == nil {
				jsonStr, err := json.Marshal(am.BookmarkInfo.List)
				if err == nil {
					err = os.WriteFile(APP_DIR+"/bookmarks.json", jsonStr, 0644)
					if err != nil {
						// fmt.Println("Write config file error:", err)
						AM.TipString = "同步失败"
					}
					AM.BookmarkInfo.Init()
					AM.TipString = "同步成功"
				}
			}
		} else if msg.String() == "enter" {
			// 打开 ssh 连接
			item := am.list.SelectedItem()
			if item != nil {
				for _, bookmark := range AM.BookmarkInfo.List {
					theVal := bookmark.Title + "(" + bookmark.Host + ")"
					if theVal == item.FilterValue() {
						// 执行 ssh 打开 ssh 连接
						// SSH连接参数
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
							fmt.Println("genSSHConfig error:", err)
							AM.TipString = "连接失败" + err.Error()
						} else {
							err = sshClient.Login()
							if err != nil {
								fmt.Println("Login error:", err)
								AM.TipString = "连接失败(002)" + err.Error()
								return am, nil
							}
							AM.TipString = "连接成功"
						}

					}
				}
			}
		}
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		am.list.SetSize(msg.Width-h, msg.Height-v)
	}

	var cmd tea.Cmd
	am.list, cmd = am.list.Update(msg)
	return am, cmd
}

func (am *AppModel) View() string {
	return docStyle.Render(am.list.View(), AM.TipString)
}
