package tools

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/shotah/go-strava-mcp/internal/strava"
)

// streamTypes lists all available Strava activity stream types.
var streamTypes = []string{
	"time", "latlng", "distance", "altitude", "velocity_smooth",
	"heartrate", "cadence", "watts", "temp", "moving", "grade_smooth",
}

var getActivityStreamsTool = mcp.NewTool("activities_get_streams",
	mcp.WithDescription(`**[TELEMETRY & DEEP ANALYSIS]** Retrieves time-series sensor data (streams) from an activity.

**Performance Coach's Secret Weapon**: While activity summaries give you averages, streams give you the complete story - every data point recorded during the activity. Essential for understanding pacing strategy, heart rate response, power distribution, and elevation profiles.

**Available Stream Types:**

**Core Metrics:**
- **time**: Time elapsed in seconds from start (array of timestamps)
- **distance**: Distance in meters at each point
- **latlng**: GPS coordinates [latitude, longitude] for mapping route
- **altitude**: Elevation in meters at each point

**Performance Metrics:**
- **velocity_smooth**: Smoothed speed in meters/second (better than raw GPS)
- **grade_smooth**: Smoothed gradient percentage (positive = uphill, negative = downhill)
- **moving**: Boolean indicating if athlete was moving (vs stopped)

**Physiological Data:**
- **heartrate**: Heart rate in beats per minute (if HR monitor used)
- **cadence**: Running cadence (steps/min) or cycling cadence (RPM)
- **temp**: Temperature in Celsius

**Power Data (cycling/running):**
- **watts**: Power output in watts (if power meter used)

**Analysis Use Cases:**

1. **Pacing Analysis**:
   - Identify if pacing was even, positive split, or negative split
   - Find where pace dropped off (fatigue points)
   - Compare intended vs actual pace strategy

2. **Heart Rate Analysis**:
   - Verify time in each HR zone
   - Check HR response to elevation changes
   - Identify cardiac drift (HR rising at same pace)
   - Recovery analysis (how fast HR drops)

3. **Elevation Strategy**:
   - Analyze power/effort on climbs vs descents
   - Identify elevation gain distribution
   - Understand route difficulty profile

4. **Efficiency Metrics**:
   - Cadence consistency
   - Power distribution (variability)
   - Speed-to-HR relationship

5. **Route Visualization**:
   - Map the exact route taken
   - Identify interesting segments
   - Plan future routes

**Performance Coaching Workflow**:
1. Get activity streams after an important workout or race
2. Analyze the data to understand execution
3. Provide specific feedback: "Your HR spiked to 175 at km 5 (the big hill) but recovered well"
4. Use insights to plan future training: "Your pacing was perfect for the first half but dropped 15s/km in the second half - let's work on endurance"

**Technical Notes**:
- Not all streams available for all activities (depends on device/sensors used)
- Data points are time-aligned across all streams
- Arrays are same length - index [i] in time corresponds to index [i] in all other streams
- Request only needed streams for efficiency (or omit 'keys' for all available)
- **Resolution**: Strava may return data at different resolutions (low, medium, high) depending on original recording frequency
- **OAuth Scope**: Requires activity:read for Everyone/Followers activities, activity:read_all for Only Me activities

**Pro Tip**: Combine streams for advanced insights:
- velocity_smooth + heartrate = aerobic efficiency
- grade_smooth + watts = climbing power
- distance + time + heartrate = heart rate zones over race segments`),
	mcp.WithNumber("id", mcp.Description("The ID of the activity"), mcp.Required()),
	mcp.WithArray("keys",
		mcp.Description("Array of stream types to retrieve: time, latlng, distance, altitude, velocity_smooth, heartrate, cadence, watts, temp, moving, grade_smooth. Omit to get all available streams."),
		mcp.WithStringEnumItems(streamTypes),
	),
	mcp.WithBoolean("key_by_type", mcp.Description("Return streams as an object keyed by type (default: true)")),
)

// HandleGetActivityStreams returns a handler for the activities_get_streams tool.
func HandleGetActivityStreams(client *strava.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := request.GetInt("id", 0)
		if id == 0 {
			return mcp.NewToolResultError("activities_get_streams: id is required"), nil
		}

		params := map[string]string{}

		// Extract keys array from arguments
		args := request.GetArguments()
		if keysRaw, ok := args["keys"]; ok {
			if keysSlice, ok := keysRaw.([]any); ok {
				keys := make([]string, 0, len(keysSlice))
				for _, k := range keysSlice {
					if s, ok := k.(string); ok {
						keys = append(keys, s)
					}
				}
				params["keys"] = strings.Join(keys, ",")
			}
		} else {
			// Default to all stream types when keys not provided
			params["keys"] = strings.Join(streamTypes, ",")
		}

		// key_by_type defaults to true
		keyByType := request.GetBool("key_by_type", true)
		params["key_by_type"] = strconv.FormatBool(keyByType)

		data, err := client.Get(ctx, fmt.Sprintf("/activities/%d/streams", id), params)
		if err != nil {
			return HandleToolError("activities_get_streams", err), nil
		}
		return FormatResponse(data, client), nil
	}
}

// registerStreams registers all streams tools with the MCP server.
func registerStreams(s *server.MCPServer, client *strava.Client) {
	s.AddTool(getActivityStreamsTool, HandleGetActivityStreams(client))
}
