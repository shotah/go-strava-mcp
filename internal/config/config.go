package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config holds application configuration loaded from environment variables.
type Config struct {
	ClientID     string
	ClientSecret string
	TokenPath    string
}

// Load reads configuration from environment variables and returns a Config.
// It validates that required variables are set and applies defaults for optional ones.
func Load() (*Config, error) {
	clientID := os.Getenv("STRAVA_CLIENT_ID")
	if clientID == "" {
		return nil, errors.New("STRAVA_CLIENT_ID environment variable is required. Get it from https://www.strava.com/settings/api")
	}

	clientSecret := os.Getenv("STRAVA_CLIENT_SECRET")
	if clientSecret == "" {
		return nil, errors.New("STRAVA_CLIENT_SECRET environment variable is required. Get it from https://www.strava.com/settings/api")
	}

	tokenPath := os.Getenv("STRAVA_TOKEN_PATH")
	if tokenPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("determine home directory: %w", err)
		}
		tokenPath = filepath.Join(home, ".strava", "tokens.json")
	}

	return &Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenPath:    tokenPath,
	}, nil
}
