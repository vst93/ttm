package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type GistResponseItem struct {
	URL         string                          `json:"url"`
	ForksURL    string                          `json:"forks_url"`
	CommitsURL  string                          `json:"commits_url"`
	ID          string                          `json:"id"`
	Description string                          `json:"description"`
	Public      bool                            `json:"public"`
	Owner       GistResponseItemOwner           `json:"owner"`
	User        GistResponseItemOwner           `json:"user"`
	Files       map[string]GistResponseItemFile `json:"files"`
	Truncated   bool                            `json:"truncated"`
	HTMLURL     string                          `json:"html_url"`
	Comments    int64                           `json:"comments"`
	CommentsURL string                          `json:"comments_url"`
	GitPullURL  string                          `json:"git_pull_url"`
	GitPushURL  string                          `json:"git_push_url"`
	CreatedAt   string                          `json:"created_at"`
	UpdatedAt   string                          `json:"updated_at"`
}

type GistResponseItemOwner struct {
	ID                int64  `json:"id"`
	Login             string `json:"login"`
	Name              string `json:"name"`
	AvatarURL         string `json:"avatar_url"`
	URL               string `json:"url"`
	HTMLURL           string `json:"html_url"`
	Remark            string `json:"remark"`
	FollowersURL      string `json:"followers_url"`
	FollowingURL      string `json:"following_url"`
	GistsURL          string `json:"gists_url"`
	StarredURL        string `json:"starred_url"`
	SubscriptionsURL  string `json:"subscriptions_url"`
	OrganizationsURL  string `json:"organizations_url"`
	ReposURL          string `json:"repos_url"`
	EventsURL         string `json:"events_url"`
	ReceivedEventsURL string `json:"received_events_url"`
	Type              string `json:"type"`
}

type GistResponseItemFile struct {
	Size      int64  `json:"size"`
	RawURL    string `json:"raw_url"`
	Type      string `json:"type"`
	Truncated bool   `json:"truncated"`
	Content   string `json:"content"`
}

type GistCreateRequest struct {
	Description string                  `json:"description"`
	Public      bool                    `json:"public"`
	Files       map[string]GistFileData `json:"files"`
}

type GistFileData struct {
	Content string `json:"content"`
}

func getAPIURL(path string) string {
	if AM.Platform == "gitee" {
		return "https://gitee.com/api/v5" + path + "?access_token=" + AM.Token
	}
	return "https://api.github.com" + path + "?access_token=" + AM.Token
}

func GetGist() error {
	if AM.Token == "" {
		return fmt.Errorf("access token is empty")
	}
	apiUrl := getAPIURL("/gists")
	result, err := http.Get(apiUrl)
	if err != nil {
		return err
	}
	defer result.Body.Close()
	if result.StatusCode != 200 {
		return fmt.Errorf("get gist list failed, status code: %d", result.StatusCode)
	}
	gistList := []GistResponseItem{}
	err = json.NewDecoder(result.Body).Decode(&gistList)
	if err != nil {
		return err
	}
	for _, gist := range gistList {
		if gist.ID == AM.GistID {
			files := gist.Files
			for fileName, file := range files {
				if strings.HasSuffix(fileName, ".json") || fileName == "bookmarks.json" {
					var Bookmarks []BookmarkItem
					bookmarkStr := file.Content
					err = json.Unmarshal([]byte(bookmarkStr), &Bookmarks)
					if err != nil {
						return err
					}
					AM.BookmarkInfo.List = Bookmarks
					return nil
				}
			}
			break
		}
	}
	return fmt.Errorf("gist not found")
}

func UploadGist() error {
	if AM.Token == "" {
		return fmt.Errorf("access token is empty")
	}

	bookmarksJSON, err := json.Marshal(AM.BookmarkInfo.List)
	if err != nil {
		return fmt.Errorf("failed to marshal bookmarks: %w", err)
	}

	gistFiles := map[string]GistFileData{
		"bookmarks.json": {
			Content: string(bookmarksJSON),
		},
	}

	gistRequest := GistCreateRequest{
		Description: "SSH Bookmarks synced by ttm",
		Public:      false,
		Files:       gistFiles,
	}

	requestBody, err := json.Marshal(gistRequest)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}

	var apiUrl string
	var httpMethod string

	if AM.GistID == "" {
		apiUrl = getAPIURL("/gists")
		httpMethod = "POST"
	} else {
		apiUrl = getAPIURL("/gists/" + AM.GistID)
		httpMethod = "PATCH"
	}

	req, err := http.NewRequest(httpMethod, apiUrl, bytes.NewReader(requestBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if AM.Platform == "gitee" {
		req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("upload gist failed, status code: %d", resp.StatusCode)
	}

	var gistResponse GistResponseItem
	if err := json.NewDecoder(resp.Body).Decode(&gistResponse); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if AM.GistID == "" && gistResponse.ID != "" {
		AM.GistID = gistResponse.ID
	}

	return nil
}
