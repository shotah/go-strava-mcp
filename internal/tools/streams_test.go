package tools_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shotah/go-strava-mcp/internal/tools"
)

// --- get_activity_streams tests ---

func TestGetActivityStreamsWithSpecificKeys(t *testing.T) {
	var gotPath string
	var gotKeys string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKeys = r.URL.Query().Get("keys")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"type":"heartrate","data":[150,155]},{"type":"time","data":[0,1]}]`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetActivityStreams(client)

	req := makeRequest(map[string]any{
		"id":   float64(123),
		"keys": []interface{}{"heartrate", "time"},
	})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if gotPath != "/activities/123/streams" {
		t.Errorf("path = %q, want /activities/123/streams", gotPath)
	}
	if gotKeys != "heartrate,time" {
		t.Errorf("keys = %q, want 'heartrate,time'", gotKeys)
	}
	if result.IsError {
		text := extractResultText(t, result)
		t.Fatalf("expected non-error result, got: %s", text)
	}

	text := extractResultText(t, result)
	if !strings.Contains(text, "heartrate") {
		t.Errorf("result should contain stream data, got: %s", text)
	}
}

func TestGetActivityStreamsDefaultAllKeys(t *testing.T) {
	var gotKeys string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKeys = r.URL.Query().Get("keys")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetActivityStreams(client)

	// No keys provided -- should default to all 11 stream types
	req := makeRequest(map[string]any{
		"id": float64(123),
	})
	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// All 11 stream types should be comma-separated in the keys param
	expectedTypes := []string{
		"time", "latlng", "distance", "altitude", "velocity_smooth",
		"heartrate", "cadence", "watts", "temp", "moving", "grade_smooth",
	}
	for _, st := range expectedTypes {
		if !strings.Contains(gotKeys, st) {
			t.Errorf("keys param should contain %q, got: %s", st, gotKeys)
		}
	}

	// Should have exactly 10 commas (11 items)
	commaCount := strings.Count(gotKeys, ",")
	if commaCount != 10 {
		t.Errorf("expected 10 commas (11 stream types), got %d commas in: %s", commaCount, gotKeys)
	}
}

func TestGetActivityStreamsKeyByType(t *testing.T) {
	var gotKeyByType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKeyByType = r.URL.Query().Get("key_by_type")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetActivityStreams(client)

	req := makeRequest(map[string]any{
		"id":          float64(123),
		"key_by_type": true,
	})
	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if gotKeyByType != "true" {
		t.Errorf("key_by_type = %q, want 'true'", gotKeyByType)
	}
}

func TestGetActivityStreamsMissingId(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when id is missing")
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetActivityStreams(client)

	req := makeRequest(map[string]any{})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !result.IsError {
		t.Fatal("expected error result for missing id")
	}
	text := extractResultText(t, result)
	if !strings.Contains(text, "id is required") {
		t.Errorf("error should mention 'id is required', got: %s", text)
	}
}

func TestGetActivityStreamsStravaError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Rate Limit Exceeded"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetActivityStreams(client)

	req := makeRequest(map[string]any{"id": float64(123)})
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
