package strava_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shotah/go-strava-mcp/internal/auth"
	"github.com/shotah/go-strava-mcp/internal/config"
	"github.com/shotah/go-strava-mcp/internal/strava"
)

// mockTokenStore is a test implementation of auth.TokenStore.
type mockTokenStore struct {
	mu       sync.Mutex
	tokens   *auth.Tokens
	expired  bool
	writeCh  chan *auth.Tokens // optional: signals writes
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
		return nil, fmt.Errorf("no tokens")
	}
	// Return a copy to avoid data races
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
	if m.writeCh != nil {
		m.writeCh <- tokens
	}
	return nil
}

func (m *mockTokenStore) IsExpired(tokens *auth.Tokens) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.expired
}

func (m *mockTokenStore) setExpired(expired bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expired = expired
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// Test 1: Get() adds Authorization Bearer header with token from store
func TestGetAddsBearerHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	store := newMockTokenStore(&auth.Tokens{
		AccessToken:  "test-access-token",
		RefreshToken: "test-refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
	}, false)

	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}
	client := strava.NewClient(cfg, store, testLogger())
	client.SetBaseURL(srv.URL)

	_, err := client.Get(context.Background(), "/athlete", nil)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}

	if gotAuth != "Bearer test-access-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-access-token")
	}
}

// Test 2: Get() auto-refreshes when expired, persists new tokens BEFORE returning
func TestGetAutoRefreshesExpiredToken(t *testing.T) {
	refreshCalled := false

	// Mock Strava API server
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer apiSrv.Close()

	// Mock Strava token refresh endpoint
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCalled = true
		json.NewEncoder(w).Encode(auth.Tokens{
			AccessToken:  "new-access-token",
			RefreshToken: "new-refresh-token",
			ExpiresAt:    time.Now().Add(6 * time.Hour).Unix(),
		})
	}))
	defer tokenSrv.Close()

	store := newMockTokenStore(&auth.Tokens{
		AccessToken:  "old-access-token",
		RefreshToken: "old-refresh-token",
		ExpiresAt:    time.Now().Add(-1 * time.Hour).Unix(),
	}, true) // expired

	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}
	client := strava.NewClient(cfg, store, testLogger())
	client.SetBaseURL(apiSrv.URL)
	client.SetTokenURL(tokenSrv.URL)

	_, err := client.Get(context.Background(), "/athlete", nil)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}

	if !refreshCalled {
		t.Error("token refresh was not called for expired token")
	}

	// Verify new tokens were persisted
	tokens, err := store.Read()
	if err != nil {
		t.Fatalf("store.Read() error: %v", err)
	}
	if tokens.AccessToken != "new-access-token" {
		t.Errorf("persisted AccessToken = %q, want %q", tokens.AccessToken, "new-access-token")
	}
}

// Test 3: Concurrent Get() calls with expired token trigger only ONE refresh
func TestSingleflightCoalescesRefresh(t *testing.T) {
	var refreshCount atomic.Int32
	const numGoroutines = 10

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer apiSrv.Close()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshCount.Add(1)
		// Small delay to ensure concurrent requests overlap
		time.Sleep(50 * time.Millisecond)
		json.NewEncoder(w).Encode(auth.Tokens{
			AccessToken:  "refreshed-token",
			RefreshToken: "refreshed-refresh",
			ExpiresAt:    time.Now().Add(6 * time.Hour).Unix(),
		})
	}))
	defer tokenSrv.Close()

	store := newMockTokenStore(&auth.Tokens{
		AccessToken:  "expired-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(-1 * time.Hour).Unix(),
	}, true)

	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}
	client := strava.NewClient(cfg, store, testLogger())
	client.SetBaseURL(apiSrv.URL)
	client.SetTokenURL(tokenSrv.URL)

	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	start := make(chan struct{})

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, err := client.Get(context.Background(), "/athlete", nil)
			if err != nil {
				t.Errorf("Get() error: %v", err)
			}
		}()
	}

	close(start) // release all goroutines at once
	wg.Wait()

	count := refreshCount.Load()
	if count != 1 {
		t.Errorf("refresh called %d times, want 1 (singleflight should coalesce)", count)
	}
}

// Test 4: Get() retries once on 401 after refreshing token
func TestGetRetries401AfterRefresh(t *testing.T) {
	var requestCount atomic.Int32

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := requestCount.Add(1)
		if count == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message":"Authorization Error"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer apiSrv.Close()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(auth.Tokens{
			AccessToken:  "refreshed-after-401",
			RefreshToken: "new-refresh",
			ExpiresAt:    time.Now().Add(6 * time.Hour).Unix(),
		})
	}))
	defer tokenSrv.Close()

	store := newMockTokenStore(&auth.Tokens{
		AccessToken:  "stale-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
	}, false)

	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}
	client := strava.NewClient(cfg, store, testLogger())
	client.SetBaseURL(apiSrv.URL)
	client.SetTokenURL(tokenSrv.URL)

	data, err := client.Get(context.Background(), "/athlete", nil)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}

	if !strings.Contains(string(data), "ok") {
		t.Errorf("response = %q, want to contain 'ok'", string(data))
	}

	if requestCount.Load() != 2 {
		t.Errorf("API requests = %d, want 2 (initial + retry)", requestCount.Load())
	}
}

// Test 5: Get() does NOT retry on second 401 (prevents infinite loop)
func TestGetDoesNotRetrySecond401(t *testing.T) {
	var requestCount atomic.Int32

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Authorization Error"}`))
	}))
	defer apiSrv.Close()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(auth.Tokens{
			AccessToken:  "still-bad-token",
			RefreshToken: "refresh-token",
			ExpiresAt:    time.Now().Add(6 * time.Hour).Unix(),
		})
	}))
	defer tokenSrv.Close()

	store := newMockTokenStore(&auth.Tokens{
		AccessToken:  "bad-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
	}, false)

	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}
	client := strava.NewClient(cfg, store, testLogger())
	client.SetBaseURL(apiSrv.URL)
	client.SetTokenURL(tokenSrv.URL)

	_, err := client.Get(context.Background(), "/athlete", nil)
	if err == nil {
		t.Fatal("expected error on persistent 401, got nil")
	}

	var stravaErr *strava.StravaError
	if !strava.AsStravaError(err, &stravaErr) {
		t.Fatalf("expected StravaError, got %T: %v", err, err)
	}
	if stravaErr.StatusCode != 401 {
		t.Errorf("StatusCode = %d, want 401", stravaErr.StatusCode)
	}

	if requestCount.Load() != 2 {
		t.Errorf("API requests = %d, want 2 (initial + one retry only)", requestCount.Load())
	}
}

// Test 6: Get() returns StravaError with StatusCode and Body for 4xx/5xx
func TestGetReturnsStravaErrorForHTTPErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Rate Limit Exceeded","errors":[]}`))
	}))
	defer srv.Close()

	store := newMockTokenStore(&auth.Tokens{
		AccessToken:  "test-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
	}, false)

	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}
	client := strava.NewClient(cfg, store, testLogger())
	client.SetBaseURL(srv.URL)

	_, err := client.Get(context.Background(), "/activities", nil)
	if err == nil {
		t.Fatal("expected error for 403, got nil")
	}

	var stravaErr *strava.StravaError
	if !strava.AsStravaError(err, &stravaErr) {
		t.Fatalf("expected StravaError, got %T: %v", err, err)
	}
	if stravaErr.StatusCode != 403 {
		t.Errorf("StatusCode = %d, want 403", stravaErr.StatusCode)
	}
	if !strings.Contains(stravaErr.Body, "Rate Limit Exceeded") {
		t.Errorf("Body = %q, want to contain 'Rate Limit Exceeded'", stravaErr.Body)
	}
}

// Test 7: Rate limit headers are parsed from response
func TestRateLimitHeadersParsed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "100,1000")
		w.Header().Set("X-RateLimit-Usage", "42,500")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	store := newMockTokenStore(&auth.Tokens{
		AccessToken:  "test-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
	}, false)

	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}
	client := strava.NewClient(cfg, store, testLogger())
	client.SetBaseURL(srv.URL)

	_, err := client.Get(context.Background(), "/athlete", nil)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}

	limits := client.GetRateLimits()
	if limits.Limit15Min != 100 {
		t.Errorf("Limit15Min = %d, want 100", limits.Limit15Min)
	}
	if limits.LimitDaily != 1000 {
		t.Errorf("LimitDaily = %d, want 1000", limits.LimitDaily)
	}
	if limits.Usage15Min != 42 {
		t.Errorf("Usage15Min = %d, want 42", limits.Usage15Min)
	}
	if limits.UsageDaily != 500 {
		t.Errorf("UsageDaily = %d, want 500", limits.UsageDaily)
	}
}

// Test 8: Post() sends JSON body with correct Content-Type
func TestPostSendsJSONBody(t *testing.T) {
	var gotContentType string
	var gotBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":123}`))
	}))
	defer srv.Close()

	store := newMockTokenStore(&auth.Tokens{
		AccessToken:  "test-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
	}, false)

	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}
	client := strava.NewClient(cfg, store, testLogger())
	client.SetBaseURL(srv.URL)

	payload := map[string]string{"name": "Morning Run"}
	_, err := client.Post(context.Background(), "/activities", payload)
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}

	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
	}
	if !strings.Contains(gotBody, "Morning Run") {
		t.Errorf("body = %q, want to contain 'Morning Run'", gotBody)
	}
}

// Test 9: Put() sends JSON body with correct Content-Type
func TestPutSendsJSONBody(t *testing.T) {
	var gotMethod string
	var gotContentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":123}`))
	}))
	defer srv.Close()

	store := newMockTokenStore(&auth.Tokens{
		AccessToken:  "test-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
	}, false)

	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}
	client := strava.NewClient(cfg, store, testLogger())
	client.SetBaseURL(srv.URL)

	payload := map[string]string{"name": "Updated Run"}
	_, err := client.Put(context.Background(), "/activities/123", payload)
	if err != nil {
		t.Fatalf("Put() error: %v", err)
	}

	if gotMethod != "PUT" {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", gotContentType, "application/json")
	}
}

// Test 10: RateLimitWarning() returns warning when usage > 80% of 15-min limit
func TestRateLimitWarningAboveThreshold(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "100,1000")
		w.Header().Set("X-RateLimit-Usage", "85,500")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	store := newMockTokenStore(&auth.Tokens{
		AccessToken:  "test-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
	}, false)

	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}
	client := strava.NewClient(cfg, store, testLogger())
	client.SetBaseURL(srv.URL)

	// Trigger a request to populate rate limits
	client.Get(context.Background(), "/athlete", nil)

	warning := client.RateLimitWarning()
	if warning == "" {
		t.Error("RateLimitWarning() = empty, want warning string")
	}
	if !strings.Contains(warning, "85") || !strings.Contains(warning, "100") {
		t.Errorf("warning = %q, want to contain usage/limit numbers", warning)
	}
}

// Test 11: RateLimitWarning() returns empty when usage <= 80%
func TestRateLimitWarningBelowThreshold(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "100,1000")
		w.Header().Set("X-RateLimit-Usage", "42,500")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	store := newMockTokenStore(&auth.Tokens{
		AccessToken:  "test-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
	}, false)

	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}
	client := strava.NewClient(cfg, store, testLogger())
	client.SetBaseURL(srv.URL)

	// Trigger a request to populate rate limits
	client.Get(context.Background(), "/athlete", nil)

	warning := client.RateLimitWarning()
	if warning != "" {
		t.Errorf("RateLimitWarning() = %q, want empty string", warning)
	}
}

// Test 12: PostMultipart sends correct Content-Type with boundary and POST method
func TestPostMultipartSendsCorrectContentType(t *testing.T) {
	var gotContentType string
	var gotMethod string
	var gotAuth string
	var gotBody string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":123,"status":"processing"}`))
	}))
	defer srv.Close()

	store := newMockTokenStore(&auth.Tokens{
		AccessToken:  "test-multipart-token",
		RefreshToken: "test-refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
	}, false)

	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}
	client := strava.NewClient(cfg, store, testLogger())
	client.SetBaseURL(srv.URL)

	// Build a simple multipart body
	body := strings.NewReader("--boundary\r\nContent-Disposition: form-data; name=\"field\"\r\n\r\nvalue\r\n--boundary--\r\n")
	contentType := "multipart/form-data; boundary=boundary"

	data, err := client.PostMultipart(context.Background(), "/uploads", body, contentType)
	if err != nil {
		t.Fatalf("PostMultipart() error: %v", err)
	}

	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotContentType != contentType {
		t.Errorf("Content-Type = %q, want %q", gotContentType, contentType)
	}
	if gotAuth != "Bearer test-multipart-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-multipart-token")
	}
	if !strings.Contains(gotBody, "value") {
		t.Errorf("body = %q, want to contain 'value'", gotBody)
	}
	if !strings.Contains(string(data), "processing") {
		t.Errorf("response = %q, want to contain 'processing'", string(data))
	}
}

// Test 13: PostMultipart returns StravaError on 4xx response
func TestPostMultipartReturnsStravaErrorOn4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"Bad Request","errors":[{"resource":"Upload","field":"file","code":"not_a_valid_file"}]}`))
	}))
	defer srv.Close()

	store := newMockTokenStore(&auth.Tokens{
		AccessToken:  "test-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
	}, false)

	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}
	client := strava.NewClient(cfg, store, testLogger())
	client.SetBaseURL(srv.URL)

	body := strings.NewReader("invalid-multipart")
	_, err := client.PostMultipart(context.Background(), "/uploads", body, "multipart/form-data; boundary=test")

	if err == nil {
		t.Fatal("expected error on 400, got nil")
	}

	var stravaErr *strava.StravaError
	if !strava.AsStravaError(err, &stravaErr) {
		t.Fatalf("expected StravaError, got %T: %v", err, err)
	}
	if stravaErr.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", stravaErr.StatusCode)
	}
}

// Test 14: Post() replays full body on 401 retry (regression test for body-reuse bug)
func TestPostReplaysBodyOn401Retry(t *testing.T) {
	var requestCount atomic.Int32
	var bodies []string
	var mu sync.Mutex

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()

		count := requestCount.Add(1)
		if count == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"message":"Authorization Error"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":123}`))
	}))
	defer apiSrv.Close()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(auth.Tokens{
			AccessToken:  "refreshed-token",
			RefreshToken: "new-refresh",
			ExpiresAt:    time.Now().Add(6 * time.Hour).Unix(),
		})
	}))
	defer tokenSrv.Close()

	store := newMockTokenStore(&auth.Tokens{
		AccessToken:  "stale-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
	}, false)

	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}
	client := strava.NewClient(cfg, store, testLogger())
	client.SetBaseURL(apiSrv.URL)
	client.SetTokenURL(tokenSrv.URL)

	payload := map[string]string{"name": "Morning Run", "description": "Easy 5K"}
	_, err := client.Post(context.Background(), "/activities", payload)
	if err != nil {
		t.Fatalf("Post() error: %v", err)
	}

	if requestCount.Load() != 2 {
		t.Fatalf("API requests = %d, want 2 (initial + retry)", requestCount.Load())
	}

	// Both requests must have received the full JSON body
	for i, body := range bodies {
		if !strings.Contains(body, "Morning Run") {
			t.Errorf("request %d body = %q, want to contain 'Morning Run'", i+1, body)
		}
		if !strings.Contains(body, "Easy 5K") {
			t.Errorf("request %d body = %q, want to contain 'Easy 5K'", i+1, body)
		}
	}
}

// TestNewClientReturnsNonNil verifies the constructor works
func TestNewClientReturnsNonNil(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "tokens.json")
	store := auth.NewFileTokenStore(tokenPath)
	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}

	client := strava.NewClient(cfg, store, testLogger())
	if client == nil {
		t.Fatal("NewClient() returned nil")
	}
}
