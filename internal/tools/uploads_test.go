package tools_test

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shotah/go-strava-mcp/internal/tools"
)

// --- uploads_create tests ---

func TestCreateUploadAutoDetectsGPX(t *testing.T) {
	var gotContentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotContentType = r.Header.Get("Content-Type")
		// Parse multipart to check data_type field
		mediaType, params, err := mime.ParseMediaType(gotContentType)
		if err != nil {
			t.Fatalf("parse content type: %v", err)
		}
		if !strings.HasPrefix(mediaType, "multipart/form-data") {
			t.Errorf("content type = %q, want multipart/form-data", mediaType)
		}
		reader := multipart.NewReader(r.Body, params["boundary"])
		form, err := reader.ReadForm(10 << 20)
		if err != nil {
			t.Fatalf("read form: %v", err)
		}
		defer form.RemoveAll()

		if dt := form.Value["data_type"]; len(dt) == 0 || dt[0] != "gpx" {
			t.Errorf("data_type = %v, want [gpx]", dt)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":100,"status":"processing"}`))
	}))
	defer srv.Close()

	// Create temp GPX file
	tmpDir := t.TempDir()
	gpxFile := filepath.Join(tmpDir, "activity.gpx")
	os.WriteFile(gpxFile, []byte("<gpx><trk></trk></gpx>"), 0o644)

	client := newTestClient(srv.URL)
	handler := tools.HandleCreateUpload(client)

	req := makeRequest(map[string]any{"file": gpxFile})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		text := extractResultText(t, result)
		t.Fatalf("expected non-error result, got: %s", text)
	}
}

func TestCreateUploadAutoDetectsFIT(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		_, params, _ := mime.ParseMediaType(ct)
		reader := multipart.NewReader(r.Body, params["boundary"])
		form, _ := reader.ReadForm(10 << 20)
		defer form.RemoveAll()

		if dt := form.Value["data_type"]; len(dt) == 0 || dt[0] != "fit" {
			t.Errorf("data_type = %v, want [fit]", dt)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":101,"status":"processing"}`))
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	fitFile := filepath.Join(tmpDir, "activity.fit")
	os.WriteFile(fitFile, []byte("FIT-binary-data"), 0o644)

	client := newTestClient(srv.URL)
	handler := tools.HandleCreateUpload(client)

	req := makeRequest(map[string]any{"file": fitFile})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		text := extractResultText(t, result)
		t.Fatalf("expected non-error result, got: %s", text)
	}
}

func TestCreateUploadAutoDetectsTCXGZ(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		_, params, _ := mime.ParseMediaType(ct)
		reader := multipart.NewReader(r.Body, params["boundary"])
		form, _ := reader.ReadForm(10 << 20)
		defer form.RemoveAll()

		if dt := form.Value["data_type"]; len(dt) == 0 || dt[0] != "tcx.gz" {
			t.Errorf("data_type = %v, want [tcx.gz]", dt)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":102,"status":"processing"}`))
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	tcxgzFile := filepath.Join(tmpDir, "activity.tcx.gz")
	os.WriteFile(tcxgzFile, []byte("compressed-tcx-data"), 0o644)

	client := newTestClient(srv.URL)
	handler := tools.HandleCreateUpload(client)

	req := makeRequest(map[string]any{"file": tcxgzFile})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		text := extractResultText(t, result)
		t.Fatalf("expected non-error result, got: %s", text)
	}
}

func TestCreateUploadRejectsTxtFile(t *testing.T) {
	tmpDir := t.TempDir()
	txtFile := filepath.Join(tmpDir, "notes.txt")
	os.WriteFile(txtFile, []byte("some text"), 0o644)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for rejected file extension")
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleCreateUpload(client)

	req := makeRequest(map[string]any{"file": txtFile})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for .txt file")
	}
	text := extractResultText(t, result)
	if !strings.Contains(text, "unrecognized file extension") {
		t.Errorf("error should mention unrecognized extension, got: %s", text)
	}
}

func TestCreateUploadUsesExplicitDataType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		_, params, _ := mime.ParseMediaType(ct)
		reader := multipart.NewReader(r.Body, params["boundary"])
		form, _ := reader.ReadForm(10 << 20)
		defer form.RemoveAll()

		if dt := form.Value["data_type"]; len(dt) == 0 || dt[0] != "tcx" {
			t.Errorf("data_type = %v, want [tcx]", dt)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":103,"status":"processing"}`))
	}))
	defer srv.Close()

	// File has .gpx extension but we override with data_type=tcx
	tmpDir := t.TempDir()
	gpxFile := filepath.Join(tmpDir, "activity.gpx")
	os.WriteFile(gpxFile, []byte("<gpx></gpx>"), 0o644)

	client := newTestClient(srv.URL)
	handler := tools.HandleCreateUpload(client)

	req := makeRequest(map[string]any{"file": gpxFile, "data_type": "tcx"})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		text := extractResultText(t, result)
		t.Fatalf("expected non-error result, got: %s", text)
	}
}

func TestCreateUploadSendsOptionalFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		_, params, _ := mime.ParseMediaType(ct)
		reader := multipart.NewReader(r.Body, params["boundary"])
		form, _ := reader.ReadForm(10 << 20)
		defer form.RemoveAll()

		// Check optional string fields
		if v := form.Value["name"]; len(v) == 0 || v[0] != "Morning Ride" {
			t.Errorf("name = %v, want [Morning Ride]", v)
		}
		if v := form.Value["description"]; len(v) == 0 || v[0] != "Great ride" {
			t.Errorf("description = %v, want [Great ride]", v)
		}
		if v := form.Value["external_id"]; len(v) == 0 || v[0] != "ext-123" {
			t.Errorf("external_id = %v, want [ext-123]", v)
		}

		// Check boolean fields
		if v := form.Value["trainer"]; len(v) == 0 || v[0] != "true" {
			t.Errorf("trainer = %v, want [true]", v)
		}
		if v := form.Value["commute"]; len(v) == 0 || v[0] != "false" {
			t.Errorf("commute = %v, want [false]", v)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":104,"status":"processing"}`))
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	fitFile := filepath.Join(tmpDir, "workout.fit")
	os.WriteFile(fitFile, []byte("FIT-data"), 0o644)

	client := newTestClient(srv.URL)
	handler := tools.HandleCreateUpload(client)

	req := makeRequest(map[string]any{
		"file":        fitFile,
		"name":        "Morning Ride",
		"description": "Great ride",
		"external_id": "ext-123",
		"trainer":     true,
		"commute":     false,
	})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		text := extractResultText(t, result)
		t.Fatalf("expected non-error result, got: %s", text)
	}
}

func TestCreateUploadSendsFileContent(t *testing.T) {
	fileContent := "<gpx><trk><name>Test Track</name></trk></gpx>"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		_, params, _ := mime.ParseMediaType(ct)
		reader := multipart.NewReader(r.Body, params["boundary"])
		form, _ := reader.ReadForm(10 << 20)
		defer form.RemoveAll()

		// Verify file field exists and contains correct content
		files := form.File["file"]
		if len(files) == 0 {
			t.Fatal("no 'file' field in multipart form")
		}
		f, err := files[0].Open()
		if err != nil {
			t.Fatalf("open file: %v", err)
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		if string(data) != fileContent {
			t.Errorf("file content = %q, want %q", string(data), fileContent)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":105,"status":"processing"}`))
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	gpxFile := filepath.Join(tmpDir, "track.gpx")
	os.WriteFile(gpxFile, []byte(fileContent), 0o644)

	client := newTestClient(srv.URL)
	handler := tools.HandleCreateUpload(client)

	req := makeRequest(map[string]any{"file": gpxFile})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if result.IsError {
		text := extractResultText(t, result)
		t.Fatalf("expected non-error result, got: %s", text)
	}
}

func TestCreateUploadMissingFileParam(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when file is missing")
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleCreateUpload(client)

	req := makeRequest(map[string]any{})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for missing file param")
	}
	text := extractResultText(t, result)
	if !strings.Contains(text, "file path is required") {
		t.Errorf("error should mention 'file path is required', got: %s", text)
	}
}

func TestCreateUploadNonexistentFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called for nonexistent file")
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleCreateUpload(client)

	req := makeRequest(map[string]any{"file": "/nonexistent/path/activity.gpx"})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for nonexistent file")
	}
	text := extractResultText(t, result)
	if !strings.Contains(text, "open file") {
		t.Errorf("error should mention 'open file', got: %s", text)
	}
}

// --- uploads_get tests ---

func TestGetUploadBasic(t *testing.T) {
	var gotPath, gotMethod string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":789,"status":"Your activity is ready.","activity_id":123456}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetUpload(client)

	req := makeRequest(map[string]any{"id": float64(789)})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if gotMethod != "GET" {
		t.Errorf("method = %q, want GET", gotMethod)
	}
	if gotPath != "/uploads/789" {
		t.Errorf("path = %q, want /uploads/789", gotPath)
	}
	if result.IsError {
		t.Fatal("expected non-error result")
	}

	text := extractResultText(t, result)
	if !strings.Contains(text, "Your activity is ready.") {
		t.Errorf("result should contain status message, got: %s", text)
	}

	// Verify it's pretty-printed JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Errorf("result should be valid JSON, got: %s", text)
	}
}

func TestGetUploadMissingId(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not be called when id is missing")
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetUpload(client)

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

// --- Error handling tests ---

func TestCreateUploadAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"Rate Limit Exceeded"}`))
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	gpxFile := filepath.Join(tmpDir, "activity.gpx")
	os.WriteFile(gpxFile, []byte("<gpx></gpx>"), 0o644)

	client := newTestClient(srv.URL)
	handler := tools.HandleCreateUpload(client)

	req := makeRequest(map[string]any{"file": gpxFile})
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

func TestGetUploadAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	handler := tools.HandleGetUpload(client)

	req := makeRequest(map[string]any{"id": float64(999)})
	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected error result for 404")
	}
	text := extractResultText(t, result)
	if !strings.Contains(text, "404") {
		t.Errorf("error should contain '404', got: %s", text)
	}
}
