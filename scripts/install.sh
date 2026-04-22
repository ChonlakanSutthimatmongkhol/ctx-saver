#!/usr/bin/env bash
# install.sh — build ctx-saver and copy the binary to /usr/local/bin.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
BINARY="ctx-saver"

echo "==> Building ${BINARY}..."
cd "${REPO_ROOT}"
CGO_ENABLED=0 go build -ldflags "-s -w" -o "bin/${BINARY}" "./cmd/${BINARY}"

echo "==> Installing to ${INSTALL_DIR}/${BINARY}..."
cp "bin/${BINARY}" "${INSTALL_DIR}/${BINARY}"
chmod 755 "${INSTALL_DIR}/${BINARY}"

echo ""
echo "Installation complete."
echo ""
echo "Next steps:"
echo ""
echo "  Claude Code:"
echo "    claude mcp add ctx-saver -- ${INSTALL_DIR}/${BINARY}"
echo ""
echo "  VS Code Copilot — add to .vscode/mcp.json:"
echo '    {"servers": {"ctx-saver": {"command": "'"${INSTALL_DIR}/${BINARY}"'"}}}'
echo ""
