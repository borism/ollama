//go:build windows || darwin

package updater

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/borism/ollama-cluster/app/version"
)

func TestNewerRelease(t *testing.T) {
	for _, c := range []struct {
		current, tag string
		want         bool
	}{
		{"0.34.4-cluster.1", "v0.34.4-cluster.2", true},
		{"0.34.4-cluster.1", "v0.34.4-cluster.1", false},
		{"0.34.4-cluster.2", "v0.34.4-cluster.1", false},
		{"0.34.4-cluster.9", "v0.35.0-cluster.1", true},
		{"0.34.4", "v0.34.4-cluster.1", true},
		{"0.34.10-cluster.1", "v0.34.9-cluster.5", false}, // numeric, not lexical
		{"0.0.0", "v0.34.4-cluster.1", true},
		{"dev", "v0.34.4-cluster.1", false},
		{"0.34.4-cluster.1", "v0.35.0-rc1", false},
		{"0.34.4-cluster.1", "nightly", false},
	} {
		if got := newerRelease(c.current, c.tag); got != c.want {
			t.Errorf("newerRelease(%q, %q) = %v, want %v", c.current, c.tag, got, c.want)
		}
	}
}

func TestCheckGitHubRelease(t *testing.T) {
	var body string
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/releases/latest" {
			t.Errorf("unexpected request %s", r.URL)
		}
		for _, h := range []string{"Authorization", "Cookie"} {
			if r.Header.Get(h) != "" {
				t.Errorf("update check sent %s", h)
			}
		}
		if r.URL.RawQuery != "" {
			t.Errorf("update check sent a query: %s", r.URL.RawQuery)
		}
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	defer server.Close()

	oldRepo, oldBase, oldURL, oldVersion, oldInstaller := UpdateGitHubRepo, githubAPIBase, UpdateCheckURLBase, version.Version, Installer
	defer func() {
		UpdateGitHubRepo, githubAPIBase, UpdateCheckURLBase, version.Version, Installer = oldRepo, oldBase, oldURL, oldVersion, oldInstaller
	}()
	Installer = "Ollama-darwin.zip"
	UpdateGitHubRepo, githubAPIBase, UpdateCheckURLBase = "o/r", server.URL, ""
	version.Version = "0.34.4-cluster.1"
	u := &Updater{}

	release := func(tag, asset string) string {
		return fmt.Sprintf(`{"tag_name": %q, "assets": [{"name": "sha256sum.txt", "browser_download_url": "https://x/sha"}, {"name": %q, "browser_download_url": "https://x/%s/%s"}]}`, tag, asset, tag, asset)
	}

	body = release("v0.34.4-cluster.2", Installer)
	ok, resp := u.checkForUpdate(t.Context())
	if !ok || resp.UpdateURL != "https://x/v0.34.4-cluster.2/"+Installer || resp.UpdateVersion != "v0.34.4-cluster.2" {
		t.Errorf("newer release: ok=%v resp=%+v", ok, resp)
	}

	body = release("v0.34.4-cluster.1", Installer)
	if ok, _ := u.checkForUpdate(t.Context()); ok {
		t.Error("the current release was offered as an update")
	}

	body = release("v0.34.4-cluster.2", "Other-linux.tgz")
	if ok, _ := u.checkForUpdate(t.Context()); ok {
		t.Error("a release with no installer for this platform was offered")
	}

	status, body = http.StatusNotFound, `{"message": "Not Found"}`
	if ok, _ := u.checkForUpdate(t.Context()); ok {
		t.Error("an update was offered with no published release")
	}
}
