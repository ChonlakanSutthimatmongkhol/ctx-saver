# VS Code Copilot — ctx-saver MCP Setup

## Install binary

```bash
make install   # builds and copies to /usr/local/bin/ctx-saver
```

## Configure VS Code

Add to **`.vscode/mcp.json`** in your project root (note: VS Code uses `servers`, not `mcpServers`):

```json
{
  "servers": {
    "ctx-saver": {
      "command": "/usr/local/bin/ctx-saver"
    }
  }
}
```

Or add globally in your VS Code `settings.json`:

```json
{
  "mcp.servers": {
    "ctx-saver": {
      "command": "/usr/local/bin/ctx-saver"
    }
  }
}
```

## Verify

Open the VS Code Command Palette → **MCP: List Servers**.  
You should see `ctx-saver` with status `running` and 10 tools listed.

VS Code Copilot may defer MCP tools until they are loaded. In a new chat, ask the
agent to run:

```text
tool_search("ctx_session_init ctx_execute ctx_read_file ctx_stats ctx_note")
```

After the tools are loaded, the first ctx-saver call should be `ctx_session_init`.

## Available tools

| Tool | Description |
|------|-------------|
| `ctx_session_init` | Start a session with project rules, recent activity, cached outputs, and saved decisions |
| `ctx_execute` | Run shell/python/go/node code; large output is stored + summarised |
| `ctx_read_file` | Read a file (optionally through a processing script) |
| `ctx_search` | Full-text search across stored outputs |
| `ctx_get_full` | Retrieve complete output or a line range |
| `ctx_outline` | Extract a table of contents from stored Markdown-like output |
| `ctx_get_section` | Retrieve a named section from a stored output |
| `ctx_stats` | Report adherence stats or list stored outputs with `view="outputs"` |
| `ctx_note` | Save/list durable decisions, or create task handoff notes |
| `ctx_purge` | Clear cached outputs for the current project after confirmation |
