package server

import (
	"encoding/json"
	"os"
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
	if _, err := os.Stat(bmFilePath); err == nil {
		configStr, err := os.ReadFile(bmFilePath)
		if err == nil {
			err = json.Unmarshal(configStr, &AM.BookmarkInfo.List)
			if err != nil {
				AM.TipString = "解析书签失败: " + err.Error()
			}
		} else {
			AM.TipString = "读取书签失败: " + err.Error()
		}
	}
	return nil
}
