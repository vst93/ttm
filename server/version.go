package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var Version = "dev"

type githubRelease struct {
	TagName string `json:"tag_name"`
}

type updateCheckResult struct {
	Available   bool
	Latest      string
	Current     string
	Err         error
	Unreachable bool
}

type updateCheckMsg struct {
	Result updateCheckResult
}

func cleanVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}

func isNewer(current, latest string) bool {
	c := cleanVersion(current)
	l := cleanVersion(latest)
	if c == "dev" || c == "" {
		return l != ""
	}
	return l != c
}

func checkUpdate() updateCheckResult {
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/vst93/ttm/releases/latest")
	if err != nil {
		return updateCheckResult{Err: err, Unreachable: true}
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return updateCheckResult{
			Err: fmt.Errorf("GitHub API returned %d", resp.StatusCode),
		}
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return updateCheckResult{Err: fmt.Errorf("failed to parse release: %w", err)}
	}

	latest := release.TagName
	if !isNewer(Version, latest) {
		return updateCheckResult{
			Available: false,
			Current:   Version,
			Latest:    latest,
		}
	}

	return updateCheckResult{
		Available: true,
		Current:   Version,
		Latest:    latest,
	}
}
