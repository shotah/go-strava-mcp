---
layout: default
title: Home
nav_order: 1
---

# StravaMCP
{: .fs-9 }

A production-grade MCP server that gives agent frameworks full access to the Strava API -- single Go binary, zero infrastructure.
{: .fs-6 .fw-300 }

[Get Started](#quick-start){: .btn .btn-primary .fs-5 .mb-4 .mb-md-0 .mr-2 }
[View on GitHub](https://github.com/shotah/go-strava-mcp){: .btn .fs-5 .mb-4 .mb-md-0 }

---

## What is this?

**StravaMCP** is a [Model Context Protocol](https://modelcontextprotocol.io) server built in Go for the [OpenClaw/ZeroClaw](https://github.com/shotah) agent framework ecosystem. It connects AI agents to the Strava API through a single static binary on your machine. Communicates over stdio, works with any MCP-compatible client, handles OAuth authentication through an automatic browser flow, and stores tokens locally. No cloud services, no containers, no runtime dependencies.

### Key Features

- **Single binary, zero runtime dependencies** -- no Docker, no cloud, no database
- **11 MCP tools** covering activities, athlete stats, streams, clubs, and uploads
- **Automatic OAuth browser flow** -- one command to authenticate
- **Concurrent token refresh via singleflight** -- no thundering herd on expired tokens
- **Atomic write-then-rename token store** -- crash-safe credential persistence
- **Zero-CGO static binary** -- no C library dependencies, runs anywhere
- **Cross-platform** -- macOS (Intel + Apple Silicon) and Linux (amd64 + arm64)

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

{: .note }
> **macOS Gatekeeper:** If you download the binary directly, remove the quarantine attribute:
> `xattr -d com.apple.quarantine strava-mcp`

### Configure

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

### Connect to Claude Desktop

Add StravaMCP to your Claude Desktop configuration at `~/Library/Application Support/Claude/claude_desktop_config.json`:

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

Restart Claude Desktop and the Strava tools will be available.

## Available Tools

| Category | Tools | Description |
|----------|-------|-------------|
| **Activities** | 5 tools | List, view, create, update activities and get zone data |
| **Athlete** | 2 tools | Profile info and aggregate statistics (recent/YTD/all-time) |
| **Streams** | 1 tool | Time-series telemetry (HR, GPS, power, cadence, altitude) |
| **Clubs** | 1 tool | Recent activities from club members |
| **Uploads** | 2 tools | Upload activity files (GPX, TCX, FIT) and check status |

See the [README](https://github.com/shotah/go-strava-mcp#tool-reference) for the complete tool reference with all 11 tools.

## Links

- [Agent Framework Integration](integration) -- wire StravaMCP into OpenClaw/ZeroClaw
- [GitHub Repository](https://github.com/shotah/go-strava-mcp)
- [Strava API Documentation](https://developers.strava.com)
- [Model Context Protocol](https://modelcontextprotocol.io)
- [GitHub Releases](https://github.com/shotah/go-strava-mcp/releases)

---

Built with Go by [shotah](https://github.com/shotah)
