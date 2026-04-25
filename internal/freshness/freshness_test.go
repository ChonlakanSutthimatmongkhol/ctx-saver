package freshness_test

import (
	"testing"
	"time"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/freshness"
)

func TestHumanAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "just now"},
		{30 * time.Second, "just now"},
		{59 * time.Second, "just now"},
		{1 * time.Minute, "1m ago"},
		{5 * time.Minute, "5m ago"},
		{59 * time.Minute, "59m ago"},
		{1 * time.Hour, "1h ago"},
		{3 * time.Hour, "3h ago"},
		{23 * time.Hour, "23h ago"},
		{24 * time.Hour, "1d ago"},
		{2 * 24 * time.Hour, "2d ago"},
		{29 * 24 * time.Hour, "29d ago"},
		{30 * 24 * time.Hour, "1mo ago"},
		{90 * 24 * time.Hour, "3mo ago"},
	}
	for _, c := range cases {
		got := freshness.HumanAge(c.d)
		if got != c.want {
			t.Errorf("HumanAge(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestClassifyStaleLevel(t *testing.T) {
	cases := []struct {
		age  time.Duration
		ttl  time.Duration
		want string
	}{
		// fresh: under 1h, no TTL
		{30 * time.Minute, 0, freshness.LevelFresh},
		// aging: 1h–24h
		{2 * time.Hour, 0, freshness.LevelAging},
		// stale: 1d–7d
		{3 * 24 * time.Hour, 0, freshness.LevelStale},
		// critical: >7d
		{8 * 24 * time.Hour, 0, freshness.LevelCritical},
		// TTL override: age > 1h but within TTL → fresh
		{2 * time.Hour, 4 * time.Hour, freshness.LevelFresh},
		// TTL expired: age > TTL → normal thresholds apply
		{6 * time.Hour, 4 * time.Hour, freshness.LevelAging},
		// TTL=0 with critical age
		{10 * 24 * time.Hour, 0, freshness.LevelCritical},
	}
	for _, c := range cases {
		got := freshness.ClassifyStaleLevel(c.age, c.ttl)
		if got != c.want {
			t.Errorf("ClassifyStaleLevel(age=%v, ttl=%v) = %q, want %q", c.age, c.ttl, got, c.want)
		}
	}
}

func TestRefreshHint(t *testing.T) {
	if freshness.RefreshHint(freshness.LevelFresh, "shell:acli") != "" {
		t.Error("fresh should return empty hint")
	}
	if freshness.RefreshHint(freshness.LevelAging, "shell:kubectl") != "" {
		t.Error("aging should return empty hint")
	}
	staleHint := freshness.RefreshHint(freshness.LevelStale, "shell:kubectl")
	if staleHint == "" {
		t.Error("stale should return non-empty hint")
	}
	criticalHint := freshness.RefreshHint(freshness.LevelCritical, "shell:acli")
	if criticalHint == "" {
		t.Error("critical should return non-empty hint")
	}
	// critical must contain stop signal
	if len(criticalHint) < 10 {
		t.Errorf("critical hint too short: %q", criticalHint)
	}
}

func TestNewFreshnessInfo(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	refreshedAt := now.Add(-3 * time.Hour)

	fi := freshness.NewFreshnessInfo("shell:acli", refreshedAt, 0, now)

	if fi.SourceKind != "shell:acli" {
		t.Errorf("SourceKind = %q, want %q", fi.SourceKind, "shell:acli")
	}
	if fi.AgeSeconds != 3*3600 {
		t.Errorf("AgeSeconds = %d, want %d", fi.AgeSeconds, 3*3600)
	}
	if fi.AgeHuman != "3h ago" {
		t.Errorf("AgeHuman = %q, want %q", fi.AgeHuman, "3h ago")
	}
	if fi.StaleLevel != freshness.LevelAging {
		t.Errorf("StaleLevel = %q, want %q", fi.StaleLevel, freshness.LevelAging)
	}
	if fi.RefreshHint != "" {
		t.Errorf("RefreshHint should be empty for aging, got %q", fi.RefreshHint)
	}
}

func TestNewFreshnessInfo_ClockSkew(t *testing.T) {
	now := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	// refreshedAt in the future — clock skew
	refreshedAt := now.Add(5 * time.Minute)

	fi := freshness.NewFreshnessInfo("shell:git", refreshedAt, 0, now)

	if fi.AgeSeconds != 0 {
		t.Errorf("clock skew: AgeSeconds = %d, want 0", fi.AgeSeconds)
	}
	if fi.StaleLevel != freshness.LevelFresh {
		t.Errorf("clock skew: StaleLevel = %q, want fresh", fi.StaleLevel)
	}
}
