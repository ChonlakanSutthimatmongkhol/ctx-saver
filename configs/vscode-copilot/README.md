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
You should see `ctx-saver` with status `running` and 5 tools listed.

## Available tools

| Tool | Description |
|------|-------------|
| `ctx_execute` | Run shell/python/go/node code; large output is stored + summarised |
| `ctx_read_file` | Read a file (optionally through a processing script) |
| `ctx_search` | Full-text search across stored outputs |
| `ctx_list_outputs` | List all stored outputs for this project |
| `ctx_get_full` | Retrieve complete output or a line range |
