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

func TestOAuthCallbackListenAddr_DefaultIsAllInterfaces(t *testing.T) {
	t.Parallel()
	for _, env := range []string{"", "  ", "\t"} {
		got := oauthCallbackListenAddr(env)
		want := "0.0.0.0:19876"
		if got != want {
			t.Errorf("oauthCallbackListenAddr(%q) = %q, want %q (Docker publish needs 0.0.0.0)", env, got, want)
		}
	}
}

func TestOAuthCallbackListenAddr_Override(t *testing.T) {
	t.Parallel()
	cases := []struct {
		env, want string
	}{
		{"127.0.0.1", "127.0.0.1:19876"},
		{" 127.0.0.1 ", "127.0.0.1:19876"},
		{"localhost", "localhost:19876"},
	}
	for _, tc := range cases {
		if got := oauthCallbackListenAddr(tc.env); got != tc.want {
			t.Errorf("oauthCallbackListenAddr(%q) = %q, want %q", tc.env, got, tc.want)
		}
	}
}

func TestCallbackListenAddr_ReadsSTRAVA_OAUTH_BIND(t *testing.T) {
	t.Setenv("STRAVA_OAUTH_BIND", "")
	if got := callbackListenAddr(); got != "0.0.0.0:19876" {
		t.Errorf("default callbackListenAddr() = %q, want 0.0.0.0:19876", got)
	}

	t.Setenv("STRAVA_OAUTH_BIND", "127.0.0.1")
	if got := callbackListenAddr(); got != "127.0.0.1:19876" {
		t.Errorf("callbackListenAddr() with STRAVA_OAUTH_BIND=127.0.0.1 = %q, want 127.0.0.1:19876", got)
	}
}

func TestBuildAuthorizeURL_RedirectURIIgnoresBindHost(t *testing.T) {
	// Strava's OAuth client is registered for localhost; bind host must not leak
	// into redirect_uri even when listening on 0.0.0.0 for Docker.
	t.Setenv("STRAVA_OAUTH_BIND", "0.0.0.0")
	u := BuildAuthorizeURL("client", "state")
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := "http://localhost:19876/callback"
	if got := parsed.Query().Get("redirect_uri"); got != want {
		t.Errorf("redirect_uri = %q, want %q", got, want)
	}
}

// bindOAuthLoopback forces the callback listener onto 127.0.0.1 so flow tests
// don't open 0.0.0.0 (Windows firewall) while unit tests still cover the default.
func bindOAuthLoopback(t *testing.T) {
	t.Helper()
	t.Setenv("STRAVA_OAUTH_BIND", "127.0.0.1")
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
	bindOAuthLoopback(t)
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
	bindOAuthLoopback(t)
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
	bindOAuthLoopback(t)
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
	bindOAuthLoopback(t)
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
	bindOAuthLoopback(t)
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
	bindOAuthLoopback(t)
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
