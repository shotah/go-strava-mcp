package tools

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/shotah/go-strava-mcp/internal/strava"
)

var listRoutesTool = mcp.NewTool("routes_list",
	mcp.WithDescription(`Lists the authenticated athlete's saved Strava routes (planned maps).

**OAuth Scope**: Requires read. Private routes require read_all.

Returns route summaries including:
- Route id, name, and description
- Distance (meters) and elevation gain (meters)
- Estimated moving time
- Sport type (ride / run)
- Map summary polyline
- Starred / private flags
- Created and updated timestamps

Use this to:
- Find a saved route by name after the athlete built it in Strava
- Get route ids to pass to routes_get
- Review planned maps before a workout

Pagination: page (default 1) and per_page (1-200, default 30).`),
	mcp.WithNumber("id", mcp.Description("Athlete ID (optional - defaults to authenticated athlete)")),
	mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
	mcp.WithNumber("per_page", mcp.Description("Number of items per page (1-200, default 30)")),
)

var getRouteTool = mcp.NewTool("routes_get",
	mcp.WithDescription(`Retrieves a saved Strava route (planned map) by id, including map polylines.

**OAuth Scope**: Requires read. Private routes require read_all.

Returns full route detail:
- Name, description, distance, elevation, estimated time
- map.summary_polyline and map.polyline (encoded Google polylines)
- Matched segments when present
- Athlete who created the route

Use this after routes_list, or after urls_resolve when a share link points at a route.

For a completed activity's GPS track, use activities_get / activities_get_streams instead.`),
	mcp.WithNumber("id", mcp.Description("The ID of the route"), mcp.Required()),
)

// HandleListRoutes returns a handler for the routes_list tool.
func HandleListRoutes(client *strava.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		athleteID := int64(request.GetInt("id", 0))
		if athleteID == 0 {
			id, err := authenticatedAthleteID(ctx, client)
			if err != nil {
				return HandleToolError("routes_list", err), nil
			}
			athleteID = id
		}

		params := map[string]string{}
		if v := request.GetInt("page", 0); v != 0 {
			params["page"] = strconv.Itoa(v)
		}
		if v := request.GetInt("per_page", 0); v != 0 {
			params["per_page"] = strconv.Itoa(v)
		}

		data, err := client.Get(ctx, fmt.Sprintf("/athletes/%d/routes", athleteID), params)
		if err != nil {
			return HandleToolError("routes_list", err), nil
		}
		return FormatResponse(data, client), nil
	}
}

// HandleGetRoute returns a handler for the routes_get tool.
func HandleGetRoute(client *strava.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := request.GetInt("id", 0)
		if id == 0 {
			return mcp.NewToolResultError("routes_get: id is required"), nil
		}

		data, err := client.Get(ctx, fmt.Sprintf("/routes/%d", id), nil)
		if err != nil {
			return HandleToolError("routes_get", err), nil
		}
		return FormatResponse(data, client), nil
	}
}

// registerRoutes registers all route tools with the MCP server.
func registerRoutes(s *server.MCPServer, client *strava.Client) {
	s.AddTool(listRoutesTool, HandleListRoutes(client))
	s.AddTool(getRouteTool, HandleGetRoute(client))
}
