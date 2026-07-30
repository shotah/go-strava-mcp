package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewCache_PathConstruction(t *testing.T) {
	c := NewCache("/tmp/.strava")
	want := "/tmp/.strava/update-check.json"
	if c.Path() != want {
		t.Errorf("NewCache path = %q, want %q", c.Path(), want)
	}
}

func TestShouldCheck_NoCacheFile(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)
	if !c.ShouldCheck(24 * time.Hour) {
		t.Error("ShouldCheck should return true when cache file does not exist")
	}
}

func TestShouldCheck_ExpiredCooldown(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)

	// Write cache with timestamp 25 hours ago.
	data := cacheData{
		LastCheck:     time.Now().Add(-25 * time.Hour),
		LatestVersion: "v1.0.0",
	}
	raw, _ := json.Marshal(data)
	if err := os.WriteFile(c.Path(), raw, 0600); err != nil {
		t.Fatal(err)
	}

	if !c.ShouldCheck(24 * time.Hour) {
		t.Error("ShouldCheck should return true when cooldown has expired")
	}
}

func TestShouldCheck_WithinCooldown(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)

	// Write cache with timestamp 1 hour ago.
	data := cacheData{
		LastCheck:     time.Now().Add(-1 * time.Hour),
		LatestVersion: "v1.0.0",
	}
	raw, _ := json.Marshal(data)
	if err := os.WriteFile(c.Path(), raw, 0600); err != nil {
		t.Fatal(err)
	}

	if c.ShouldCheck(24 * time.Hour) {
		t.Error("ShouldCheck should return false when within cooldown")
	}
}

func TestShouldCheck_CorruptFile(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)

	// Write invalid JSON.
	if err := os.WriteFile(c.Path(), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}

	if !c.ShouldCheck(24 * time.Hour) {
		t.Error("ShouldCheck should return true when cache file is corrupt")
	}
}

func TestWrite_AtomicCreation(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)

	if err := c.Write("v2.0.0", "https://github.com/example/releases/v2.0.0"); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Verify file exists.
	info, err := os.Stat(c.Path())
	if err != nil {
		t.Fatalf("cache file missing after Write: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %o, want 0600", info.Mode().Perm())
	}

	// Verify content.
	data, err := c.Read()
	if err != nil {
		t.Fatalf("Read after Write failed: %v", err)
	}
	if data.LatestVersion != "v2.0.0" {
		t.Errorf("LatestVersion = %q, want %q", data.LatestVersion, "v2.0.0")
	}
	if data.ReleaseURL != "https://github.com/example/releases/v2.0.0" {
		t.Errorf("ReleaseURL = %q, want %q", data.ReleaseURL, "https://github.com/example/releases/v2.0.0")
	}

	// Verify last_check is recent (within 2 seconds).
	if time.Since(data.LastCheck) > 2*time.Second {
		t.Errorf("LastCheck = %v, expected within 2s of now", data.LastCheck)
	}
}

func TestWrite_CreatesDirectory(t *testing.T) {
	base := t.TempDir()
	nested := filepath.Join(base, "deep", "nested")
	c := NewCache(nested)

	if err := c.Write("v1.0.0", ""); err != nil {
		t.Fatalf("Write to non-existent directory failed: %v", err)
	}

	if _, err := os.Stat(c.Path()); err != nil {
		t.Errorf("cache file not created in nested directory: %v", err)
	}
}

func TestReadWrite_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	c := NewCache(dir)

	version := "v3.1.4"
	url := "https://github.com/shotah/go-strava-mcp/releases/tag/v3.1.4"

	if err := c.Write(version, url); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	data, err := c.Read()
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}

	if data.LatestVersion != version {
		t.Errorf("LatestVersion = %q, want %q", data.LatestVersion, version)
	}
	if data.ReleaseURL != url {
		t.Errorf("ReleaseURL = %q, want %q", data.ReleaseURL, url)
	}
	if data.LastCheck.IsZero() {
		t.Error("LastCheck should not be zero after Write")
	}
}
