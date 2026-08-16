package tools_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/shotah/go-strava-mcp/internal/tools"
)

func TestListRoutesAutoFetchId(t *testing.T) {
	var athleteCalls atomic.Int32
	var routesPath, routesQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/athlete":
			athleteCalls.Add(1)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id":99,"firstname":"Jane"}`))
		case "/athletes/99/routes":
			routesPath = r.URL.Path
			routesQuery = r.URL.RawQuery
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`[{"id":7,"name":"River Loop","map":{"summary_polyline":"abc"}}]`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	handler := tools.HandleListRoutes(newTestClient(srv.URL))
	result, err := handler(context.Background(), makeRequest(map[string]any{
		"page":     float64(2),
		"per_page": float64(10),
	}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if athleteCalls.Load() != 1 {
		t.Errorf("expected /athlete once, got %d", athleteCalls.Load())
	}
	if routesPath != "/athletes/99/routes" {
		t.Errorf("path = %q, want /athletes/99/routes", routesPath)
	}
	if !strings.Contains(routesQuery, "page=2") || !strings.Contains(routesQuery, "per_page=10") {
		t.Errorf("query = %q, want page=2 and per_page=10", routesQuery)
	}
	if result.IsError {
		t.Fatalf("expected non-error result, got: %s", extractResultText(t, result))
	}
	text := extractResultText(t, result)
	if !strings.Contains(text, "River Loop") {
		t.Errorf("result should contain route name, got: %s", text)
	}
}

func TestListRoutesExplicitIdSkipsAthlete(t *testing.T) {
	var athleteCalls atomic.Int32
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/athlete" {
			athleteCalls.Add(1)
			t.Error("/athlete should not be called when id is provided")
		}
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	handler := tools.HandleListRoutes(newTestClient(srv.URL))
	result, err := handler(context.Background(), makeRequest(map[string]any{"id": float64(12345)}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if gotPath != "/athletes/12345/routes" {
		t.Errorf("path = %q, want /athletes/12345/routes", gotPath)
	}
	if result.IsError {
		t.Fatalf("expected non-error result, got: %s", extractResultText(t, result))
	}
	if athleteCalls.Load() != 0 {
		t.Error("/athlete was called")
	}
}

func TestListRoutesAthleteFetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/athlete" {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message":"server error"}`))
			return
		}
		t.Error("routes endpoint should not be called when athlete fetch fails")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	handler := tools.HandleListRoutes(newTestClient(srv.URL))
	result, err := handler(context.Background(), makeRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	text := extractResultText(t, result)
	if !strings.Contains(text, "routes_list") {
		t.Errorf("error should mention tool name, got: %s", text)
	}
}

func TestListRoutesUnparseableAthleteProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{`))
	}))
	defer srv.Close()

	handler := tools.HandleListRoutes(newTestClient(srv.URL))
	result, err := handler(context.Background(), makeRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for unparseable athlete profile")
	}
	if !strings.Contains(extractResultText(t, result), "parse athlete profile") {
		t.Errorf("error = %s", extractResultText(t, result))
	}
}

func TestListRoutesInvalidAthleteProfile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"firstname":"NoId"}`))
	}))
	defer srv.Close()

	handler := tools.HandleListRoutes(newTestClient(srv.URL))
	result, err := handler(context.Background(), makeRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for missing athlete id")
	}
	if !strings.Contains(extractResultText(t, result), "missing id") {
		t.Errorf("error should mention missing id, got: %s", extractResultText(t, result))
	}
}

func TestListRoutesStravaError403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/athlete" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id":1}`))
			return
		}
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Rate Limit Exceeded"}`))
	}))
	defer srv.Close()

	handler := tools.HandleListRoutes(newTestClient(srv.URL))
	result, err := handler(context.Background(), makeRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for 403")
	}
	text := extractResultText(t, result)
	if !strings.Contains(text, "403") {
		t.Errorf("error should contain 403, got: %s", text)
	}
}

func TestGetRouteBasic(t *testing.T) {
	var gotPath, gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":777,"name":"Hill Repeats","map":{"summary_polyline":"_p~iF~ps|U"}}`))
	}))
	defer srv.Close()

	handler := tools.HandleGetRoute(newTestClient(srv.URL))
	result, err := handler(context.Background(), makeRequest(map[string]any{"id": float64(777)}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if gotMethod != "GET" {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/routes/777" {
		t.Errorf("path = %q, want /routes/777", gotPath)
	}
	if result.IsError {
		t.Fatalf("expected non-error result, got: %s", extractResultText(t, result))
	}
	text := extractResultText(t, result)
	if !strings.Contains(text, "Hill Repeats") || !strings.Contains(text, "summary_polyline") {
		t.Errorf("result should include route map, got: %s", text)
	}
}

func TestGetRouteMissingId(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("API should not be called without id")
	}))
	defer srv.Close()

	handler := tools.HandleGetRoute(newTestClient(srv.URL))
	result, err := handler(context.Background(), makeRequest(map[string]any{}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result")
	}
	if !strings.Contains(extractResultText(t, result), "id is required") {
		t.Errorf("error = %s", extractResultText(t, result))
	}
}

func TestGetRouteStravaError403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Rate Limit Exceeded"}`))
	}))
	defer srv.Close()

	handler := tools.HandleGetRoute(newTestClient(srv.URL))
	result, err := handler(context.Background(), makeRequest(map[string]any{"id": float64(1)}))
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for 403")
	}
	if !strings.Contains(extractResultText(t, result), "403") {
		t.Errorf("error = %s", extractResultText(t, result))
	}
}
