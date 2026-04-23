#!/usr/bin/env bash
# uninstall-hooks.sh — remove ctx-saver hooks from Claude Code or VS Code Copilot.
#
# Usage:
#   ./scripts/uninstall-hooks.sh claude    # → removes hooks from ~/.claude/settings.json
#   ./scripts/uninstall-hooks.sh copilot  # → no-op for hooks (schema does not support hooks)
#
# Requirements: jq  (brew install jq / apt-get install jq)
set -euo pipefail

die() { echo "error: $*" >&2; exit 1; }
require_cmd() {
    command -v "$1" >/dev/null 2>&1 || die "'$1' is required but not installed. $2"
}

require_cmd jq "Install with: brew install jq"

remove_hooks() {
    local target="$1"
    if [[ ! -f "$target" ]]; then
        echo "  nothing to do — $target does not exist"
        return
    fi
    # Backup before modifying.
    cp "$target" "${target}.bak.$(date +%Y%m%d%H%M%S)"
    local tmp
    tmp="$(mktemp)"
    jq 'del(.hooks)' "$target" > "$tmp" \
        || die "jq failed — is $target valid JSON?"
    mv "$tmp" "$target"
    echo "  removed hooks from $target"
}

PLATFORM="${1:-}"
case "$PLATFORM" in
  claude)
    TARGET="${HOME}/.claude/settings.json"
    echo "Removing ctx-saver hooks from Claude Code → $TARGET"
    remove_hooks "$TARGET"
    echo "Done."
    ;;
  copilot)
    echo "VS Code Copilot mcp.json does not support a top-level 'hooks' key."
    echo "Nothing to remove for hooks on Copilot."
    ;;
  *)
    echo "Usage: $0 <platform>"
    echo ""
    echo "Platforms:"
    echo "  claude   — Remove hooks from ~/.claude/settings.json"
    echo "  copilot  — Remove hooks from .vscode/mcp.json (current directory)"
    exit 1
    ;;
esac
