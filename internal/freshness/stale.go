package freshness

import (
	"time"
)

const (
	LevelFresh    = "fresh"
	LevelAging    = "aging"
	LevelStale    = "stale"
	LevelCritical = "critical"
)

// ClassifyStaleLevel returns the staleness level for an item.
// If ttl > 0 and age ≤ ttl, the item is always "fresh".
func ClassifyStaleLevel(age, ttl time.Duration) string {
	if ttl > 0 && age <= ttl {
		return LevelFresh
	}
	switch {
	case age < time.Hour:
		return LevelFresh
	case age < 24*time.Hour:
		return LevelAging
	case age < 7*24*time.Hour:
		return LevelStale
	default:
		return LevelCritical
	}
}

// RefreshHint returns guidance text appropriate for the stale level.
// Returns "" for fresh/aging items.
func RefreshHint(level, sourceKind string) string {
	switch level {
	case LevelFresh, LevelAging:
		return ""
	case LevelStale:
		return "This output may be outdated. Consider calling ctx_execute to refresh " + sourceKind + " data."
	case LevelCritical:
		return "🛑 STOP — this output is over 7 days old. Do NOT use it for decisions. You must refresh via ctx_execute or confirm with the user before proceeding."
	default:
		return ""
	}
}

// FreshnessInfo is the per-output freshness metadata included in handler responses.
type FreshnessInfo struct {
	SourceKind  string `json:"source_kind"`
	CachedAt    string `json:"cached_at"`              // RFC3339
	AgeSeconds  int64  `json:"age_seconds"`
	AgeHuman    string `json:"age_human"`
	StaleLevel  string `json:"stale_level"`
	RefreshHint string `json:"refresh_hint,omitempty"`
}

// NewFreshnessInfo builds a FreshnessInfo for an output given its source kind,
// the time it was last refreshed, its TTL in seconds, and the current time.
func NewFreshnessInfo(sourceKind string, refreshedAt time.Time, ttlSeconds int, now time.Time) FreshnessInfo {
	age := now.Sub(refreshedAt)
	if age < 0 {
		age = 0 // clock skew guard
	}
	ttl := time.Duration(ttlSeconds) * time.Second
	level := ClassifyStaleLevel(age, ttl)
	return FreshnessInfo{
		SourceKind:  sourceKind,
		CachedAt:    refreshedAt.UTC().Format(time.RFC3339),
		AgeSeconds:  int64(age.Seconds()),
		AgeHuman:    HumanAge(age),
		StaleLevel:  level,
		RefreshHint: RefreshHint(level, sourceKind),
	}
}
