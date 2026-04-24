// Package configs exposes embedded configuration templates for ctx-saver init.
package configs

import _ "embed"

// CopilotInstructionsTemplate is the .github/copilot-instructions.md template
// that teaches Copilot Enterprise to prefer ctx-saver tools.
//
//go:embed copilot-enterprise/copilot-instructions.md
var CopilotInstructionsTemplate string
