package server

import (
	"encoding/json"
	"os"
	"sort"
)

type BookmarkInfo struct {
	List []BookmarkItem
}

type BookmarkItem struct {
	ID                  string `json:"id"`
	Title               string `json:"title"`
	Host                string `json:"host"`
	Username            string `json:"username"`
	AuthType            string `json:"authType"`
	Password            string `json:"password"`
	TmuxScroll          string `json:"tmuxScroll"` // "" | "on" | "off" — remote tmux mouse config after connect
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
	Starred             bool   `json:"starred,omitempty"`
}

func sortBookmarksByStarred(list []BookmarkItem) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Starred != list[j].Starred {
			return list[i].Starred
		}
		return false
	})
}

func (b *BookmarkInfo) Init() error {
	bmFilePath := APP_DIR + "/bookmarks.json"
	if _, err := os.Stat(bmFilePath); err == nil {
		configStr, err := os.ReadFile(bmFilePath)
		if err == nil {
			err = json.Unmarshal(configStr, &AM.BookmarkInfo.List)
			if err != nil {
				AM.TipString = AM.t("failed to parse bookmarks: ", "解析书签失败: ") + err.Error()
			}
		} else {
			AM.TipString = AM.t("failed to read bookmarks: ", "读取书签失败: ") + err.Error()
		}
	}
	sortBookmarksByStarred(AM.BookmarkInfo.List)
	return nil
}
