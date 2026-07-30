package tools

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/shotah/go-strava-mcp/internal/update"
)

func newRegistryChecker(t *testing.T) *update.Checker {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return update.NewChecker("1.0.0", update.NewCache(t.TempDir()), logger)
}

func TestRegisterUpdateTools_BothDependencies(t *testing.T) {
	s := mcpserver.NewMCPServer("strava", "test")
	checker := newRegistryChecker(t)
	updater := update.NewUpdater(checker, slog.New(slog.NewTextHandler(io.Discard, nil)))

	RegisterUpdateTools(s, checker, updater)

	names := registeredToolNames(t, s)
	for _, want := range []string{"utility_check_update", "utility_self_update"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestRegisterUpdateTools_CheckerOnly(t *testing.T) {
	s := mcpserver.NewMCPServer("strava", "test")

	RegisterUpdateTools(s, newRegistryChecker(t), nil)

	names := registeredToolNames(t, s)
	if !names["utility_check_update"] {
		t.Error("missing tool utility_check_update")
	}
	if names["utility_self_update"] {
		t.Error("utility_self_update should not be registered without an updater")
	}
}

func TestRegisterUpdateTools_NoDependencies(t *testing.T) {
	s := mcpserver.NewMCPServer("strava", "test")

	RegisterUpdateTools(s, nil, nil)

	// mcp-go rejects tools/list on a server with no tools, which is exactly
	// what "nothing was registered" looks like from the client side.
	resp := s.HandleMessage(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if _, ok := resp.(mcp.JSONRPCError); !ok {
		t.Fatalf("expected no registered tools, got %T", resp)
	}
}
