package tools_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/shotah/go-strava-mcp/internal/strava"
	"github.com/shotah/go-strava-mcp/internal/tools"
)

func newToolResolver(t *testing.T, srv *httptest.Server) *strava.URLResolver {
	t.Helper()
	r := strava.NewURLResolver()
	r.AllowHTTP()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	r.AllowHost(u.Hostname())
	return r
}

func TestResolveURLCanonicalFetchesRoute(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":777,"name":"River Loop","map":{"summary_polyline":"abc"}}`))
	}))
	defer srv.Close()

	handler := tools.HandleResolveURL(newTestClient(srv.URL), nil)
	result, err := handler(context.Background(), makeRequest(map[string]any{
		"url": "https://www.strava.com/routes/777",
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if gotPath != "/routes/777" {
		t.Errorf("path = %q, want /routes/777", gotPath)
	}
	if result.IsError {
		t.Fatalf("expected non-error result, got: %s", extractResultText(t, result))
	}
	text := extractResultText(t, result)
	for _, want := range []string{`"type": "route"`, `"id": 777`, "River Loop", "summary_polyline"} {
		if !strings.Contains(text, want) {
			t.Errorf("result missing %q, got: %s", want, text)
		}
	}
}

func TestResolveURLFetchFalseSkipsAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("API should not be called when fetch=false, path=%s", r.URL.Path)
	}))
	defer srv.Close()

	handler := tools.HandleResolveURL(newTestClient(srv.URL), nil)
	result, err := handler(context.Background(), makeRequest(map[string]any{
		"url":   "https://www.strava.com/activities/42",
		"fetch": false,
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		t.Fatalf("expected non-error result, got: %s", extractResultText(t, result))
	}
	text := extractResultText(t, result)
	if strings.Contains(text, `"resource"`) {
		t.Errorf("resource should be omitted when fetch=false, got: %s", text)
	}
	if !strings.Contains(text, `"type": "activity"`) {
		t.Errorf("expected activity type, got: %s", text)
	}
}

func TestResolveURLShortLinkThenFetch(t *testing.T) {
	var apiPath string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":99,"name":"Morning Ride"}`))
	}))
	defer api.Close()

	short := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://www.strava.com/activities/99", http.StatusFound)
	}))
	defer short.Close()

	handler := tools.HandleResolveURL(newTestClient(api.URL), newToolResolver(t, short))
	result, err := handler(context.Background(), makeRequest(map[string]any{
		"url": short.URL + "/abc",
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if apiPath != "/activities/99" {
		t.Errorf("api path = %q, want /activities/99", apiPath)
	}
	if result.IsError {
		t.Fatalf("expected non-error result, got: %s", extractResultText(t, result))
	}
	text := extractResultText(t, result)
	if !strings.Contains(text, "Morning Ride") {
		t.Errorf("expected fetched activity, got: %s", text)
	}
}

func TestResolveURLMissingURL(t *testing.T) {
	handler := tools.HandleResolveURL(newTestClient("http://127.0.0.1:1"), nil)
	result, err := handler(context.Background(), makeRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	if !strings.Contains(extractResultText(t, result), "url is required") {
		t.Errorf("error = %s", extractResultText(t, result))
	}
}

func TestResolveURLResolveError(t *testing.T) {
	handler := tools.HandleResolveURL(newTestClient("http://127.0.0.1:1"), nil)
	result, err := handler(context.Background(), makeRequest(map[string]any{
		"url": "https://evil.example/routes/1",
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	if !strings.Contains(extractResultText(t, result), "urls_resolve") {
		t.Errorf("error = %s", extractResultText(t, result))
	}
}

func TestResolveURLInvalidResourceJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not-json`))
	}))
	defer srv.Close()

	handler := tools.HandleResolveURL(newTestClient(srv.URL), nil)
	result, err := handler(context.Background(), makeRequest(map[string]any{
		"url": "https://www.strava.com/routes/1",
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for invalid resource JSON")
	}
}

func TestResolveURLFetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Rate Limit Exceeded"}`))
	}))
	defer srv.Close()

	handler := tools.HandleResolveURL(newTestClient(srv.URL), nil)
	result, err := handler(context.Background(), makeRequest(map[string]any{
		"url": "https://www.strava.com/routes/1",
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for 403")
	}
	text := extractResultText(t, result)
	if !strings.Contains(text, "403") {
		t.Errorf("error = %s", text)
	}
}
