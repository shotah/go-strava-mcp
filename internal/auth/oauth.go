package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/pkg/browser"

	"github.com/shotah/go-strava-mcp/internal/config"
)

const (
	callbackPort      = 19876
	oauthTimeout      = 2 * time.Minute
	authorizeURL      = "https://www.strava.com/oauth/authorize"
	tokenURL          = "https://www.strava.com/api/v3/oauth/token"
	athleteURL        = "https://www.strava.com/api/v3/athlete"
	oauthScopes       = "read,read_all,profile:read_all,profile:write,activity:read_all,activity:write"
	callbackPath      = "/callback"
	httpTimeout       = 10 * time.Second
	readHeaderTimeout = 5 * time.Second
)

// openBrowser is the browser launcher, indirected so tests can stub it.
var openBrowser = browser.OpenURL

const successPageHTML = `<!DOCTYPE html>
<html>
<head><title>Strava MCP - Authorized</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0; background: #f7f7f7; }
  .card { background: white; border-radius: 12px; padding: 40px; text-align: center; box-shadow: 0 2px 10px rgba(0,0,0,0.1); }
  h1 { color: #fc4c02; margin-bottom: 8px; }
  p { color: #666; }
</style>
</head>
<body>
<div class="card">
  <h1>Done!</h1>
  <p>You can close this tab.</p>
</div>
<script>setTimeout(function(){window.close()},1500)</script>
</body>
</html>`

const errorPageHTML = `<!DOCTYPE html>
<html>
<head><title>Strava MCP - Authorization Failed</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, sans-serif; display: flex; justify-content: center; align-items: center; min-height: 100vh; margin: 0; background: #f7f7f7; }
  .card { background: white; border-radius: 12px; padding: 40px; text-align: center; box-shadow: 0 2px 10px rgba(0,0,0,0.1); max-width: 500px; }
  h1 { color: #d32f2f; margin-bottom: 8px; }
  .error { color: #333; background: #fee; border-radius: 8px; padding: 12px; margin: 16px 0; }
  p { color: #666; }
</style>
</head>
<body>
<div class="card">
  <h1>Authorization Failed</h1>
  <div class="error">%s</div>
  <p>Please try again by running <code>strava-mcp auth</code>.</p>
</div>
</body>
</html>`

// generateState produces a cryptographically random state string for CSRF protection.
func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate random state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// BuildAuthorizeURL constructs the Strava OAuth authorization URL with all required parameters.
func BuildAuthorizeURL(clientID, state string) string {
	params := url.Values{
		"client_id":       {clientID},
		"redirect_uri":    {fmt.Sprintf("http://localhost:%d%s", callbackPort, callbackPath)},
		"response_type":   {"code"},
		"scope":           {oauthScopes},
		"state":           {state},
		"approval_prompt": {"force"},
	}
	return authorizeURL + "?" + params.Encode()
}

// NewCallbackHandler creates an HTTP handler for the OAuth callback endpoint.
// It validates the state parameter, checks for errors from Strava, and extracts
// the authorization code.
func NewCallbackHandler(expectedState string, codeCh chan<- string, errCh chan<- error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		// Check state parameter first (CSRF protection)
		state := query.Get("state")
		if state != expectedState {
			errCh <- fmt.Errorf("OAuth state mismatch: expected %q, got %q", expectedState, state)
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, errorPageHTML, "State parameter mismatch (possible CSRF). Please try running <code>strava-mcp auth</code> again.")
			return
		}

		// Check for error parameter from Strava
		if stravaErr := query.Get("error"); stravaErr != "" {
			errCh <- fmt.Errorf("authorization error from Strava: %s", stravaErr)
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, errorPageHTML, html.EscapeString(stravaErr))
			return
		}

		// Extract authorization code
		code := query.Get("code")
		if code == "" {
			errCh <- errors.New("no authorization code in callback")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, errorPageHTML, "No authorization code received.")
			return
		}

		// Success: serve success page and send code
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, successPageHTML)
		codeCh <- code
	})
}

// ExchangeCode exchanges an authorization code for tokens by POSTing to the Strava token endpoint.
// The tokenEndpoint parameter allows overriding for tests; pass tokenURL for production.
func ExchangeCode(clientID, clientSecret, code, tokenEndpoint string) (*Tokens, error) {
	data := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"grant_type":    {"authorization_code"},
	}

	req, err := http.NewRequest(http.MethodPost, tokenEndpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token exchange request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: httpTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, body)
	}

	var tokens Tokens
	if err := json.NewDecoder(resp.Body).Decode(&tokens); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}

	return &tokens, nil
}

// FetchAthleteName calls GET /athlete with the given access token and returns "firstname lastname".
// This validates the full auth chain end-to-end: tokens were stored correctly and work for API calls.
// The athleteEndpoint parameter allows overriding for tests; pass athleteURL for production.
func FetchAthleteName(accessToken, athleteEndpoint string) (string, error) {
	client := &http.Client{Timeout: httpTimeout}

	req, err := http.NewRequest(http.MethodGet, athleteEndpoint, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("create athlete request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET /athlete request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("GET /athlete failed (%d): %s", resp.StatusCode, body)
	}

	var athlete struct {
		Firstname string `json:"firstname"`
		Lastname  string `json:"lastname"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&athlete); err != nil {
		return "", fmt.Errorf("decode athlete response: %w", err)
	}

	return athlete.Firstname + " " + athlete.Lastname, nil
}

// RunOAuthFlow runs the complete OAuth browser flow:
// 1. Starts a callback server on port 19876
// 2. Opens system browser to Strava authorization page
// 3. Waits for callback with authorization code
// 4. Exchanges code for tokens
// 5. Persists tokens to disk
// 6. Validates by calling GET /athlete
// 7. Prints "Authenticated as [Name]!" to stderr
func RunOAuthFlow(cfg *config.Config, store TokenStore, logger *slog.Logger) error {
	return runOAuthFlow(cfg, store, logger, tokenURL, athleteURL)
}

// runOAuthFlow is the implementation behind RunOAuthFlow. The endpoints are
// parameters so tests can point them at a local server.
func runOAuthFlow(cfg *config.Config, store TokenStore, logger *slog.Logger, tokenEndpoint, athleteEndpoint string) error {
	state, err := generateState()
	if err != nil {
		return err
	}

	authURL := BuildAuthorizeURL(cfg.ClientID, state)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	mux.Handle(callbackPath, NewCallbackHandler(state, codeCh, errCh))

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", callbackPort),
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	go func() {
		if srvErr := srv.ListenAndServe(); srvErr != nil && !errors.Is(srvErr, http.ErrServerClosed) {
			errCh <- fmt.Errorf("callback server: %w", srvErr)
		}
	}()

	// Open browser
	browser.Stdout = os.Stderr
	browser.Stderr = os.Stderr
	if err := openBrowser(authURL); err != nil {
		fmt.Fprintf(os.Stderr, "Open this URL in your browser:\n%s\n", authURL)
	}

	logger.Info("waiting for Strava authorization", "url", authURL)

	select {
	case code := <-codeCh:
		tokens, err := ExchangeCode(cfg.ClientID, cfg.ClientSecret, code, tokenEndpoint)
		if err != nil {
			return err
		}

		if err := store.Write(tokens); err != nil {
			return fmt.Errorf("persist tokens: %w", err)
		}

		// CRITICAL: Validate end-to-end by calling GET /athlete with the
		// persisted access token. This confirms: code exchange succeeded,
		// token store write succeeded, and the token is usable for API calls.
		name, err := FetchAthleteName(tokens.AccessToken, athleteEndpoint)
		if err != nil {
			return fmt.Errorf("token validation failed: %w", err)
		}

		fmt.Fprintf(os.Stderr, "Authenticated as %s!\n", name)
		return nil

	case err := <-errCh:
		return err

	case <-time.After(oauthTimeout):
		return errors.New("OAuth timed out; run `strava-mcp auth` again")
	}
}
