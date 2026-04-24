#!/usr/bin/env bash
# install-hooks.sh — install ctx-saver hooks into Claude Code or VS Code Copilot.
#
# Usage:
#   ./scripts/install-hooks.sh claude                # → ~/.claude/settings.json
#   ./scripts/install-hooks.sh copilot              # → .vscode/mcp.json (server only; current dir)
#   ./scripts/install-hooks.sh copilot-instructions # → .github/copilot-instructions.md (current dir)
#
# Requirements: jq  (brew install jq / apt-get install jq) — NOT needed for copilot-instructions
set -euo pipefail

# ── helpers ────────────────────────────────────────────────────────────────

die() { echo "error: $*" >&2; exit 1; }

# ── copilot-instructions (no jq / binary required) ─────────────────────────

if [[ "${1:-}" == "copilot-instructions" ]]; then
    REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
    TEMPLATE="$REPO_ROOT/configs/copilot-enterprise/copilot-instructions.md"
    TARGET_DIR="$(pwd)/.github"
    TARGET_FILE="$TARGET_DIR/copilot-instructions.md"

    [[ -f "$TEMPLATE" ]] || die "template not found: $TEMPLATE"

    mkdir -p "$TARGET_DIR"

    if [[ -f "$TARGET_FILE" ]]; then
        echo "  $TARGET_FILE already exists."
        read -r -p "  Append ctx-saver rules? [y/N] " confirm || confirm=""
        if [[ "$confirm" != "y" && "$confirm" != "Y" ]]; then
            echo "  Skipped."
            exit 0
        fi
        { echo ""; echo "---"; echo ""; cat "$TEMPLATE"; } >> "$TARGET_FILE"
        echo "  Appended ctx-saver rules to $TARGET_FILE"
    else
        cp "$TEMPLATE" "$TARGET_FILE"
        echo "  Created $TARGET_FILE"
    fi

    echo "Done. Commit .github/copilot-instructions.md to share rules with your team."
    exit 0
fi

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || die "'$1' is required but not installed. $2"
}

# Atomically merge $1 (JSON patch) into $2 (target file).
# Creates $2 if missing; backs up existing file to $2.bak.<timestamp>.
merge_json() {
    local patch="$1"
    local target="$2"
    local tmp
    tmp="$(mktemp)"

    # Start from existing file or empty object.
    local base="{}"
    if [[ -f "$target" ]]; then
        base="$(cat "$target")"
    fi

    # Deep-merge: patch keys win over base keys.
    jq -s '.[0] * .[1]' <(echo "$base") <(echo "$patch") > "$tmp" \
        || die "jq merge failed — is the existing file valid JSON?"

    # Validate before writing.
    jq . "$tmp" > /dev/null || die "merged output is not valid JSON"

    # Backup + atomic replace.
    if [[ -f "$target" ]]; then
        cp "$target" "${target}.bak.$(date +%Y%m%d%H%M%S)"
    fi
    mkdir -p "$(dirname "$target")"
    mv "$tmp" "$target"
    echo "  written: $target"
}

# ── locate binary ──────────────────────────────────────────────────────────

require_cmd jq "Install with: brew install jq"

CTX_BIN="${CTX_SAVER_BIN:-}"
if [[ -z "$CTX_BIN" ]]; then
    CTX_BIN="$(command -v ctx-saver 2>/dev/null || true)"
fi
if [[ -z "$CTX_BIN" ]]; then
    die "ctx-saver binary not found in PATH.\n       Run 'make install' first, or set CTX_SAVER_BIN=/path/to/ctx-saver."
fi
echo "Using binary: $CTX_BIN"

# ── platform dispatch ──────────────────────────────────────────────────────

PLATFORM="${1:-}"
case "$PLATFORM" in

  claude)
    TARGET="${HOME}/.claude/settings.json"
    PATCH=$(jq -n --arg bin "$CTX_BIN" '{
      "hooks": {
        "PreToolUse": [
          {
            "matcher": "Bash|Shell",
            "hooks": [{"type": "command", "command": ($bin + " hook pretooluse")}]
          }
        ],
        "PostToolUse": [
          {
            "matcher": ".*",
            "hooks": [{"type": "command", "command": ($bin + " hook posttooluse")}]
          }
        ],
        "SessionStart": [
          {
            "hooks": [{"type": "command", "command": ($bin + " hook sessionstart")}]
          }
        ]
      }
    }')
    echo "Installing ctx-saver hooks for Claude Code → $TARGET"
    merge_json "$PATCH" "$TARGET"
    echo "Done. Restart Claude Code to activate the hooks."
    ;;

  copilot)
    TARGET="${PWD}/.vscode/mcp.json"
    PATCH=$(jq -n --arg bin "$CTX_BIN" '{
      "servers": {
        "ctx-saver": {"command": $bin}
      }
    }')
    echo "Installing ctx-saver MCP server for VS Code Copilot → $TARGET"
    merge_json "$PATCH" "$TARGET"
    echo "Done. Reload VS Code to activate the MCP server."
    echo "Note: VS Code mcp.json schema currently does not allow a top-level 'hooks' key."
    echo "      Use Claude Code for PreToolUse/PostToolUse/SessionStart hook execution."
    ;;

  *)
    echo "Usage: $0 <platform>"
    echo ""
    echo "Platforms:"
    echo "  claude                — Install hooks into ~/.claude/settings.json"
    echo "  copilot              — Install MCP server into .vscode/mcp.json (current directory)"
    echo "  copilot-instructions — Install .github/copilot-instructions.md (current directory)"
    echo ""
    echo "Environment:"
    echo "  CTX_SAVER_BIN  Override binary path (default: \$(command -v ctx-saver))"
    echo "  Note: copilot-instructions does not require jq or ctx-saver binary."
    exit 1
    ;;
esac
