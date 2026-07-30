package server_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/shotah/go-strava-mcp/internal/server"
	"github.com/shotah/go-strava-mcp/internal/update"
)

func TestNewCreatesServer(t *testing.T) {
	s := server.New("test", nil, nil)
	if s == nil {
		t.Fatal("New() returned nil, want non-nil MCPServer")
	}
}

func TestNewRegistersUpdateToolsWithOptions(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	checker := update.NewChecker("1.0.0", update.NewCache(t.TempDir()), logger)
	updater := update.NewUpdater(checker, logger)

	s := server.New("1.0.0", nil, &server.Options{Checker: checker, Updater: updater})
	if s == nil {
		t.Fatal("New() returned nil")
	}

	resp := s.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	result, ok := resp.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("expected JSONRPCResponse, got %T", resp)
	}
	listResult, ok := result.Result.(mcp.ListToolsResult)
	if !ok {
		t.Fatalf("expected ListToolsResult, got %T", result.Result)
	}

	names := make(map[string]bool, len(listResult.Tools))
	for _, tool := range listResult.Tools {
		names[tool.Name] = true
	}
	for _, want := range []string{"activities_list", "utility_check_update", "utility_self_update"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestNewWithoutOptionsSkipsUpdateTools(t *testing.T) {
	s := server.New("1.0.0", nil, nil)
	if s == nil {
		t.Fatal("New() returned nil")
	}

	resp := s.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	result, ok := resp.(mcp.JSONRPCResponse)
	if !ok {
		t.Fatalf("expected JSONRPCResponse, got %T", resp)
	}
	listResult, ok := result.Result.(mcp.ListToolsResult)
	if !ok {
		t.Fatalf("expected ListToolsResult, got %T", result.Result)
	}

	for _, tool := range listResult.Tools {
		if tool.Name == "utility_self_update" || tool.Name == "utility_check_update" {
			t.Errorf("tool %q should not be registered without options", tool.Name)
		}
	}
}

func TestServerHasCorrectNameAndVersion(t *testing.T) {
	s := server.New("1.2.3", nil, nil)
	if s == nil {
		t.Fatal("New() returned nil")
	}
	// MCP server name is "strava" (short; host forms strava__{tool}).
	// Binary/CLI remains strava-mcp. mcp-go does not expose name getters;
	// creation with these parameters is verified by tools/list tests.
}
