package update

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// cacheData is the JSON structure persisted to ~/.strava/update-check.json.
type cacheData struct {
	LastCheck     time.Time `json:"last_check"`
	LatestVersion string    `json:"latest_version,omitempty"`
	ReleaseURL    string    `json:"release_url,omitempty"`
}

// Cache manages the update check cooldown file.
type Cache struct {
	path string
}

// NewCache creates a Cache that stores state in dir/update-check.json.
func NewCache(dir string) *Cache {
	return &Cache{path: filepath.Join(dir, "update-check.json")}
}

// Path returns the full path to the cache file.
func (c *Cache) Path() string {
	return c.path
}

// ShouldCheck returns true if the cache is missing, corrupt, or the cooldown
// has expired. A true return means the caller should query the GitHub API.
func (c *Cache) ShouldCheck(cooldown time.Duration) bool {
	data, err := c.Read()
	if err != nil {
		return true // missing or corrupt → check
	}
	return time.Since(data.LastCheck) >= cooldown
}

// Read loads and unmarshals the cache file.
func (c *Cache) Read() (*cacheData, error) {
	raw, err := os.ReadFile(c.path)
	if err != nil {
		return nil, fmt.Errorf("read cache file: %w", err)
	}
	var data cacheData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("parse cache file: %w", err)
	}
	return &data, nil
}

// Write persists the cache to disk using atomic write-then-rename.
// Pattern replicates FileTokenStore.Write() from internal/auth/tokenstore.go.
func (c *Cache) Write(latestVersion, releaseURL string) error {
	data := cacheData{
		LastCheck:     time.Now().UTC(),
		LatestVersion: latestVersion,
		ReleaseURL:    releaseURL,
	}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}

	// Ensure directory exists.
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}

	// Write to temp file.
	tmpPath := c.path + ".tmp"
	if err := os.WriteFile(tmpPath, raw, 0o600); err != nil {
		return fmt.Errorf("write temp cache file: %w", err)
	}

	// Sync to ensure data reaches disk before rename.
	f, err := os.Open(tmpPath)
	if err == nil {
		_ = f.Sync()
		_ = f.Close()
	}

	// Atomic rename.
	if err := os.Rename(tmpPath, c.path); err != nil {
		return fmt.Errorf("rename cache file: %w", err)
	}

	return nil
}
