package hooks

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
)

// resolveProjectPath returns a canonical absolute path for the project.
// It falls back to the current working directory when cwd is empty.
func resolveProjectPath(cwd string) string {
	if cwd == "" {
		wd, err := os.Getwd()
		if err == nil {
			return filepath.Clean(wd)
		}
		return "unknown"
	}
	return filepath.Clean(cwd)
}

// resolveSessionID returns sessionID if non-empty, otherwise generates a
// random 8-byte hex string as a fallback.
func resolveSessionID(sessionID string) string {
	if sessionID != "" {
		return sessionID
	}
	b := make([]byte, 8)
	_, err := rand.Read(b)
	if err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}
