package tools_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/shotah/go-strava-mcp/internal/tools"
)

// makeRequest creates a mcp.CallToolRequest with the given arguments.
func makeRequest(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: args,
		},
	}
}

// --- activities_list tests ---

func TestGetActivitiesBasic(t *testing.T) {
	var gotPath, gotMethod string
	var gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id":1,"name":"Morning Run"}]`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetActivities(client)

	req := makeRequest(map[string]any{"per_page": float64(10)})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if gotMethod != "GET" {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/athlete/activities" {
		t.Errorf("path = %q, want /athlete/activities", gotPath)
	}
	if !strings.Contains(gotQuery, "per_page=10") {
		t.Errorf("query = %q, want to contain per_page=10", gotQuery)
	}
	if result.IsError {
		t.Errorf("expected non-error result")
	}

	text := extractResultText(t, result)
	if !strings.Contains(text, "Morning Run") {
		t.Errorf("result text should contain 'Morning Run', got: %s", text)
	}
}

func TestGetActivitiesBeforeAfterParams(t *testing.T) {
	var gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetActivities(client)

	req := makeRequest(map[string]any{
		"before": float64(1700000000),
		"after":  float64(1690000000),
	})
	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(gotQuery, "before=1700000000") {
		t.Errorf("query should contain before=1700000000, got: %s", gotQuery)
	}
	if !strings.Contains(gotQuery, "after=1690000000") {
		t.Errorf("query should contain after=1690000000, got: %s", gotQuery)
	}
}

// --- activities_get tests ---

func TestGetActivityByIdBasic(t *testing.T) {
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":123,"name":"Afternoon Run"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetActivityById(client)

	req := makeRequest(map[string]any{"id": float64(123)})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if gotPath != "/activities/123" {
		t.Errorf("path = %q, want /activities/123", gotPath)
	}
	if result.IsError {
		t.Errorf("expected non-error result")
	}

	text := extractResultText(t, result)
	if !strings.Contains(text, "Afternoon Run") {
		t.Errorf("result should contain 'Afternoon Run', got: %s", text)
	}
}

func TestGetActivityByIdIncludeAllEfforts(t *testing.T) {
	var gotQuery string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":123}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetActivityById(client)

	req := makeRequest(map[string]any{"id": float64(123), "include_all_efforts": true})
	_, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(gotQuery, "include_all_efforts=true") {
		t.Errorf("query should contain include_all_efforts=true, got: %s", gotQuery)
	}
}

func TestGetActivityByIdMissingId(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when id is missing")
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetActivityById(client)

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

// --- activities_create tests ---

func TestCreateActivityBasic(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":456,"name":"Morning Run"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleCreateActivity(client)

	req := makeRequest(map[string]any{
		"name":             "Morning Run",
		"sport_type":       "Run",
		"start_date_local": "2024-01-13T06:00:00Z",
		"elapsed_time":     float64(3600),
		"distance":         float64(10000),
	})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/activities" {
		t.Errorf("path = %q, want /activities", gotPath)
	}
	if result.IsError {
		text := extractResultText(t, result)
		t.Fatalf("expected non-error result, got: %s", text)
	}
	if gotBody["name"] != "Morning Run" {
		t.Errorf("body name = %v, want 'Morning Run'", gotBody["name"])
	}
	if gotBody["sport_type"] != "Run" {
		t.Errorf("body sport_type = %v, want 'Run'", gotBody["sport_type"])
	}
}

func TestCreateActivityMissingRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when required fields are missing")
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleCreateActivity(client)

	// Missing name
	req := makeRequest(map[string]any{
		"sport_type":       "Run",
		"start_date_local": "2024-01-13T06:00:00Z",
		"elapsed_time":     float64(3600),
	})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !result.IsError {
		t.Fatal("expected error result for missing required field 'name'")
	}
}

// --- activities_update tests ---

func TestUpdateActivitySendsOnlyProvidedFields(t *testing.T) {
	var gotBody map[string]any
	var gotPath, gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":123,"name":"New Name"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleUpdateActivity(client)

	// Only send id and name -- NOT description, trainer, commute, etc.
	req := makeRequest(map[string]any{
		"id":   float64(123),
		"name": "New Name",
	})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if gotMethod != "PUT" {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/activities/123" {
		t.Errorf("path = %q, want /activities/123", gotPath)
	}
	if result.IsError {
		text := extractResultText(t, result)
		t.Fatalf("expected non-error result, got: %s", text)
	}

	// CRITICAL: verify only "name" is in body, NOT other fields
	if gotBody["name"] != "New Name" {
		t.Errorf("body name = %v, want 'New Name'", gotBody["name"])
	}

	// These fields should NOT be present since they weren't provided
	for _, field := range []string{"description", "trainer", "commute", "hide_from_home", "sport_type", "type", "gear_id"} {
		if _, ok := gotBody[field]; ok {
			t.Errorf("body should NOT contain %q when not provided, but it does: %v", field, gotBody[field])
		}
	}

	// "id" should NOT be in the body (it's in the URL path)
	if _, ok := gotBody["id"]; ok {
		t.Errorf("body should NOT contain 'id' (it goes in URL path)")
	}
}

func TestUpdateActivityMissingId(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when id is missing")
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleUpdateActivity(client)

	req := makeRequest(map[string]any{"name": "New Name"})
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

// --- activities_get_zones tests ---

func TestGetActivityZonesBasic(t *testing.T) {
	var gotPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"type":"heartrate","distribution_buckets":[]}]`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetActivityZones(client)

	req := makeRequest(map[string]any{"id": float64(123)})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if gotPath != "/activities/123/zones" {
		t.Errorf("path = %q, want /activities/123/zones", gotPath)
	}
	if result.IsError {
		t.Errorf("expected non-error result")
	}
}

// --- Error handling tests ---

func TestAllHandlersReturnStravaErrorOn403(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Rate Limit Exceeded"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)

	tests := []struct {
		name    string
		handler func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error)
		args    map[string]any
	}{
		{
			name:    "activities_list",
			handler: tools.HandleGetActivities(client),
			args:    map[string]any{},
		},
		{
			name:    "activities_get",
			handler: tools.HandleGetActivityById(client),
			args:    map[string]any{"id": float64(123)},
		},
		{
			name:    "activities_create",
			handler: tools.HandleCreateActivity(client),
			args: map[string]any{
				"name":             "Test",
				"sport_type":       "Run",
				"start_date_local": "2024-01-13T06:00:00Z",
				"elapsed_time":     float64(3600),
			},
		},
		{
			name:    "activities_update",
			handler: tools.HandleUpdateActivity(client),
			args:    map[string]any{"id": float64(123), "name": "Updated"},
		},
		{
			name:    "activities_get_zones",
			handler: tools.HandleGetActivityZones(client),
			args:    map[string]any{"id": float64(123)},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := makeRequest(tc.args)
			result, err := tc.handler(context.Background(), req)
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
			if !strings.Contains(text, "Rate Limit Exceeded") {
				t.Errorf("error should contain 'Rate Limit Exceeded', got: %s", text)
			}
		})
	}
}
