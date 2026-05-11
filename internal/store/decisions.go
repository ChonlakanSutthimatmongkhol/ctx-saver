package store

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// Decision is a small, durable note recording an architectural decision
// or rationale that should survive context compaction.
type Decision struct {
	ID          int64
	DecisionID  string // "dec_<unix_micro>_<random4hex>"
	SessionID   string // empty if recorded outside a tool call
	ProjectPath string
	Text        string
	Tags        []string
	LinksTo     []string // related output_ids
	Importance  string   // "low" | "normal" | "high"
	Task        string   // optional work item scope for handoff/resume workflows
	CreatedAt   time.Time
}

// Importance levels.
const (
	ImportanceLow    = "low"
	ImportanceNormal = "normal"
	ImportanceHigh   = "high"
)

// ListDecisionsOptions are parameters for ListDecisions.
type ListDecisionsOptions struct {
	ProjectPath   string
	SessionID     string   // for scope="session"
	Scope         string   // "session" | "today" | "7d" | "all"
	MinImportance string   // "low" | "normal" | "high"
	Tags          []string // OR-match
	Limit         int      // 0 = default (50), max 200
	Task          *string  // nil = no filter; "" = unscoped only; value = exact task
}

// newDecisionID generates a unique ID for a decision.
// Format: "dec_<unix_micro>_<4hex>"
func newDecisionID() string {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("dec_%d_0000", time.Now().UnixMicro())
	}
	return fmt.Sprintf("dec_%d_%s", time.Now().UnixMicro(), hex.EncodeToString(b))
}
