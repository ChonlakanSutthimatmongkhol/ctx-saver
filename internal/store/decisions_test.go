package store_test

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

func newDecisionStore(t *testing.T) *store.SQLiteStore {
	t.Helper()
	st, err := store.NewSQLiteStore(t.TempDir(), "/proj")
	require.NoError(t, err)
	t.Cleanup(func() { st.Close() })
	return st
}

func TestSaveDecision_Basic(t *testing.T) {
	st := newDecisionStore(t)
	d := &store.Decision{
		ProjectPath: "/proj",
		Text:        "chose X because Y",
		Tags:        []string{"arch"},
		Importance:  store.ImportanceHigh,
	}
	require.NoError(t, st.SaveDecision(context.Background(), d))
	assert.NotEmpty(t, d.DecisionID)
	assert.False(t, d.CreatedAt.IsZero())

	got, err := st.GetDecision(context.Background(), d.DecisionID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, d.Text, got.Text)
	assert.Equal(t, d.Importance, got.Importance)
	assert.Equal(t, d.Tags, got.Tags)
}

func TestSaveDecision_AutoFillsDefaults(t *testing.T) {
	st := newDecisionStore(t)
	d := &store.Decision{
		ProjectPath: "/proj",
		Text:        "some note",
	}
	require.NoError(t, st.SaveDecision(context.Background(), d))
	assert.Equal(t, store.ImportanceNormal, d.Importance)
	assert.NotEmpty(t, d.DecisionID)
	assert.False(t, d.CreatedAt.IsZero())
}

func TestListDecisions_ScopeAll(t *testing.T) {
	st := newDecisionStore(t)
	for i := 0; i < 3; i++ {
		require.NoError(t, st.SaveDecision(context.Background(), &store.Decision{
			ProjectPath: "/proj",
			Text:        "decision",
			Importance:  store.ImportanceNormal,
		}))
	}
	got, err := st.ListDecisions(context.Background(), store.ListDecisionsOptions{
		ProjectPath: "/proj",
		Scope:       "all",
	})
	require.NoError(t, err)
	assert.Len(t, got, 3)
}

func TestListDecisions_ScopeSession(t *testing.T) {
	st := newDecisionStore(t)
	require.NoError(t, st.SaveDecision(context.Background(), &store.Decision{
		ProjectPath: "/proj", SessionID: "sess-A", Text: "A1", Importance: store.ImportanceNormal,
	}))
	require.NoError(t, st.SaveDecision(context.Background(), &store.Decision{
		ProjectPath: "/proj", SessionID: "sess-A", Text: "A2", Importance: store.ImportanceNormal,
	}))
	require.NoError(t, st.SaveDecision(context.Background(), &store.Decision{
		ProjectPath: "/proj", SessionID: "sess-B", Text: "B1", Importance: store.ImportanceNormal,
	}))

	got, err := st.ListDecisions(context.Background(), store.ListDecisionsOptions{
		ProjectPath: "/proj", SessionID: "sess-A", Scope: "session",
	})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestListDecisions_ScopeToday(t *testing.T) {
	st := newDecisionStore(t)
	// Save one now (today).
	require.NoError(t, st.SaveDecision(context.Background(), &store.Decision{
		ProjectPath: "/proj", Text: "today", Importance: store.ImportanceNormal,
	}))
	// Save one with a past timestamp (yesterday) by using a pre-set CreatedAt.
	yesterday := &store.Decision{
		ProjectPath: "/proj",
		Text:        "yesterday",
		Importance:  store.ImportanceNormal,
		CreatedAt:   time.Now().Add(-25 * time.Hour),
	}
	require.NoError(t, st.SaveDecision(context.Background(), yesterday))

	got, err := st.ListDecisions(context.Background(), store.ListDecisionsOptions{
		ProjectPath: "/proj", Scope: "today",
	})
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "today", got[0].Text)
}

func TestListDecisions_ScopeSevenDays(t *testing.T) {
	st := newDecisionStore(t)
	require.NoError(t, st.SaveDecision(context.Background(), &store.Decision{
		ProjectPath: "/proj", Text: "recent", Importance: store.ImportanceNormal,
	}))
	old := &store.Decision{
		ProjectPath: "/proj", Text: "old",
		Importance: store.ImportanceNormal,
		CreatedAt:  time.Now().Add(-8 * 24 * time.Hour),
	}
	require.NoError(t, st.SaveDecision(context.Background(), old))

	got, err := st.ListDecisions(context.Background(), store.ListDecisionsOptions{
		ProjectPath: "/proj", Scope: "7d",
	})
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "recent", got[0].Text)
}

func TestListDecisions_TagsFilter(t *testing.T) {
	st := newDecisionStore(t)
	require.NoError(t, st.SaveDecision(context.Background(), &store.Decision{
		ProjectPath: "/proj", Text: "arch note", Tags: []string{"arch"}, Importance: store.ImportanceNormal,
	}))
	require.NoError(t, st.SaveDecision(context.Background(), &store.Decision{
		ProjectPath: "/proj", Text: "perf note", Tags: []string{"perf"}, Importance: store.ImportanceNormal,
	}))
	require.NoError(t, st.SaveDecision(context.Background(), &store.Decision{
		ProjectPath: "/proj", Text: "untagged", Importance: store.ImportanceNormal,
	}))

	// OR-match: arch OR perf → 2 results
	got, err := st.ListDecisions(context.Background(), store.ListDecisionsOptions{
		ProjectPath: "/proj", Scope: "all", Tags: []string{"arch", "perf"},
	})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestListDecisions_ImportanceFilter(t *testing.T) {
	st := newDecisionStore(t)
	for _, imp := range []string{"low", "normal", "high"} {
		require.NoError(t, st.SaveDecision(context.Background(), &store.Decision{
			ProjectPath: "/proj", Text: imp + " note", Importance: imp,
		}))
	}

	high, err := st.ListDecisions(context.Background(), store.ListDecisionsOptions{
		ProjectPath: "/proj", Scope: "all", MinImportance: "high",
	})
	require.NoError(t, err)
	assert.Len(t, high, 1)

	normalPlus, err := st.ListDecisions(context.Background(), store.ListDecisionsOptions{
		ProjectPath: "/proj", Scope: "all", MinImportance: "normal",
	})
	require.NoError(t, err)
	assert.Len(t, normalPlus, 2)

	all, err := st.ListDecisions(context.Background(), store.ListDecisionsOptions{
		ProjectPath: "/proj", Scope: "all", MinImportance: "low",
	})
	require.NoError(t, err)
	assert.Len(t, all, 3)
}

func TestListDecisions_LimitAndOrder(t *testing.T) {
	st := newDecisionStore(t)
	for i := 0; i < 5; i++ {
		require.NoError(t, st.SaveDecision(context.Background(), &store.Decision{
			ProjectPath: "/proj",
			Text:        "note",
			Importance:  store.ImportanceNormal,
			// stagger timestamps
			CreatedAt: time.Now().Add(time.Duration(i) * time.Second),
		}))
	}

	got, err := st.ListDecisions(context.Background(), store.ListDecisionsOptions{
		ProjectPath: "/proj", Scope: "all", Limit: 3,
	})
	require.NoError(t, err)
	assert.Len(t, got, 3)
	// Newest first.
	assert.True(t, got[0].CreatedAt.After(got[1].CreatedAt))
}

func TestListDecisions_DifferentProject(t *testing.T) {
	st := newDecisionStore(t)
	require.NoError(t, st.SaveDecision(context.Background(), &store.Decision{
		ProjectPath: "/proj-a", Text: "proj a", Importance: store.ImportanceNormal,
	}))
	require.NoError(t, st.SaveDecision(context.Background(), &store.Decision{
		ProjectPath: "/proj-b", Text: "proj b", Importance: store.ImportanceNormal,
	}))

	got, err := st.ListDecisions(context.Background(), store.ListDecisionsOptions{
		ProjectPath: "/proj-a", Scope: "all",
	})
	require.NoError(t, err)
	assert.Len(t, got, 1)
	assert.Equal(t, "/proj-a", got[0].ProjectPath)
}

func TestListDecisions_InvalidScope(t *testing.T) {
	st := newDecisionStore(t)
	_, err := st.ListDecisions(context.Background(), store.ListDecisionsOptions{
		ProjectPath: "/proj", Scope: "badscope",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid scope")
}

func TestNewDecisionID_Format(t *testing.T) {
	st := newDecisionStore(t)
	d := &store.Decision{ProjectPath: "/proj", Text: "x", Importance: store.ImportanceNormal}
	require.NoError(t, st.SaveDecision(context.Background(), d))

	pattern := regexp.MustCompile(`^dec_\d+_[0-9a-f]{4}$`)
	assert.True(t, pattern.MatchString(d.DecisionID), "unexpected decision_id format: %s", d.DecisionID)
}
