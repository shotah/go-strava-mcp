package strava

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/shotah/go-strava-mcp/internal/auth"
	"github.com/shotah/go-strava-mcp/internal/config"
)

const (
	defaultBaseURL  = "https://www.strava.com/api/v3"
	defaultTokenURL = "https://www.strava.com/api/v3/oauth/token"
)

// StravaError represents an error response from the Strava API.
type StravaError struct {
	StatusCode int
	Body       string
}

func (e *StravaError) Error() string {
	return fmt.Sprintf("Strava API error (%d): %s", e.StatusCode, e.Body)
}

// AsStravaError checks if an error is a StravaError and assigns it to the target.
func AsStravaError(err error, target **StravaError) bool {
	return errors.As(err, target)
}

// RateLimits holds the current Strava API rate limit state.
type RateLimits struct {
	Limit15Min int
	LimitDaily int
	Usage15Min int
	UsageDaily int
}

// Client is the Strava API HTTP client with automatic token refresh.
type Client struct {
	tokenStore   auth.TokenStore
	httpClient   *http.Client
	baseURL      string
	tokenURL     string
	clientID     string
	clientSecret string
	refreshGroup singleflight.Group
	rateLimits   RateLimits
	rateLimitsMu sync.RWMutex
	logger       *slog.Logger
}

// NewClient creates a new Strava API client.
func NewClient(cfg *config.Config, store auth.TokenStore, logger *slog.Logger) *Client {
	return &Client{
		tokenStore:   store,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		baseURL:      defaultBaseURL,
		tokenURL:     defaultTokenURL,
		clientID:     cfg.ClientID,
		clientSecret: cfg.ClientSecret,
		logger:       logger,
	}
}

// Get makes an authenticated GET request to the Strava API.
func (c *Client) Get(ctx context.Context, path string, params map[string]string) ([]byte, error) {
	fullURL := c.baseURL + path
	if len(params) > 0 {
		v := url.Values{}
		for key, val := range params {
			v.Set(key, val)
		}
		fullURL += "?" + v.Encode()
	}
	return c.doRequest(ctx, http.MethodGet, fullURL, nil, "")
}

// Post makes an authenticated POST request to the Strava API.
func (c *Client) Post(ctx context.Context, path string, body interface{}) ([]byte, error) {
	return c.jsonRequest(ctx, http.MethodPost, path, body)
}

// Put makes an authenticated PUT request to the Strava API.
func (c *Client) Put(ctx context.Context, path string, body interface{}) ([]byte, error) {
	return c.jsonRequest(ctx, http.MethodPut, path, body)
}

func (c *Client) jsonRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request body: %w", err)
	}
	return c.doRequest(ctx, method, c.baseURL+path, jsonBody, "application/json")
}

// PostMultipart makes an authenticated POST request with a pre-built multipart body.
// The contentType must include the multipart boundary (use writer.FormDataContentType()).
func (c *Client) PostMultipart(ctx context.Context, path string, body io.Reader, contentType string) ([]byte, error) {
	bodyBytes, err := io.ReadAll(body)
	if err != nil {
		return nil, fmt.Errorf("buffer multipart body: %w", err)
	}
	return c.doRequest(ctx, http.MethodPost, c.baseURL+path, bodyBytes, contentType)
}

// GetRateLimits returns the current rate limit state.
func (c *Client) GetRateLimits() RateLimits {
	c.rateLimitsMu.RLock()
	defer c.rateLimitsMu.RUnlock()
	return c.rateLimits
}

// RateLimitWarning returns a warning string if rate limit usage exceeds 80%.
// Returns empty string when usage is within acceptable limits.
func (c *Client) RateLimitWarning() string {
	c.rateLimitsMu.RLock()
	defer c.rateLimitsMu.RUnlock()
	if c.rateLimits.Usage15Min > 0 && c.rateLimits.Limit15Min > 0 &&
		c.rateLimits.Usage15Min > int(0.8*float64(c.rateLimits.Limit15Min)) {
		return fmt.Sprintf("Note: %d/%d API calls used in this 15-min window.", c.rateLimits.Usage15Min, c.rateLimits.Limit15Min)
	}
	return ""
}

// SetBaseURL overrides the API base URL (for testing).
func (c *Client) SetBaseURL(u string) {
	c.baseURL = u
}

// SetTokenURL overrides the token refresh URL (for testing).
func (c *Client) SetTokenURL(u string) {
	c.tokenURL = u
}

// doRequest executes an authenticated HTTP request with automatic token refresh.
// The body is accepted as []byte so it can be replayed on 401 retry without re-reading.
func (c *Client) doRequest(ctx context.Context, method, fullURL string, body []byte, contentType string) ([]byte, error) {
	tokens, err := c.tokenStore.Read()
	if err != nil {
		return nil, fmt.Errorf("read tokens: %w", err)
	}

	// Auto-refresh if expired
	if c.tokenStore.IsExpired(tokens) {
		tokens, err = c.refresh(ctx)
		if err != nil {
			return nil, fmt.Errorf("token refresh: %w", err)
		}
	}

	bodyReader := func() io.Reader {
		if body == nil {
			return nil
		}
		return bytes.NewReader(body)
	}

	respBody, err := c.executeRequest(ctx, method, fullURL, bodyReader(), contentType, tokens.AccessToken)
	if err != nil {
		// Check for 401 â€” retry once after refresh
		var stravaErr *StravaError
		if errors.As(err, &stravaErr) && stravaErr.StatusCode == http.StatusUnauthorized {
			tokens, refreshErr := c.refresh(ctx)
			if refreshErr != nil {
				return nil, fmt.Errorf("token refresh after 401: %w", refreshErr)
			}
			// Retry with new token and a fresh reader
			return c.executeRequest(ctx, method, fullURL, bodyReader(), contentType, tokens.AccessToken)
		}
		return nil, err
	}
	return respBody, nil
}

// executeRequest builds and executes a single HTTP request.
func (c *Client) executeRequest(ctx context.Context, method, fullURL string, body io.Reader, contentType, accessToken string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, fullURL, body)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+accessToken)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	c.updateRateLimits(resp.Header)

	c.logger.Debug("strava request", "method", method, "url", fullURL, "status", resp.StatusCode)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, &StravaError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	return respBody, nil
}

// refresh performs a token refresh using singleflight to coalesce concurrent requests.
func (c *Client) refresh(ctx context.Context) (*auth.Tokens, error) {
	result, err, shared := c.refreshGroup.Do("refresh", func() (interface{}, error) {
		tokens, err := c.tokenStore.Read()
		if err != nil {
			return nil, fmt.Errorf("read tokens for refresh: %w", err)
		}

		c.logger.Debug("refreshing strava token")

		data := url.Values{
			"client_id":     {c.clientID},
			"client_secret": {c.clientSecret},
			"grant_type":    {"refresh_token"},
			"refresh_token": {tokens.RefreshToken},
		}

		resp, err := http.PostForm(c.tokenURL, data)
		if err != nil {
			return nil, fmt.Errorf("refresh request: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("refresh failed (%d): %s", resp.StatusCode, body)
		}

		var newTokens auth.Tokens
		if err := json.NewDecoder(resp.Body).Decode(&newTokens); err != nil {
			return nil, fmt.Errorf("decode refresh response: %w", err)
		}

		// CRITICAL: Persist BEFORE using
		if err := c.tokenStore.Write(&newTokens); err != nil {
			return nil, fmt.Errorf("persist refreshed tokens: %w", err)
		}

		c.logger.Debug("token refreshed successfully")
		return &newTokens, nil
	})
	if err != nil {
		return nil, err
	}
	if shared {
		c.logger.Debug("token refresh result shared with concurrent caller")
	}
	return result.(*auth.Tokens), nil
}

// updateRateLimits parses and stores rate limit information from response headers.
func (c *Client) updateRateLimits(header http.Header) {
	limitStr := header.Get("X-RateLimit-Limit")
	usageStr := header.Get("X-RateLimit-Usage")

	if limitStr == "" && usageStr == "" {
		return
	}

	c.rateLimitsMu.Lock()
	defer c.rateLimitsMu.Unlock()

	if limitStr != "" {
		parts := strings.SplitN(limitStr, ",", 2)
		if len(parts) >= 1 {
			c.rateLimits.Limit15Min, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
		}
		if len(parts) >= 2 {
			c.rateLimits.LimitDaily, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
		}
	}

	if usageStr != "" {
		parts := strings.SplitN(usageStr, ",", 2)
		if len(parts) >= 1 {
			c.rateLimits.Usage15Min, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
		}
		if len(parts) >= 2 {
			c.rateLimits.UsageDaily, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
		}
	}

	if c.rateLimits.Limit15Min > 0 && c.rateLimits.Usage15Min > int(0.8*float64(c.rateLimits.Limit15Min)) {
		c.logger.Warn("approaching rate limit", "usage", c.rateLimits.Usage15Min, "limit", c.rateLimits.Limit15Min)
	}
}
