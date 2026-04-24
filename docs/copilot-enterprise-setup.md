# Copilot Enterprise Setup Guide

This guide covers installing and verifying **ctx-saver** with GitHub Copilot Enterprise (Agent mode in VS Code).

---

## 1. Prerequisites

| Requirement | Notes |
|-------------|-------|
| **Copilot Enterprise plan** | Individual / Teams plans do not support MCP servers |
| **Admin MCP policy enabled** | Ask your IT/Security admin to enable "MCP servers in Copilot" in the GitHub Enterprise settings |
| **VS Code 1.99+** | Required for MCP server support in Copilot Agent mode |
| **Go 1.22+** | For `go install` or building from source |
| **jq** | Required only if using `install-hooks.sh` (not needed for `ctx-saver init`) |

---

## 2. Installation

### Step 1 — Install the ctx-saver binary

**Option A — via `go install` (recommended; no repo clone required):**

```bash
go install github.com/ChonlakanSutthimatmongkhol/ctx-saver/cmd/ctx-saver@latest
```

Make sure `$(go env GOPATH)/bin` is in your `PATH` (add to `~/.zshrc` or `~/.bashrc`):

```bash
export PATH="$PATH:$(go env GOPATH)/bin"
```

**Option B — build from source:**

```bash
git clone https://github.com/ChonlakanSutthimatmongkhol/ctx-saver
cd ctx-saver
make install   # → /usr/local/bin/ctx-saver
```

### Step 2 — Register the MCP server in VS Code

```bash
# From inside your project directory:
ctx-saver init copilot
```

> Works with both `go install` and source-build users. If you cloned the repo, `./scripts/install-hooks.sh copilot` also works (requires `jq`).

This creates (or updates) `.vscode/mcp.json` in your project with:

```json
{
  "servers": {
    "ctx-saver": {
      "command": "/usr/local/bin/ctx-saver"
    }
  }
}
```

> **Trust the workspace folder** — VS Code requires you to trust the folder before MCP servers are activated. Click "Trust" when prompted, or go to **File → Open Folder** and accept the trust dialog.

### Step 3 — Install Copilot instruction rules

```bash
# From your repo root:
ctx-saver init copilot-instructions
git add .github/copilot-instructions.md
git commit -m "chore: add ctx-saver Copilot instructions"
```

> If you cloned the repo, `./scripts/install-hooks.sh copilot-instructions` also works (no `jq` required).

Committing the file shares the rules with your entire team automatically.

### Step 4 — Reload VS Code

Restart VS Code (or run **Developer: Reload Window** from the Command Palette) so the MCP server is picked up.

---

## 3. Verification

1. Open a Copilot Chat pane in Agent mode (click the **robot icon** or press the agent toggle).
2. Type: `Call ctx_session_init and show me the output`
3. Copilot should call `ctx_session_init` and return a response that includes:
   - `project_rules` — the tool routing rules
   - `cached_outputs` — what is already stored (will be empty on first run)
   - `next_action_hint` — recommended next step

If you see the response, ctx-saver is working correctly.

---

## 4. Expected behaviour

| Turn | What should happen |
|------|--------------------|
| **First turn** | Copilot calls `ctx_session_init` (prompted by `.github/copilot-instructions.md` + tool description) |
| **Build / test commands** | Copilot uses `ctx_execute` → response is a compact summary + `output_id` |
| **File reads (> 50 lines)** | Copilot uses `ctx_read_file` instead of native `readFile` |
| **Follow-up questions** | Copilot calls `ctx_search` or `ctx_get_section` on the cached output |
| **Every ~20 turns** | Call `ctx_stats` — `adherence_score` should be > 80% |

---

## 5. Troubleshooting

### "Copilot doesn't see ctx-saver tools"

- Confirm the admin policy is enabled: GitHub Enterprise → Settings → Copilot → **Enable MCP servers**.
- Verify `.vscode/mcp.json` exists and contains `"ctx-saver"`.
- Reload VS Code.
- Check that the binary path in `mcp.json` is correct: `which ctx-saver`.

### "Copilot ignores ctx_execute and uses runInTerminal"

- Verify `.github/copilot-instructions.md` is present and committed.
- Confirm the file is not excluded by your enterprise content exclusion policy (see below).
- Re-read the rules: ask Copilot "Call ctx_session_init" to refresh its context.
- Check `ctx_stats` → `adherence_score`. If it is below 50%, the model is consistently ignoring instructions.

### "Low adherence_score in ctx_stats"

1. Call `ctx_session_init` — refresh rules at the start of each new session.
2. Look at `native_shell_count` and `native_read_count` in `ctx_stats` to understand which tools are being over-used.
3. Verify `.github/copilot-instructions.md` is present and up to date (`ctx-saver init copilot-instructions`).
4. Check whether the tool descriptions in `.vscode/mcp.json` are loading correctly — in VS Code, go to **Output → Copilot** and look for MCP server errors.

### "Content exclusion blocks my files"

Copilot Enterprise content exclusion policies may prevent Copilot from reading certain files (e.g., `.env`, secrets directories, large binary files). If a file is blocked:

- The `ctx_read_file` tool will return an error or empty output.
- Contact your IT/Security admin to review the exclusion policy for the affected path.
- As a workaround, use `ctx_execute` with `cat <file>` for files outside the exclusion scope.

---

## 6. Known limitations

| Limitation | Impact | Workaround |
|------------|--------|------------|
| **Cannot disable native tools** | Copilot can still use `runInTerminal` / `readFile` | Use `copilot-instructions.md` + verbose tool descriptions to preference ctx-saver |
| **MCP allowlist (VS Code 2026+)** | Future VS Code versions may require admin approval for each MCP server | Submit ctx-saver to your company's MCP allowlist (see §7) |
| **No PreToolUse / PostToolUse hooks** | ctx-saver cannot intercept native tool calls in Copilot | Use `ctx_stats adherence_score` to measure and `ctx_session_init` to nudge |
| **Folder Trust required** | Each workspace must be explicitly trusted | One-time step per workspace |
| **User-level MCP only** | ctx-saver is in `.vscode/mcp.json`, not the enterprise registry | Submit to IT for enterprise-level registration (see §7) |

---

## 7. Engaging IT / Security

If your company requires MCP servers to be approved before use, here is a checklist and a message template.

### IT checklist

- [ ] Is the "MCP servers in Copilot" policy enabled in GitHub Enterprise?
- [ ] Is the ctx-saver binary reviewed and approved?
- [ ] Is the binary hash verified against the published release?
- [ ] Is the data directory (default: `<project>/.ctx-saver/`) within acceptable storage policy?
- [ ] Is the ctx-saver server added to the enterprise MCP allowlist?

### Message template to send to your admin

> Subject: Request to approve ctx-saver as an MCP server for Copilot Enterprise
>
> Hi [Admin name],
>
> I would like to use the **ctx-saver** MCP server with GitHub Copilot Enterprise to reduce context window exhaustion during AI-assisted development sessions.
>
> **What it does:** ctx-saver is a local MCP server that captures large command outputs (build logs, test results, API spec fetches) in a local SQLite database and returns compact summaries instead of raw text. This prevents Copilot's context window from filling up after a few commands.
>
> **Privacy:** ctx-saver makes no network calls. All data is stored locally in `<project>/.ctx-saver/` (SQLite, permissions `0600`). No data leaves the machine.
>
> **Source:** https://github.com/ChonlakanSutthimatmongkhol/ctx-saver
>
> **Request:** Please enable the MCP server policy and/or add ctx-saver to the enterprise MCP allowlist so I can use it without Folder Trust limitations.
>
> Let me know if you need any additional information or a security review.
>
> Thanks,
> [Your name]

---

## Related links

- [ctx-saver README](../README.md)
- [Tool Reference](skills/ctx-saver/references/tools.md)
- [Copilot instructions template](../configs/copilot-enterprise/copilot-instructions.md)
