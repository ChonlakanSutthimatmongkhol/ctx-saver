package freshness

import (
	"time"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
)

// Resolution holds the outcome of a freshness policy decision.
type Resolution struct {
	Action string // "use_cache" | "auto_refresh" | "ask_user"
	TTL    time.Duration
}

// Resolve decides what to do with a cached output based on its age and the
// configured freshness policy. Returns "use_cache" when freshness is disabled.
func Resolve(sourceKind string, refreshedAt time.Time, cfg config.FreshnessConfig) Resolution {
	if !cfg.Enabled {
		return Resolution{Action: "use_cache"}
	}

	age := time.Since(refreshedAt)
	if age < 0 {
		age = 0
	}

	confirmThreshold := time.Duration(cfg.UserConfirmThresholdSeconds) * time.Second
	if confirmThreshold > 0 && age >= confirmThreshold {
		return Resolution{Action: "ask_user"}
	}

	rule, ttl := ruleFor(sourceKind, cfg)

	if rule.NeverCache {
		return Resolution{Action: "auto_refresh", TTL: ttl}
	}
	if ttl > 0 && age >= ttl {
		if rule.AutoRefresh {
			return Resolution{Action: "auto_refresh", TTL: ttl}
		}
		return Resolution{Action: "use_cache", TTL: ttl}
	}
	return Resolution{Action: "use_cache", TTL: ttl}
}

// ruleFor returns the FreshnessRule and effective TTL for a given source kind.
// Falls back to DefaultMaxAgeSeconds when no specific rule exists.
func ruleFor(sourceKind string, cfg config.FreshnessConfig) (config.FreshnessRule, time.Duration) {
	if cfg.Sources != nil {
		if r, ok := cfg.Sources[sourceKind]; ok {
			return r, time.Duration(r.MaxAgeSeconds) * time.Second
		}
	}
	return config.FreshnessRule{}, time.Duration(cfg.DefaultMaxAgeSeconds) * time.Second
}
