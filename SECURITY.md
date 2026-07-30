# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Older releases | No |

Only the latest release receives security fixes. Update to the latest version with:

```bash
strava-mcp --update
```

## Reporting a Vulnerability

**Do not open a public issue for security vulnerabilities.**

Instead, report vulnerabilities privately:

1. Go to [GitHub Security Advisories](https://github.com/shotah/go-strava-mcp/security/advisories/new)
2. Click **"Report a vulnerability"**
3. Provide a description, steps to reproduce, and any relevant logs (redact tokens)

You will receive an acknowledgment within 48 hours. We will work with you to understand the issue and coordinate a fix before any public disclosure.

## Scope

StravaMCP handles OAuth tokens and communicates with the Strava API. Security-relevant areas include:

- **Token storage** (`~/.strava/tokens.json`) -- file permissions, encryption at rest
- **OAuth flow** -- redirect URI validation, PKCE, token exchange
- **Binary updates** -- SHA256 verification, download integrity
- **MCP transport** -- stdio protocol boundary, input validation

## Best Practices for Users

- Keep your `STRAVA_CLIENT_SECRET` in environment variables, not in config files committed to git
- Use the default token path (`~/.strava/tokens.json`) which is created with restrictive permissions
- Run `strava-mcp --check-update` periodically or enable the automatic startup check
