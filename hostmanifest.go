package main

import (
	"encoding/json"
	"io"
)

func writeHostManifest(w io.Writer) error {
	return json.NewEncoder(w).Encode(map[string]any{
		"name":      "strava",
		"command":   "strava-mcp",
		"auth_args": []string{"auth"},
		"auth_flow": "pkce",
		"env_keys":  []string{"STRAVA_CLIENT_ID", "STRAVA_CLIENT_SECRET"},
		"blurb":     "Client id/secret, then OAuth hop.",
	})
}
