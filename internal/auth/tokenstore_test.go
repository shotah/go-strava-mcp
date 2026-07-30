package auth_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shotah/go-strava-mcp/internal/auth"
)

func TestWriteCreatesDirAndFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "tokens.json")
	store := auth.NewFileTokenStore(path)

	tokens := &auth.Tokens{
		AccessToken:  "access-123",
		RefreshToken: "refresh-456",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
	}

	if err := store.Write(tokens); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	// Verify directory was created
	info, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("expected directory")
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestWriteProducesValidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	store := auth.NewFileTokenStore(path)

	tokens := &auth.Tokens{
		AccessToken:  "access-abc",
		RefreshToken: "refresh-def",
		ExpiresAt:    1700000000,
	}

	if err := store.Write(tokens); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	var parsed auth.Tokens
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if parsed.AccessToken != "access-abc" {
		t.Errorf("AccessToken = %q, want %q", parsed.AccessToken, "access-abc")
	}
	if parsed.RefreshToken != "refresh-def" {
		t.Errorf("RefreshToken = %q, want %q", parsed.RefreshToken, "refresh-def")
	}
	if parsed.ExpiresAt != 1700000000 {
		t.Errorf("ExpiresAt = %d, want %d", parsed.ExpiresAt, 1700000000)
	}
}

func TestReadReturnsTokensFromValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")

	data := `{"access_token":"read-access","refresh_token":"read-refresh","expires_at":1700000000}`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	store := auth.NewFileTokenStore(path)
	tokens, err := store.Read()
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}

	if tokens.AccessToken != "read-access" {
		t.Errorf("AccessToken = %q, want %q", tokens.AccessToken, "read-access")
	}
	if tokens.RefreshToken != "read-refresh" {
		t.Errorf("RefreshToken = %q, want %q", tokens.RefreshToken, "read-refresh")
	}
	if tokens.ExpiresAt != 1700000000 {
		t.Errorf("ExpiresAt = %d, want %d", tokens.ExpiresAt, 1700000000)
	}
}

func TestReadErrorForMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")
	store := auth.NewFileTokenStore(path)

	_, err := store.Read()
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestReadErrorForInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")

	if err := os.WriteFile(path, []byte("not valid json{{{"), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	store := auth.NewFileTokenStore(path)
	_, err := store.Read()
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestIsExpiredReturnsTrueWhenExpired(t *testing.T) {
	store := auth.NewFileTokenStore("")

	// Token expires at current time - so ExpiresAt - 300 is in the past
	tokens := &auth.Tokens{
		ExpiresAt: time.Now().Unix(), // current time, minus 300 buffer = expired
	}

	if !store.IsExpired(tokens) {
		t.Error("IsExpired() = false, want true (token expires within 5-min buffer)")
	}
}

func TestIsExpiredReturnsFalseWhenValid(t *testing.T) {
	store := auth.NewFileTokenStore("")

	// Token expires far in the future
	tokens := &auth.Tokens{
		ExpiresAt: time.Now().Unix() + 3600, // 1 hour from now
	}

	if store.IsExpired(tokens) {
		t.Error("IsExpired() = true, want false (token has plenty of time)")
	}
}

func TestWriteUsesAtomicRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	store := auth.NewFileTokenStore(path)

	tokens := &auth.Tokens{
		AccessToken:  "atomic-test",
		RefreshToken: "atomic-refresh",
		ExpiresAt:    1700000000,
	}

	if err := store.Write(tokens); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	// After successful write, the .tmp file should NOT remain
	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error("temp file still exists after Write(); expected it to be renamed")
	}

	// The final file should exist and have correct content
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("final file missing: %v", err)
	}

	var parsed auth.Tokens
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("invalid JSON in final file: %v", err)
	}
	if parsed.AccessToken != "atomic-test" {
		t.Errorf("AccessToken = %q, want %q", parsed.AccessToken, "atomic-test")
	}
}

func TestWriteFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tokens.json")
	store := auth.NewFileTokenStore(path)

	tokens := &auth.Tokens{
		AccessToken:  "perm-test",
		RefreshToken: "perm-refresh",
		ExpiresAt:    1700000000,
	}

	if err := store.Write(tokens); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permissions = %o, want %o", perm, 0600)
	}
}
