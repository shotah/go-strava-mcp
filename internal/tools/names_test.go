package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// expectedToolNames is the canonical MCP tool surface after the
// {service}_{verb}_{object} rename. Host-facing names are strava__{name}.
var expectedToolNames = []string{
	"activities_list",
	"activities_get",
	"activities_create",
	"activities_update",
	"activities_get_zones",
	"activities_get_streams",
	"athlete_get",
	"athlete_get_stats",
	"clubs_list_activities",
	"uploads_create",
	"uploads_get",
}

func registeredToolNames(t *testing.T, s *mcpserver.MCPServer) map[string]bool {
	t.Helper()
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
	return names
}

func TestRegisteredToolNames(t *testing.T) {
	s := mcpserver.NewMCPServer("strava", "test")
	RegisterAll(s, nil)

	names := registeredToolNames(t, s)

	for _, want := range expectedToolNames {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}

	// No dual aliases / leftover strava_ prefixes.
	for name := range names {
		if strings.HasPrefix(name, "strava_") {
			t.Errorf("tool %q still has strava_ prefix (server id already supplies that)", name)
		}
	}

	if len(names) != len(expectedToolNames) {
		t.Errorf("registered %d tools, want %d", len(names), len(expectedToolNames))
	}
}

func TestUpdateToolNames(t *testing.T) {
	if checkUpdateTool.Name != "utility_check_update" {
		t.Errorf("checkUpdateTool.Name = %q, want utility_check_update", checkUpdateTool.Name)
	}
	if selfUpdateTool.Name != "utility_self_update" {
		t.Errorf("selfUpdateTool.Name = %q, want utility_self_update", selfUpdateTool.Name)
	}
}
