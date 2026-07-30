package tools

import (
	"github.com/mark3labs/mcp-go/server"
	"github.com/shotah/go-strava-mcp/internal/strava"
)

// RegisterAll registers all MCP tools with the server.
func RegisterAll(s *server.MCPServer, client *strava.Client) {
	registerActivities(s, client)
	registerAthlete(s, client)
	registerStreams(s, client)
	registerClubs(s, client)
	registerUploads(s, client)
}
