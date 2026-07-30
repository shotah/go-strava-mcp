# Contributing to StravaMCP

Thank you for your interest in contributing to StravaMCP! This guide covers everything you need to get started.

## Reporting Issues

Found a bug or have an idea? Open an issue using one of our templates:

- **[Bug Report](https://github.com/shotah/go-strava-mcp/issues/new?template=bug_report.yml)** -- something broken or unexpected
- **[Feature Request](https://github.com/shotah/go-strava-mcp/issues/new?template=feature_request.yml)** -- new tool, enhancement, or improvement

Before opening an issue:

1. Search [existing issues](https://github.com/shotah/go-strava-mcp/issues) to avoid duplicates
2. For bugs, include your version (`strava-mcp --version`), OS, and MCP client
3. For security vulnerabilities, **do not open a public issue** -- see [SECURITY.md](SECURITY.md)

## Prerequisites

- **Go 1.25+** (match [go.mod](go.mod))
- **Git**
- A **Strava API application** â€” create one at https://www.strava.com/settings/api

## Development Setup

```bash
git clone https://github.com/shotah/go-strava-mcp.git
cd go-strava-mcp
make tools          # goimports-reviser + golangci-lint
make install-hooks  # pre-commit: autofix → lint → test
make check          # fmt + lint + test
```

## Environment Setup

Required environment variables:

| Variable | Required | Description |
|----------|----------|-------------|
| `STRAVA_CLIENT_ID` | Yes | Your Strava API application client ID |
| `STRAVA_CLIENT_SECRET` | Yes | Your Strava API application client secret |
| `STRAVA_TOKEN_PATH` | No | Path to token storage (default: `~/.strava/tokens.json`) |

Authenticate with the OAuth browser flow:

```bash
export STRAVA_CLIENT_ID=your_id
export STRAVA_CLIENT_SECRET=your_secret
./strava-mcp auth
```

This opens your browser to complete Strava authorization and saves tokens locally.

## Running Tests

```bash
make test                    # all packages
make test PKG=./internal/tools/...
make coverage                # internal/ packages
make check                   # fmt + lint + test (same as pre-commit)
```

## Project Structure

```
internal/
  auth/       - OAuth browser flow and file-based token store
  config/     - Environment variable loading and validation
  server/     - MCP server setup and tool registration
  strava/     - Strava HTTP client with auto-refresh
  tools/      - MCP tool handlers (11 tools across 5 categories)
main.go       - Entry point (auth subcommand + MCP server mode)
```

## Adding a New Tool

1. Create a handler in `internal/tools/` following the existing pattern (e.g., `activities.go`)
2. Handler function signature: `func HandleXxx(client *strava.Client) mcp.ToolHandlerFunc`
3. Register in `internal/tools/register.go` via a `registerXxx` function
4. Add tests in the corresponding `_test.go` file

## Submitting a Pull Request

1. Fork the repo and create a branch from `main`
2. Make your changes
3. Run `go test ./...` and `go vet ./...` before submitting
4. Open a PR -- the [PR template](https://github.com/shotah/go-strava-mcp/blob/main/.github/PULL_REQUEST_TEMPLATE.md) will guide you through the checklist
5. Follow conventional commits: `feat:`, `fix:`, `docs:`, `refactor:`
6. Keep PRs focused on a single concern

We review PRs within a few days. If your PR adds a new MCP tool, make sure you've followed the steps in [Adding a New Tool](#adding-a-new-tool) above.

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- All logging to stderr via `slog` (stdout is MCP JSON-RPC)
- Use the established handler closure pattern: `HandleXxx(client)` returns `ToolHandlerFunc`
- Error handling: use `FormatResponse`/`HandleToolError` helpers from `internal/tools/helpers.go`

## License

By contributing, you agree that your contributions will be licensed under the [ISC License](LICENSE).
