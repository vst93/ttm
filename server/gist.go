package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type GistResponseItem struct {
	ID    string                          `json:"id"`
	Files map[string]GistResponseItemFile `json:"files"`
}

type GistResponseItemFile struct {
	Content string `json:"content"`
}

type GistCreateRequest struct {
	Description string                  `json:"description"`
	Public      bool                    `json:"public"`
	Files       map[string]GistFileData `json:"files"`
}

type GistFileData struct {
	Content string `json:"content"`
}

var gistHTTPClient = &http.Client{Timeout: 15 * time.Second}

func gistRequest(method, url string, body []byte) (*http.Response, error) {
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequest(method, url, bytes.NewReader(body))
	} else {
		req, err = http.NewRequest(method, url, nil)
	}
	if err != nil {
		return nil, err
	}

	if AM.Platform == "gitee" {
		req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	} else {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+AM.Token)
		req.Header.Set("Accept", "application/vnd.github+json")
	}

	return gistHTTPClient.Do(req)
}

func getAPIURL(path string) string {
	if AM.Platform == "gitee" {
		return "https://gitee.com/api/v5" + path + "?access_token=" + AM.Token
	}
	return "https://api.github.com" + path
}

func GetGist() error {
	if AM.Token == "" {
		return fmt.Errorf("access token is empty")
	}
	if AM.GistID == "" {
		return fmt.Errorf("gist_id is empty")
	}

	apiURL := getAPIURL("/gists/" + AM.GistID)
	resp, err := gistRequest("GET", apiURL, nil)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return fmt.Errorf("gist not found, check gist_id")
	}
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("auth failed, check token")
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("get gist failed, status: %d", resp.StatusCode)
	}

	var gist GistResponseItem
	if err := json.NewDecoder(resp.Body).Decode(&gist); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	file, ok := gist.Files["bookmarks.json"]
	if !ok {
		return fmt.Errorf("bookmarks.json not found in gist")
	}

	var bookmarks []BookmarkItem
	if err := json.Unmarshal([]byte(file.Content), &bookmarks); err != nil {
		return fmt.Errorf("failed to parse bookmarks: %w", err)
	}

	AM.BookmarkInfo.List = bookmarks
	return nil
}

func UploadGist() error {
	if AM.Token == "" {
		return fmt.Errorf("access token is empty")
	}

	bookmarksJSON, err := json.MarshalIndent(AM.BookmarkInfo.List, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal bookmarks: %w", err)
	}

	gistFiles := map[string]GistFileData{
		"bookmarks.json": {Content: string(bookmarksJSON)},
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

	var apiURL string
	var httpMethod string

	if AM.GistID == "" {
		apiURL = getAPIURL("/gists")
		httpMethod = "POST"
	} else {
		apiURL = getAPIURL("/gists/" + AM.GistID)
		httpMethod = "PATCH"
	}

	resp, err := gistHTTPClient.Do(func() *http.Request {
		var req *http.Request
		if AM.Platform == "gitee" {
			req, _ = http.NewRequest(httpMethod, apiURL, bytes.NewReader(requestBody))
			req.Header.Set("Content-Type", "application/json;charset=UTF-8")
		} else {
			req, _ = http.NewRequest(httpMethod, apiURL, bytes.NewReader(requestBody))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+AM.Token)
			req.Header.Set("Accept", "application/vnd.github+json")
		}
		return req
	}())
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("auth failed, check token")
	}
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return fmt.Errorf("upload failed, status: %d", resp.StatusCode)
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
