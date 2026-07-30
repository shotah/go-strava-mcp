package update

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

// ReleaseAsset represents a downloadable file attached to a GitHub release.
type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Result holds the outcome of a version check.
type Result struct {
	UpdateAvailable bool           `json:"update_available"`
	CurrentVersion  string         `json:"current_version"`
	LatestVersion   string         `json:"latest_version"`
	ReleaseURL      string         `json:"release_url"`
	Assets          []ReleaseAsset `json:"assets,omitempty"`
}

// Checker queries GitHub Releases for the latest version.
type Checker struct {
	httpClient *http.Client
	apiURL     string
	cache      *Cache
	currentVer *semver.Version
	rawVersion string
	logger     *slog.Logger
}

// githubRelease is the subset of GitHub's release JSON we parse.
type githubRelease struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Assets  []ReleaseAsset `json:"assets"`
}

// NewChecker creates a Checker for the given current version.
// If currentVersion cannot be parsed as semver (e.g. "dev"), IsDev() returns true
// and all checks are no-ops.
func NewChecker(currentVersion string, cache *Cache, logger *slog.Logger) *Checker {
	c := &Checker{
		httpClient: &http.Client{Timeout: 5 * time.Second},
		apiURL:     "https://api.github.com/repos/shotah/go-strava-mcp/releases/latest",
		cache:      cache,
		rawVersion: currentVersion,
		logger:     logger,
	}

	// Strip leading "v" for semver parsing (goreleaser sets Version without prefix).
	cleaned := strings.TrimPrefix(currentVersion, "v")
	v, err := semver.NewVersion(cleaned)
	if err == nil {
		c.currentVer = v
	}
	// If parse fails (e.g. "dev"), currentVer stays nil â†’ IsDev() returns true.

	return c
}

// SetAPIURL overrides the GitHub API URL. Intended for testing.
func (c *Checker) SetAPIURL(url string) {
	c.apiURL = url
}

// IsDev returns true if the binary is a local development build.
// Dev builds never trigger version checks or network calls.
func (c *Checker) IsDev() bool {
	return c.rawVersion == "dev" || c.currentVer == nil
}

// Check queries the GitHub Releases API and compares the latest tag against
// the current version. It does NOT read or write the cache â€” use
// CheckWithCooldown for cache-gated checks.
func (c *Checker) Check(ctx context.Context) (*Result, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github api request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("parse github response: %w", err)
	}

	// Parse the tag (GitHub tags are "v1.2.0").
	cleaned := strings.TrimPrefix(release.TagName, "v")
	latestVer, err := semver.NewVersion(cleaned)
	if err != nil {
		return nil, fmt.Errorf("parse release tag %q: %w", release.TagName, err)
	}

	result := &Result{
		CurrentVersion: c.currentVer.Original(),
		LatestVersion:  release.TagName,
		ReleaseURL:     release.HTMLURL,
		Assets:         release.Assets,
	}

	if latestVer.GreaterThan(c.currentVer) {
		result.UpdateAvailable = true
	}

	return result, nil
}

// CheckWithCooldown is the background-safe entry point. It respects the dev
// guard and 24h cooldown cache before hitting the GitHub API.
func (c *Checker) CheckWithCooldown(ctx context.Context, cooldown time.Duration) (*Result, error) {
	// Dev guard: never check, never call network.
	if c.IsDev() {
		return nil, nil
	}

	// Cooldown gate: return cached result if within window.
	if !c.cache.ShouldCheck(cooldown) {
		data, err := c.cache.Read()
		if err != nil {
			// Cache unreadable after ShouldCheck said "don't check" â€” skip silently.
			return nil, nil
		}
		// Reconstruct a Result from cached data.
		if data.LatestVersion == "" {
			return nil, nil
		}
		cleaned := strings.TrimPrefix(data.LatestVersion, "v")
		latestVer, err := semver.NewVersion(cleaned)
		if err != nil {
			return nil, nil
		}
		return &Result{
			UpdateAvailable: latestVer.GreaterThan(c.currentVer),
			CurrentVersion:  c.currentVer.Original(),
			LatestVersion:   data.LatestVersion,
			ReleaseURL:      data.ReleaseURL,
		}, nil
	}

	// Cooldown expired or no cache: hit the API.
	result, err := c.Check(ctx)
	if err != nil {
		return nil, err
	}

	// Update cache with fresh data.
	if writeErr := c.cache.Write(result.LatestVersion, result.ReleaseURL); writeErr != nil {
		c.logger.Debug("failed to update cache", "err", writeErr)
	}

	return result, nil
}

// FormatNotification returns the two-line gh CLI style notification.
// Returns empty string if no update is available or result is nil.
func (c *Checker) FormatNotification(r *Result) string {
	if r == nil || !r.UpdateAvailable {
		return ""
	}
	current := ensureVPrefix(r.CurrentVersion)
	latest := ensureVPrefix(r.LatestVersion)
	return fmt.Sprintf(
		"A new release of strava-mcp is available: %s -> %s\nhttps://github.com/shotah/go-strava-mcp/releases/latest",
		current, latest,
	)
}

// FormatCheckOutput returns the --check-update formatted output.
// Returns empty string if result is nil.
func (c *Checker) FormatCheckOutput(r *Result) string {
	if r == nil {
		return ""
	}
	current := ensureVPrefix(r.CurrentVersion)
	if r.UpdateAvailable {
		latest := ensureVPrefix(r.LatestVersion)
		return fmt.Sprintf("Current: %s\nLatest:  %s\nUpdate available! Run: strava-mcp --update", current, latest)
	}
	return fmt.Sprintf("Up to date (%s)", current)
}

// ensureVPrefix adds a "v" prefix if not already present.
func ensureVPrefix(version string) string {
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}
