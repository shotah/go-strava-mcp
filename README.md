[![Go Version](https://img.shields.io/github/go-mod/go-version/shotah/go-strava-mcp)](https://go.dev/)
[![Go Report Card](https://goreportcard.com/badge/github.com/shotah/go-strava-mcp)](https://goreportcard.com/report/github.com/shotah/go-strava-mcp)
[![GoDoc](https://pkg.go.dev/badge/github.com/shotah/go-strava-mcp)](https://pkg.go.dev/github.com/shotah/go-strava-mcp)
[![License](https://img.shields.io/github/license/shotah/go-strava-mcp)](LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/shotah/go-strava-mcp/ci.yml)](https://github.com/shotah/go-strava-mcp/actions)
[![Release](https://img.shields.io/github/v/release/shotah/go-strava-mcp)](https://github.com/shotah/go-strava-mcp/releases/latest)
[![Downloads](https://img.shields.io/github/downloads/shotah/go-strava-mcp/total)](https://github.com/shotah/go-strava-mcp/releases)
[![Tools](https://img.shields.io/badge/MCP_Tools-11-blue)](README.md#tool-reference)

# StravaMCP

**A production-grade MCP server that gives agent frameworks full access to the Strava API -- single Go binary, zero infrastructure.**

StravaMCP is a [Model Context Protocol](https://modelcontextprotocol.io) server built in Go. It connects AI agents to the Strava API through a single static binary running on your machine. Communicates over stdio, works with any MCP-compatible client, handles OAuth authentication through an automatic browser flow, and stores tokens locally. No cloud services, no containers, no runtime dependencies -- just download and run.

<!-- Terminal recording: run `vhs record.tape` to generate usage.gif, then uncomment the line below -->
<!-- ![Usage](usage.gif) -->
<!-- *Authentication and tool usage with an MCP client* -->

## How It Works

```mermaid
graph LR
    A["MCP Client<br/>(Claude Desktop, Cursor, etc.)"] -- stdio --> B["strava-mcp<br/>Go Binary"]
    B -- HTTPS --> C["Strava API v3"]
    B -- read/write --> D["~/.strava/tokens.json"]
```

## Features

- **Single binary, zero runtime dependencies** -- no Docker, no cloud, no database
- **11 MCP tools** covering activities, athlete stats, streams, clubs, and uploads
- **Automatic OAuth browser flow** -- one command to authenticate
- **Concurrent token refresh via singleflight** -- no thundering herd on expired tokens
- **Atomic write-then-rename token store** -- crash-safe credential persistence
- **Zero-CGO static binary** -- no C library dependencies, runs anywhere
- **Cross-platform** -- macOS (Intel + Apple Silicon), Linux, and Windows (amd64)

## Why Go?

StravaMCP is written in Go for the same reason the RustyClaw ecosystem exists: **performance and simplicity matter for tool servers that agents call hundreds of times per session.**

| | Go (StravaMCP) | Python | Node.js |
|---|---|---|---|
| **Startup time** | ~10ms | ~500ms | ~200ms |
| **Memory footprint** | ~8MB | ~30MB | ~40MB |
| **Binary size** | 7MB (single file) | ~50MB+ (runtime + deps) | ~60MB+ (runtime + node_modules) |
| **Dependencies** | 3 direct | Varies (pip) | Varies (npm) |
| **Runtime required** | None | Python interpreter | Node.js runtime |

*Estimates based on known Go/Python/Node.js runtime characteristics for comparable MCP servers. Not formal benchmarks.*

## Quick Start

### Install

Choose your preferred installation method:

**Option A: Go install**

```bash
go install github.com/shotah/go-strava-mcp@latest
```

**Option B: Download binary**

Download the latest binary for your platform from [GitHub Releases](https://github.com/shotah/go-strava-mcp/releases/latest).

> **macOS Gatekeeper note:** If you download the binary directly, macOS may quarantine it. Remove the quarantine attribute before running:
> ```bash
> xattr -d com.apple.quarantine strava-mcp
> ```

### Set Up Strava API Credentials

1. Create a Strava API application at [https://www.strava.com/settings/api](https://www.strava.com/settings/api)
2. Set the **Authorization Callback Domain** to `localhost`
3. Export your credentials:

```bash
export STRAVA_CLIENT_ID=your_client_id
export STRAVA_CLIENT_SECRET=your_client_secret
```

### Authenticate

Run the built-in OAuth flow. This opens your browser, completes authorization, and saves tokens locally:

```bash
strava-mcp auth
```

You should see: `Authenticated as [Your Name]!`

### Configure Your MCP Client

Add StravaMCP to your client's configuration. For **Claude Desktop**, edit `~/Library/Application Support/Claude/claude_desktop_config.json`:

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

Restart your MCP client and the Strava tools will be available.

## Agent Framework Integration

StravaMCP is one of several high-performance MCP servers under [shotah](https://github.com/shotah) that give AI agents access to external services via stdio transport.

```mermaid
graph TD
    AF["Agent Framework<br/>(OpenClaw / ZeroClaw)"]
    AF -- stdio --> SM["StravaMCP<br/>Go &middot; 7MB"]
    AF -- stdio --> SK["SlackMCP<br/>Rust"]
    AF -- stdio --> WR["WebResearchMCP<br/>Rust"]
    AF -- stdio --> VM["VideoMCP<br/>Rust"]
    SM -- HTTPS --> SA["Strava API v3"]
    SK -- HTTPS --> SKA["Slack API"]
    WR -- HTTPS --> WRA["Web Search APIs"]
    VM -- HTTPS --> VMA["Video APIs"]
```

To wire StravaMCP into an agent framework as a tool provider, add it to your MCP server configuration:

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

The agent framework launches StravaMCP as a subprocess, communicates over stdio using the MCP protocol, and routes tool calls to Strava. No HTTP server, no port configuration -- stdio transport handles everything.

<details>
<summary><strong>Tool Reference (11 tools)</strong></summary>

| Tool | Category | Description |
|------|----------|-------------|
| `strava_get_activities` | Activities | List recent activities with date filtering and pagination |
| `strava_get_activity_by_id` | Activities | Get detailed activity info including laps, splits, and segment efforts |
| `strava_create_activity` | Activities | Create a new manual activity |
| `strava_update_activity` | Activities | Update an existing activity (name, description, sport type, gear) |
| `strava_get_activity_zones` | Activities | Get heart rate and power zone distribution |
| `strava_get_athlete` | Athlete | Get authenticated athlete profile |
| `strava_get_athlete_stats` | Athlete | Get aggregate statistics (recent/YTD/all-time totals) |
| `strava_get_activity_streams` | Streams | Get time-series telemetry data (HR, GPS, power, cadence, altitude) |
| `strava_get_club_activities` | Clubs | List recent activities from club members |
| `strava_create_upload` | Uploads | Upload activity files (GPX, TCX, FIT) |
| `strava_get_upload` | Uploads | Check upload processing status |

</details>

<details>
<summary><strong>Architecture</strong></summary>

```mermaid
graph TD
    M["main.go"] --> AUTH["auth subcommand"]
    M --> MCP["MCP Server (default)"]

    AUTH --> OAUTH["OAuth Browser Flow"]
    OAUTH --> TS["Token Store<br/>~/.strava/tokens.json"]

    MCP --> TH["Tool Handlers<br/>(11 tools)"]
    TH --> SC["Strava Client"]
    SC --> AR["Auto Token Refresh<br/>+ singleflight"]
    AR --> TS
    SC --> API["Strava API v3"]
```

**Key design decisions:**

- **stderr-only logging** -- all logging via `slog` to stderr; stdout is reserved exclusively for MCP JSON-RPC protocol messages
- **singleflight.Group** -- concurrent token refresh requests are coalesced into a single Strava API call, preventing thundering herd
- **Atomic write-then-rename token store** -- token file updates are crash-safe; partial writes never corrupt saved credentials
- **Static binary with zero CGO** -- compiles to a single static binary with no C dependencies, enabling simple cross-platform distribution

</details>

## Configuration

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `STRAVA_CLIENT_ID` | Yes | *(none)* | Strava API application client ID |
| `STRAVA_CLIENT_SECRET` | Yes | *(none)* | Strava API application client secret |
| `STRAVA_TOKEN_PATH` | No | `~/.strava/tokens.json` | Path to token storage file |

### CLI Flags

| Flag | Description |
|------|-------------|
| `strava-mcp auth` | Run OAuth browser flow to authenticate |
| `strava-mcp --version` | Print version, commit, and build date |
| `strava-mcp --debug` | Enable debug-level logging |
| `strava-mcp` | Start MCP server on stdio (default) |

## Development

```bash
# Build
go build .

# Run tests
go test ./...

# Run with debug logging
STRAVA_CLIENT_ID=xxx STRAVA_CLIENT_SECRET=xxx ./strava-mcp --debug
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines.

## License

[ISC](LICENSE)

## Contributing

Found a bug or have an idea? See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup and guidelines. Security issues should be reported privately via [SECURITY.md](SECURITY.md).

## Links

- [Strava API Documentation](https://developers.strava.com)
- [Model Context Protocol Specification](https://modelcontextprotocol.io)
- [GitHub Releases](https://github.com/shotah/go-strava-mcp/releases)
- [Security Policy](SECURITY.md)
