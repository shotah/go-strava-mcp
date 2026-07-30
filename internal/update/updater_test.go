package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// createTestArchive builds a tar.gz containing a "strava-mcp" binary and README.md.
func createTestArchive(t *testing.T, dir, binaryContent string) string {
	t.Helper()
	path := filepath.Join(dir, "test.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	// Add strava-mcp binary.
	if err := tw.WriteHeader(&tar.Header{
		Name:     "strava-mcp",
		Size:     int64(len(binaryContent)),
		Mode:     0755,
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(binaryContent)); err != nil {
		t.Fatal(err)
	}

	// Add README.md.
	readme := "# Test"
	if err := tw.WriteHeader(&tar.Header{
		Name:     "README.md",
		Size:     int64(len(readme)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(readme)); err != nil {
		t.Fatal(err)
	}

	tw.Close()
	gw.Close()
	f.Close()
	return path
}

// createReadmeOnlyArchive builds a tar.gz with only README.md (no binary).
func createReadmeOnlyArchive(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "no-binary.tar.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	readme := "# Test"
	if err := tw.WriteHeader(&tar.Header{
		Name:     "README.md",
		Size:     int64(len(readme)),
		Mode:     0644,
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(readme)); err != nil {
		t.Fatal(err)
	}

	tw.Close()
	gw.Close()
	f.Close()
	return path
}

// sha256Hex computes the SHA256 hex digest of the file at path.
func sha256Hex(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func TestIsHomebrew(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/opt/homebrew/Caskroom/strava-mcp/1.0/strava-mcp", true},
		{"/usr/local/Caskroom/strava-mcp/1.0/strava-mcp", true},
		{"/usr/local/Cellar/strava-mcp/1.0/bin/strava-mcp", true},
		{"/opt/homebrew/Cellar/strava-mcp/1.0/bin/strava-mcp", true},
		{"/usr/local/bin/strava-mcp", false},
		{"/home/user/.local/bin/strava-mcp", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := IsHomebrew(tt.path)
			if got != tt.want {
				t.Errorf("IsHomebrew(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestCheckWritePermission_WritableDir(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "strava-mcp")
	if err := os.WriteFile(binaryPath, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := CheckWritePermission(binaryPath); err != nil {
		t.Errorf("CheckWritePermission on writable dir should return nil, got: %v", err)
	}
}

func TestCheckWritePermission_ReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "strava-mcp")
	if err := os.WriteFile(binaryPath, []byte("binary"), 0755); err != nil {
		t.Fatal(err)
	}

	// Make directory read-only.
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0755)

	err := CheckWritePermission(binaryPath)
	if err == nil {
		t.Error("CheckWritePermission on read-only dir should return error")
	}
	if err != nil && !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("error should contain 'permission denied', got: %v", err)
	}
}

func TestArchiveName(t *testing.T) {
	expected := fmt.Sprintf("strava-mcp_1.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)

	// With v prefix.
	if got := archiveName("v1.2.0"); got != expected {
		t.Errorf("archiveName(\"v1.2.0\") = %q, want %q", got, expected)
	}

	// Without v prefix.
	if got := archiveName("1.2.0"); got != expected {
		t.Errorf("archiveName(\"1.2.0\") = %q, want %q", got, expected)
	}
}

func TestChecksumsName(t *testing.T) {
	expected := "strava-mcp_1.2.0_checksums.txt"

	if got := checksumsName("v1.2.0"); got != expected {
		t.Errorf("checksumsName(\"v1.2.0\") = %q, want %q", got, expected)
	}
	if got := checksumsName("1.2.0"); got != expected {
		t.Errorf("checksumsName(\"1.2.0\") = %q, want %q", got, expected)
	}
}

func TestFindAssetURL(t *testing.T) {
	assets := []ReleaseAsset{
		{Name: "file1.tar.gz", BrowserDownloadURL: "https://example.com/file1"},
		{Name: "file2.tar.gz", BrowserDownloadURL: "https://example.com/file2"},
	}

	t.Run("found", func(t *testing.T) {
		url, err := findAssetURL(assets, "file1.tar.gz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if url != "https://example.com/file1" {
			t.Errorf("url = %q, want %q", url, "https://example.com/file1")
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := findAssetURL(assets, "missing.tar.gz")
		if err == nil {
			t.Error("expected error for missing asset")
		}
		if err != nil && !strings.Contains(err.Error(), "not found in release") {
			t.Errorf("error should contain 'not found in release', got: %v", err)
		}
	})

	t.Run("empty download URL", func(t *testing.T) {
		emptyAssets := []ReleaseAsset{
			{Name: "file.tar.gz", BrowserDownloadURL: ""},
		}
		_, err := findAssetURL(emptyAssets, "file.tar.gz")
		if err == nil {
			t.Error("expected error for empty download URL")
		}
	})
}

func TestParseChecksum(t *testing.T) {
	dir := t.TempDir()
	checksumFile := filepath.Join(dir, "checksums.txt")
	content := "abc123def456  strava-mcp_1.2.0_darwin_arm64.tar.gz\nfed789abc012  strava-mcp_1.2.0_linux_amd64.tar.gz\n"
	if err := os.WriteFile(checksumFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("found", func(t *testing.T) {
		hash, err := parseChecksum(checksumFile, "strava-mcp_1.2.0_darwin_arm64.tar.gz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hash != "abc123def456" {
			t.Errorf("hash = %q, want %q", hash, "abc123def456")
		}
	})

	t.Run("second entry", func(t *testing.T) {
		hash, err := parseChecksum(checksumFile, "strava-mcp_1.2.0_linux_amd64.tar.gz")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if hash != "fed789abc012" {
			t.Errorf("hash = %q, want %q", hash, "fed789abc012")
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := parseChecksum(checksumFile, "missing.tar.gz")
		if err == nil {
			t.Error("expected error for missing file")
		}
	})

	t.Run("file does not exist", func(t *testing.T) {
		_, err := parseChecksum(filepath.Join(dir, "nonexistent.txt"), "anything")
		if err == nil {
			t.Error("expected error for nonexistent checksums file")
		}
	})
}

func TestVerifySHA256_Match(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")
	content := []byte("hello world")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}

	hash := sha256Hex(t, path)
	if err := verifySHA256(path, hash); err != nil {
		t.Errorf("verifySHA256 should pass for matching hash, got: %v", err)
	}
}

func TestVerifySHA256_Mismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "testfile")
	if err := os.WriteFile(path, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	err := verifySHA256(path, "0000000000000000000000000000000000000000000000000000000000000000")
	if err == nil {
		t.Error("verifySHA256 should fail for mismatched hash")
	}
	if err != nil && !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error should contain 'checksum mismatch', got: %v", err)
	}
}

func TestExtractBinary(t *testing.T) {
	dir := t.TempDir()
	binaryContent := "#!/bin/sh\necho updated"
	archivePath := createTestArchive(t, dir, binaryContent)
	dest := filepath.Join(dir, "extracted-binary")

	if err := extractBinary(archivePath, dest); err != nil {
		t.Fatalf("extractBinary failed: %v", err)
	}

	// Verify content.
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read extracted binary: %v", err)
	}
	if string(data) != binaryContent {
		t.Errorf("extracted content = %q, want %q", string(data), binaryContent)
	}

	// Verify permissions.
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("stat extracted binary: %v", err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("extracted binary should be executable, mode = %v", info.Mode())
	}
}

func TestExtractBinary_NoBinary(t *testing.T) {
	dir := t.TempDir()
	archivePath := createReadmeOnlyArchive(t, dir)
	dest := filepath.Join(dir, "extracted-binary")

	err := extractBinary(archivePath, dest)
	if err == nil {
		t.Error("extractBinary should fail when no strava-mcp in archive")
	}
	if err != nil && !strings.Contains(err.Error(), "not found in archive") {
		t.Errorf("error should contain 'not found in archive', got: %v", err)
	}
}

func TestUpdate_AlreadyUpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(githubRelease{
			TagName: "v1.0.0",
			HTMLURL: "https://example.com",
		})
	}))
	defer srv.Close()

	dir := t.TempDir()
	cache := NewCache(dir)
	checker := NewChecker("1.0.0", cache, discardLogger())
	checker.apiURL = srv.URL
	updater := NewUpdater(checker, discardLogger())

	var messages []string
	progress := func(msg string) {
		messages = append(messages, msg)
	}

	binaryPath := filepath.Join(dir, "strava-mcp")
	if err := os.WriteFile(binaryPath, []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}

	err := updater.Update(context.Background(), binaryPath, progress)
	if err != nil {
		t.Fatalf("Update should not error when up to date, got: %v", err)
	}

	found := false
	for _, m := range messages {
		if strings.Contains(m, "Already up to date") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'Already up to date' in progress messages, got: %v", messages)
	}
}

func TestUpdate_FullSuccess(t *testing.T) {
	dir := t.TempDir()

	// Create a test archive with "new binary content".
	newBinaryContent := "#!/bin/sh\necho new-version"
	archiveName := fmt.Sprintf("strava-mcp_2.0.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archivePath := createTestArchive(t, dir, newBinaryContent)

	// Compute the real SHA256 of the archive.
	archiveHash := sha256Hex(t, archivePath)

	// Read archive bytes for serving.
	archiveBytes, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}

	// Create checksums content.
	checksumsContent := fmt.Sprintf("%s  %s\n", archiveHash, archiveName)
	checksumsFileName := "strava-mcp_2.0.0_checksums.txt"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/releases/latest"):
			json.NewEncoder(w).Encode(githubRelease{
				TagName: "v2.0.0",
				HTMLURL: "https://example.com/v2",
				Assets: []ReleaseAsset{
					{Name: archiveName, BrowserDownloadURL: r.Host + "/archive"},
					{Name: checksumsFileName, BrowserDownloadURL: r.Host + "/checksums"},
				},
			})
		case strings.Contains(r.URL.Path, "/checksums") || r.URL.Query().Get("file") == "checksums":
			w.Write([]byte(checksumsContent))
		case strings.Contains(r.URL.Path, "/archive") || r.URL.Query().Get("file") == "archive":
			w.Write(archiveBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// Fix asset URLs to point to test server.
	releaseHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(githubRelease{
			TagName: "v2.0.0",
			HTMLURL: "https://example.com/v2",
			Assets: []ReleaseAsset{
				{Name: archiveName, BrowserDownloadURL: srv.URL + "/archive"},
				{Name: checksumsFileName, BrowserDownloadURL: srv.URL + "/checksums"},
			},
		})
	})

	releaseSrv := httptest.NewServer(releaseHandler)
	defer releaseSrv.Close()

	cache := NewCache(dir)
	checker := NewChecker("1.0.0", cache, discardLogger())
	checker.apiURL = releaseSrv.URL
	updater := NewUpdater(checker, discardLogger())

	// Write the "old" binary.
	binaryPath := filepath.Join(dir, "strava-mcp")
	oldContent := "#!/bin/sh\necho old-version"
	if err := os.WriteFile(binaryPath, []byte(oldContent), 0755); err != nil {
		t.Fatal(err)
	}

	var messages []string
	progress := func(msg string) {
		messages = append(messages, msg)
	}

	err = updater.Update(context.Background(), binaryPath, progress)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify .bak exists with old content.
	bakPath := binaryPath + ".bak"
	bakData, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatalf("read .bak file: %v", err)
	}
	if string(bakData) != oldContent {
		t.Errorf(".bak content = %q, want %q", string(bakData), oldContent)
	}

	// Verify new binary exists with new content.
	newData, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read new binary: %v", err)
	}
	if string(newData) != newBinaryContent {
		t.Errorf("new binary content = %q, want %q", string(newData), newBinaryContent)
	}

	// Verify progress messages include key steps.
	allMsgs := strings.Join(messages, "\n")
	for _, substr := range []string{"Checking for updates", "Downloading", "Verifying checksum", "Updating binary", "Updated to"} {
		if !strings.Contains(allMsgs, substr) {
			t.Errorf("progress messages missing %q, got:\n%s", substr, allMsgs)
		}
	}
}

func TestUpdate_ChecksumMismatch(t *testing.T) {
	dir := t.TempDir()

	newBinaryContent := "#!/bin/sh\necho new"
	archName := fmt.Sprintf("strava-mcp_2.0.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	archivePath := createTestArchive(t, dir, newBinaryContent)

	archiveBytes, err := os.ReadFile(archivePath)
	if err != nil {
		t.Fatal(err)
	}

	// Wrong checksum on purpose.
	checksumsContent := fmt.Sprintf("%s  %s\n", "0000000000000000000000000000000000000000000000000000000000000000", archName)
	checksumsFileName := "strava-mcp_2.0.0_checksums.txt"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/checksums"):
			w.Write([]byte(checksumsContent))
		case strings.Contains(r.URL.Path, "/archive"):
			w.Write(archiveBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	releaseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(githubRelease{
			TagName: "v2.0.0",
			HTMLURL: "https://example.com/v2",
			Assets: []ReleaseAsset{
				{Name: archName, BrowserDownloadURL: srv.URL + "/archive"},
				{Name: checksumsFileName, BrowserDownloadURL: srv.URL + "/checksums"},
			},
		})
	}))
	defer releaseSrv.Close()

	cache := NewCache(dir)
	checker := NewChecker("1.0.0", cache, discardLogger())
	checker.apiURL = releaseSrv.URL
	updater := NewUpdater(checker, discardLogger())

	binaryPath := filepath.Join(dir, "strava-mcp")
	oldContent := "#!/bin/sh\necho old"
	if err := os.WriteFile(binaryPath, []byte(oldContent), 0755); err != nil {
		t.Fatal(err)
	}

	err = updater.Update(context.Background(), binaryPath, func(string) {})
	if err == nil {
		t.Fatal("Update should fail on checksum mismatch")
	}
	if !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("error should contain 'checksum mismatch', got: %v", err)
	}

	// Verify original binary is untouched (no .bak should exist).
	data, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("original binary should still exist: %v", err)
	}
	if string(data) != oldContent {
		t.Errorf("original binary content changed, got %q want %q", string(data), oldContent)
	}

	bakPath := binaryPath + ".bak"
	if _, err := os.Stat(bakPath); err == nil {
		t.Error(".bak should not exist after checksum mismatch (failure before rename)")
	}
}
