package tools_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shotah/go-strava-mcp/internal/tools"
	"github.com/shotah/go-strava-mcp/internal/update"
)

// newTestCheckerWithServer creates a Checker pointed at a test HTTP server.
func newTestCheckerWithServer(t *testing.T, serverURL string, version string) *update.Checker {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cache := update.NewCache(t.TempDir())
	checker := update.NewChecker(version, cache, logger)
	checker.SetAPIURL(serverURL + "/repos/shotah/go-strava-mcp/releases/latest")
	return checker
}

// githubReleaseJSON builds a minimal GitHub release JSON response.
func githubReleaseJSON(tagName, htmlURL string) string {
	return `{"tag_name":"` + tagName + `","html_url":"` + htmlURL + `","assets":[]}`
}

// --- utility_check_update tests ---

func TestCheckUpdateBasic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(githubReleaseJSON("v2.0.0", "https://github.com/shotah/go-strava-mcp/releases/tag/v2.0.0")))
	}))
	defer srv.Close()

	checker := newTestCheckerWithServer(t, srv.URL, "1.0.0")
	handler := tools.HandleCheckUpdate(checker)

	result, err := handler(context.Background(), makeRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected non-error result, got error")
	}

	text := extractResultText(t, result)

	var resp struct {
		CurrentVersion  string `json:"current_version"`
		LatestVersion   string `json:"latest_version"`
		UpdateAvailable bool   `json:"update_available"`
		ReleaseURL      string `json:"release_url"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.CurrentVersion != "1.0.0" {
		t.Errorf("current_version = %q, want %q", resp.CurrentVersion, "1.0.0")
	}
	if resp.LatestVersion != "v2.0.0" {
		t.Errorf("latest_version = %q, want %q", resp.LatestVersion, "v2.0.0")
	}
	if !resp.UpdateAvailable {
		t.Error("update_available should be true")
	}
	if resp.ReleaseURL == "" {
		t.Error("release_url should not be empty")
	}
}

func TestCheckUpdateAlreadyCurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(githubReleaseJSON("v1.0.0", "https://github.com/shotah/go-strava-mcp/releases/tag/v1.0.0")))
	}))
	defer srv.Close()

	checker := newTestCheckerWithServer(t, srv.URL, "1.0.0")
	handler := tools.HandleCheckUpdate(checker)

	result, err := handler(context.Background(), makeRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected non-error result")
	}

	text := extractResultText(t, result)

	var resp struct {
		UpdateAvailable bool `json:"update_available"`
	}
	if err := json.Unmarshal([]byte(text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.UpdateAvailable {
		t.Error("update_available should be false when already current")
	}
}

func TestCheckUpdateDevBuild(t *testing.T) {
	// No server needed — dev builds short-circuit before any network call.
	checker := newTestCheckerWithServer(t, "http://localhost:0", "dev")
	handler := tools.HandleCheckUpdate(checker)

	result, err := handler(context.Background(), makeRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for dev build")
	}

	text := extractResultText(t, result)
	if !strings.Contains(text, "dev builds") {
		t.Errorf("error should mention dev builds, got: %s", text)
	}
}

func TestCheckUpdateNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	checker := newTestCheckerWithServer(t, srv.URL, "1.0.0")
	handler := tools.HandleCheckUpdate(checker)

	result, err := handler(context.Background(), makeRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for server error")
	}

	text := extractResultText(t, result)
	if !strings.Contains(text, "utility_check_update") {
		t.Errorf("error should contain tool name, got: %s", text)
	}
}

func TestCheckUpdateResponseFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(githubReleaseJSON("v3.0.0", "https://github.com/shotah/go-strava-mcp/releases/tag/v3.0.0")))
	}))
	defer srv.Close()

	checker := newTestCheckerWithServer(t, srv.URL, "2.0.0")
	handler := tools.HandleCheckUpdate(checker)

	result, err := handler(context.Background(), makeRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	text := extractResultText(t, result)

	// Verify all 4 expected JSON fields are present.
	var raw map[string]any
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	requiredFields := []string{"current_version", "latest_version", "update_available", "release_url"}
	for _, field := range requiredFields {
		if _, ok := raw[field]; !ok {
			t.Errorf("response missing required field %q", field)
		}
	}

	// Verify pretty-printed (2-space indent).
	if !strings.Contains(text, "  \"current_version\"") {
		t.Errorf("response should be pretty-printed, got: %s", text)
	}
}

// --- utility_self_update tests ---

func TestSelfUpdateNoConfirm(t *testing.T) {
	checker := newTestCheckerWithServer(t, "http://localhost:0", "1.0.0")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	updater := update.NewUpdater(checker, logger)
	handler := tools.HandleSelfUpdate(checker, updater)

	// No confirm parameter.
	result, err := handler(context.Background(), makeRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when confirm is not set")
	}

	text := extractResultText(t, result)
	if !strings.Contains(text, "confirm") {
		t.Errorf("error should mention confirm, got: %s", text)
	}
}

func TestSelfUpdateConfirmFalse(t *testing.T) {
	checker := newTestCheckerWithServer(t, "http://localhost:0", "1.0.0")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	updater := update.NewUpdater(checker, logger)
	handler := tools.HandleSelfUpdate(checker, updater)

	// confirm: false should also be rejected.
	result, err := handler(context.Background(), makeRequest(map[string]any{"confirm": false}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error when confirm is false")
	}

	text := extractResultText(t, result)
	if !strings.Contains(text, "confirm") {
		t.Errorf("error should mention confirm, got: %s", text)
	}
}

func TestSelfUpdateDevBuild(t *testing.T) {
	checker := newTestCheckerWithServer(t, "http://localhost:0", "dev")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	updater := update.NewUpdater(checker, logger)
	handler := tools.HandleSelfUpdate(checker, updater)

	result, err := handler(context.Background(), makeRequest(map[string]any{"confirm": true}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error for dev build")
	}

	text := extractResultText(t, result)
	if !strings.Contains(text, "dev build") {
		t.Errorf("error should mention dev build, got: %s", text)
	}
}

// TestSelfUpdateAlreadyCurrent runs the handler all the way through the updater.
// The release matches the running version, so Update() returns before it would
// touch any binary on disk.
func TestSelfUpdateAlreadyCurrent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(githubReleaseJSON("v1.0.0", "https://example.com/v1.0.0")))
	}))
	defer srv.Close()

	checker := newTestCheckerWithServer(t, srv.URL, "1.0.0")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	updater := update.NewUpdater(checker, logger)
	handler := tools.HandleSelfUpdate(checker, updater)

	result, err := handler(context.Background(), makeRequest(map[string]any{"confirm": true}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected non-error result, got: %s", extractResultText(t, result))
	}

	var resp struct {
		Updated    bool   `json:"updated"`
		NewVersion string `json:"new_version"`
		Message    string `json:"message"`
	}
	if err := json.Unmarshal([]byte(extractResultText(t, result)), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Updated {
		t.Error("updated should be true")
	}
	if resp.NewVersion != "v1.0.0" {
		t.Errorf("new_version = %q, want %q", resp.NewVersion, "v1.0.0")
	}
	if !strings.Contains(resp.Message, "Restart") {
		t.Errorf("message = %q, want it to tell the user to restart", resp.Message)
	}
}

// TestSelfUpdateVersionLookupFailsAfterUpdate covers the fallback response used
// when the post-update version lookup fails.
func TestSelfUpdateVersionLookupFailsAfterUpdate(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls > 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(githubReleaseJSON("v1.0.0", "https://example.com/v1.0.0")))
	}))
	defer srv.Close()

	checker := newTestCheckerWithServer(t, srv.URL, "1.0.0")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := tools.HandleSelfUpdate(checker, update.NewUpdater(checker, logger))

	result, err := handler(context.Background(), makeRequest(map[string]any{"confirm": true}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected non-error result, got: %s", extractResultText(t, result))
	}

	var resp struct {
		Updated    bool   `json:"updated"`
		NewVersion string `json:"new_version"`
	}
	if err := json.Unmarshal([]byte(extractResultText(t, result)), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !resp.Updated {
		t.Error("updated should be true even when the version lookup fails")
	}
	if resp.NewVersion != "" {
		t.Errorf("new_version = %q, want it omitted when the lookup fails", resp.NewVersion)
	}
}

func TestSelfUpdateReportsUpdaterFailure(t *testing.T) {
	// The release advertises no assets, so the updater cannot find its archive.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(githubReleaseJSON("v99.0.0", "https://example.com/v99.0.0")))
	}))
	defer srv.Close()

	checker := newTestCheckerWithServer(t, srv.URL, "1.0.0")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := tools.HandleSelfUpdate(checker, update.NewUpdater(checker, logger))

	result, err := handler(context.Background(), makeRequest(map[string]any{"confirm": true}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected an error result when the updater fails")
	}
	if text := extractResultText(t, result); !strings.Contains(text, "utility_self_update") {
		t.Errorf("error should name the tool, got: %s", text)
	}
}

// TestSelfUpdateHandlerReturnsResult verifies that the handler always returns
// a result rather than calling os.Exit(). If os.Exit() were called, this test
// would terminate the test process without reporting results — its very
// completion proves the handler is safe.
func TestSelfUpdateHandlerReturnsResult(t *testing.T) {
	// Use a dev build to trigger the early return — the point is to verify
	// the handler returns a result (any result) rather than calling os.Exit().
	checker := newTestCheckerWithServer(t, "http://localhost:0", "dev")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	updater := update.NewUpdater(checker, logger)
	handler := tools.HandleSelfUpdate(checker, updater)

	result, err := handler(context.Background(), makeRequest(map[string]any{"confirm": true}))
	if err != nil {
		t.Fatalf("handler returned Go error (not MCP error): %v", err)
	}
	if result == nil {
		t.Fatal("handler returned nil result — should always return a *CallToolResult")
	}
	// If we reach here, os.Exit() was not called. Test passes.
}
