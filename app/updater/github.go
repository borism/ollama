//go:build windows || darwin

package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"

	"github.com/borism/ollama-cluster/app/version"
)

// This fork's releases live on GitHub, which doesn't speak the update
// protocol the stock app expects from ollama.com (a 204 when current, else
// {"url", "version"}). So when there's no UpdateCheckURLBase, look at the
// repo's latest release instead: the newest published (non-draft,
// non-prerelease) one, and its Installer asset.
var (
	UpdateGitHubRepo = "borism/ollama-cluster"
	githubAPIBase    = "https://api.github.com"
)

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (u *Updater) checkGitHubRelease(ctx context.Context) (bool, UpdateResponse) {
	var updateResp UpdateResponse
	if UpdateGitHubRepo == "" {
		return false, updateResp
	}

	// No device ID, no signature: nothing about this install is sent.
	requestURL := fmt.Sprintf("%s/repos/%s/releases/latest", githubAPIBase, UpdateGitHubRepo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		slog.Warn(fmt.Sprintf("failed to check for update: %s", err))
		return false, updateResp
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ollama-cluster-app/"+version.Version)

	slog.Debug("checking for available update", "requestURL", requestURL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn(fmt.Sprintf("failed to check for update: %s", err))
		return false, updateResp
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Warn(fmt.Sprintf("failed to read body response: %s", err))
	}
	if resp.StatusCode != http.StatusOK { // 404: no published release yet
		slog.Info(fmt.Sprintf("check update error %d - %.96s", resp.StatusCode, string(body)))
		return false, updateResp
	}

	var rel githubRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		slog.Warn(fmt.Sprintf("malformed response checking for update: %s", err))
		return false, updateResp
	}
	if !newerRelease(version.Version, rel.TagName) {
		slog.Debug("no newer release", "current", version.Version, "latest", rel.TagName)
		return false, updateResp
	}
	for _, a := range rel.Assets {
		if a.Name == Installer {
			updateResp = UpdateResponse{UpdateURL: a.URL, UpdateVersion: rel.TagName}
			slog.Info("New update available at " + updateResp.UpdateURL)
			return true, updateResp
		}
	}
	slog.Info("newer release has no installer for this platform", "release", rel.TagName, "installer", Installer)
	return false, updateResp
}

var releaseVersion = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:-cluster\.(\d+))?$`)

// newerRelease reports whether tag is a later release than current. This
// fork's versions are the upstream one plus a cluster counter
// ("0.34.4-cluster.2"); no counter counts as 0. Anything else -- a dev
// build's "0.0.0" parses, but "abc" or an -rc tag doesn't -- is never newer,
// so an unrecognized version can't trigger an update.
func newerRelease(current, tag string) bool {
	parse := func(s string) (v [4]int, ok bool) {
		m := releaseVersion.FindStringSubmatch(s)
		if m == nil {
			return v, false
		}
		for i := range v {
			if m[i+1] != "" {
				v[i], _ = strconv.Atoi(m[i+1])
			}
		}
		return v, true
	}
	cur, ok := parse(current)
	if !ok {
		return false
	}
	latest, ok := parse(tag)
	if !ok {
		return false
	}
	for i := range cur {
		if latest[i] != cur[i] {
			return latest[i] > cur[i]
		}
	}
	return false
}
