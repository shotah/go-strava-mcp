package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/shotah/go-strava-mcp/internal/update"
)

// checkUpdateResponse is the structured JSON returned by strava_check_update.
type checkUpdateResponse struct {
	CurrentVersion  string `json:"current_version"`
	LatestVersion   string `json:"latest_version"`
	UpdateAvailable bool   `json:"update_available"`
	ReleaseURL      string `json:"release_url"`
}

// selfUpdateResponse is the structured JSON returned by strava_self_update.
type selfUpdateResponse struct {
	Updated    bool   `json:"updated"`
	NewVersion string `json:"new_version,omitempty"`
	Message    string `json:"message"`
}

var checkUpdateTool = mcp.NewTool("strava_check_update",
	mcp.WithDescription(`Checks if a newer version of strava-mcp is available.

Returns structured JSON with version information:
- current_version: The running version
- latest_version: The newest release on GitHub
- update_available: Whether an update exists
- release_url: Link to the GitHub release page

This is a read-only operation with no side effects. Safe to call at any time.
Uses cached results when available (24h cooldown on GitHub API calls).`),
)

var selfUpdateTool = mcp.NewTool("strava_self_update",
	mcp.WithDescription(`Updates strava-mcp to the latest version.

Downloads the latest release, verifies the SHA256 checksum, and replaces
the current binary. The server continues running after the update completes â€”
restart strava-mcp to use the new version.

Returns structured JSON with:
- updated: Whether the update was applied
- new_version: The version that was installed (if updated)
- message: Human-readable status message

Will return an error if:
- Already on the latest version
- Installed via Homebrew (use 'brew upgrade strava-mcp' instead)
- Insufficient file permissions to replace the binary
- Running a dev build`),
	mcp.WithBoolean("confirm", mcp.Description("Set to true to proceed with the update. Required to prevent accidental updates.")),
)

// HandleCheckUpdate returns a handler for the strava_check_update tool.
func HandleCheckUpdate(checker *update.Checker) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if checker.IsDev() {
			return mcp.NewToolResultError("strava_check_update: version check not available for dev builds"), nil
		}

		result, err := checker.Check(ctx)
		if err != nil {
			return mcp.NewToolResultErrorf("strava_check_update: %v", err), nil
		}

		resp := checkUpdateResponse{
			CurrentVersion:  result.CurrentVersion,
			LatestVersion:   result.LatestVersion,
			UpdateAvailable: result.UpdateAvailable,
			ReleaseURL:      result.ReleaseURL,
		}

		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return mcp.NewToolResultErrorf("strava_check_update: marshal response: %v", err), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}

// HandleSelfUpdate returns a handler for the strava_self_update tool.
// CRITICAL: This handler never calls os.Exit(). The MCP response must be
// sent before any process state changes.
func HandleSelfUpdate(checker *update.Checker, updater *update.Updater) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Require explicit confirmation to prevent accidental updates.
		confirm := request.GetBool("confirm", false)
		if !confirm {
			return mcp.NewToolResultError("strava_self_update: set confirm: true to proceed with the update"), nil
		}

		if checker.IsDev() {
			return mcp.NewToolResultError("strava_self_update: update not available for dev builds"), nil
		}

		// Resolve the actual binary path (follows symlinks).
		exe, err := os.Executable()
		if err != nil {
			return mcp.NewToolResultErrorf("strava_self_update: cannot determine binary path: %v", err), nil
		}
		binaryPath, err := filepath.EvalSymlinks(exe)
		if err != nil {
			binaryPath = exe
		}

		// Homebrew detection â€” refuse and hint at brew upgrade.
		if update.IsHomebrew(binaryPath) {
			return mcp.NewToolResultError("strava_self_update: installed via Homebrew â€” use 'brew upgrade strava-mcp' instead"), nil
		}

		// Permission pre-check before downloading anything.
		if err := update.CheckWritePermission(binaryPath); err != nil {
			return mcp.NewToolResultErrorf("strava_self_update: %v", err), nil
		}

		// Run the update. Progress is a no-op â€” MCP tools return structured
		// results, not streaming stderr output.
		progress := func(string) {}

		if err := updater.Update(ctx, binaryPath, progress); err != nil {
			return mcp.NewToolResultErrorf("strava_self_update: %v", err), nil
		}

		// Check latest version for the response.
		result, err := checker.Check(ctx)
		if err != nil {
			// Update succeeded but we can't determine the version â€” still report success.
			resp := selfUpdateResponse{
				Updated: true,
				Message: "Update complete. Restart strava-mcp to use the new version.",
			}
			data, _ := json.MarshalIndent(resp, "", "  ")
			return mcp.NewToolResultText(string(data)), nil
		}

		resp := selfUpdateResponse{
			Updated:    true,
			NewVersion: result.LatestVersion,
			Message:    "Update complete. Restart strava-mcp to use the new version.",
		}

		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return mcp.NewToolResultErrorf("strava_self_update: marshal response: %v", err), nil
		}

		return mcp.NewToolResultText(string(data)), nil
	}
}

// RegisterUpdateTools registers the update MCP tools with the server.
// If checker or updater is nil, the corresponding tools are not registered.
// This is called separately from RegisterAll because update tools depend on
// *update.Checker/*update.Updater, not *strava.Client.
func RegisterUpdateTools(s *server.MCPServer, checker *update.Checker, updater *update.Updater) {
	if checker != nil {
		s.AddTool(checkUpdateTool, HandleCheckUpdate(checker))
	}
	if checker != nil && updater != nil {
		s.AddTool(selfUpdateTool, HandleSelfUpdate(checker, updater))
	}
}
