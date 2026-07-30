package auth

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shotah/go-strava-mcp/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestGenerateState(t *testing.T) {
	state, err := generateState()
	if err != nil {
		t.Fatalf("generateState() error: %v", err)
	}
	// 16 random bytes hex-encoded.
	if len(state) != 32 {
		t.Errorf("len(state) = %d, want 32", len(state))
	}
	if _, err := hex.DecodeString(state); err != nil {
		t.Errorf("state %q is not hex: %v", state, err)
	}

	other, err := generateState()
	if err != nil {
		t.Fatalf("generateState() error: %v", err)
	}
	if state == other {
		t.Error("two calls to generateState() produced the same value")
	}
}

// stubBrowser replaces openBrowser for the duration of a test.
func stubBrowser(t *testing.T, fn func(rawURL string) error) {
	t.Helper()
	original := openBrowser
	openBrowser = fn
	t.Cleanup(func() { openBrowser = original })
}

// deliverCallback extracts the state from the authorize URL the flow tried to
// open, then hits the local callback endpoint with the given query values.
// It retries because the callback server starts in a goroutine.
func deliverCallback(t *testing.T, authorizeURL string, extra url.Values) {
	t.Helper()

	parsed, err := url.Parse(authorizeURL)
	if err != nil {
		t.Errorf("parse authorize URL %q: %v", authorizeURL, err)
		return
	}

	query := url.Values{}
	maps.Copy(query, extra)
	if _, ok := query["state"]; !ok {
		query.Set("state", parsed.Query().Get("state"))
	}

	target := "http://127.0.0.1:19876" + callbackPath + "?" + query.Encode()
	client := &http.Client{Timeout: 2 * time.Second}

	for range 100 {
		resp, err := client.Get(target)
		if err == nil {
			resp.Body.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("callback endpoint never became reachable at %s", target)
}

// tokenServer serves a successful token-exchange response.
func tokenServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode(Tokens{
			AccessToken:  "flow-access",
			RefreshToken: "flow-refresh",
			ExpiresAt:    time.Now().Add(6 * time.Hour).Unix(),
		}); err != nil {
			t.Errorf("encode tokens: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// athleteServer serves a successful GET /athlete response.
func athleteServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]any{
			"firstname": "Ada",
			"lastname":  "Lovelace",
		}); err != nil {
			t.Errorf("encode athlete: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestStore(t *testing.T) *FileTokenStore {
	t.Helper()
	return NewFileTokenStore(filepath.Join(t.TempDir(), "tokens.json"))
}

func testConfig() *config.Config {
	return &config.Config{ClientID: "test-client-id", ClientSecret: "test-client-secret"}
}

func TestRunOAuthFlow_Success(t *testing.T) {
	store := newTestStore(t)
	stubBrowser(t, func(rawURL string) error {
		go deliverCallback(t, rawURL, url.Values{"code": {"flow-code"}})
		return nil
	})

	err := runOAuthFlow(testConfig(), store, discardLogger(),
		tokenServer(t).URL, athleteServer(t).URL)
	if err != nil {
		t.Fatalf("runOAuthFlow() error: %v", err)
	}

	tokens, err := store.Read()
	if err != nil {
		t.Fatalf("tokens were not persisted: %v", err)
	}
	if tokens.AccessToken != "flow-access" {
		t.Errorf("AccessToken = %q, want %q", tokens.AccessToken, "flow-access")
	}
}

// TestRunOAuthFlow_BrowserFailureStillCompletes covers the fallback that prints
// the URL when no browser can be launched (headless machines).
func TestRunOAuthFlow_BrowserFailureStillCompletes(t *testing.T) {
	store := newTestStore(t)
	stubBrowser(t, func(rawURL string) error {
		go deliverCallback(t, rawURL, url.Values{"code": {"flow-code"}})
		return errors.New("no browser available")
	})

	if err := runOAuthFlow(testConfig(), store, discardLogger(),
		tokenServer(t).URL, athleteServer(t).URL); err != nil {
		t.Fatalf("runOAuthFlow() error: %v", err)
	}
}

func TestRunOAuthFlow_StravaError(t *testing.T) {
	stubBrowser(t, func(rawURL string) error {
		go deliverCallback(t, rawURL, url.Values{"error": {"access_denied"}})
		return nil
	})

	err := runOAuthFlow(testConfig(), newTestStore(t), discardLogger(),
		tokenServer(t).URL, athleteServer(t).URL)
	if err == nil {
		t.Fatal("runOAuthFlow() = nil, want the Strava error from the callback")
	}
	if !strings.Contains(err.Error(), "access_denied") {
		t.Errorf("error = %v, want it to contain 'access_denied'", err)
	}
}

func TestRunOAuthFlow_StateMismatch(t *testing.T) {
	stubBrowser(t, func(rawURL string) error {
		go deliverCallback(t, rawURL, url.Values{
			"code":  {"flow-code"},
			"state": {"not-the-expected-state"},
		})
		return nil
	})

	err := runOAuthFlow(testConfig(), newTestStore(t), discardLogger(),
		tokenServer(t).URL, athleteServer(t).URL)
	if err == nil {
		t.Fatal("runOAuthFlow() = nil, want a state mismatch error")
	}
	if !strings.Contains(err.Error(), "state mismatch") {
		t.Errorf("error = %v, want it to mention a state mismatch", err)
	}
}

func TestRunOAuthFlow_TokenExchangeFailure(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		if _, err := w.Write([]byte(`{"message":"Bad Request"}`)); err != nil {
			t.Errorf("write body: %v", err)
		}
	}))
	defer failing.Close()

	stubBrowser(t, func(rawURL string) error {
		go deliverCallback(t, rawURL, url.Values{"code": {"flow-code"}})
		return nil
	})

	err := runOAuthFlow(testConfig(), newTestStore(t), discardLogger(),
		failing.URL, athleteServer(t).URL)
	if err == nil {
		t.Fatal("runOAuthFlow() = nil, want a token exchange error")
	}
	if !strings.Contains(err.Error(), "token exchange failed") {
		t.Errorf("error = %v, want it to mention a failed token exchange", err)
	}
}

func TestRunOAuthFlow_AthleteValidationFailure(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer failing.Close()

	stubBrowser(t, func(rawURL string) error {
		go deliverCallback(t, rawURL, url.Values{"code": {"flow-code"}})
		return nil
	})

	err := runOAuthFlow(testConfig(), newTestStore(t), discardLogger(),
		tokenServer(t).URL, failing.URL)
	if err == nil {
		t.Fatal("runOAuthFlow() = nil, want a token validation error")
	}
	if !strings.Contains(err.Error(), "token validation failed") {
		t.Errorf("error = %v, want it to mention failed token validation", err)
	}
}

func TestExchangeCode_BadEndpoint(t *testing.T) {
	if _, err := ExchangeCode("id", "secret", "code", "http://\x7f/invalid"); err == nil {
		t.Fatal("ExchangeCode() = nil, want an error for an unparseable endpoint")
	}
}

func TestExchangeCode_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("not json")); err != nil {
			t.Errorf("write body: %v", err)
		}
	}))
	defer srv.Close()

	_, err := ExchangeCode("id", "secret", "code", srv.URL)
	if err == nil {
		t.Fatal("ExchangeCode() = nil, want a decode error")
	}
	if !strings.Contains(err.Error(), "decode token response") {
		t.Errorf("error = %v, want it to mention decoding", err)
	}
}

func TestFetchAthleteName_BadEndpoint(t *testing.T) {
	if _, err := FetchAthleteName("token", "http://\x7f/invalid"); err == nil {
		t.Fatal("FetchAthleteName() = nil, want an error for an unparseable endpoint")
	}
}

func TestFetchAthleteName_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("not json")); err != nil {
			t.Errorf("write body: %v", err)
		}
	}))
	defer srv.Close()

	_, err := FetchAthleteName("token", srv.URL)
	if err == nil {
		t.Fatal("FetchAthleteName() = nil, want a decode error")
	}
	if !strings.Contains(err.Error(), "decode athlete response") {
		t.Errorf("error = %v, want it to mention decoding", err)
	}
}
