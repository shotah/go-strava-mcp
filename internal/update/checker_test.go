package update

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewChecker_DevVersion(t *testing.T) {
	c := NewChecker("dev", NewCache(t.TempDir()), discardLogger())
	if !c.IsDev() {
		t.Error("IsDev() should be true for version 'dev'")
	}
}

func TestNewChecker_ValidVersion(t *testing.T) {
	c := NewChecker("1.2.0", NewCache(t.TempDir()), discardLogger())
	if c.IsDev() {
		t.Error("IsDev() should be false for version '1.2.0'")
	}
	if c.currentVer == nil {
		t.Error("currentVer should be parsed for '1.2.0'")
	}
}

func TestNewChecker_VPrefixVersion(t *testing.T) {
	c := NewChecker("v1.2.0", NewCache(t.TempDir()), discardLogger())
	if c.IsDev() {
		t.Error("IsDev() should be false for version 'v1.2.0'")
	}
	if c.currentVer == nil {
		t.Error("currentVer should be parsed for 'v1.2.0'")
	}
}

func TestCheck_UpdateAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github.v3+json" {
			t.Error("missing Accept header")
		}
		json.NewEncoder(w).Encode(githubRelease{
			TagName: "v2.0.0",
			HTMLURL: "https://github.com/shotah/go-strava-mcp/releases/tag/v2.0.0",
		})
	}))
	defer srv.Close()

	c := NewChecker("1.0.0", NewCache(t.TempDir()), discardLogger())
	c.apiURL = srv.URL

	result, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if !result.UpdateAvailable {
		t.Error("UpdateAvailable should be true when latest > current")
	}
	if result.LatestVersion != "v2.0.0" {
		t.Errorf("LatestVersion = %q, want %q", result.LatestVersion, "v2.0.0")
	}
	if result.CurrentVersion != "1.0.0" {
		t.Errorf("CurrentVersion = %q, want %q", result.CurrentVersion, "1.0.0")
	}
}

func TestCheck_UpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(githubRelease{
			TagName: "v1.0.0",
			HTMLURL: "https://github.com/shotah/go-strava-mcp/releases/tag/v1.0.0",
		})
	}))
	defer srv.Close()

	c := NewChecker("1.0.0", NewCache(t.TempDir()), discardLogger())
	c.apiURL = srv.URL

	result, err := c.Check(context.Background())
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if result.UpdateAvailable {
		t.Error("UpdateAvailable should be false when latest == current")
	}
}

func TestCheck_NetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Close connection immediately.
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("server doesn't support hijacking")
		}
		conn, _, _ := hj.Hijack()
		conn.Close()
	}))
	defer srv.Close()

	c := NewChecker("1.0.0", NewCache(t.TempDir()), discardLogger())
	c.apiURL = srv.URL

	result, err := c.Check(context.Background())
	if err == nil {
		t.Error("Check should return error on network failure")
	}
	if result != nil {
		t.Error("result should be nil on error")
	}
}

func TestCheck_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	c := NewChecker("1.0.0", NewCache(t.TempDir()), discardLogger())
	c.apiURL = srv.URL

	_, err := c.Check(context.Background())
	if err == nil {
		t.Error("Check should return error on invalid JSON")
	}
}

func TestCheck_Non200Status(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewChecker("1.0.0", NewCache(t.TempDir()), discardLogger())
	c.apiURL = srv.URL

	_, err := c.Check(context.Background())
	if err == nil {
		t.Error("Check should return error on non-200 status")
	}
}

func TestCheckWithCooldown_DevSkips(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		json.NewEncoder(w).Encode(githubRelease{TagName: "v2.0.0", HTMLURL: "https://example.com"})
	}))
	defer srv.Close()

	c := NewChecker("dev", NewCache(t.TempDir()), discardLogger())
	c.apiURL = srv.URL

	result, err := c.CheckWithCooldown(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("result should be nil for dev version")
	}
	if callCount.Load() != 0 {
		t.Errorf("HTTP requests = %d, want 0 (dev should skip)", callCount.Load())
	}
}

func TestCheckWithCooldown_WithinCooldownUsesCache(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		json.NewEncoder(w).Encode(githubRelease{TagName: "v2.0.0", HTMLURL: "https://example.com"})
	}))
	defer srv.Close()

	dir := t.TempDir()
	cache := NewCache(dir)
	// Write cache with recent timestamp.
	if err := cache.Write("v2.0.0", "https://example.com/release"); err != nil {
		t.Fatal(err)
	}

	c := NewChecker("1.0.0", cache, discardLogger())
	c.apiURL = srv.URL

	result, err := c.CheckWithCooldown(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount.Load() != 0 {
		t.Errorf("HTTP requests = %d, want 0 (within cooldown)", callCount.Load())
	}
	if result == nil {
		t.Fatal("result should not be nil when cache has data")
	}
	if !result.UpdateAvailable {
		t.Error("UpdateAvailable should be true from cached data")
	}
}

func TestCheckWithCooldown_ExpiredCooldownCallsAPI(t *testing.T) {
	var callCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		json.NewEncoder(w).Encode(githubRelease{TagName: "v3.0.0", HTMLURL: "https://example.com/v3"})
	}))
	defer srv.Close()

	dir := t.TempDir()
	cache := NewCache(dir)
	// Write cache with old timestamp (25 hours ago).
	data := cacheData{
		LastCheck:     time.Now().Add(-25 * time.Hour),
		LatestVersion: "v2.0.0",
		ReleaseURL:    "https://example.com/old",
	}
	raw, _ := json.Marshal(data)
	os.WriteFile(cache.Path(), raw, 0600)

	c := NewChecker("1.0.0", cache, discardLogger())
	c.apiURL = srv.URL

	result, err := c.CheckWithCooldown(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount.Load() != 1 {
		t.Errorf("HTTP requests = %d, want 1 (cooldown expired)", callCount.Load())
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}
	if result.LatestVersion != "v3.0.0" {
		t.Errorf("LatestVersion = %q, want %q (fresh from API)", result.LatestVersion, "v3.0.0")
	}
}

func TestCheckWithCooldown_WritesCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(githubRelease{TagName: "v2.0.0", HTMLURL: "https://example.com/v2"})
	}))
	defer srv.Close()

	dir := t.TempDir()
	cache := NewCache(dir)

	c := NewChecker("1.0.0", cache, discardLogger())
	c.apiURL = srv.URL

	_, err := c.CheckWithCooldown(context.Background(), 24*time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify cache was written.
	data, err := cache.Read()
	if err != nil {
		t.Fatalf("cache Read failed after CheckWithCooldown: %v", err)
	}
	if data.LatestVersion != "v2.0.0" {
		t.Errorf("cached LatestVersion = %q, want %q", data.LatestVersion, "v2.0.0")
	}
	if data.ReleaseURL != "https://example.com/v2" {
		t.Errorf("cached ReleaseURL = %q, want %q", data.ReleaseURL, "https://example.com/v2")
	}
}

func TestFormatNotification_UpdateAvailable(t *testing.T) {
	c := NewChecker("1.0.0", NewCache(t.TempDir()), discardLogger())
	r := &Result{
		UpdateAvailable: true,
		CurrentVersion:  "1.0.0",
		LatestVersion:   "v2.0.0",
	}
	got := c.FormatNotification(r)
	want := "A new release of strava-mcp is available: v1.0.0 -> v2.0.0\nhttps://github.com/shotah/go-strava-mcp/releases/latest"
	if got != want {
		t.Errorf("FormatNotification =\n%q\nwant\n%q", got, want)
	}
}

func TestFormatNotification_NoUpdate(t *testing.T) {
	c := NewChecker("1.0.0", NewCache(t.TempDir()), discardLogger())
	r := &Result{
		UpdateAvailable: false,
		CurrentVersion:  "1.0.0",
		LatestVersion:   "v1.0.0",
	}
	got := c.FormatNotification(r)
	if got != "" {
		t.Errorf("FormatNotification should be empty when no update, got %q", got)
	}
}

func TestFormatCheckOutput_UpdateAvailable(t *testing.T) {
	c := NewChecker("1.0.0", NewCache(t.TempDir()), discardLogger())
	r := &Result{
		UpdateAvailable: true,
		CurrentVersion:  "1.0.0",
		LatestVersion:   "v2.0.0",
	}
	got := c.FormatCheckOutput(r)
	if got == "" {
		t.Fatal("FormatCheckOutput should not be empty")
	}
	for _, substr := range []string{"Current:", "Latest:", "Update available!"} {
		if !contains(got, substr) {
			t.Errorf("FormatCheckOutput missing %q in:\n%s", substr, got)
		}
	}
}

func TestFormatCheckOutput_UpToDate(t *testing.T) {
	c := NewChecker("1.0.0", NewCache(t.TempDir()), discardLogger())
	r := &Result{
		UpdateAvailable: false,
		CurrentVersion:  "1.0.0",
		LatestVersion:   "v1.0.0",
	}
	got := c.FormatCheckOutput(r)
	if !contains(got, "Up to date") {
		t.Errorf("FormatCheckOutput should contain 'Up to date', got %q", got)
	}
}

// contains checks if s contains substr (simple helper for readable tests).
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
