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

// --- athlete_get tests ---

func TestGetAthleteBasic(t *testing.T) {
	var gotPath, gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":123,"firstname":"John","lastname":"Doe","city":"Portland"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetAthlete(client)

	req := makeRequest(map[string]any{})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if gotMethod != "GET" {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/athlete" {
		t.Errorf("path = %q, want /athlete", gotPath)
	}
	if result.IsError {
		t.Errorf("expected non-error result")
	}

	text := extractResultText(t, result)
	if !strings.Contains(text, "John") {
		t.Errorf("result should contain 'John', got: %s", text)
	}
	// Verify pretty-printed JSON (2-space indent)
	if !strings.Contains(text, "  \"firstname\"") {
		t.Errorf("expected pretty-printed JSON, got: %s", text)
	}
}

func TestGetAthleteNoQueryParams(t *testing.T) {
	var gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":123}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetAthlete(client)

	req := makeRequest(map[string]any{})
	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if gotQuery != "" {
		t.Errorf("expected no query params, got: %s", gotQuery)
	}
}

func TestGetAthleteStravaError403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Rate Limit Exceeded"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetAthlete(client)

	req := makeRequest(map[string]any{})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !result.IsError {
		t.Fatal("expected error result for 403")
	}
	text := extractResultText(t, result)
	if !strings.Contains(text, "403") {
		t.Errorf("error should contain '403', got: %s", text)
	}
}

// --- athlete_get_stats tests ---

func TestGetAthleteStatsWithExplicitId(t *testing.T) {
	var gotPath string
	var athleteCallCount atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/athlete" {
			athleteCallCount.Add(1)
			t.Error("/athlete should NOT be called when id is provided")
		}
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"biggest_ride_distance":100000,"biggest_climb_elevation_gain":500}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetAthleteStats(client)

	req := makeRequest(map[string]any{"id": float64(12345)})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if gotPath != "/athletes/12345/stats" {
		t.Errorf("path = %q, want /athletes/12345/stats", gotPath)
	}
	if result.IsError {
		text := extractResultText(t, result)
		t.Fatalf("expected non-error result, got: %s", text)
	}

	text := extractResultText(t, result)
	if !strings.Contains(text, "biggest_ride_distance") {
		t.Errorf("result should contain stats data, got: %s", text)
	}

	if athleteCallCount.Load() != 0 {
		t.Error("/athlete was called but should NOT have been (id was provided)")
	}
}

func TestGetAthleteStatsAutoFetchId(t *testing.T) {
	var athleteCallCount atomic.Int32
	var statsPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/athlete":
			athleteCallCount.Add(1)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"id":99,"firstname":"Jane"}`))
		case "/athletes/99/stats":
			statsPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"biggest_ride_distance":50000}`))
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetAthleteStats(client)

	// Empty arguments -- should auto-fetch athlete ID
	req := makeRequest(map[string]any{})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if athleteCallCount.Load() != 1 {
		t.Errorf("expected /athlete to be called once for auto-fetch, got %d calls", athleteCallCount.Load())
	}
	if statsPath != "/athletes/99/stats" {
		t.Errorf("stats path = %q, want /athletes/99/stats", statsPath)
	}
	if result.IsError {
		text := extractResultText(t, result)
		t.Fatalf("expected non-error result, got: %s", text)
	}

	text := extractResultText(t, result)
	if !strings.Contains(text, "biggest_ride_distance") {
		t.Errorf("result should contain stats data, got: %s", text)
	}
}

func TestGetAthleteStatsAutoFetchError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/athlete" {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message":"server error"}`))
			return
		}
		t.Error("stats endpoint should not be called when athlete fetch fails")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetAthleteStats(client)

	req := makeRequest(map[string]any{})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !result.IsError {
		t.Fatal("expected error result when /athlete fails during auto-fetch")
	}
	text := extractResultText(t, result)
	if !strings.Contains(text, "athlete_get_stats") {
		t.Errorf("error should mention tool name, got: %s", text)
	}
}
