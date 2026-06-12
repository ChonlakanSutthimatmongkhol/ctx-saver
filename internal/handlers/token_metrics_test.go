package handlers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/tokenize"
)

func TestSaveOutput_SmallSync(t *testing.T) {
	st, err := store.NewSQLiteStore(t.TempDir(), "/proj")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	out := tokenTestOutput("small", "hello world")
	require.NoError(t, saveOutput(context.Background(), st, out, "summary"))

	got, err := st.Get(context.Background(), out.OutputID)
	require.NoError(t, err)
	assert.Positive(t, got.RawTokens)
	assert.Positive(t, got.ResponseTokens)
	assert.Equal(t, tokenize.Encoding, got.Tokenizer)
}

func TestSaveOutput_LargeAsync(t *testing.T) {
	st, err := store.NewSQLiteStore(t.TempDir(), "/proj")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	out := tokenTestOutput("large", strings.Repeat("large output line\n", syncTokenizeLimit/10))
	require.Greater(t, len(out.FullOutput), syncTokenizeLimit)
	require.NoError(t, saveOutput(context.Background(), st, out, "summary"))

	require.Eventually(t, func() bool {
		got, getErr := st.Get(context.Background(), out.OutputID)
		return getErr == nil && got.RawTokens > 0 && got.Tokenizer == tokenize.Encoding
	}, 10*time.Second, 20*time.Millisecond)
}

func TestUpdateTokenMetrics_RowGone(t *testing.T) {
	st, err := store.NewSQLiteStore(t.TempDir(), "/proj")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, st.Close()) })

	require.NoError(t, st.UpdateTokenMetrics(
		context.Background(), "missing", 10, 2, 8, tokenize.Encoding,
	))
}

type panicTokenStore struct {
	store.Store
	called chan struct{}
}

func (s *panicTokenStore) Save(context.Context, *store.Output) error { return nil }

func (s *panicTokenStore) UpdateTokenMetrics(context.Context, string, int64, int64, int64, string) error {
	close(s.called)
	panic("test panic")
}

func TestBackfillTokenMetrics_PanicContained(t *testing.T) {
	st := &panicTokenStore{called: make(chan struct{})}
	go backfillTokenMetrics(st, "panic", "small output", "summary")

	select {
	case <-st.called:
	case <-time.After(10 * time.Second):
		t.Fatal("token backfill did not reach the store")
	}
}

func BenchmarkTokenize_10MB(b *testing.B) {
	text := strings.Repeat("benchmark token payload\n", (10<<20)/24)
	b.ReportAllocs()
	b.SetBytes(int64(len(text)))
	for range b.N {
		if _, err := tokenize.Count(text); err != nil {
			b.Fatal(err)
		}
	}
}

func tokenTestOutput(id, body string) *store.Output {
	return &store.Output{
		OutputID:    id,
		Command:     "[shell] test",
		FullOutput:  body,
		SizeBytes:   int64(len(body)),
		LineCount:   1,
		CreatedAt:   time.Now(),
		ProjectPath: "/proj",
	}
}
