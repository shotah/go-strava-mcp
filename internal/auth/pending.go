package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	pendingFile = "oauth_pending.json"
	pendingTTL  = 10 * time.Minute
)

// OAuthPending holds the PKCE verifier and state between the "auth url" and
// "auth exchange" steps.
type OAuthPending struct {
	Verifier    string `json:"verifier"`
	State       string `json:"state"`
	RedirectURI string `json:"redirect_uri"`
	Expires     int64  `json:"expires"`
}

// GeneratePKCE creates a PKCE code_verifier (43-char base64url random) and its
// S256 code_challenge.
func GeneratePKCE() (verifier, challenge string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate PKCE verifier: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(b)

	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])

	return verifier, challenge, nil
}

// SavePending writes the PKCE pending state to oauth_pending.json in dir
// with mode 0600.
func SavePending(dir string, p *OAuthPending) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create pending directory: %w", err)
	}

	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal pending state: %w", err)
	}

	path := filepath.Join(dir, pendingFile)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write pending state: %w", err)
	}

	return nil
}

// LoadPending reads the PKCE pending state from dir. Returns an error if
// the file is missing or the TTL has expired.
func LoadPending(dir string) (*OAuthPending, error) {
	path := filepath.Join(dir, pendingFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, errors.New("no pending authorization (run `strava-mcp auth url` first)")
		}
		return nil, fmt.Errorf("read pending state: %w", err)
	}

	var p OAuthPending
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse pending state: %w", err)
	}

	if time.Now().Unix() > p.Expires {
		_ = DeletePending(dir)
		return nil, errors.New("pending authorization expired (run `strava-mcp auth url` again)")
	}

	return &p, nil
}

// DeletePending removes the oauth_pending.json file from dir. It is
// idempotent — a missing file is not an error.
func DeletePending(dir string) error {
	err := os.Remove(filepath.Join(dir, pendingFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
