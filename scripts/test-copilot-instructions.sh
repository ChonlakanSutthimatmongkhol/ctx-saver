#!/usr/bin/env bash
# test-copilot-instructions.sh — smoke test for the copilot-instructions install option.
# Runs in a temp directory; no side effects on the real repo.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SCRIPT="$REPO_ROOT/scripts/install-hooks.sh"

# ── test 1: fresh install ───────────────────────────────────────────────────

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

(
    cd "$TMPDIR"
    bash "$SCRIPT" copilot-instructions < /dev/null
)

TARGET="$TMPDIR/.github/copilot-instructions.md"
[[ -f "$TARGET" ]] || { echo "FAIL: file not created at $TARGET"; exit 1; }
grep -q "ctx_execute" "$TARGET"      || { echo "FAIL: ctx_execute not in file"; exit 1; }
grep -q "ctx_read_file" "$TARGET"    || { echo "FAIL: ctx_read_file not in file"; exit 1; }
grep -q "ctx_session_init" "$TARGET" || { echo "FAIL: ctx_session_init not in file"; exit 1; }
echo "PASS: fresh install"

# ── test 2: skip when user declines append ──────────────────────────────────

ORIGINAL_LINES="$(wc -l < "$TARGET")"

# Simulate user pressing Enter (empty input = decline)
(
    cd "$TMPDIR"
    echo "" | bash "$SCRIPT" copilot-instructions
)

AFTER_LINES="$(wc -l < "$TARGET")"
[[ "$ORIGINAL_LINES" -eq "$AFTER_LINES" ]] || { echo "FAIL: file was modified on decline (was $ORIGINAL_LINES, now $AFTER_LINES lines)"; exit 1; }
echo "PASS: skip on decline"

# ── test 3: append when user accepts ────────────────────────────────────────

(
    cd "$TMPDIR"
    echo "y" | bash "$SCRIPT" copilot-instructions
)

APPENDED_LINES="$(wc -l < "$TARGET")"
[[ "$APPENDED_LINES" -gt "$ORIGINAL_LINES" ]] || { echo "FAIL: file not extended on append (still $APPENDED_LINES lines)"; exit 1; }
echo "PASS: append on confirm"

echo ""
echo "All tests passed."
