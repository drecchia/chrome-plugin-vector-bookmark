# vector-bookmark-mcp-server

A read-only [MCP](https://modelcontextprotocol.io) server that exposes the local
Vector Bookmark daemon (`vbmd`) browsing-memory API as tools, letting an LLM
agent (Claude Code, Claude Desktop) search and browse what you've read.

It wraps `vbmd`'s GET endpoints only — it never ingests, deletes, or modifies
your memory.

## Tools

| Tool | What it does |
|---|---|
| `vbm_search` | Hybrid BM25 + vector search over your browsing memory (tags/source/confidence filters). |
| `vbm_get_status` | Daemon health + index/queue counters; whether semantic search is active. |
| `vbm_list_tags` | All tags with per-tag page counts. |
| `vbm_list_pages_by_tag` | Pages carrying a given tag, newest first. |
| `vbm_get_history` | Chronological history in a time window + daily activity histogram. |
| `vbm_get_topics` | Top keywords for a time window (interest snapshot). |
| `vbm_get_page` | Whether a URL is in memory, indexed, and its tags. |
| `vbm_export` | Bulk dump of indexed pages/chunks (truncated to a response budget). |

## Prerequisites

The `vbmd` daemon must be running (default `http://127.0.0.1:7532`). Verify:

```bash
curl http://127.0.0.1:7532/healthz   # -> {"ok":true}
```

## Build

```bash
cd mcp
npm install
npm run build      # compiles to dist/index.js
```

## Configuration (environment variables)

| Var | Default | Purpose |
|---|---|---|
| `VBM_HOST` | `127.0.0.1` | Daemon host. |
| `VBM_PORT` | `7532` | Daemon port. |
| `VBM_AUTH_TOKEN` | _(unset)_ | Bearer token. Set this **only if** the daemon was started with `VBM_AUTH_TOKEN`; it must match. |

## Wiring it up

In every example, replace `/ABS/PATH/TO` with the absolute path to this repo's
`mcp` directory. The server is launched as `node .../mcp/dist/index.js`, so run
`npm run build` first.

### Claude Code

Add it from the CLI (the `--` separates the launch command; `--env` injects the
daemon config):

```bash
claude mcp add vector-bookmark \
  --env VBM_PORT=7532 \
  -- node /ABS/PATH/TO/mcp/dist/index.js
```

Scope controls where the entry is written:

- *(default)* `--scope local` — only you, only this project.
- `--scope project` — writes a shared `.mcp.json` at the repo root (commit it so
  teammates get the server too).
- `--scope user` — available across all your projects.

If the daemon uses a token, add `--env VBM_AUTH_TOKEN=<token>` (must match the
daemon's `VBM_AUTH_TOKEN`).

Manage and inspect:

```bash
claude mcp list            # show configured servers + reachability
claude mcp get vector-bookmark
claude mcp remove vector-bookmark
```

Inside a session, `/mcp` lists the server and its tools. Then just ask in natural
language, e.g. *"search my browsing memory for rust async"* — Claude will call
`vbm_search`.

Equivalent manual config — `.mcp.json` (project scope) or `~/.claude.json`
(user scope):

```json
{
  "mcpServers": {
    "vector-bookmark": {
      "command": "node",
      "args": ["/ABS/PATH/TO/mcp/dist/index.js"],
      "env": { "VBM_PORT": "7532" }
    }
  }
}
```

### opencode

opencode reads `mcp` entries from its config — either project-local
`opencode.json` at the repo root, or global `~/.config/opencode/opencode.json`.
Local (stdio) servers use `type: "local"` with `command` as an argv array:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "vector-bookmark": {
      "type": "local",
      "command": ["node", "/ABS/PATH/TO/mcp/dist/index.js"],
      "enabled": true,
      "environment": {
        "VBM_PORT": "7532"
      }
    }
  }
}
```

Add `"VBM_AUTH_TOKEN": "<token>"` to `environment` if the daemon requires it.
Restart opencode after editing the config; the eight `vbm_*` tools then appear to
the model automatically. Set `"enabled": false` to disable without removing the
entry.

## Testing locally

Inspect the tools interactively without a client:

```bash
npx @modelcontextprotocol/inspector node dist/index.js
```

Then call `vbm_get_status` and a `vbm_search` query to confirm the daemon
connection and output shapes.

## Notes

- Transport is **stdio** — the server runs as a subprocess of the client.
- All diagnostics are written to **stderr**; stdout carries the MCP stream only.
- Timestamps in/out are Unix **milliseconds** (project-wide convention). For
  `vbm_get_history` / `vbm_get_topics`, compute `from`/`to` windows client-side.
