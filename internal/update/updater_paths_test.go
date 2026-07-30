package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// releaseServer serves a GitHub "latest release" payload with the given assets.
func releaseServer(t *testing.T, tag string, assets []ReleaseAsset) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode(githubRelease{
			TagName: tag,
			HTMLURL: "https://example.com/" + tag,
			Assets:  assets,
		}); err != nil {
			t.Errorf("encode release: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTestUpdater returns an updater whose release lookups hit apiURL.
func newTestUpdater(t *testing.T, currentVersion, apiURL string) *Updater {
	t.Helper()
	checker := NewChecker(currentVersion, NewCache(t.TempDir()), discardLogger())
	checker.SetAPIURL(apiURL)
	return NewUpdater(checker, discardLogger())
}

// stubBinary writes a placeholder "current binary" and returns its path.
func stubBinary(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "strava-mcp")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func platformArchiveName(version string) string {
	return fmt.Sprintf("strava-mcp_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
}

func TestDownload_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	u := NewUpdater(nil, discardLogger())
	_, err := u.download(context.Background(), srv.URL, t.TempDir())
	if err == nil {
		t.Fatal("download() = nil, want an error for a 404 response")
	}
	if !strings.Contains(err.Error(), "status 404") {
		t.Errorf("error = %v, want it to report the status code", err)
	}
}

func TestDownload_InvalidURL(t *testing.T) {
	u := NewUpdater(nil, discardLogger())
	if _, err := u.download(context.Background(), "http://\x7f/bad", t.TempDir()); err == nil {
		t.Fatal("download() = nil, want an error for an unparseable URL")
	}
}

func TestDownload_UnreachableHost(t *testing.T) {
	u := NewUpdater(nil, discardLogger())
	// Port 1 is reserved and refuses connections.
	if _, err := u.download(context.Background(), "http://127.0.0.1:1/x", t.TempDir()); err == nil {
		t.Fatal("download() = nil, want an error for an unreachable host")
	}
}

func TestDownload_MissingTargetDirectory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("payload")); err != nil {
			t.Errorf("write body: %v", err)
		}
	}))
	defer srv.Close()

	u := NewUpdater(nil, discardLogger())
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	_, err := u.download(context.Background(), srv.URL, missing)
	if err == nil {
		t.Fatal("download() = nil, want an error when the temp directory is missing")
	}
	if !strings.Contains(err.Error(), "create temp file") {
		t.Errorf("error = %v, want it to mention creating the temp file", err)
	}
}

func TestUpdate_CheckFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	u := newTestUpdater(t, "1.0.0", srv.URL)

	err := u.Update(context.Background(), stubBinary(t, dir), nil)
	if err == nil {
		t.Fatal("Update() = nil, want an error when the release lookup fails")
	}
	if !strings.Contains(err.Error(), "check for updates") {
		t.Errorf("error = %v, want it to mention checking for updates", err)
	}
}

// TestUpdate_NilProgressIsSafe covers the default no-op progress callback.
func TestUpdate_NilProgressIsSafe(t *testing.T) {
	dir := t.TempDir()
	u := newTestUpdater(t, "1.5.0", releaseServer(t, "v1.5.0", nil).URL)

	if err := u.Update(context.Background(), stubBinary(t, dir), nil); err != nil {
		t.Fatalf("Update() error = %v, want nil when already up to date", err)
	}
}

func TestUpdate_ArchiveAssetMissing(t *testing.T) {
	dir := t.TempDir()
	assets := []ReleaseAsset{
		{Name: "strava-mcp_2.0.0_checksums.txt", BrowserDownloadURL: "https://example.com/checksums"},
	}
	u := newTestUpdater(t, "1.0.0", releaseServer(t, "v2.0.0", assets).URL)

	err := u.Update(context.Background(), stubBinary(t, dir), func(string) {})
	if err == nil {
		t.Fatal("Update() = nil, want an error when the platform archive is missing")
	}
	if !strings.Contains(err.Error(), "find archive asset") {
		t.Errorf("error = %v, want it to mention the missing archive asset", err)
	}
}

func TestUpdate_ChecksumsAssetMissing(t *testing.T) {
	dir := t.TempDir()
	assets := []ReleaseAsset{
		{Name: platformArchiveName("2.0.0"), BrowserDownloadURL: "https://example.com/archive"},
	}
	u := newTestUpdater(t, "1.0.0", releaseServer(t, "v2.0.0", assets).URL)

	err := u.Update(context.Background(), stubBinary(t, dir), func(string) {})
	if err == nil {
		t.Fatal("Update() = nil, want an error when the checksums asset is missing")
	}
	if !strings.Contains(err.Error(), "find checksums asset") {
		t.Errorf("error = %v, want it to mention the missing checksums asset", err)
	}
}

func TestUpdate_ChecksumsDownloadFails(t *testing.T) {
	dir := t.TempDir()
	assets := []ReleaseAsset{
		{Name: platformArchiveName("2.0.0"), BrowserDownloadURL: "https://example.com/archive"},
		{Name: "strava-mcp_2.0.0_checksums.txt", BrowserDownloadURL: "http://127.0.0.1:1/checksums"},
	}
	u := newTestUpdater(t, "1.0.0", releaseServer(t, "v2.0.0", assets).URL)

	err := u.Update(context.Background(), stubBinary(t, dir), func(string) {})
	if err == nil {
		t.Fatal("Update() = nil, want an error when checksums cannot be downloaded")
	}
	if !strings.Contains(err.Error(), "download checksums") {
		t.Errorf("error = %v, want it to mention downloading checksums", err)
	}
}

func TestUpdate_ChecksumNotListed(t *testing.T) {
	dir := t.TempDir()

	assetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Valid file, but it lists a different platform.
		if _, err := w.Write([]byte("deadbeef  strava-mcp_2.0.0_plan9_mips.tar.gz\n")); err != nil {
			t.Errorf("write body: %v", err)
		}
	}))
	defer assetSrv.Close()

	assets := []ReleaseAsset{
		{Name: platformArchiveName("2.0.0"), BrowserDownloadURL: assetSrv.URL + "/archive"},
		{Name: "strava-mcp_2.0.0_checksums.txt", BrowserDownloadURL: assetSrv.URL + "/checksums"},
	}
	u := newTestUpdater(t, "1.0.0", releaseServer(t, "v2.0.0", assets).URL)

	err := u.Update(context.Background(), stubBinary(t, dir), func(string) {})
	if err == nil {
		t.Fatal("Update() = nil, want an error when no checksum matches this platform")
	}
	if !strings.Contains(err.Error(), "parse checksum") {
		t.Errorf("error = %v, want it to mention parsing the checksum", err)
	}
}

func TestUpdate_ArchiveDownloadFails(t *testing.T) {
	dir := t.TempDir()
	archive := platformArchiveName("2.0.0")

	checksumsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := fmt.Fprintf(w, "deadbeef  %s\n", archive); err != nil {
			t.Errorf("write body: %v", err)
		}
	}))
	defer checksumsSrv.Close()

	assets := []ReleaseAsset{
		{Name: archive, BrowserDownloadURL: "http://127.0.0.1:1/archive"},
		{Name: "strava-mcp_2.0.0_checksums.txt", BrowserDownloadURL: checksumsSrv.URL},
	}
	u := newTestUpdater(t, "1.0.0", releaseServer(t, "v2.0.0", assets).URL)

	err := u.Update(context.Background(), stubBinary(t, dir), func(string) {})
	if err == nil {
		t.Fatal("Update() = nil, want an error when the archive cannot be downloaded")
	}
	if !strings.Contains(err.Error(), "download archive") {
		t.Errorf("error = %v, want it to mention downloading the archive", err)
	}
}

func TestUpdate_ExtractFailsWhenArchiveHasNoBinary(t *testing.T) {
	dir := t.TempDir()
	archive := platformArchiveName("3.0.0")

	archivePath := createReadmeOnlyArchive(t, dir)
	archiveBytes, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256Hex(t, archivePath)

	assetSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var writeErr error
		if strings.Contains(r.URL.Path, "checksums") {
			_, writeErr = fmt.Fprintf(w, "%s  %s\n", hash, archive)
		} else {
			_, writeErr = w.Write(archiveBytes)
		}
		if writeErr != nil {
			t.Errorf("write body: %v", writeErr)
		}
	}))
	defer assetSrv.Close()

	assets := []ReleaseAsset{
		{Name: archive, BrowserDownloadURL: assetSrv.URL + "/archive"},
		{Name: "strava-mcp_3.0.0_checksums.txt", BrowserDownloadURL: assetSrv.URL + "/checksums"},
	}
	u := newTestUpdater(t, "2.0.0", releaseServer(t, "v3.0.0", assets).URL)

	binaryPath := stubBinary(t, dir)
	err = u.Update(context.Background(), binaryPath, func(string) {})
	if err == nil {
		t.Fatal("Update() = nil, want an error when the archive has no binary")
	}
	if !strings.Contains(err.Error(), "extract binary") {
		t.Errorf("error = %v, want it to mention extracting the binary", err)
	}

	// The original binary must survive a failure before the rename step.
	if data, readErr := os.ReadFile(binaryPath); readErr != nil || string(data) != "old" {
		t.Errorf("original binary = %q (err %v), want it untouched", data, readErr)
	}
	if _, statErr := os.Stat(binaryPath + ".new"); statErr == nil {
		t.Error(".new file was left behind after a failed extract")
	}
}

func TestExtractBinary_NotAnArchive(t *testing.T) {
	dir := t.TempDir()
	notGzip := filepath.Join(dir, "plain.tar.gz")
	if err := os.WriteFile(notGzip, []byte("definitely not gzip"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := extractBinary(notGzip, filepath.Join(dir, "out"))
	if err == nil {
		t.Fatal("extractBinary() = nil, want an error for a non-gzip file")
	}
	if !strings.Contains(err.Error(), "gzip reader") {
		t.Errorf("error = %v, want it to mention the gzip reader", err)
	}
}

func TestExtractBinary_MissingArchive(t *testing.T) {
	dir := t.TempDir()
	err := extractBinary(filepath.Join(dir, "nope.tar.gz"), filepath.Join(dir, "out"))
	if err == nil {
		t.Fatal("extractBinary() = nil, want an error for a missing archive")
	}
	if !strings.Contains(err.Error(), "open archive") {
		t.Errorf("error = %v, want it to mention opening the archive", err)
	}
}

func TestVerifySHA256_MissingFile(t *testing.T) {
	err := verifySHA256(filepath.Join(t.TempDir(), "nope"), "abc")
	if err == nil {
		t.Fatal("verifySHA256() = nil, want an error for a missing file")
	}
	if !strings.Contains(err.Error(), "open for checksum") {
		t.Errorf("error = %v, want it to mention opening the file", err)
	}
}

func TestEnsureVPrefix(t *testing.T) {
	if got := ensureVPrefix("1.2.3"); got != "v1.2.3" {
		t.Errorf("ensureVPrefix(\"1.2.3\") = %q, want %q", got, "v1.2.3")
	}
	if got := ensureVPrefix("v1.2.3"); got != "v1.2.3" {
		t.Errorf("ensureVPrefix(\"v1.2.3\") = %q, want %q", got, "v1.2.3")
	}
}

func TestSetAPIURL(t *testing.T) {
	c := NewChecker("1.0.0", NewCache(t.TempDir()), discardLogger())
	c.SetAPIURL("https://example.test/releases")
	if c.apiURL != "https://example.test/releases" {
		t.Errorf("apiURL = %q, want the overridden URL", c.apiURL)
	}
}

func TestCheck_UnparseableTag(t *testing.T) {
	srv := releaseServer(t, "not-a-version", nil)
	c := NewChecker("1.0.0", NewCache(t.TempDir()), discardLogger())
	c.SetAPIURL(srv.URL)

	_, err := c.Check(context.Background())
	if err == nil {
		t.Fatal("Check() = nil, want an error for an unparseable release tag")
	}
	if !strings.Contains(err.Error(), "parse release tag") {
		t.Errorf("error = %v, want it to mention parsing the tag", err)
	}
}

func TestCheck_InvalidAPIURL(t *testing.T) {
	c := NewChecker("1.0.0", NewCache(t.TempDir()), discardLogger())
	c.SetAPIURL("http://\x7f/bad")

	if _, err := c.Check(context.Background()); err == nil {
		t.Fatal("Check() = nil, want an error for an unparseable API URL")
	}
}

// writeCache writes a cache file directly so tests can control its contents.
func writeCache(t *testing.T, c *Cache, data cacheData) {
	t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.Path(), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCheckWithCooldown_CachedWithoutVersion(t *testing.T) {
	cache := NewCache(t.TempDir())
	writeCache(t, cache, cacheData{LastCheck: time.Now()})

	c := NewChecker("1.0.0", cache, discardLogger())
	c.SetAPIURL("http://127.0.0.1:1")

	result, err := c.CheckWithCooldown(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("CheckWithCooldown() error = %v", err)
	}
	if result != nil {
		t.Errorf("result = %+v, want nil when the cache holds no version", result)
	}
}

func TestCheckWithCooldown_CachedVersionUnparseable(t *testing.T) {
	cache := NewCache(t.TempDir())
	writeCache(t, cache, cacheData{LastCheck: time.Now(), LatestVersion: "garbage"})

	c := NewChecker("1.0.0", cache, discardLogger())
	c.SetAPIURL("http://127.0.0.1:1")

	result, err := c.CheckWithCooldown(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("CheckWithCooldown() error = %v", err)
	}
	if result != nil {
		t.Errorf("result = %+v, want nil when the cached version is unparseable", result)
	}
}

func TestCheckWithCooldown_APIErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewChecker("1.0.0", NewCache(t.TempDir()), discardLogger())
	c.SetAPIURL(srv.URL)

	if _, err := c.CheckWithCooldown(context.Background(), 24*time.Hour); err == nil {
		t.Fatal("CheckWithCooldown() = nil, want the API error")
	}
}

func TestFormatNotification_NilResult(t *testing.T) {
	c := NewChecker("1.0.0", NewCache(t.TempDir()), discardLogger())
	if got := c.FormatNotification(nil); got != "" {
		t.Errorf("FormatNotification(nil) = %q, want empty", got)
	}
}

func TestFormatCheckOutput_NilResult(t *testing.T) {
	c := NewChecker("1.0.0", NewCache(t.TempDir()), discardLogger())
	if got := c.FormatCheckOutput(nil); got != "" {
		t.Errorf("FormatCheckOutput(nil) = %q, want empty", got)
	}
}

func TestCachePath(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)
	if want := filepath.Join(dir, "update-check.json"); c.Path() != want {
		t.Errorf("Path() = %q, want %q", c.Path(), want)
	}
}

func TestCacheWriteFailsWhenParentIsAFile(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	c := NewCache(filepath.Join(blocker, "nested"))
	err := c.Write("v1.0.0", "https://example.com")
	if err == nil {
		t.Fatal("Write() = nil, want an error when the parent path is a file")
	}
	if !strings.Contains(err.Error(), "create cache directory") {
		t.Errorf("error = %v, want it to mention creating the directory", err)
	}
}
