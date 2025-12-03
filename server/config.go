package server

import (
	"encoding/json"
	"fmt"
	"os"
)

var APP_DIR string
var ConfigFile string = ""

type GistConfig struct {
	Platform string `json:"platform"`
	Token    string `json:"token"`
	GistID   string `json:"gist_id"`
}

func InitConfig() (error, GistConfig) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err, GistConfig{}
	}
	APP_DIR = configDir + "/ttm"
	if _, err := os.Stat(APP_DIR); os.IsNotExist(err) {
		os.Mkdir(APP_DIR, os.ModePerm)
	}
	ConfigFile = APP_DIR + "/config.json"
	fmt.Println("config path:", ConfigFile)
	// 判断文件是否存在，不存在则创建
	if _, err := os.Stat(ConfigFile); os.IsNotExist(err) {
		config := GistConfig{
			Platform: "github",
			Token:    "",
			GistID:   "",
		}
		configStr, _ := json.Marshal(config)
		err = os.WriteFile(ConfigFile, configStr, 0644)
		if err != nil {
			return err, GistConfig{}
		}
	}
	gistConfig := GistConfig{}
	configStr, err := os.ReadFile(ConfigFile)
	if err != nil {
		return err, GistConfig{}
	}
	err = json.Unmarshal(configStr, &gistConfig)
	if err != nil {
		return err, GistConfig{}
	}

	return nil, gistConfig
}

func (g *GistConfig) SyncBookmarks() error {

	return nil
}
