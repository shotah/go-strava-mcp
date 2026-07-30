package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/shotah/go-strava-mcp/internal/update"
)

// TestBinary builds the binary once for all tests in this file.
// Tests that need the binary call buildTestBinary(t).
func buildTestBinary(t *testing.T) string {
	t.Helper()
	name := "strava-mcp-test"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binPath := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build test binary: %v", err)
	}
	return binPath
}

func TestCheckUpdateFlag_DevVersion(t *testing.T) {
	// Default build has Version="dev" (no ldflags override).
	bin := buildTestBinary(t)

	cmd := exec.Command(bin, "--check-update")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// --check-update with dev version should exit 0.
		t.Fatalf("--check-update exited with error: %v\noutput: %s", err, output)
	}

	if got := string(output); !strings.Contains(got, "dev build") {
		t.Errorf("expected 'dev build' in output, got: %s", got)
	}
}

func TestVersionFlag_StillWorks(t *testing.T) {
	bin := buildTestBinary(t)

	cmd := exec.Command(bin, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--version exited with error: %v\noutput: %s", err, output)
	}

	got := string(output)
	if !strings.Contains(got, "strava-mcp") {
		t.Errorf("expected 'strava-mcp' in --version output, got: %s", got)
	}
	if !strings.Contains(got, "dev") {
		t.Errorf("expected 'dev' in --version output, got: %s", got)
	}
}

func TestCheckUpdateFlag_ParsesCorrectly(t *testing.T) {
	// Verify --check-update and --debug can coexist without conflict.
	bin := buildTestBinary(t)

	cmd := exec.Command(bin, "--debug", "--check-update")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("--debug --check-update exited with error: %v\noutput: %s", err, output)
	}

	if got := string(output); !strings.Contains(got, "dev build") {
		t.Errorf("expected 'dev build' in output, got: %s", got)
	}
}

func TestCacheDir_ReturnsValidPath(t *testing.T) {
	dir := cacheDir()
	if dir == "" {
		t.Fatal("cacheDir() returned empty string")
	}
	if !strings.Contains(dir, ".strava") {
		t.Errorf("cacheDir() = %q, expected path containing '.strava'", dir)
	}
}

func TestParseCLI(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want cliOptions
	}{
		{
			name: "no args",
			args: nil,
			want: cliOptions{},
		},
		{
			name: "debug only",
			args: []string{"--debug"},
			want: cliOptions{debug: true},
		},
		{
			name: "version only",
			args: []string{"--version"},
			want: cliOptions{showVersion: true},
		},
		{
			name: "check update with debug",
			args: []string{"--debug", "--check-update"},
			want: cliOptions{debug: true, checkUpdate: true},
		},
		{
			name: "update with force",
			args: []string{"--update", "--force"},
			want: cliOptions{doUpdate: true, forceUpdate: true},
		},
		{
			name: "auth subcommand",
			args: []string{"auth"},
			want: cliOptions{positional: []string{"auth"}},
		},
		{
			name: "flags and positionals mixed",
			args: []string{"--debug", "auth", "extra"},
			want: cliOptions{debug: true, positional: []string{"auth", "extra"}},
		},
		{
			name: "unknown flag becomes positional",
			args: []string{"--nope"},
			want: cliOptions{positional: []string{"--nope"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCLI(tt.args)
			if got.debug != tt.want.debug ||
				got.showVersion != tt.want.showVersion ||
				got.checkUpdate != tt.want.checkUpdate ||
				got.doUpdate != tt.want.doUpdate ||
				got.forceUpdate != tt.want.forceUpdate {
				t.Errorf("parseCLI(%v) flags = %+v, want %+v", tt.args, got, tt.want)
			}
			if strings.Join(got.positional, ",") != strings.Join(tt.want.positional, ",") {
				t.Errorf("parseCLI(%v) positional = %v, want %v", tt.args, got.positional, tt.want.positional)
			}
		})
	}
}

func TestRun_Version(t *testing.T) {
	var buf bytes.Buffer
	if code := run([]string{"--version"}, &buf); code != 0 {
		t.Errorf("run(--version) = %d, want 0", code)
	}
	out := buf.String()
	if !strings.Contains(out, "strava-mcp") {
		t.Errorf("output = %q, want it to contain 'strava-mcp'", out)
	}
	if !strings.Contains(out, Version) {
		t.Errorf("output = %q, want it to contain version %q", out, Version)
	}
}

func TestRun_CheckUpdateDevBuild(t *testing.T) {
	var buf bytes.Buffer
	if code := run([]string{"--check-update"}, &buf); code != 0 {
		t.Errorf("run(--check-update) = %d, want 0 for a dev build", code)
	}
	if out := buf.String(); !strings.Contains(out, "dev build") {
		t.Errorf("output = %q, want it to mention 'dev build'", out)
	}
}

func TestRun_UpdateDevBuild(t *testing.T) {
	var buf bytes.Buffer
	if code := run([]string{"--update", "--force"}, &buf); code != 0 {
		t.Errorf("run(--update) = %d, want 0 for a dev build", code)
	}
	if out := buf.String(); !strings.Contains(out, "dev build") {
		t.Errorf("output = %q, want it to mention 'dev build'", out)
	}
}

func TestRun_AuthWithoutConfigFails(t *testing.T) {
	clearStravaEnv(t)

	var buf bytes.Buffer
	if code := run([]string{"auth"}, &buf); code != 1 {
		t.Errorf("run(auth) = %d, want 1 without credentials", code)
	}
	if out := buf.String(); !strings.Contains(out, "configuration error") {
		t.Errorf("output = %q, want it to mention 'configuration error'", out)
	}
}

func TestRun_ServerWithoutConfigFails(t *testing.T) {
	clearStravaEnv(t)

	// config.Load fails before the server ever reads stdin, so this returns.
	var buf bytes.Buffer
	if code := run(nil, &buf); code != 1 {
		t.Errorf("run(nil) = %d, want 1 without credentials", code)
	}
	if out := buf.String(); !strings.Contains(out, "STRAVA_CLIENT_ID") {
		t.Errorf("output = %q, want it to name the missing variable", out)
	}
}

func TestReport(t *testing.T) {
	var buf bytes.Buffer
	if code := report(&buf, nil); code != 0 {
		t.Errorf("report(nil) = %d, want 0", code)
	}
	if buf.Len() != 0 {
		t.Errorf("report(nil) wrote %q, want nothing", buf.String())
	}

	if code := report(&buf, errNoHomeDir); code != 1 {
		t.Errorf("report(err) = %d, want 1", code)
	}
	if !strings.Contains(buf.String(), errNoHomeDir.Error()) {
		t.Errorf("report(err) wrote %q, want it to contain the error text", buf.String())
	}
}

func TestRunCheckUpdate_DevBuild(t *testing.T) {
	var buf bytes.Buffer
	if err := runCheckUpdate(&buf); err != nil {
		t.Fatalf("runCheckUpdate() error = %v, want nil for a dev build", err)
	}
	if !strings.Contains(buf.String(), "version check not available") {
		t.Errorf("output = %q, want the dev-build notice", buf.String())
	}
}

func TestRunSelfUpdate_DevBuild(t *testing.T) {
	var buf bytes.Buffer
	if err := runSelfUpdate(&buf, false); err != nil {
		t.Fatalf("runSelfUpdate() error = %v, want nil for a dev build", err)
	}
	if !strings.Contains(buf.String(), "update not available") {
		t.Errorf("output = %q, want the dev-build notice", buf.String())
	}
}

func TestBuildServer_Succeeds(t *testing.T) {
	t.Setenv("STRAVA_CLIENT_ID", "test-id")
	t.Setenv("STRAVA_CLIENT_SECRET", "test-secret")
	t.Setenv("STRAVA_TOKEN_PATH", filepath.Join(t.TempDir(), "tokens.json"))

	s, err := buildServer()
	if err != nil {
		t.Fatalf("buildServer() error = %v", err)
	}
	if s == nil {
		t.Fatal("buildServer() = nil, want an MCP server")
	}
}

func TestBuildServer_ConfigError(t *testing.T) {
	clearStravaEnv(t)

	if _, err := buildServer(); err == nil {
		t.Fatal("buildServer() = nil error, want a configuration error")
	}
}

func TestRunCheckUpdate_UpToDate(t *testing.T) {
	stubChecker(t, "1.0.0", releaseHandler("v1.0.0"))

	var buf bytes.Buffer
	if err := runCheckUpdate(&buf); err != nil {
		t.Fatalf("runCheckUpdate() error = %v", err)
	}
	if !strings.Contains(buf.String(), "Up to date") {
		t.Errorf("output = %q, want it to report being up to date", buf.String())
	}
}

func TestRunCheckUpdate_UpdateAvailable(t *testing.T) {
	stubChecker(t, "1.2.3", releaseHandler("v2.0.0"))

	var buf bytes.Buffer
	if err := runCheckUpdate(&buf); err != nil {
		t.Fatalf("runCheckUpdate() error = %v", err)
	}
	if !strings.Contains(buf.String(), "Update available!") {
		t.Errorf("output = %q, want it to report an available update", buf.String())
	}
}

func TestRunCheckUpdate_APIError(t *testing.T) {
	stubChecker(t, "1.0.0", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	var buf bytes.Buffer
	err := runCheckUpdate(&buf)
	if err == nil {
		t.Fatal("runCheckUpdate() = nil, want an error when the API fails")
	}
	if !strings.Contains(err.Error(), "checking for updates") {
		t.Errorf("error = %v, want it to mention 'checking for updates'", err)
	}
}

func TestRunCheckUpdate_NoHomeDir(t *testing.T) {
	newChecker = func(*slog.Logger) (*update.Checker, error) { return nil, errNoHomeDir }
	t.Cleanup(func() { newChecker = defaultChecker })

	var buf bytes.Buffer
	if err := runCheckUpdate(&buf); !errors.Is(err, errNoHomeDir) {
		t.Errorf("runCheckUpdate() error = %v, want errNoHomeDir", err)
	}
}

func TestRunSelfUpdate_AlreadyUpToDate(t *testing.T) {
	stubChecker(t, "1.0.0", releaseHandler("v1.0.0"))

	// An up-to-date release means Update() returns before touching any binary.
	var buf bytes.Buffer
	if err := runSelfUpdate(&buf, false); err != nil {
		t.Fatalf("runSelfUpdate() error = %v", err)
	}
	if !strings.Contains(buf.String(), "Already up to date") {
		t.Errorf("output = %q, want it to report being up to date", buf.String())
	}
}

func TestRunSelfUpdate_NoHomeDir(t *testing.T) {
	newChecker = func(*slog.Logger) (*update.Checker, error) { return nil, errNoHomeDir }
	t.Cleanup(func() { newChecker = defaultChecker })

	var buf bytes.Buffer
	if err := runSelfUpdate(&buf, false); !errors.Is(err, errNoHomeDir) {
		t.Errorf("runSelfUpdate() error = %v, want errNoHomeDir", err)
	}
}

func TestStartBackgroundUpdateCheck_NotifiesOnNewRelease(t *testing.T) {
	t.Setenv("STRAVA_MCP_NO_UPDATE_CHECK", "")

	called := make(chan struct{}, 1)
	stubChecker(t, "1.0.0", func(w http.ResponseWriter, r *http.Request) {
		releaseHandler("v2.0.0")(w, r)
		select {
		case called <- struct{}{}:
		default:
		}
	})

	done := startBackgroundUpdateCheck()

	select {
	case <-called:
	case <-time.After(10 * time.Second):
		t.Fatal("background update check never queried the release API")
	}
	// Wait for the goroutine to finish writing the cache before TempDir cleanup.
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("background update check did not finish")
	}
}

func TestRunServer_ConfigError(t *testing.T) {
	clearStravaEnv(t)

	err := runServer()
	if err == nil {
		t.Fatal("runServer() = nil, want a configuration error")
	}
	if !strings.Contains(err.Error(), "configuration error") {
		t.Errorf("error = %v, want it to mention 'configuration error'", err)
	}
}

func TestRunAuth_ConfigError(t *testing.T) {
	clearStravaEnv(t)

	err := runAuth()
	if err == nil {
		t.Fatal("runAuth() = nil, want a configuration error")
	}
	if !strings.Contains(err.Error(), "configuration error") {
		t.Errorf("error = %v, want it to mention 'configuration error'", err)
	}
}

func TestNewChecker_DevBuild(t *testing.T) {
	checker, err := newChecker(quietLogger(io.Discard))
	if err != nil {
		t.Fatalf("newChecker() error = %v", err)
	}
	if !checker.IsDev() {
		t.Errorf("IsDev() = false, want true for Version %q", Version)
	}
}

func TestServerOptions_NilForDevBuild(t *testing.T) {
	if opts := serverOptions(); opts != nil {
		t.Errorf("serverOptions() = %+v, want nil for a dev build", opts)
	}
}

func TestStartBackgroundUpdateCheck_OptOut(t *testing.T) {
	t.Setenv("STRAVA_MCP_NO_UPDATE_CHECK", "1")
	// Opt-out returns before touching the cache or the network.
	select {
	case <-startBackgroundUpdateCheck():
	case <-time.After(time.Second):
		t.Fatal("opt-out path should finish immediately")
	}
}

func TestStartBackgroundUpdateCheck_DevBuildSkips(t *testing.T) {
	t.Setenv("STRAVA_MCP_NO_UPDATE_CHECK", "")
	// A dev build has no comparable version, so the goroutine is never started.
	select {
	case <-startBackgroundUpdateCheck():
	case <-time.After(time.Second):
		t.Fatal("dev-build skip should finish immediately")
	}
}

func TestResolveBinaryPath(t *testing.T) {
	path, err := resolveBinaryPath()
	if err != nil {
		t.Fatalf("resolveBinaryPath() error = %v", err)
	}
	if path == "" {
		t.Error("resolveBinaryPath() = empty, want the test binary path")
	}
	if !filepath.IsAbs(path) {
		t.Errorf("resolveBinaryPath() = %q, want an absolute path", path)
	}
}

// clearStravaEnv blanks the credential environment for a single test so
// config.Load() fails deterministically.
func clearStravaEnv(t *testing.T) {
	t.Helper()
	t.Setenv("STRAVA_CLIENT_ID", "")
	t.Setenv("STRAVA_CLIENT_SECRET", "")
}

// stubChecker replaces newChecker with one at the given version whose GitHub
// API calls are served by handler, so update paths never touch the network.
func stubChecker(t *testing.T, version string, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cacheDirPath := t.TempDir()
	newChecker = func(logger *slog.Logger) (*update.Checker, error) {
		checker := update.NewChecker(version, update.NewCache(cacheDirPath), logger)
		checker.SetAPIURL(srv.URL)
		return checker, nil
	}
	t.Cleanup(func() { newChecker = defaultChecker })
}

// releaseHandler serves a minimal GitHub "latest release" payload.
func releaseHandler(tag string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"tag_name":%q,"html_url":"https://example.com/releases/%s","assets":[]}`, tag, tag)
	}
}
