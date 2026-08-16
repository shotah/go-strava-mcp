package tools

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/shotah/go-strava-mcp/internal/strava"
)

var resolveURLTool = mcp.NewTool("urls_resolve",
	mcp.WithDescription(`Resolves a Strava URL — including shortened strava.app.link share links — to a resource and optionally fetches it.

Strava share sheets often produce short links the model cannot open. This tool follows those redirects (Strava/Branch hosts only) and returns:

- type: activity, route, segment, athlete, or club
- id: numeric Strava id
- canonical_url: https://www.strava.com/{type}s/{id}
- resource: the API object when fetch is true (default)

**Getting a map from a share link**:
1. Pass the shortened or full URL
2. When type is route, resource includes map.summary_polyline / map.polyline
3. When type is activity, resource includes the activity map polyline

Also accepts already-canonical URLs (www.strava.com/routes/…, /activities/…).

**OAuth Scope**: Fetching the resource uses the same scopes as the underlying GET (read / read_all / activity:read).`),
	mcp.WithString("url", mcp.Description("Strava URL or strava.app.link short link"), mcp.Required()),
	mcp.WithBoolean("fetch", mcp.Description("Fetch the resolved resource from the Strava API (default: true)")),
)

type resolvedURLResult struct {
	InputURL     string `json:"input_url"`
	CanonicalURL string `json:"canonical_url"`
	Type         string `json:"type"`
	ID           int64  `json:"id"`
	Resource     any    `json:"resource,omitempty"`
}

// HandleResolveURL returns a handler for the urls_resolve tool.
func HandleResolveURL(client *strava.Client, resolver *strava.URLResolver) server.ToolHandlerFunc {
	if resolver == nil {
		resolver = strava.NewURLResolver()
	}
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		raw, _ := args["url"].(string)
		if raw == "" {
			return mcp.NewToolResultError("urls_resolve: url is required"), nil
		}

		resolved, err := resolver.Resolve(ctx, raw)
		if err != nil {
			return HandleToolError("urls_resolve", err), nil
		}

		out := resolvedURLResult{
			InputURL:     resolved.InputURL,
			CanonicalURL: resolved.CanonicalURL,
			Type:         string(resolved.Type),
			ID:           resolved.ID,
		}

		if request.GetBool("fetch", true) {
			path, err := resolved.APIPath()
			if err != nil {
				return HandleToolError("urls_resolve", err), nil
			}
			data, err := client.Get(ctx, path, nil)
			if err != nil {
				return HandleToolError("urls_resolve", err), nil
			}
			var resource any
			if err := json.Unmarshal(data, &resource); err != nil {
				return HandleToolError("urls_resolve", err), nil
			}
			out.Resource = resource
		}

		payload, err := json.Marshal(out)
		if err != nil {
			return HandleToolError("urls_resolve", err), nil
		}
		return FormatResponse(payload, client), nil
	}
}

// registerURLs registers URL tools with the MCP server.
func registerURLs(s *server.MCPServer, client *strava.Client) {
	s.AddTool(resolveURLTool, HandleResolveURL(client, strava.NewURLResolver()))
}
