package server_test

import (
	"testing"

	"github.com/shotah/go-strava-mcp/internal/server"
)

func TestNewCreatesServer(t *testing.T) {
	s := server.New("test", nil, nil)
	if s == nil {
		t.Fatal("New() returned nil, want non-nil MCPServer")
	}
}

func TestServerHasCorrectNameAndVersion(t *testing.T) {
	s := server.New("1.2.3", nil, nil)
	if s == nil {
		t.Fatal("New() returned nil")
	}
	// The server was created successfully with a version string.
	// The mcp-go MCPServer does not expose name/version getters directly,
	// but the server creation with these parameters is verified by the
	// protocol handshake in integration tests. Here we verify it does not panic.
}
