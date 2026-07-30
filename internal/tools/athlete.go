package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/shotah/go-strava-mcp/internal/strava"
)

var getAthleteTool = mcp.NewTool("strava_get_athlete",
	mcp.WithDescription(`Retrieves the authenticated athlete's profile information.

**OAuth Scope**: Requires profile:read_all for detailed representation.

Returns comprehensive profile data including:
- Name and username
- Location (city, state, country)
- Profile pictures (avatar, profile medium, profile)
- Account type (premium/summit status)
- Account creation and update dates
- Weight (if profile:read_all scope)
- Basic settings and preferences

Use this to:
- Get the athlete's ID for other API calls
- Display profile information
- Check account status
- Personalize responses with the athlete's name ("Hi Sarah!")

This is useful when you need to reference the athlete by name or understand their account status.`),
)

var getAthleteStatsTool = mcp.NewTool("strava_get_athlete_stats",
	mcp.WithDescription(`Retrieves comprehensive statistics about an athlete's activities.

**OAuth Scope**: Requires profile:read_all. Can only retrieve stats for the authenticated athlete.

**Performance Coaching Value**: Essential for understanding training volume, trends, and progress over different time periods.

Returns activity totals broken down by:
- **Recent** (last 4 weeks)
- **Year-to-date** (current calendar year)
- **All-time** (entire Strava history)

For each period, provides data for three activity types:
- **Running**: Distance, time, elevation, count
- **Cycling**: Distance, time, elevation, count
- **Swimming**: Distance, time, count

Statistics include:
- Total distance (meters)
- Total moving time (seconds)
- Total elevation gain (meters)
- Activity count
- Achievement count

**Coaching Applications**:
- Track training volume week over week
- Compare current year progress to goals
- Identify trends (increasing/decreasing volume)
- Assess training consistency
- Plan future workouts based on recent load
- Celebrate milestones and achievements

**Example Insights**:
- "You've run 150km this month, up 20% from last month"
- "YTD you've climbed 5,000m - halfway to your annual goal!"
- "Recent activity shows consistent 4x/week training"`),
	mcp.WithNumber("id", mcp.Description("Athlete ID (optional - defaults to authenticated athlete)")),
)

// HandleGetAthlete returns a handler for the get_athlete tool.
func HandleGetAthlete(client *strava.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		data, err := client.Get(ctx, "/athlete", nil)
		if err != nil {
			return HandleToolError("get_athlete", err), nil
		}
		return FormatResponse(data, client), nil
	}
}

// HandleGetAthleteStats returns a handler for the get_athlete_stats tool.
// When id is omitted (0), auto-fetches the authenticated athlete's ID first.
func HandleGetAthleteStats(client *strava.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		athleteID := request.GetInt("id", 0)

		// Auto-fetch athlete ID if not provided
		if athleteID == 0 {
			profileData, err := client.Get(ctx, "/athlete", nil)
			if err != nil {
				return HandleToolError("get_athlete_stats", err), nil
			}

			var profile struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(profileData, &profile); err != nil {
				return HandleToolError("get_athlete_stats", fmt.Errorf("parse athlete profile: %w", err)), nil
			}
			athleteID = int(profile.ID)
		}

		data, err := client.Get(ctx, fmt.Sprintf("/athletes/%d/stats", athleteID), nil)
		if err != nil {
			return HandleToolError("get_athlete_stats", err), nil
		}
		return FormatResponse(data, client), nil
	}
}

// registerAthlete registers all athlete tools with the MCP server.
func registerAthlete(s *server.MCPServer, client *strava.Client) {
	s.AddTool(getAthleteTool, HandleGetAthlete(client))
	s.AddTool(getAthleteStatsTool, HandleGetAthleteStats(client))
}
