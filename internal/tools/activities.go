package tools

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/shotah/go-strava-mcp/internal/strava"
)

var getActivitiesTool = mcp.NewTool("activities_list",
	mcp.WithDescription(`Retrieves the authenticated athlete's activities.

**Key for Enrichment Workflow**: Use this to find activities from specific time periods, especially "today's run" or recent activities that need updating.

**OAuth Scope**: Requires activity:read. Note: "Only Me" privacy activities require activity:read_all scope.

Features:
- Filter by date range using 'before' and 'after' epoch timestamps
- Paginate through results (max 200 per page)
- Returns summary information for each activity (not full details)

Common use cases:
- Find today's activities: Set 'after' to today's start timestamp
- Find this week's activities: Set 'after' to the start of the week
- Browse recent activities: Use default parameters

**Privacy Note**: Only returns activities the authenticated athlete has permission to view based on privacy settings and OAuth scope.

Example: To find today's runs, calculate today's start epoch timestamp and use it as the 'after' parameter.`),
	mcp.WithNumber("before", mcp.Description("Epoch timestamp to retrieve activities before (exclusive)")),
	mcp.WithNumber("after", mcp.Description("Epoch timestamp to retrieve activities after (inclusive). Use this to find recent activities.")),
	mcp.WithNumber("page", mcp.Description("Page number (default: 1)")),
	mcp.WithNumber("per_page", mcp.Description("Number of items per page (1-200, default 30)")),
)

var getActivityByIdTool = mcp.NewTool("activities_get",
	mcp.WithDescription(`Retrieves detailed information about a specific activity by its ID.

Returns comprehensive activity data including:
- Full description and name
- Detailed statistics (distance, time, elevation, speed, heart rate, etc.)
- Gear information
- Splits and laps
- Photos and kudos
- Device information

Use this after 'activities_list' to get full details about a specific activity before updating it.`),
	mcp.WithNumber("id", mcp.Description("The ID of the activity"), mcp.Required()),
	mcp.WithBoolean("include_all_efforts", mcp.Description("Include all segment efforts (default: false)")),
)

var createActivityTool = mcp.NewTool("activities_create",
	mcp.WithDescription(`Creates a new manual activity on Strava.

**OAuth Scope**: Requires activity:write permission.

Use this when:
- Adding activities that weren't automatically recorded
- Logging cross-training or activities from non-connected devices
- Backdating activities that were missed
- Creating placeholder activities for training logs

**Required Fields**:
- name: Activity title
- sport_type: Type of activity (Run, Ride, Swim, etc.)
- start_date_local: When the activity started (ISO 8601 format: YYYY-MM-DDTHH:MM:SSZ)
- elapsed_time: Duration in seconds

**Optional but Recommended**:
- distance: Distance in meters
- description: Detailed notes about the activity
- trainer: Whether it was indoors on a trainer (boolean)
- commute: Whether this was a commute (boolean)

**Valid Sport Types**: Run, TrailRun, Walk, Hike, VirtualRun, Ride, MountainBikeRide, GravelRide, EBikeRide, VirtualRide, Handcycle, Swim, Crossfit, Elliptical, Rowing, StairStepper, WeightTraining, Workout, Yoga, and many more.

**Example**: Logging a gym workout that wasn't tracked: name="Strength Training", sport_type="WeightTraining", elapsed_time=3600 (1 hour).`),
	mcp.WithString("name", mcp.Description("The name of the activity"), mcp.Required()),
	mcp.WithString("sport_type", mcp.Description("Sport type: Run, TrailRun, Walk, Hike, Ride, MountainBikeRide, GravelRide, VirtualRide, Swim, etc."), mcp.Required()),
	mcp.WithString("start_date_local", mcp.Description("ISO 8601 formatted date time (e.g., 2024-01-13T06:00:00Z)"), mcp.Required()),
	mcp.WithNumber("elapsed_time", mcp.Description("Total elapsed time in seconds"), mcp.Required()),
	mcp.WithString("type", mcp.Description("Legacy activity type (deprecated, use sport_type)")),
	mcp.WithString("description", mcp.Description("Description of the activity")),
	mcp.WithNumber("distance", mcp.Description("Distance in meters")),
	mcp.WithBoolean("trainer", mcp.Description("Whether this was a trainer/indoor activity")),
	mcp.WithBoolean("commute", mcp.Description("Whether this was a commute")),
)

var updateActivityTool = mcp.NewTool("activities_update",
	mcp.WithDescription(`**[CRITICAL - PRIMARY ENRICHMENT TOOL]** Updates an existing Strava activity.

**OAuth Scope**: Requires activity:write permission.

**This is THE most important tool for the enrichment workflow.** Use this to transform basic auto-imported activities (especially from Apple Watch) into detailed, meaningful training logs.

**The Enrichment Pattern:**
1. Use 'activities_list' with 'after' parameter to find today's or recent activities
2. Identify the activity that needs enrichment (often has generic names like "Morning Run")
3. Use 'activities_update' to add:
   - Meaningful name (e.g., "Progressive Long Run - 10K" instead of "Morning Run")
   - Detailed description (weather, effort level, training notes, how you felt, route details)
   - Correct sport_type if needed (Run vs TrailRun vs VirtualRun)

**Supports partial updates**: Only provide the fields you want to change. All other fields remain unchanged.

**Apple Watch Enhancement Examples:**
- Generic "Afternoon Run" -> "Hill Repeats Workout - 8x400m"
- No description -> "Perfect weather (60F). Focused on form. Felt strong on the hills. HR avg 155 bpm. Recovery: 2 min between reps."
- Type: Run -> sport_type: TrailRun (if it was on trails)

**Available Fields:**
- name: Activity title
- description: Detailed training notes (weather, effort, feelings, splits, etc.)
- sport_type: Accurate sport classification
- trainer: Mark as indoor if needed
- commute: Mark as commute
- hide_from_home: Hide from feed
- gear_id: Associate with specific shoes/bike

**Coach's Perspective**: Rich descriptions help track training patterns, identify what works, and review progress over time. Transform data into insights!`),
	mcp.WithNumber("id", mcp.Description("The ID of the activity to update"), mcp.Required()),
	mcp.WithString("name", mcp.Description("New activity name/title")),
	mcp.WithString("type", mcp.Description("Legacy activity type (deprecated, use sport_type)")),
	mcp.WithString("sport_type", mcp.Description("Sport type: Run, TrailRun, Walk, Hike, VirtualRun, Ride, MountainBikeRide, etc.")),
	mcp.WithString("description", mcp.Description("Detailed description with training notes, weather, effort, feelings, route details, etc.")),
	mcp.WithBoolean("trainer", mcp.Description("Mark as trainer/indoor activity")),
	mcp.WithBoolean("commute", mcp.Description("Mark as commute")),
	mcp.WithBoolean("hide_from_home", mcp.Description("Hide from home feed")),
	mcp.WithString("gear_id", mcp.Description("ID of the gear (shoes, bike) used")),
)

var getActivityZonesTool = mcp.NewTool("activities_get_zones",
	mcp.WithDescription(`Retrieves the zones of a given activity.

**Note**: This is a **Strava Summit feature**. Requires appropriate activity:read scope based on privacy settings.

Returns time spent in different intensity zones:
- Heart rate zones (if HR data available)
- Power zones (if power data available)

Zone response includes:
- Distribution buckets showing time in each zone
- Whether zones are sensor-based vs calculated
- Whether custom zones or default zones are used
- Zone score and max values

Useful for:
- Analyzing training intensity distribution
- Verifying if an activity met zone targets (e.g., "Did I stay in Zone 2?")
- Understanding effort distribution across an activity
- Performance coaching and analysis
- Tracking training load by zone

**Coaching Value**: Essential for verifying that easy runs stayed easy, tempo runs hit the right intensity, and interval workouts achieved target zones.`),
	mcp.WithNumber("id", mcp.Description("The ID of the activity"), mcp.Required()),
)

// HandleGetActivities returns a handler for the activities_list tool.
func HandleGetActivities(client *strava.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		params := map[string]string{}

		if v := request.GetInt("before", 0); v != 0 {
			params["before"] = strconv.Itoa(v)
		}
		if v := request.GetInt("after", 0); v != 0 {
			params["after"] = strconv.Itoa(v)
		}
		if v := request.GetInt("page", 0); v != 0 {
			params["page"] = strconv.Itoa(v)
		}
		if v := request.GetInt("per_page", 0); v != 0 {
			params["per_page"] = strconv.Itoa(v)
		}

		data, err := client.Get(ctx, "/athlete/activities", params)
		if err != nil {
			return HandleToolError("activities_list", err), nil
		}
		return FormatResponse(data, client), nil
	}
}

// HandleGetActivityById returns a handler for the activities_get tool.
func HandleGetActivityById(client *strava.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := request.GetInt("id", 0)
		if id == 0 {
			return mcp.NewToolResultError("activities_get: id is required"), nil
		}

		params := map[string]string{}
		if request.GetBool("include_all_efforts", false) {
			params["include_all_efforts"] = "true"
		}

		data, err := client.Get(ctx, fmt.Sprintf("/activities/%d", id), params)
		if err != nil {
			return HandleToolError("activities_get", err), nil
		}
		return FormatResponse(data, client), nil
	}
}

// HandleCreateActivity returns a handler for the activities_create tool.
func HandleCreateActivity(client *strava.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()

		// Validate required fields
		name, _ := args["name"].(string)
		if name == "" {
			return mcp.NewToolResultError("activities_create: name is required"), nil
		}
		sportType, _ := args["sport_type"].(string)
		if sportType == "" {
			return mcp.NewToolResultError("activities_create: sport_type is required"), nil
		}
		startDateLocal, _ := args["start_date_local"].(string)
		if startDateLocal == "" {
			return mcp.NewToolResultError("activities_create: start_date_local is required"), nil
		}
		elapsedTime := request.GetInt("elapsed_time", 0)
		if elapsedTime == 0 {
			return mcp.NewToolResultError("activities_create: elapsed_time is required"), nil
		}

		// Build body from all provided arguments
		body := map[string]any{
			"name":             name,
			"sport_type":       sportType,
			"start_date_local": startDateLocal,
			"elapsed_time":     elapsedTime,
		}

		for _, field := range []string{"type", "description", "distance", "trainer", "commute"} {
			if v, ok := args[field]; ok {
				body[field] = v
			}
		}

		data, err := client.Post(ctx, "/activities", body)
		if err != nil {
			return HandleToolError("activities_create", err), nil
		}
		return FormatResponse(data, client), nil
	}
}

// HandleUpdateActivity returns a handler for the activities_update tool.
// CRITICAL: Only sends user-provided fields to avoid zero-value overwrite.
func HandleUpdateActivity(client *strava.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := request.GetInt("id", 0)
		if id == 0 {
			return mcp.NewToolResultError("activities_update: id is required"), nil
		}

		args := request.GetArguments()
		body := map[string]any{}

		// Only include fields that were explicitly provided in the request.
		// Do NOT include "id" in the body (it goes in the URL path).
		stringFields := []string{"name", "type", "sport_type", "description", "gear_id"}
		for _, field := range stringFields {
			if v, ok := args[field]; ok {
				body[field] = v
			}
		}

		boolFields := []string{"trainer", "commute", "hide_from_home"}
		for _, field := range boolFields {
			if v, ok := args[field]; ok {
				body[field] = v
			}
		}

		data, err := client.Put(ctx, fmt.Sprintf("/activities/%d", id), body)
		if err != nil {
			return HandleToolError("activities_update", err), nil
		}
		return FormatResponse(data, client), nil
	}
}

// HandleGetActivityZones returns a handler for the activities_get_zones tool.
func HandleGetActivityZones(client *strava.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := request.GetInt("id", 0)
		if id == 0 {
			return mcp.NewToolResultError("activities_get_zones: id is required"), nil
		}

		data, err := client.Get(ctx, fmt.Sprintf("/activities/%d/zones", id), nil)
		if err != nil {
			return HandleToolError("activities_get_zones", err), nil
		}
		return FormatResponse(data, client), nil
	}
}

// registerActivities registers all activity tools with the MCP server.
func registerActivities(s *server.MCPServer, client *strava.Client) {
	s.AddTool(getActivitiesTool, HandleGetActivities(client))
	s.AddTool(getActivityByIdTool, HandleGetActivityById(client))
	s.AddTool(createActivityTool, HandleCreateActivity(client))
	s.AddTool(updateActivityTool, HandleUpdateActivity(client))
	s.AddTool(getActivityZonesTool, HandleGetActivityZones(client))
}
