package strava_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shotah/go-strava-mcp/internal/auth"
	"github.com/shotah/go-strava-mcp/internal/config"
	"github.com/shotah/go-strava-mcp/internal/strava"
)

func TestStravaErrorMessage(t *testing.T) {
	err := &strava.StravaError{StatusCode: http.StatusNotFound, Body: `{"message":"Record Not Found"}`}

	msg := err.Error()
	if !strings.Contains(msg, "404") {
		t.Errorf("Error() = %q, want it to contain the status code", msg)
	}
	if !strings.Contains(msg, "Record Not Found") {
		t.Errorf("Error() = %q, want it to contain the response body", msg)
	}
}

func TestAsStravaErrorFalseForOtherErrors(t *testing.T) {
	var target *strava.StravaError
	if strava.AsStravaError(errors.New("plain error"), &target) {
		t.Error("AsStravaError() = true for a non-Strava error, want false")
	}
}

// newValidTokens returns tokens that are far from expiry.
func newValidTokens() *auth.Tokens {
	return &auth.Tokens{
		AccessToken:  "valid-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(1 * time.Hour).Unix(),
	}
}

func newClientFor(store auth.TokenStore, baseURL, tokenURL string) *strava.Client {
	cfg := &config.Config{ClientID: "id", ClientSecret: "secret"}
	client := strava.NewClient(cfg, store, testLogger())
	if baseURL != "" {
		client.SetBaseURL(baseURL)
	}
	if tokenURL != "" {
		client.SetTokenURL(tokenURL)
	}
	return client
}

func TestGetFailsWhenTokenStoreIsEmpty(t *testing.T) {
	client := newClientFor(newMockTokenStore(nil, false), "http://127.0.0.1:1", "")

	_, err := client.Get(context.Background(), "/athlete", nil)
	if err == nil {
		t.Fatal("Get() = nil, want an error when no tokens are stored")
	}
	if !strings.Contains(err.Error(), "read tokens") {
		t.Errorf("error = %v, want it to mention reading tokens", err)
	}
}

func TestGetEncodesQueryParams(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := newClientFor(newMockTokenStore(newValidTokens(), false), srv.URL, "")

	_, err := client.Get(context.Background(), "/activities", map[string]string{"per_page": "5"})
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if gotQuery != "per_page=5" {
		t.Errorf("query = %q, want %q", gotQuery, "per_page=5")
	}
}

func TestRefreshFailsOnNon200(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message":"Bad Request"}`))
	}))
	defer tokenSrv.Close()

	client := newClientFor(newMockTokenStore(newValidTokens(), true), "http://127.0.0.1:1", tokenSrv.URL)

	_, err := client.Get(context.Background(), "/athlete", nil)
	if err == nil {
		t.Fatal("Get() = nil, want an error when the refresh is rejected")
	}
	if !strings.Contains(err.Error(), "refresh failed") {
		t.Errorf("error = %v, want it to mention a failed refresh", err)
	}
}

func TestRefreshFailsOnInvalidJSON(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("not json"))
	}))
	defer tokenSrv.Close()

	client := newClientFor(newMockTokenStore(newValidTokens(), true), "http://127.0.0.1:1", tokenSrv.URL)

	_, err := client.Get(context.Background(), "/athlete", nil)
	if err == nil {
		t.Fatal("Get() = nil, want an error when the refresh response is invalid")
	}
	if !strings.Contains(err.Error(), "decode refresh response") {
		t.Errorf("error = %v, want it to mention decoding the refresh response", err)
	}
}

func TestRefreshFailsWhenPersistFails(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(auth.Tokens{
			AccessToken:  "fresh",
			RefreshToken: "fresh-refresh",
			ExpiresAt:    time.Now().Add(6 * time.Hour).Unix(),
		})
	}))
	defer tokenSrv.Close()

	store := newMockTokenStore(newValidTokens(), true)
	store.writeErr = errors.New("disk full")

	client := newClientFor(store, "http://127.0.0.1:1", tokenSrv.URL)

	_, err := client.Get(context.Background(), "/athlete", nil)
	if err == nil {
		t.Fatal("Get() = nil, want an error when tokens cannot be persisted")
	}
	if !strings.Contains(err.Error(), "persist refreshed tokens") {
		t.Errorf("error = %v, want it to mention persisting tokens", err)
	}
}

func TestRefreshFailsWhenTokenEndpointUnreachable(t *testing.T) {
	// Port 1 is reserved and refuses connections.
	client := newClientFor(newMockTokenStore(newValidTokens(), true), "http://127.0.0.1:1", "http://127.0.0.1:1")

	_, err := client.Get(context.Background(), "/athlete", nil)
	if err == nil {
		t.Fatal("Get() = nil, want an error when the token endpoint is unreachable")
	}
	if !strings.Contains(err.Error(), "refresh request") {
		t.Errorf("error = %v, want it to mention the refresh request", err)
	}
}

// TestExpiredFlagFlipsBackAfterRefresh exercises the mock's setExpired helper
// and confirms a second call reuses the refreshed token instead of refreshing.
func TestExpiredFlagFlipsBackAfterRefresh(t *testing.T) {
	var refreshCount int
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		refreshCount++
		json.NewEncoder(w).Encode(auth.Tokens{
			AccessToken:  "second-token",
			RefreshToken: "second-refresh",
			ExpiresAt:    time.Now().Add(6 * time.Hour).Unix(),
		})
	}))
	defer tokenSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer apiSrv.Close()

	store := newMockTokenStore(newValidTokens(), false)
	client := newClientFor(store, apiSrv.URL, tokenSrv.URL)

	if _, err := client.Get(context.Background(), "/athlete", nil); err != nil {
		t.Fatalf("first Get() error: %v", err)
	}
	if refreshCount != 0 {
		t.Fatalf("refreshes = %d, want 0 for a valid token", refreshCount)
	}

	store.setExpired(true)
	if _, err := client.Get(context.Background(), "/athlete", nil); err != nil {
		t.Fatalf("second Get() error: %v", err)
	}
	if refreshCount != 1 {
		t.Errorf("refreshes = %d, want 1 after the token expired", refreshCount)
	}

	// Write() clears the expired flag, so a third call must not refresh again.
	if _, err := client.Get(context.Background(), "/athlete", nil); err != nil {
		t.Fatalf("third Get() error: %v", err)
	}
	if refreshCount != 1 {
		t.Errorf("refreshes = %d, want 1; the refreshed token should be reused", refreshCount)
	}
}

func TestPostFailsForUnmarshalableBody(t *testing.T) {
	client := newClientFor(newMockTokenStore(newValidTokens(), false), "http://127.0.0.1:1", "")

	// Channels cannot be marshalled to JSON.
	_, err := client.Post(context.Background(), "/activities", make(chan int))
	if err == nil {
		t.Fatal("Post() = nil, want a marshal error")
	}
	if !strings.Contains(err.Error(), "marshal request body") {
		t.Errorf("error = %v, want it to mention marshalling", err)
	}
}

func TestRequestFailsForInvalidURL(t *testing.T) {
	client := newClientFor(newMockTokenStore(newValidTokens(), false), "http://\x7f", "")

	_, err := client.Get(context.Background(), "/athlete", nil)
	if err == nil {
		t.Fatal("Get() = nil, want an error for an unparseable URL")
	}
	if !strings.Contains(err.Error(), "create request") {
		t.Errorf("error = %v, want it to mention creating the request", err)
	}
}

func TestPostMultipartFailsWhenBodyUnreadable(t *testing.T) {
	client := newClientFor(newMockTokenStore(newValidTokens(), false), "http://127.0.0.1:1", "")

	_, err := client.PostMultipart(context.Background(), "/uploads", errReader{}, "multipart/form-data")
	if err == nil {
		t.Fatal("PostMultipart() = nil, want an error when the body cannot be read")
	}
	if !strings.Contains(err.Error(), "buffer multipart body") {
		t.Errorf("error = %v, want it to mention buffering the body", err)
	}
}

// errReader always fails, standing in for an unreadable upload body.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestGetRateLimitsIgnoresMissingHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := newClientFor(newMockTokenStore(newValidTokens(), false), srv.URL, "")

	if _, err := client.Get(context.Background(), "/athlete", nil); err != nil {
		t.Fatalf("Get() error: %v", err)
	}

	limits := client.GetRateLimits()
	if limits != (strava.RateLimits{}) {
		t.Errorf("GetRateLimits() = %+v, want the zero value when no headers are sent", limits)
	}
	if warning := client.RateLimitWarning(); warning != "" {
		t.Errorf("RateLimitWarning() = %q, want empty", warning)
	}
}

func TestUpdateRateLimitsHandlesSingleValueHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "200")
		w.Header().Set("X-RateLimit-Usage", "10")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := newClientFor(newMockTokenStore(newValidTokens(), false), srv.URL, "")

	if _, err := client.Get(context.Background(), "/athlete", nil); err != nil {
		t.Fatalf("Get() error: %v", err)
	}

	limits := client.GetRateLimits()
	if limits.Limit15Min != 200 {
		t.Errorf("Limit15Min = %d, want 200", limits.Limit15Min)
	}
	if limits.Usage15Min != 10 {
		t.Errorf("Usage15Min = %d, want 10", limits.Usage15Min)
	}
	if limits.LimitDaily != 0 || limits.UsageDaily != 0 {
		t.Errorf("daily limits = %d/%d, want 0/0 when the header has one value", limits.UsageDaily, limits.LimitDaily)
	}
}
