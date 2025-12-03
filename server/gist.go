package server

import (
	"encoding/json"
	"fmt"
	"net/http"
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

func GetGist() error {
	if AM.Token == "" {
		return fmt.Errorf("access token is empty")
	}
	var apiUrl string
	if AM.Platform == "gitee" {
		apiUrl = "https://gitee.com/api/v5/gists?access_token=" + AM.Token
	} else {
		apiUrl = "https://api.github.com/gists?access_token=" + AM.Token
	}
	// fmt.Println("api url:", apiUrl)
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
				if fileName == "bookmarks.json" {
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
