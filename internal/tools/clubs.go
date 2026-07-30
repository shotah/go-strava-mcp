package tools

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/shotah/go-strava-mcp/internal/strava"
)

var getClubActivitiesTool = mcp.NewTool("clubs_list_activities",
	mcp.WithDescription(`Retrieves recent activities from members of a specific club.

**OAuth Scope**: Requires read scope. Only shows activities visible based on member privacy settings.

Clubs on Strava are groups of athletes who share activities, compete on leaderboards, and stay connected. This tool lets you see what club members have been up to.

Returns activity summaries including:
- Activity details (name, distance, time, elevation, etc.)
- Athlete information (name of club member who did the activity)
- Activity statistics and metadata

**Use Cases:**
- Monitor club activity and engagement
- See what training club members are doing
- Find popular routes or workout types
- Track club challenges or group goals
- Encourage and support club members

**Coaching Applications:**
- Review team training consistency
- Identify athletes who might need check-ins
- Celebrate club achievements
- Coordinate group workouts based on activity patterns

Note: Only shows activities from club members who have their privacy settings set to allow club visibility.`),
	mcp.WithNumber("id", mcp.Description("The ID of the club"), mcp.Required()),
	mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
	mcp.WithNumber("per_page", mcp.Description("Number of items per page (1-200, default 30)")),
)

// HandleGetClubActivities returns a handler for the clubs_list_activities tool.
func HandleGetClubActivities(client *strava.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := request.GetInt("id", 0)
		if id == 0 {
			return mcp.NewToolResultError("clubs_list_activities: id is required"), nil
		}

		params := map[string]string{}

		if v := request.GetInt("page", 0); v != 0 {
			params["page"] = strconv.Itoa(v)
		}
		if v := request.GetInt("per_page", 0); v != 0 {
			params["per_page"] = strconv.Itoa(v)
		}

		data, err := client.Get(ctx, fmt.Sprintf("/clubs/%d/activities", id), params)
		if err != nil {
			return HandleToolError("clubs_list_activities", err), nil
		}
		return FormatResponse(data, client), nil
	}
}

// registerClubs registers all club tools with the MCP server.
func registerClubs(s *server.MCPServer, client *strava.Client) {
	s.AddTool(getClubActivitiesTool, HandleGetClubActivities(client))
}
