package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shotah/go-strava-mcp/internal/config"
)

func TestLoadReturnsConfigFromEnvVars(t *testing.T) {
	t.Setenv("STRAVA_CLIENT_ID", "test-client-id")
	t.Setenv("STRAVA_CLIENT_SECRET", "test-client-secret")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.ClientID != "test-client-id" {
		t.Errorf("ClientID = %q, want %q", cfg.ClientID, "test-client-id")
	}
	if cfg.ClientSecret != "test-client-secret" {
		t.Errorf("ClientSecret = %q, want %q", cfg.ClientSecret, "test-client-secret")
	}
}

func TestLoadErrorsOnMissingClientID(t *testing.T) {
	t.Setenv("STRAVA_CLIENT_ID", "")
	t.Setenv("STRAVA_CLIENT_SECRET", "test-secret")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for missing STRAVA_CLIENT_ID, got nil")
	}
	if !strings.Contains(err.Error(), "STRAVA_CLIENT_ID") {
		t.Errorf("error message should mention STRAVA_CLIENT_ID, got: %v", err)
	}
}

func TestLoadErrorsOnMissingClientSecret(t *testing.T) {
	t.Setenv("STRAVA_CLIENT_ID", "test-id")
	t.Setenv("STRAVA_CLIENT_SECRET", "")

	_, err := config.Load()
	if err == nil {
		t.Fatal("expected error for missing STRAVA_CLIENT_SECRET, got nil")
	}
	if !strings.Contains(err.Error(), "STRAVA_CLIENT_SECRET") {
		t.Errorf("error message should mention STRAVA_CLIENT_SECRET, got: %v", err)
	}
}

func TestLoadDefaultTokenPath(t *testing.T) {
	t.Setenv("STRAVA_CLIENT_ID", "test-id")
	t.Setenv("STRAVA_CLIENT_SECRET", "test-secret")
	// Ensure STRAVA_TOKEN_PATH is unset
	os.Unsetenv("STRAVA_TOKEN_PATH")
	t.Setenv("STRAVA_TOKEN_PATH", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("cannot get home dir: %v", err)
	}

	expected := filepath.Join(home, ".strava", "tokens.json")
	if cfg.TokenPath != expected {
		t.Errorf("TokenPath = %q, want %q", cfg.TokenPath, expected)
	}
}

func TestLoadCustomTokenPath(t *testing.T) {
	t.Setenv("STRAVA_CLIENT_ID", "test-id")
	t.Setenv("STRAVA_CLIENT_SECRET", "test-secret")
	t.Setenv("STRAVA_TOKEN_PATH", "/custom/path/tokens.json")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.TokenPath != "/custom/path/tokens.json" {
		t.Errorf("TokenPath = %q, want %q", cfg.TokenPath, "/custom/path/tokens.json")
	}
}
