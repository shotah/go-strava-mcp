package server

import (
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/shotah/go-strava-mcp/internal/strava"
	"github.com/shotah/go-strava-mcp/internal/tools"
	"github.com/shotah/go-strava-mcp/internal/update"
)

// Options holds optional dependencies for the MCP server.
// When nil, update tools are not registered (e.g., dev builds).
type Options struct {
	Checker *update.Checker
	Updater *update.Updater
}

// New creates a new MCP server with the given version string and Strava client.
// opts may be nil — update tools are simply not registered in that case.
func New(version string, client *strava.Client, opts *Options) *mcpserver.MCPServer {
	s := mcpserver.NewMCPServer(
		"strava",
		version,
		mcpserver.WithLogging(),
	)
	tools.RegisterAll(s, client)

	// Register update tools if dependencies are provided.
	if opts != nil {
		tools.RegisterUpdateTools(s, opts.Checker, opts.Updater)
	}

	return s
}
