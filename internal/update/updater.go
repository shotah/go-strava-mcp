package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// maxBinarySize bounds the decompressed binary extracted from a release
// archive, guarding against a decompression bomb.
const maxBinarySize = 256 << 20 // 256 MiB

// ProgressFunc is called with status messages during the update process.
// All messages go to stderr. The caller provides the callback.
type ProgressFunc func(msg string)

// Updater downloads, verifies, and installs binary updates from GitHub Releases.
type Updater struct {
	checker    *Checker
	httpClient *http.Client
	logger     *slog.Logger
}

// NewUpdater creates an Updater that uses the given Checker for version lookups.
func NewUpdater(checker *Checker, logger *slog.Logger) *Updater {
	return &Updater{
		checker:    checker,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		logger:     logger,
	}
}

// IsHomebrew returns true if the resolved binary path indicates Homebrew management.
// Checks both Intel (/usr/local/Caskroom/, /usr/local/Cellar/) and
// Apple Silicon (/opt/homebrew/Caskroom/, /opt/homebrew/Cellar/) paths.
func IsHomebrew(binaryPath string) bool {
	resolved, err := filepath.EvalSymlinks(binaryPath)
	if err != nil {
		resolved = binaryPath
	}
	return strings.Contains(resolved, "/Caskroom/") ||
		strings.Contains(resolved, "/Cellar/")
}

// CheckWritePermission verifies the binary's directory is writable before downloading.
// Returns nil if writable, or an error with an actionable message.
func CheckWritePermission(binaryPath string) error {
	dir := filepath.Dir(binaryPath)
	// Try creating a temp file in the target directory.
	tmp, err := os.CreateTemp(dir, ".strava-mcp-update-check-*")
	if err != nil {
		return fmt.Errorf("permission denied: cannot write to %s - run with sudo or move binary to a user-writable location", dir)
	}
	tmp.Close()
	os.Remove(tmp.Name())
	return nil
}

// archiveName returns the goreleaser archive name for the current platform.
// Format: strava-mcp_{version}_{os}_{arch}.tar.gz
// The version's "v" prefix is stripped (goreleaser uses bare version numbers).
func archiveName(version string) string {
	ver := strings.TrimPrefix(version, "v")
	return fmt.Sprintf("strava-mcp_%s_%s_%s.tar.gz", ver, runtime.GOOS, runtime.GOARCH)
}

// checksumsName returns the goreleaser checksums filename.
// Format: strava-mcp_{version}_checksums.txt
func checksumsName(version string) string {
	ver := strings.TrimPrefix(version, "v")
	return fmt.Sprintf("strava-mcp_%s_checksums.txt", ver)
}

// findAssetURL searches the release assets for a file by name and returns its download URL.
func findAssetURL(assets []ReleaseAsset, name string) (string, error) {
	for _, a := range assets {
		if a.Name == name {
			if a.BrowserDownloadURL == "" {
				return "", fmt.Errorf("asset %q has no download URL", name)
			}
			return a.BrowserDownloadURL, nil
		}
	}
	return "", fmt.Errorf("asset %q not found in release", name)
}

// download fetches a URL to a temp file in the given directory. Returns the temp file path.
func (u *Updater) download(ctx context.Context, url, dir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := u.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp(dir, ".strava-mcp-download-*")
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer tmp.Close()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("write download: %w", err)
	}

	return tmp.Name(), nil
}

// verifySHA256 checks that the file at path matches the expected hex-encoded SHA256 hash.
func verifySHA256(path, expectedHash string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open for checksum: %w", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash file: %w", err)
	}

	actual := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(actual, expectedHash) {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actual)
	}
	return nil
}

// parseChecksum reads a goreleaser checksums.txt file and returns the hash for the given filename.
// Format: "{hash}  {filename}" (two spaces between hash and filename, standard sha256sum format).
func parseChecksum(checksumsPath, targetFilename string) (string, error) {
	data, err := os.ReadFile(checksumsPath)
	if err != nil {
		return "", fmt.Errorf("read checksums: %w", err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 2 && parts[1] == targetFilename {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("no checksum found for %s in checksums file", targetFilename)
}

// extractBinary opens a tar.gz archive and extracts the file named "strava-mcp" to dest.
// Returns error if the binary is not found in the archive.
func extractBinary(archivePath, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip reader: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}
		// Match the binary name, handling possible directory prefix.
		name := filepath.Base(hdr.Name)
		if name == "strava-mcp" && hdr.Typeflag == tar.TypeReg {
			// The binary needs the executable bit; goreleaser archives are trusted
			// (checksum-verified) but the size is still bounded below.
			out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755) //nolint:gosec // G302: executable must be runnable
			if err != nil {
				return fmt.Errorf("create binary: %w", err)
			}
			// LimitReader bounds the decompressed size (gosec G110).
			written, err := io.Copy(out, io.LimitReader(tr, maxBinarySize+1))
			out.Close()
			if err != nil {
				return fmt.Errorf("extract binary: %w", err)
			}
			if written > maxBinarySize {
				return fmt.Errorf("extracted binary exceeds %d bytes", maxBinarySize)
			}
			return nil
		}
	}
	return errors.New("strava-mcp binary not found in archive")
}

// Update performs the full self-update sequence:
//  1. Check for latest version via GitHub API
//  2. Verify not already up to date
//  3. Find platform-specific archive and checksums in release assets
//  4. Download checksums.txt, parse expected hash
//  5. Download platform archive, verify SHA256
//  6. Extract binary from tar.gz to temp file
//  7. Rename current binary to .bak (rollback preservation)
//  8. Rename new binary to target path (atomic replace)
//
// On any failure after step 7, the .bak is restored.
// binaryPath is the resolved path to the current executable.
func (u *Updater) Update(ctx context.Context, binaryPath string, progress ProgressFunc) error {
	if progress == nil {
		progress = func(string) {}
	}

	// Step 1: Check for updates.
	progress("Checking for updates...")
	result, err := u.checker.Check(ctx)
	if err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}

	// Step 2: Already up to date?
	if !result.UpdateAvailable {
		progress(fmt.Sprintf("Already up to date (%s)", ensureVPrefix(result.CurrentVersion)))
		return nil
	}

	latestTag := result.LatestVersion
	progress(fmt.Sprintf("Downloading %s (%s/%s)...", ensureVPrefix(latestTag), runtime.GOOS, runtime.GOARCH))

	// Step 3: Find asset URLs.
	archName := archiveName(latestTag)
	checksumFile := checksumsName(latestTag)

	archiveURL, err := findAssetURL(result.Assets, archName)
	if err != nil {
		return fmt.Errorf("find archive asset: %w", err)
	}
	checksumsURL, err := findAssetURL(result.Assets, checksumFile)
	if err != nil {
		return fmt.Errorf("find checksums asset: %w", err)
	}

	// Use the binary's directory for all temp files (same filesystem = atomic rename).
	dir := filepath.Dir(binaryPath)

	// Step 4: Download and parse checksums.
	checksumsPath, err := u.download(ctx, checksumsURL, dir)
	if err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	defer os.Remove(checksumsPath)

	expectedHash, err := parseChecksum(checksumsPath, archName)
	if err != nil {
		return fmt.Errorf("parse checksum: %w", err)
	}

	// Step 5: Download archive and verify SHA256.
	archivePath, err := u.download(ctx, archiveURL, dir)
	if err != nil {
		return fmt.Errorf("download archive: %w", err)
	}
	defer os.Remove(archivePath)

	progress("Verifying checksum...")
	if err := verifySHA256(archivePath, expectedHash); err != nil {
		return fmt.Errorf("archive verification failed: %w", err)
	}

	// Step 6: Extract binary to temp file.
	newBinaryPath := binaryPath + ".new"
	if err := extractBinary(archivePath, newBinaryPath); err != nil {
		os.Remove(newBinaryPath)
		return fmt.Errorf("extract binary: %w", err)
	}

	// Step 7: Rollback preservation — rename current to .bak.
	progress("Updating binary...")
	bakPath := binaryPath + ".bak"
	// Remove any previous .bak to avoid rename failure.
	os.Remove(bakPath)
	if err := os.Rename(binaryPath, bakPath); err != nil {
		os.Remove(newBinaryPath)
		return fmt.Errorf("backup current binary: %w", err)
	}

	// Step 8: Atomic replace — rename .new to target.
	if err := os.Rename(newBinaryPath, binaryPath); err != nil {
		// Restore from backup on failure.
		if restoreErr := os.Rename(bakPath, binaryPath); restoreErr != nil {
			return fmt.Errorf("CRITICAL: rename failed (%w) and restore failed (%w) - manually restore from %s", err, restoreErr, bakPath)
		}
		return fmt.Errorf("replace binary: %w (restored from backup)", err)
	}

	progress(fmt.Sprintf("Updated to %s! Previous version saved as %s", ensureVPrefix(latestTag), filepath.Base(bakPath)))
	progress("Restart strava-mcp to use " + ensureVPrefix(latestTag))
	return nil
}
