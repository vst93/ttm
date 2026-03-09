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
	Locale   string `json:"locale"`
}

func SaveConfig(config GistConfig) error {
	if ConfigFile == "" {
		if err, _ := InitConfig(); err != nil {
			return err
		}
	}
	configStr, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}
	if err := os.WriteFile(ConfigFile, configStr, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}
	return nil
}

func InitConfig() (error, GistConfig) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return err, GistConfig{}
	}
	APP_DIR = configDir + "/ttm"
	if _, err := os.Stat(APP_DIR); os.IsNotExist(err) {
		err = os.Mkdir(APP_DIR, os.ModePerm)
		if err != nil {
			return fmt.Errorf("failed to create config dir: %w", err), GistConfig{}
		}
	}
	ConfigFile = APP_DIR + "/config.json"
	// 判断文件是否存在，不存在则创建
	if _, err := os.Stat(ConfigFile); os.IsNotExist(err) {
		config := GistConfig{
			Platform: "github",
			Token:    "",
			GistID:   "",
			Locale:   "en",
		}
		configStr, _ := json.Marshal(config)
		err = os.WriteFile(ConfigFile, configStr, 0644)
		if err != nil {
			return fmt.Errorf("failed to create config file: %w", err), GistConfig{}
		}
	}
	gistConfig := GistConfig{}
	configStr, err := os.ReadFile(ConfigFile)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err), GistConfig{}
	}
	err = json.Unmarshal(configStr, &gistConfig)
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err), GistConfig{}
	}

	return nil, gistConfig
}
