# go-strava-mcp

Strava MCP server (Go)

<p align="center">
  <a href="https://github.com/shotah/go-strava-mcp/actions/workflows/ci.yml"><img src="https://github.com/shotah/go-strava-mcp/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/shotah/go-strava-mcp/actions/workflows/release.yml"><img src="https://github.com/shotah/go-strava-mcp/actions/workflows/release.yml/badge.svg" alt="Release"></a>
  <a href="https://github.com/shotah/go-strava-mcp/actions/workflows/ci.yml"><img src="https://github.com/shotah/go-strava-mcp/raw/gh-pages/badges/coverage.svg" alt="Coverage"></a>
  <a href="https://pkg.go.dev/github.com/shotah/go-strava-mcp"><img src="https://pkg.go.dev/badge/github.com/shotah/go-strava-mcp.svg" alt="Go Reference"></a>
  <img src="https://img.shields.io/github/go-mod/go-version/shotah/go-strava-mcp" alt="Go version">
  <a href="LICENSE"><img src="https://img.shields.io/github/license/shotah/go-strava-mcp" alt="License"></a>
</p>

<p align="center">
  <strong>Give Claude, Cursor, and other MCP clients real access to your Strava data.</strong><br>
  Activities, streams, athlete stats, clubs, uploads — one small binary, no runtime.
</p>

**11 tools · server id `strava` · single binary · OAuth that just works**

Drop it into your MCP config and ask your agent for recent runs, HR streams, YTD totals, or to update an activity description — over stdio, with tokens stored on your machine.

| Service | What agents can do |
| ------- | ------------------ |
| **Activities** | List, get, create, update; zones and streams |
| **Athlete** | Profile and aggregate stats |
| **Clubs** | Recent club member activities |
| **Uploads** | Push FIT / TCX / GPX and poll processing |

Built for **local, single-user** AI tool use. Sibling MCP servers in the same style: [google-mcp](https://github.com/shotah/google-mcp), [go-garmin](https://github.com/shotah/go-garmin).

## Why this one

- **Zero runtime** — download a binary (or `go install`) and run; no Python, no Node
- **Service-first tool names** — hosts expose `strava__activities_list`, not ambiguous `get_activities` (aligned with [google-mcp](https://github.com/shotah/google-mcp))
- **Short server id** — MCP server name is `strava`; binary / CLI is `strava-mcp`
- **Local-first auth** — `strava-mcp auth` once; tokens under `~/.strava/`; refresh is silent and coalesced with `singleflight`
- **Works where you already work** — Claude Desktop, Cursor, Claude Code, and any stdio MCP client

## Quick start

### 1. Strava API app

1. Create an application at [Strava API settings](https://www.strava.com/settings/api)
2. Set **Authorization Callback Domain** to `localhost`
3. Note the **Client ID** and **Client Secret**

### 2. Install

**Pre-built binary** (no Go required) — grab the archive for your platform from [Releases](https://github.com/shotah/go-strava-mcp/releases/latest):

| Platform | File |
| --- | --- |
| Linux x86_64 | `strava-mcp_*_linux_amd64.tar.gz` |
| Linux ARM64 | `strava-mcp_*_linux_arm64.tar.gz` |
| macOS Apple Silicon | `strava-mcp_*_darwin_arm64.tar.gz` |
| macOS Intel | `strava-mcp_*_darwin_amd64.tar.gz` |
| Windows x86_64 | `strava-mcp_*_windows_amd64.zip` |

```bash
tar xzf strava-mcp_*_darwin_arm64.tar.gz
chmod +x strava-mcp
mv strava-mcp ~/.local/bin/
```

> **macOS Gatekeeper:** if the binary was downloaded from the browser, clear quarantine first:
> `xattr -d com.apple.quarantine strava-mcp`

**Or with Go** (1.26+):

```bash
go install github.com/shotah/go-strava-mcp@latest
```

### 3. Environment

```bash
export STRAVA_CLIENT_ID="your_client_id"
export STRAVA_CLIENT_SECRET="your_client_secret"
# optional: STRAVA_TOKEN_PATH=~/.strava/tokens.json
```

### 4. Authenticate

```bash
strava-mcp auth
```

Opens a browser, completes OAuth, writes tokens locally. You should see `Authenticated as [Your Name]!`.

### 5. MCP client config

Use server id **`strava`** so hosts expose tools as `strava__activities_list`.

```json
{
  "mcpServers": {
    "strava": {
      "command": "strava-mcp",
      "env": {
        "STRAVA_CLIENT_ID": "your_client_id",
        "STRAVA_CLIENT_SECRET": "your_client_secret"
      }
    }
  }
}
```

Restart the client. Ask things like “what were my last five runs?” or “show heart-rate stream for activity 123”.

## Tool reference

Host-facing names are `{server}__{tool}` → e.g. `strava__activities_list`.

| Tool | Description |
| --- | --- |
| `activities_list` | Recent activities (date filters, pagination) |
| `activities_get` | Full activity detail (laps, splits, segments) |
| `activities_create` | Create a manual activity |
| `activities_update` | Update name, description, sport type, gear |
| `activities_get_zones` | HR / power zone distribution |
| `activities_get_streams` | Time-series telemetry (HR, GPS, power, …) |
| `athlete_get` | Authenticated athlete profile |
| `athlete_get_stats` | Recent / YTD / all-time totals |
| `clubs_list_activities` | Recent activities from club members |
| `uploads_create` | Upload FIT / TCX / GPX |
| `uploads_get` | Upload processing status |

Release builds also register `utility_check_update` / `utility_self_update` (not present on `dev` builds).

## Configuration

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `STRAVA_CLIENT_ID` | Yes | — | Strava API client ID |
| `STRAVA_CLIENT_SECRET` | Yes | — | Strava API client secret |
| `STRAVA_TOKEN_PATH` | No | `~/.strava/tokens.json` | Token store path |
| `STRAVA_OAUTH_BIND` | No | `0.0.0.0` | OAuth callback bind host (`0.0.0.0` for Docker `-p`; use `127.0.0.1` for loopback-only). `redirect_uri` stays `http://localhost:19876/callback` |
| `STRAVA_MCP_NO_UPDATE_CHECK` | No | unset | Set to skip background update check on startup |

### CLI

| Command | Description |
| --- | --- |
| `strava-mcp` | Start MCP server on stdio (default) |
| `strava-mcp auth` | OAuth browser flow |
| `strava-mcp --version` | Version / commit / build date |
| `strava-mcp --check-update` | Print whether a newer release exists |
| `strava-mcp --update` | Self-update from GitHub Releases |
| `strava-mcp --debug` | Debug logging on stderr |

Stdout is reserved for MCP JSON-RPC; all logs go to stderr.

## Development

```bash
make tools          # goimports-reviser + golangci-lint
make install-hooks  # pre-commit: fmt → lint → test + ≥70% coverage
make check          # same gate as CI
make coverage       # coverprofile + threshold
```

```bash
go test ./...
STRAVA_CLIENT_ID=… STRAVA_CLIENT_SECRET=… go run . --debug
```

CI builds, lints, enforces **≥70%** coverage on `./...`, and publishes a coverage badge to `gh-pages`. Releases are cut with `make release` (GoReleaser on `v*` tags).

See [CONTRIBUTING.md](CONTRIBUTING.md) and [SECURITY.md](SECURITY.md).

## License

[ISC](LICENSE)

Forked from [Stealinglight/StravaMCP](https://github.com/Stealinglight/StravaMCP); module path, tooling, and MCP naming reshaped for the [shotah](https://github.com/shotah) MCP set.

## Links

- [Strava API docs](https://developers.strava.com)
- [Model Context Protocol](https://modelcontextprotocol.io)
- [GitHub Releases](https://github.com/shotah/go-strava-mcp/releases)
