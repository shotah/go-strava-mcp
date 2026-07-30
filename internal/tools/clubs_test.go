package tools_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shotah/go-strava-mcp/internal/tools"
)

// --- clubs_list_activities tests ---

func TestGetClubActivitiesBasic(t *testing.T) {
	var gotPath, gotMethod string
	var gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"name":"Morning Run","athlete":{"firstname":"Alice"}}]`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetClubActivities(client)

	req := makeRequest(map[string]any{
		"id":       float64(456),
		"per_page": float64(10),
	})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if gotMethod != "GET" {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/clubs/456/activities" {
		t.Errorf("path = %q, want /clubs/456/activities", gotPath)
	}
	if !strings.Contains(gotQuery, "per_page=10") {
		t.Errorf("query should contain per_page=10, got: %s", gotQuery)
	}
	if result.IsError {
		text := extractResultText(t, result)
		t.Fatalf("expected non-error result, got: %s", text)
	}

	text := extractResultText(t, result)
	if !strings.Contains(text, "Morning Run") {
		t.Errorf("result should contain 'Morning Run', got: %s", text)
	}
}

func TestGetClubActivitiesMissingId(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when id is missing")
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetClubActivities(client)

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

func TestGetClubActivitiesWithPagination(t *testing.T) {
	var gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetClubActivities(client)

	req := makeRequest(map[string]any{
		"id":       float64(456),
		"page":     float64(2),
		"per_page": float64(50),
	})
	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(gotQuery, "page=2") {
		t.Errorf("query should contain page=2, got: %s", gotQuery)
	}
	if !strings.Contains(gotQuery, "per_page=50") {
		t.Errorf("query should contain per_page=50, got: %s", gotQuery)
	}
}

func TestGetClubActivitiesStravaError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Rate Limit Exceeded"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetClubActivities(client)

	req := makeRequest(map[string]any{"id": float64(456)})
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
