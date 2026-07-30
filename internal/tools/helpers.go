package tools

import (
	"bytes"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/shotah/go-strava-mcp/internal/strava"
)

// FormatResponse pretty-prints raw JSON data with 2-space indentation and
// conditionally appends a rate limit warning when API usage exceeds 80%.
// If the data is not valid JSON, returns the raw string as-is.
func FormatResponse(data []byte, client *strava.Client) *mcp.CallToolResult {
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, data, "", "  "); err != nil {
		// Not valid JSON -- return raw string
		return mcp.NewToolResultText(string(data))
	}

	result := pretty.String()

	// Append rate limit warning if usage is high
	if warning := client.RateLimitWarning(); warning != "" {
		result += "\n\n" + warning
	}

	return mcp.NewToolResultText(result)
}

// HandleToolError formats an error into an MCP error result.
// If the error is a StravaError, includes the HTTP status code and response body.
// Otherwise, includes the raw error message.
func HandleToolError(toolName string, err error) *mcp.CallToolResult {
	var stravaErr *strava.StravaError
	if strava.AsStravaError(err, &stravaErr) {
		return mcp.NewToolResultErrorf("%s: Strava API error (%d): %s", toolName, stravaErr.StatusCode, stravaErr.Body)
	}
	return mcp.NewToolResultErrorf("%s: %v", toolName, err)
}
