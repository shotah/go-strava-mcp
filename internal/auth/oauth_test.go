package auth_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/shotah/go-strava-mcp/internal/auth"
)

// Test 1: Callback handler extracts code from query parameter and sends to channel
func TestCallbackHandlerExtractsCode(t *testing.T) {
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	state := "test-state-123"

	handler := auth.NewCallbackHandler(state, codeCh, errCh)

	req := httptest.NewRequest(http.MethodGet, "/callback?code=test-auth-code&state=test-state-123", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	select {
	case code := <-codeCh:
		if code != "test-auth-code" {
			t.Errorf("code = %q, want %q", code, "test-auth-code")
		}
	default:
		t.Fatal("no code sent to channel")
	}

	// Should serve success page
	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(body, "window.close") {
		t.Error("success page missing window.close script")
	}
}

// Test 2: Callback handler returns 400 when state parameter mismatches
func TestCallbackHandlerRejectsBadState(t *testing.T) {
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	state := "expected-state"

	handler := auth.NewCallbackHandler(state, codeCh, errCh)

	req := httptest.NewRequest(http.MethodGet, "/callback?code=test-code&state=wrong-state", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected non-nil error")
		}
		if !strings.Contains(err.Error(), "state") {
			t.Errorf("error = %q, want to mention 'state'", err.Error())
		}
	default:
		t.Fatal("no error sent to channel")
	}
}

// Test 3: Callback handler returns 400 when Strava sends error parameter
func TestCallbackHandlerRejectsStravaError(t *testing.T) {
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	state := "test-state"

	handler := auth.NewCallbackHandler(state, codeCh, errCh)

	req := httptest.NewRequest(http.MethodGet, "/callback?error=access_denied&state=test-state", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "try again") {
		t.Error("error page missing 'try again' text")
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected non-nil error")
		}
		if !strings.Contains(err.Error(), "access_denied") {
			t.Errorf("error = %q, want to contain 'access_denied'", err.Error())
		}
	default:
		t.Fatal("no error sent to channel")
	}
}

// Test 4: Code exchange POSTs to Strava token endpoint with correct form fields
func TestExchangeCodePostsCorrectFields(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %q, want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		gotForm, _ = url.ParseQuery(string(body))

		json.NewEncoder(w).Encode(auth.Tokens{
			AccessToken:  "new-access",
			RefreshToken: "new-refresh",
			ExpiresAt:    9999999999,
		})
	}))
	defer srv.Close()

	_, err := auth.ExchangeCode("test-client-id", "test-client-secret", "test-code", srv.URL)
	if err != nil {
		t.Fatalf("ExchangeCode() error: %v", err)
	}

	if gotForm.Get("client_id") != "test-client-id" {
		t.Errorf("client_id = %q, want %q", gotForm.Get("client_id"), "test-client-id")
	}
	if gotForm.Get("client_secret") != "test-client-secret" {
		t.Errorf("client_secret = %q, want %q", gotForm.Get("client_secret"), "test-client-secret")
	}
	if gotForm.Get("code") != "test-code" {
		t.Errorf("code = %q, want %q", gotForm.Get("code"), "test-code")
	}
	if gotForm.Get("grant_type") != "authorization_code" {
		t.Errorf("grant_type = %q, want %q", gotForm.Get("grant_type"), "authorization_code")
	}
}

// Test 5: Successful code exchange persists tokens via store.Write()
func TestExchangeCodeReturnsTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(auth.Tokens{
			AccessToken:  "exchange-access",
			RefreshToken: "exchange-refresh",
			ExpiresAt:    9999999999,
		})
	}))
	defer srv.Close()

	tokens, err := auth.ExchangeCode("id", "secret", "code", srv.URL)
	if err != nil {
		t.Fatalf("ExchangeCode() error: %v", err)
	}

	if tokens.AccessToken != "exchange-access" {
		t.Errorf("AccessToken = %q, want %q", tokens.AccessToken, "exchange-access")
	}
	if tokens.RefreshToken != "exchange-refresh" {
		t.Errorf("RefreshToken = %q, want %q", tokens.RefreshToken, "exchange-refresh")
	}
}

// Test 6: Success page HTML contains auto-close JavaScript (window.close)
func TestSuccessPageContainsAutoClose(t *testing.T) {
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	state := "test-state"

	handler := auth.NewCallbackHandler(state, codeCh, errCh)

	req := httptest.NewRequest(http.MethodGet, "/callback?code=test-code&state=test-state", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "window.close") {
		t.Error("success page missing window.close() JavaScript")
	}
}

// Test 7: Error page HTML contains error description and "try again" text
func TestErrorPageContainsTryAgain(t *testing.T) {
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	state := "test-state"

	handler := auth.NewCallbackHandler(state, codeCh, errCh)

	req := httptest.NewRequest(http.MethodGet, "/callback?error=access_denied&state=test-state", http.NoBody)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "try again") {
		t.Error("error page missing 'try again' text")
	}
	if !strings.Contains(body, "access_denied") {
		t.Error("error page missing error description")
	}
}

// Test 8: buildAuthorizeURL includes all required params
func TestBuildAuthorizeURLContainsRequiredParams(t *testing.T) {
	u := auth.BuildAuthorizeURL("test-client-id", "test-state-xyz")

	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}

	params := parsed.Query()

	if params.Get("client_id") != "test-client-id" {
		t.Errorf("client_id = %q, want %q", params.Get("client_id"), "test-client-id")
	}
	if !strings.Contains(params.Get("redirect_uri"), "19876") {
		t.Errorf("redirect_uri = %q, want to contain port 19876", params.Get("redirect_uri"))
	}
	if !strings.Contains(params.Get("redirect_uri"), "/callback") {
		t.Errorf("redirect_uri = %q, want to contain /callback", params.Get("redirect_uri"))
	}
	if params.Get("response_type") != "code" {
		t.Errorf("response_type = %q, want %q", params.Get("response_type"), "code")
	}
	if params.Get("state") != "test-state-xyz" {
		t.Errorf("state = %q, want %q", params.Get("state"), "test-state-xyz")
	}
	if params.Get("approval_prompt") != "force" {
		t.Errorf("approval_prompt = %q, want %q", params.Get("approval_prompt"), "force")
	}
	scope := params.Get("scope")
	for _, required := range []string{"read", "read_all", "activity:read_all", "activity:write"} {
		if !strings.Contains(scope, required) {
			t.Errorf("scope = %q, missing %q", scope, required)
		}
	}
}

// Test 9: fetchAthleteName makes GET /athlete with Bearer token and returns name
func TestFetchAthleteNameReturnsName(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]any{
			"firstname": "Jane",
			"lastname":  "Doe",
		})
	}))
	defer srv.Close()

	name, err := auth.FetchAthleteName("test-bearer-token", srv.URL+"/api/v3/athlete")
	if err != nil {
		t.Fatalf("FetchAthleteName() error: %v", err)
	}

	if name != "Jane Doe" {
		t.Errorf("name = %q, want %q", name, "Jane Doe")
	}
	if gotAuth != "Bearer test-bearer-token" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer test-bearer-token")
	}
}

// Test 10: fetchAthleteName returns descriptive error when GET /athlete fails
func TestFetchAthleteNameReturnsErrorOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Authorization Error"}`))
	}))
	defer srv.Close()

	_, err := auth.FetchAthleteName("bad-token", srv.URL+"/api/v3/athlete")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "GET /athlete failed") {
		t.Errorf("error = %q, want to contain 'GET /athlete failed'", errMsg)
	}
	if !strings.Contains(errMsg, "401") {
		t.Errorf("error = %q, want to contain status code '401'", errMsg)
	}
}
