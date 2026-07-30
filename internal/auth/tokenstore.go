package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Tokens holds the OAuth2 token data persisted to disk.
// Only access_token, refresh_token, and expires_at are stored.
// Client credentials come from environment variables, never from the token file.
type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
}

// TokenStore defines the interface for reading and writing OAuth tokens.
type TokenStore interface {
	Read() (*Tokens, error)
	Write(tokens *Tokens) error
	IsExpired(tokens *Tokens) bool
}

// FileTokenStore implements TokenStore with atomic file writes.
type FileTokenStore struct {
	path string
	mu   sync.RWMutex
}

// NewFileTokenStore creates a new FileTokenStore at the given path.
func NewFileTokenStore(path string) *FileTokenStore {
	return &FileTokenStore{path: path}
}

// Read loads tokens from the file on disk.
func (s *FileTokenStore) Read() (*Tokens, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("read token file: %w", err)
	}

	var tokens Tokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, fmt.Errorf("parse token file: %w", err)
	}

	return &tokens, nil
}

// Write persists tokens to disk using atomic write-then-rename.
// It creates the parent directory if it does not exist, writes to a
// temporary file with 0o600 permissions, fsyncs, then renames atomically.
func (s *FileTokenStore) Write(tokens *Tokens) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tokens: %w", err)
	}

	// Ensure directory exists
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create token directory: %w", err)
	}

	// Write to temp file
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return fmt.Errorf("write temp token file: %w", err)
	}

	// Sync to ensure data reaches disk before rename
	f, err := os.Open(tmpPath)
	if err == nil {
		_ = f.Sync()
		_ = f.Close()
	}

	// Atomic rename
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("rename token file: %w", err)
	}

	return nil
}

// IsExpired returns true if the token is expired or will expire within 5 minutes (300 seconds).
func (s *FileTokenStore) IsExpired(tokens *Tokens) bool {
	return time.Now().Unix() >= tokens.ExpiresAt-300
}
