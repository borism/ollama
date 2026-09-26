//go:build (windows || darwin) && updater_live

package updater

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/borism/ollama-cluster/app/store"
	"github.com/borism/ollama-cluster/app/version"
)

// TestLiveAppUpdate exercises the production update source (this repo's
// latest published GitHub release, see github.go) and downloads the current
// OS update artifact. It is intentionally excluded from normal test runs
// because it depends on the network and downloads a release artifact. It
// skips while there's nothing to download: no published release yet, or no
// installer for this OS (this fork ships no Windows app).
//
// Run with:
//
//	go test -tags updater_live -run TestLiveAppUpdate ./app/updater
func TestLiveAppUpdate(t *testing.T) {
	const spoofedVersion = "0.20.0"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	oldUpdateStageDir := UpdateStageDir
	oldUpdateDownloaded := UpdateDownloaded
	oldVerifyDownload := VerifyDownload
	oldVersion := version.Version
	defer func() {
		UpdateStageDir = oldUpdateStageDir
		UpdateDownloaded = oldUpdateDownloaded
		VerifyDownload = oldVerifyDownload
		version.Version = oldVersion
	}()

	version.Version = spoofedVersion

	expectedFilename := ""
	switch runtime.GOOS {
	case "windows":
		t.Setenv("LOCALAPPDATA", t.TempDir())
		expectedFilename = "OllamaSetup.exe"
	case "darwin":
		expectedFilename = "Ollama-darwin.zip"
	default:
		t.Fatalf("unsupported updater live test OS %q", runtime.GOOS)
	}

	UpdateStageDir = filepath.Join(t.TempDir(), "updates")
	UpdateDownloaded = false
	verifyCalled := false
	VerifyDownload = func() error {
		verifyCalled = true
		return verifyDownload()
	}

	updater := &Updater{Store: &store.Store{DBPath: filepath.Join(t.TempDir(), "db.sqlite")}}
	defer updater.Store.Close()

	available, updateResp := updater.checkForUpdate(ctx)
	if !available {
		t.Skipf("no published release of %s with a %s asset newer than spoofed version %s", UpdateGitHubRepo, Installer, spoofedVersion)
	}
	if updateResp.UpdateURL == "" {
		t.Fatal("production update response did not include a download URL")
	}
	t.Logf("production update version=%q url=%q", updateResp.UpdateVersion, updateResp.UpdateURL)

	if err := updater.DownloadNewRelease(ctx, updateResp); err != nil {
		t.Fatalf("download production update: %v", err)
	}

	staged := getStagedUpdate()
	if staged == "" {
		t.Fatal("production update was not staged")
	}
	t.Logf("staged production update at %s", staged)

	assertPathInsideDir(t, UpdateStageDir, staged)
	if filepath.Base(staged) != expectedFilename {
		t.Fatalf("expected staged %s update filename to be %q, got %q", runtime.GOOS, expectedFilename, filepath.Base(staged))
	}
	expectedExt := filepath.Ext(expectedFilename)
	if filepath.Ext(staged) != expectedExt {
		t.Fatalf("expected staged %s update to be a %s artifact, got %s", runtime.GOOS, expectedExt, staged)
	}

	info, err := os.Stat(staged)
	if err != nil {
		t.Fatalf("stat staged update: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("staged production update is empty")
	}

	if !verifyCalled {
		t.Fatal("DownloadNewRelease did not call VerifyDownload")
	}
	t.Logf("production updater download path verified staged %s update", runtime.GOOS)
}

func assertPathInsideDir(t *testing.T, dir, name string) {
	t.Helper()

	dir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	name, err = filepath.Abs(name)
	if err != nil {
		t.Fatal(err)
	}

	rel, err := filepath.Rel(dir, name)
	if err != nil {
		t.Fatal(err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		t.Fatalf("staged update escaped update stage dir: %s", name)
	}
}
