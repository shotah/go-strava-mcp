# TODO — MCP tool rename (`{service}_{verb}_{object}`)

**Status:** done (2026-07-29)
**Why:** Hosts expose `{server}__{tool}` (ai-gantry server id = `strava`). Bare
or double-prefixed names (`strava_get_activities`) collide mentally with
Garmin/Google and starve small models. Align with
[google-mcp](https://github.com/shotah/google-mcp): **service first**, then
verb + object.

**Rule:** Do **not** prefix tools with `strava_` — the server id already supplies
that. Host forms look like `strava__activities_list`, `strava__athlete_get`.

**Also:** MCP server name is short `strava` (not `strava-mcp`). Binary /
CLI stays `strava-mcp`.

**Consumer:** [ai-gantry](https://github.com/shotah/ai-gantry) persona `TOOLS.md`
+ master plan in repo `todo.md` (Gemini train → cut back to Qwen).

---

## Convention

```text
{service}_{verb}_{object}[_{qualifier}]
```

Services (match endpoint packages / tool prefix): `activities`, `athlete`,
`clubs`, `uploads`, `utility`, …

Source of truth: `mcp.NewTool(...)` names in `internal/tools/*.go`.

---

## Core tier (gantry `--tool-tier core`)

| Old | New | Host after |
| --- | --- | --- |
| `strava_get_activities` | `activities_list` | `strava__activities_list` |
| `strava_get_activity_by_id` | `activities_get` | `strava__activities_get` |
| `strava_get_athlete` | `athlete_get` | `strava__athlete_get` |
| `strava_get_athlete_stats` | `athlete_get_stats` | `strava__athlete_get_stats` |
| `strava_get_activity_streams` | `activities_get_streams` | `strava__activities_get_streams` |
| `strava_get_activity_zones` | `activities_get_zones` | `strava__activities_get_zones` |

Checklist:

- [x] Rename `mcp.NewTool` strings in `internal/tools/*.go`
- [x] Update MCP server name `strava-mcp` → `strava`
- [x] Update README / MCP docs examples
- [x] Tests that assert tool names
- [ ] Cut release; ai-gantry persona + any hardcoding

---

## Extended + complete

Same rule applied to every tool (no dual aliases).

| Old | New |
| --- | --- |
| `strava_create_activity` | `activities_create` |
| `strava_update_activity` | `activities_update` |
| `strava_get_club_activities` | `clubs_list_activities` |
| `strava_create_upload` | `uploads_create` |
| `strava_get_upload` | `uploads_get` |
| `strava_check_update` | `utility_check_update` |
| `strava_self_update` | `utility_self_update` |

- [x] Full pass over `internal/tools/`
- [x] No dual aliases (one name set per release — same as google-mcp)
- [ ] Release notes with old→new table (when cutting the release)

---

## Repo hygiene (fork ownership)

- [x] Module path → `github.com/shotah/go-strava-mcp`
- [x] Go 1.26
- [x] Makefile / CI / release / pre-commit / `.vscode/settings.json` (match siblings)
- [x] Tool rename wave above
- [ ] Announce breaking change in GitHub Release notes (on next release)

---

## Out of scope / follow-ups

- Changing OAuth / token paths
- Growing the core tier count until rename ships
- ai-gantry `TOOLS.md` / persona hardcoding update after release
