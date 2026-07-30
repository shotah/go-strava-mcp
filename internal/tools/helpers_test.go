package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/shotah/go-strava-mcp/internal/auth"
	"github.com/shotah/go-strava-mcp/internal/config"
	"github.com/shotah/go-strava-mcp/internal/strava"
	"github.com/shotah/go-strava-mcp/internal/tools"
)

// mockTokenStore is a test implementation of auth.TokenStore.
type mockTokenStore struct {
	mu       sync.Mutex
	tokens   *auth.Tokens
	expired  bool
	writeErr error
}

func newMockTokenStore(tokens *auth.Tokens, expired bool) *mockTokenStore {
	return &mockTokenStore{
		tokens:  tokens,
		expired: expired,
	}
}

func (m *mockTokenStore) Read() (*auth.Tokens, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tokens == nil {
		return nil, errors.New("no tokens")
	}
	cpy := *m.tokens
	return &cpy, nil
}

func (m *mockTokenStore) Write(tokens *auth.Tokens) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.writeErr != nil {
		return m.writeErr
	}
	m.tokens = tokens
	m.expired = false
	return nil
}

func (m *mockTokenStore) IsExpired(tokens *auth.Tokens) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.expired
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// newTestClient creates a strava.Client pointed at the given test server URL.
func newTestClient(serverURL string) *strava.Client {
	store := newMockTokenStore(&auth.Tokens{
		AccessToken:  "test-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
	}, false)
	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}
	client := strava.NewClient(cfg, store, testLogger())
	client.SetBaseURL(serverURL)
	return client
}

func TestNewTestClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	data, err := client.Get(context.Background(), "/test", nil)
	if err != nil {
		t.Fatalf("newTestClient client.Get() error: %v", err)
	}
	if !strings.Contains(string(data), "ok") {
		t.Errorf("response = %q, want to contain 'ok'", string(data))
	}
}

func TestFormatResponseValidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	// Make a request to initialize the client (no rate limit headers)
	client.Get(context.Background(), "/init", nil)

	input := `{"name":"Morning Run","distance":10000}`
	result := tools.FormatResponse([]byte(input), client)

	// Should be pretty-printed with 2-space indent
	var pretty json.RawMessage
	if err := json.Unmarshal([]byte(input), &pretty); err != nil {
		t.Fatal(err)
	}

	// Check that result contains formatted JSON
	if result.IsError {
		t.Fatal("expected non-error result")
	}

	// Extract text from result
	text := extractResultText(t, result)
	if !strings.Contains(text, "  \"name\"") {
		t.Errorf("expected 2-space indented JSON, got:\n%s", text)
	}
	if !strings.Contains(text, "Morning Run") {
		t.Errorf("expected 'Morning Run' in result, got:\n%s", text)
	}
}

func TestFormatResponseInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.Get(context.Background(), "/init", nil)

	input := `not valid json at all`
	result := tools.FormatResponse([]byte(input), client)

	if result.IsError {
		t.Fatal("expected non-error result even for invalid JSON")
	}

	text := extractResultText(t, result)
	if text != input {
		t.Errorf("expected raw string %q, got %q", input, text)
	}
}

func TestFormatResponseWithRateLimitWarning(t *testing.T) {
	// Server that sets high rate limit usage headers
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "100,1000")
		w.Header().Set("X-RateLimit-Usage", "85,500")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	// Make a request to populate rate limits (>80% usage)
	client.Get(context.Background(), "/init", nil)

	input := `{"id":123}`
	result := tools.FormatResponse([]byte(input), client)

	text := extractResultText(t, result)
	if !strings.Contains(text, "85") {
		t.Errorf("expected rate limit warning with usage '85' in result, got:\n%s", text)
	}
	if !strings.Contains(text, "100") {
		t.Errorf("expected rate limit warning with limit '100' in result, got:\n%s", text)
	}
}

func TestFormatResponseWithoutRateLimitWarning(t *testing.T) {
	// Server with low rate limit usage
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "100,1000")
		w.Header().Set("X-RateLimit-Usage", "10,50")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.Get(context.Background(), "/init", nil)

	input := `{"id":123}`
	result := tools.FormatResponse([]byte(input), client)

	text := extractResultText(t, result)
	// Should NOT contain rate limit info
	if strings.Contains(text, "API calls") {
		t.Errorf("expected no rate limit warning, got:\n%s", text)
	}
}

func TestHandleToolErrorStravaError(t *testing.T) {
	// Create a StravaError by triggering a 403 from the server
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Rate Limit Exceeded"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.Get(context.Background(), "/test", nil)
	if err == nil {
		t.Fatal("expected error from 403 response")
	}

	result := tools.HandleToolError("activities_list", err)

	if !result.IsError {
		t.Fatal("expected IsError=true")
	}

	text := extractResultText(t, result)
	if !strings.Contains(text, "activities_list") {
		t.Errorf("expected tool name in error, got: %s", text)
	}
	if !strings.Contains(text, "403") {
		t.Errorf("expected status code 403 in error, got: %s", text)
	}
	if !strings.Contains(text, "Rate Limit Exceeded") {
		t.Errorf("expected body in error, got: %s", text)
	}
}

func TestHandleToolErrorGenericError(t *testing.T) {
	err := errors.New("connection refused")
	result := tools.HandleToolError("athlete_get", err)

	if !result.IsError {
		t.Fatal("expected IsError=true")
	}

	text := extractResultText(t, result)
	if !strings.Contains(text, "athlete_get") {
		t.Errorf("expected tool name in error, got: %s", text)
	}
	if !strings.Contains(text, "connection refused") {
		t.Errorf("expected error message in result, got: %s", text)
	}
}

// extractResultText extracts the text content from a CallToolResult.
func extractResultText(t *testing.T, result *mcp.CallToolResult) string {
	t.Helper()
	if result == nil {
		t.Fatal("result is nil")
	}
	if len(result.Content) == 0 {
		t.Fatal("result has no content")
	}
	// Marshal and unmarshal to get the text
	data, err := json.Marshal(result.Content[0])
	if err != nil {
		t.Fatalf("marshal content: %v", err)
	}
	var tc struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &tc); err != nil {
		t.Fatalf("unmarshal content: %v", err)
	}
	return tc.Text
}
