// Package store defines the storage interface and data types for ctx-saver outputs.
package store

import (
	"context"
	"time"
)

// Output is a complete record of one command execution stored in SQLite.
type Output struct {
	OutputID    string
	Command     string // sanitised for display (no secrets)
	Intent      string
	FullOutput  string
	SizeBytes   int64
	LineCount   int
	ExitCode    int
	DurationMs  int64
	CreatedAt   time.Time
	ProjectPath string
}

// OutputMeta is a lightweight summary used by ctx_list_outputs.
type OutputMeta struct {
	OutputID  string
	Command   string
	CreatedAt time.Time
	SizeBytes int64
	LineCount int
}

// Match is a single full-text search hit.
type Match struct {
	OutputID string
	Line     int
	Snippet  string
	Score    float64
}

// SessionEvent records one hook lifecycle event for session tracking.
type SessionEvent struct {
	ID          int64
	SessionID   string
	ProjectPath string
	EventType   string // "pretooluse" | "posttooluse" | "sessionstart"
	ToolName    string
	ToolInput   string // JSON string
	ToolOutput  string // JSON string or plain text
	Summary     string // human-readable one-liner
	CreatedAt   time.Time
}

// Stats holds aggregate statistics for stored outputs and hook activity.
type Stats struct {
	OutputsStored    int
	RawBytes         int64
	LargestBytes     int64
	AvgDurationMs    int64
	TopCommands      []CommandStat
	LargestOutputs   []*OutputMeta
	DangerousBlocked int
	RedirectedToMCP  int
	EventsCaptured   int
}

// CommandStat is the aggregate for one command bucket.
type CommandStat struct {
	Command    string
	Count      int
	TotalBytes int64
}

// Store is the repository interface for stored outputs.
// Implementations must be safe for concurrent use.
type Store interface {
	// Save persists an Output and indexes it for full-text search.
	Save(ctx context.Context, output *Output) error

	// Get retrieves a single Output by ID.
	Get(ctx context.Context, id string) (*Output, error)

	// List returns metadata for outputs belonging to projectPath, newest first.
	List(ctx context.Context, projectPath string, limit int) ([]*OutputMeta, error)

	// Search runs a single FTS5 query and returns up to maxResults matches.
	// If outputID is non-empty the search is limited to that output.
	Search(ctx context.Context, query, outputID string, maxResults int) ([]*Match, error)

	// Cleanup deletes outputs older than retentionDays for projectPath.
	Cleanup(ctx context.Context, projectPath string, retentionDays int) error

	// SaveSessionEvent persists one hook lifecycle event.
	SaveSessionEvent(ctx context.Context, event *SessionEvent) error

	// ListSessionEvents returns recent events for a session (newest last).
	ListSessionEvents(ctx context.Context, sessionID string, limit int) ([]*SessionEvent, error)

	// ListProjectSessionEvents returns recent events across all sessions for
	// a project (newest last), useful for SessionStart context restoration.
	ListProjectSessionEvents(ctx context.Context, projectPath string, limit int) ([]*SessionEvent, error)

	// GetStats returns aggregate statistics for outputs and session events
	// belonging to projectPath created at or after since.
	// A zero since means no time filter (all time).
	GetStats(ctx context.Context, projectPath string, since time.Time) (*Stats, error)

	// Close releases database resources.
	Close() error
}
