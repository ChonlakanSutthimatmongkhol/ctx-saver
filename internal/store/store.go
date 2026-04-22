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

	// Close releases database resources.
	Close() error
}
