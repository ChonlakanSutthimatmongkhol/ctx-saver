// Package store — knowledge-aggregation data types.
package store

import "time"

// KnowledgeData holds aggregated project statistics for knowledge generation.
type KnowledgeData struct {
	SessionCount  int
	OutputCount   int
	DecisionCount int

	TopFiles      []FileFreq    // most-read files with hash stability
	TopCommands   []CommandFreq // most-run commands with avg size
	Sequences     []CmdSequence // co-occurrence patterns
	KeyDecisions  []DecisionOut // high-importance decisions
	RecentCommits []string      // last 7 git commits, "--oneline" format
}

// FileFreq is one row from the most-read-files aggregation.
type FileFreq struct {
	Path        string
	ReadCount   int
	HashStable  bool
	LastChanged time.Time
}

// CommandFreq is one row from the most-run-commands aggregation.
type CommandFreq struct {
	Command  string
	RunCount int
	AvgBytes int64
}

// CmdSequence is a pair of commands that frequently run within 5 minutes of each other.
type CmdSequence struct {
	First   string
	Second  string
	Percent float64 // percentage of First runs that were followed by Second
}

// DecisionOut is a lightweight view of a high-importance decision note.
type DecisionOut struct {
	Text      string
	Tags      []string
	Task      string
	CreatedAt time.Time
}
