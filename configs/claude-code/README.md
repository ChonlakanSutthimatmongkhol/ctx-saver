# Claude Code — ctx-saver MCP Setup

## One-line install

```bash
make install
claude mcp add ctx-saver -- /usr/local/bin/ctx-saver
```

## Verify

```bash
claude mcp list
# Should show ctx-saver with 5 tools:
#   ctx_execute, ctx_read_file, ctx_search, ctx_get_full, ctx_stats, ctx_note ...
```

## Usage examples inside Claude Code

```
# Instead of: run this jira CLI command and show me the output
# Use:
ctx_execute(language="shell", code="jira issue list --project MYPROJ", intent="find open issues")

# Search within the stored output:
ctx_search(queries=["error", "timeout"], output_id="out_20260422_a3f8")

# Get specific lines from a large log:
ctx_get_full(output_id="out_20260422_a3f8", line_range=[100, 150])
```

## Per-project config (optional)

Create `.ctx-saver.yaml` in your project root to override defaults:

```yaml
sandbox:
  timeout_seconds: 120

summary:
  head_lines: 30
  tail_lines: 10
  auto_index_threshold_bytes: 10240

deny_commands:
  - "rm -rf /"
  - "sudo *"
  - "dd if=*"
```
