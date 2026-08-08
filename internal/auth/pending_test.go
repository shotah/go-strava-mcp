package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestGeneratePKCE_FormatAndLength(t *testing.T) {
	t.Parallel()
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE() error: %v", err)
	}

	if len(verifier) != 43 {
		t.Errorf("verifier length = %d, want 43 (32 bytes base64url)", len(verifier))
	}
	if len(challenge) != 43 {
		t.Errorf("challenge length = %d, want 43 (SHA-256 base64url)", len(challenge))
	}

	if _, err := base64.RawURLEncoding.DecodeString(verifier); err != nil {
		t.Errorf("verifier is not valid base64url: %v", err)
	}
	if _, err := base64.RawURLEncoding.DecodeString(challenge); err != nil {
		t.Errorf("challenge is not valid base64url: %v", err)
	}
}

func TestGeneratePKCE_S256Verification(t *testing.T) {
	t.Parallel()
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE() error: %v", err)
	}

	h := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(h[:])
	if challenge != want {
		t.Errorf("challenge = %q, want SHA256(verifier) = %q", challenge, want)
	}
}

func TestGeneratePKCE_Unique(t *testing.T) {
	t.Parallel()
	v1, _, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("first GeneratePKCE() error: %v", err)
	}
	v2, _, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("second GeneratePKCE() error: %v", err)
	}
	if v1 == v2 {
		t.Error("two calls produced identical verifiers")
	}
}

func TestSavePending_WritesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := &OAuthPending{
		Verifier:    "test-verifier",
		State:       "test-state",
		RedirectURI: "https://example.com/callback",
		Expires:     time.Now().Add(10 * time.Minute).Unix(),
	}

	if err := SavePending(dir, p); err != nil {
		t.Fatalf("SavePending() error: %v", err)
	}

	path := filepath.Join(dir, pendingFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("pending file not created: %v", err)
	}

	var got OAuthPending
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Verifier != "test-verifier" {
		t.Errorf("Verifier = %q, want %q", got.Verifier, "test-verifier")
	}
	if got.State != "test-state" {
		t.Errorf("State = %q, want %q", got.State, "test-state")
	}
	if got.RedirectURI != "https://example.com/callback" {
		t.Errorf("RedirectURI = %q, want %q", got.RedirectURI, "https://example.com/callback")
	}
}

func TestSavePending_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not implement POSIX file mode bits")
	}
	t.Parallel()
	dir := t.TempDir()
	p := &OAuthPending{Verifier: "v", State: "s", RedirectURI: "u", Expires: time.Now().Add(time.Minute).Unix()}

	if err := SavePending(dir, p); err != nil {
		t.Fatalf("SavePending() error: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, pendingFile))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file permissions = %o, want 600", perm)
	}
}

func TestSavePending_CreatesMissingDir(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	p := &OAuthPending{Verifier: "v", State: "s", RedirectURI: "u", Expires: time.Now().Add(time.Minute).Unix()}

	if err := SavePending(dir, p); err != nil {
		t.Fatalf("SavePending() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, pendingFile)); err != nil {
		t.Fatalf("file not created in nested dir: %v", err)
	}
}

func TestLoadPending_Roundtrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	want := &OAuthPending{
		Verifier:    "round-verifier",
		State:       "round-state",
		RedirectURI: "https://example.com/cb",
		Expires:     time.Now().Add(5 * time.Minute).Unix(),
	}

	if err := SavePending(dir, want); err != nil {
		t.Fatalf("SavePending() error: %v", err)
	}

	got, err := LoadPending(dir)
	if err != nil {
		t.Fatalf("LoadPending() error: %v", err)
	}
	if got.Verifier != want.Verifier {
		t.Errorf("Verifier = %q, want %q", got.Verifier, want.Verifier)
	}
	if got.State != want.State {
		t.Errorf("State = %q, want %q", got.State, want.State)
	}
	if got.RedirectURI != want.RedirectURI {
		t.Errorf("RedirectURI = %q, want %q", got.RedirectURI, want.RedirectURI)
	}
}

func TestLoadPending_Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	_, err := LoadPending(dir)
	if err == nil {
		t.Fatal("LoadPending() = nil error, want an error for missing file")
	}
}

func TestLoadPending_Expired(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := &OAuthPending{
		Verifier:    "expired-verifier",
		State:       "expired-state",
		RedirectURI: "https://example.com/cb",
		Expires:     time.Now().Add(-1 * time.Minute).Unix(),
	}

	if err := SavePending(dir, p); err != nil {
		t.Fatalf("SavePending() error: %v", err)
	}

	_, err := LoadPending(dir)
	if err == nil {
		t.Fatal("LoadPending() = nil error, want expiry error")
	}

	if _, statErr := os.Stat(filepath.Join(dir, pendingFile)); !os.IsNotExist(statErr) {
		t.Error("expired pending file should be deleted")
	}
}

func TestDeletePending_RemovesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := &OAuthPending{Verifier: "v", State: "s", RedirectURI: "u", Expires: time.Now().Add(time.Minute).Unix()}
	if err := SavePending(dir, p); err != nil {
		t.Fatalf("SavePending() error: %v", err)
	}

	if err := DeletePending(dir); err != nil {
		t.Fatalf("DeletePending() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, pendingFile)); !os.IsNotExist(err) {
		t.Error("pending file should be deleted")
	}
}

func TestDeletePending_Idempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if err := DeletePending(dir); err != nil {
		t.Fatalf("DeletePending() on missing file should be nil, got: %v", err)
	}
}
