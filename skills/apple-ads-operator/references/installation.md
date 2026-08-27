# Installation and MCP client connection

Use this reference when the binary is missing, a client cannot find the server, or the user asks an agent to perform setup. Get the user's approval before installing software or changing a client configuration.

## Install the binary

Homebrew is the supported default on macOS and Linux:

```bash
brew install zelentsov-dev/tap/apple-ads-mcp
```

Resolve the installed binary to an absolute path so GUI clients do not depend on their inherited `PATH`:

```bash
APPLE_ADS_MCP_BIN="$(brew --prefix)/bin/apple-ads-mcp"
"$APPLE_ADS_MCP_BIN" version
```

Windows users should download the matching ZIP and `checksums.txt` from GitHub Releases, verify the checksum, and use the extracted executable's absolute path. The repository's `.codex-plugin/plugin.json`, `.mcp.json`, and skill metadata do not install the executable by themselves.

## Register the stdio server

Codex global configuration:

```bash
codex mcp remove apple-ads 2>/dev/null || true
codex mcp add apple-ads -- "$APPLE_ADS_MCP_BIN" serve --stdio
codex mcp list
```

Claude Code user configuration:

```bash
claude mcp remove apple-ads --scope user 2>/dev/null || true
claude mcp add --scope user apple-ads -- "$APPLE_ADS_MCP_BIN" serve --stdio
claude mcp get apple-ads
```

Restart an already running desktop app, CLI session, or IDE extension after changing MCP configuration. A project-scoped `.mcp.json` may also require explicit workspace/server approval in the client.

## Verify safely

1. Confirm `apple-ads-mcp version` succeeds from the absolute path.
2. Confirm the client lists `apple-ads` as enabled or connected.
3. Start a new client session and call `server_info`.
4. After local credentials are configured, call `auth_check`, `ad_accounts_list`, and `account_health` with an explicit profile and ad account.

If credentials are not configured, run `apple-ads-mcp config init` locally. Never ask the user to paste a private key, access token, or client secret into chat. Do not enable `--allow-writes`, `--allow-deletes`, `allowWrites`, or `allowDeletes` as part of installation or troubleshooting.
