package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/shotah/go-strava-mcp/internal/strava"
)

var createUploadTool = mcp.NewTool("uploads_create",
	mcp.WithDescription(`Uploads a new activity file to Strava.

**OAuth Scope**: Requires activity:write permission.

Supports uploading activity files in these formats:
- **FIT** (.fit) - Garmin and most GPS watches
- **TCX** (.tcx) - Training Center XML format
- **GPX** (.gpx) - GPS Exchange Format
- All formats can be gzip compressed (.gz)

**Upload Process:**
1. File is read from the local file path provided
2. File format is auto-detected from extension (or use data_type override)
3. File is uploaded and queued for processing
4. Strava processes the file (can take a few seconds to minutes)
5. Once processed, an activity is created
6. Use 'uploads_get' to check processing status

**Parameters:**
- file: Local file path to the activity file (.fit, .tcx, .gpx, or gzip compressed variants)
- data_type: Optional file format override (fit, tcx, gpx, fit.gz, tcx.gz, gpx.gz). Auto-detected from extension if omitted.
- name: Optional activity name (can also be set via activities_update after processing)
- description: Optional description
- trainer: Mark as trainer activity
- commute: Mark as commute

**Use Cases:**
- Import activities from non-integrated devices
- Bulk upload historical activities
- Upload activities from custom tracking apps
- Migrate data from other platforms

**Important Notes:**
- Uploads are processed asynchronously
- Check upload status with 'uploads_get' using the returned upload ID
- Once processed, the activity ID is available for further updates
- Duplicate activities may be automatically detected and rejected

**Typical Workflow:**
1. Upload file -> get upload ID
2. Poll 'uploads_get' to check status
3. When status is complete, get the activity_id
4. Use activity tools to view or update the created activity`),
	mcp.WithString("file", mcp.Description("Local file path to the activity file (.fit, .tcx, .gpx, or gzip compressed variants)"), mcp.Required()),
	mcp.WithString("name", mcp.Description("Desired activity name")),
	mcp.WithString("description", mcp.Description("Desired activity description")),
	mcp.WithBoolean("trainer", mcp.Description("Whether performed on a trainer")),
	mcp.WithBoolean("commute", mcp.Description("Whether this is a commute")),
	mcp.WithString("data_type", mcp.Description("File format override: fit, fit.gz, tcx, tcx.gz, gpx, gpx.gz. Auto-detected from extension if omitted.")),
	mcp.WithString("external_id", mcp.Description("External identifier for the upload")),
)

var getUploadTool = mcp.NewTool("uploads_get",
	mcp.WithDescription(`Retrieves the status of a file upload.

**OAuth Scope**: Requires activity:read permission.

After uploading an activity file with 'uploads_create', use this tool to check the processing status.

**Upload Statuses:**
- "Your activity is still being processed." - Processing in progress
- "Your activity is ready." - Successfully processed
- "There was an error processing your activity." - Processing failed

**Response Fields:**
- id: Upload ID
- external_id: External identifier if provided
- error: Error message if processing failed
- status: Current status message
- activity_id: The created activity's ID (only present when successfully processed)

**Typical Usage:**
1. Upload a file with 'uploads_create'
2. Get the upload ID from the response
3. Poll this endpoint to check status
4. Once activity_id is present, use activity tools to view or modify

**Coaching Workflow:**
After an athlete uploads a workout file:
1. Check upload status
2. When ready, get the activity details
3. Analyze and provide feedback
4. Enrich with descriptions and coaching notes`),
	mcp.WithNumber("id", mcp.Description("The ID of the upload"), mcp.Required()),
)

// validDataTypes lists all valid upload data types.
var validDataTypes = map[string]bool{
	"fit": true, "tcx": true, "gpx": true,
	"fit.gz": true, "tcx.gz": true, "gpx.gz": true,
}

// HandleCreateUpload returns a handler for the uploads_create tool.
func HandleCreateUpload(client *strava.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()

		// Parse file path (required)
		filePath, _ := args["file"].(string)
		if filePath == "" {
			return mcp.NewToolResultError("uploads_create: file path is required"), nil
		}

		// Determine data_type: explicit override or auto-detect from extension
		dataType, _ := args["data_type"].(string)
		if dataType == "" {
			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filePath), "."))
			switch ext {
			case "fit", "tcx", "gpx":
				dataType = ext
			case "gz":
				base := strings.TrimSuffix(filePath, ".gz")
				innerExt := strings.ToLower(strings.TrimPrefix(filepath.Ext(base), "."))
				switch innerExt {
				case "fit", "tcx", "gpx":
					dataType = innerExt + ".gz"
				default:
					return mcp.NewToolResultError("uploads_create: cannot detect data_type from extension; provide data_type explicitly"), nil
				}
			default:
				return mcp.NewToolResultError("uploads_create: unrecognized file extension '" + ext + "'; provide data_type explicitly (fit, tcx, gpx, fit.gz, tcx.gz, gpx.gz)"), nil
			}
		}

		// Validate data_type (security: prevents arbitrary file reads)
		if !validDataTypes[dataType] {
			return mcp.NewToolResultError("uploads_create: invalid data_type '" + dataType + "'; must be one of: fit, tcx, gpx, fit.gz, tcx.gz, gpx.gz"), nil
		}

		// Open file
		file, err := os.Open(filePath)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("uploads_create: open file: %v", err)), nil
		}
		defer file.Close()

		// Build multipart form
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)

		part, err := writer.CreateFormFile("file", filepath.Base(filePath))
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("uploads_create: create form file: %v", err)), nil
		}
		if _, err := io.Copy(part, file); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("uploads_create: copy file: %v", err)), nil
		}
		file.Close()

		// Write data_type field
		if err := writer.WriteField("data_type", dataType); err != nil {
			return mcp.NewToolResultErrorf("uploads_create: write data_type field: %v", err), nil
		}

		// Optional string fields
		for _, field := range []string{"name", "description", "external_id"} {
			if v, ok := args[field]; ok {
				if s, ok := v.(string); ok && s != "" {
					if err := writer.WriteField(field, s); err != nil {
						return mcp.NewToolResultErrorf("uploads_create: write %s field: %v", field, err), nil
					}
				}
			}
		}

		// Optional boolean fields
		for _, field := range []string{"trainer", "commute"} {
			if v, ok := args[field]; ok {
				if b, ok := v.(bool); ok {
					if err := writer.WriteField(field, strconv.FormatBool(b)); err != nil {
						return mcp.NewToolResultErrorf("uploads_create: write %s field: %v", field, err), nil
					}
				}
			}
		}

		// MUST close writer before reading buf (finalizes boundary)
		writer.Close()

		// Send multipart request using FormDataContentType (includes boundary)
		data, err := client.PostMultipart(ctx, "/uploads", &buf, writer.FormDataContentType())
		if err != nil {
			return HandleToolError("uploads_create", err), nil
		}
		return FormatResponse(data, client), nil
	}
}

// HandleGetUpload returns a handler for the uploads_get tool.
func HandleGetUpload(client *strava.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		id := request.GetInt("id", 0)
		if id == 0 {
			return mcp.NewToolResultError("uploads_get: id is required"), nil
		}

		data, err := client.Get(ctx, fmt.Sprintf("/uploads/%d", id), nil)
		if err != nil {
			return HandleToolError("uploads_get", err), nil
		}
		return FormatResponse(data, client), nil
	}
}

// registerUploads registers all upload tools with the MCP server.
func registerUploads(s *server.MCPServer, client *strava.Client) {
	s.AddTool(createUploadTool, HandleCreateUpload(client))
	s.AddTool(getUploadTool, HandleGetUpload(client))
}
