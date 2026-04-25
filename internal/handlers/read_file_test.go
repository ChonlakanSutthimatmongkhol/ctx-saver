package handlers_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/config"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/handlers"
	"github.com/ChonlakanSutthimatmongkhol/ctx-saver/internal/store"
)

// readFileMock overrides FindRecentSameCommand to search m.saved by projectPath+command,
// making cache-hit/miss tests work without pre-seeding dedupMeta.
type readFileMock struct {
	mockStore
}

func (m *readFileMock) FindRecentSameCommand(_ context.Context, projectPath, command string, _ time.Duration) (*store.OutputMeta, error) {
	for i := len(m.saved) - 1; i >= 0; i-- {
		o := m.saved[i]
		if o.ProjectPath == projectPath && o.Command == command {
			return &store.OutputMeta{
				OutputID:    o.OutputID,
				Command:     o.Command,
				CreatedAt:   o.CreatedAt,
				SizeBytes:   o.SizeBytes,
				LineCount:   o.LineCount,
				SourceKind:  o.SourceKind,
				RefreshedAt: o.RefreshedAt,
				TTLSeconds:  o.TTLSeconds,
			}, nil
		}
	}
	return nil, nil
}

// largeCfg returns a config where the auto-index threshold is tiny so test files
// (>10 bytes) are always stored in the DB, enabling cache-hit logic.
func largeCfg() *config.Config {
	cfg := defaultCfg()
	cfg.Summary.AutoIndexThresholdBytes = 5
	return cfg
}

func TestReadFile_CacheHit_FileUnchanged(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "foo.go")
	require.NoError(t, os.WriteFile(file, []byte("package foo\nfunc Bar() {}\n"), 0644))

	ms := &readFileMock{}
	h := handlers.NewReadFileHandler(largeCfg(), &mockSandbox{}, ms, "/proj", tmp)
	ctx := context.Background()

	// First call — reads from disk, stores with hash.
	_, out1, err := h.Handle(ctx, nil, handlers.ReadFileInput{Path: file})
	require.NoError(t, err)
	require.NotEmpty(t, out1.OutputID)

	// Second call — file unchanged, should return cached output_id with no work done.
	_, out2, err := h.Handle(ctx, nil, handlers.ReadFileInput{Path: file})
	require.NoError(t, err)
	require.Equal(t, out1.OutputID, out2.OutputID, "unchanged file should return cached output_id")
	require.Equal(t, int64(0), out2.Stats.DurationMs, "cache hit must report DurationMs=0")
	require.Contains(t, out2.SearchHint, "cached")
}

func TestReadFile_CacheMiss_FileChanged(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "foo.go")
	require.NoError(t, os.WriteFile(file, []byte("package foo // v1\n"), 0644))

	ms := &readFileMock{}
	h := handlers.NewReadFileHandler(largeCfg(), &mockSandbox{}, ms, "/proj", tmp)
	ctx := context.Background()

	_, out1, err := h.Handle(ctx, nil, handlers.ReadFileInput{Path: file})
	require.NoError(t, err)
	require.NotEmpty(t, out1.OutputID)

	// Modify the file so the hash changes.
	require.NoError(t, os.WriteFile(file, []byte("package foo // v2 — new function added\n"), 0644))

	_, out2, err := h.Handle(ctx, nil, handlers.ReadFileInput{Path: file})
	require.NoError(t, err)
	require.NotEqual(t, out1.OutputID, out2.OutputID,
		"modified file must produce a new output_id")
	require.Len(t, ms.saved, 2, "two distinct outputs must be stored (before and after edit)")
}

func TestReadFile_ProcessScript_AlwaysReads(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "data.txt")
	require.NoError(t, os.WriteFile(file, []byte("hello world\n"), 0644))

	ms := &readFileMock{}
	sb := &mockSandbox{output: []byte("filtered\n")}
	h := handlers.NewReadFileHandler(largeCfg(), sb, ms, "/proj", tmp)
	ctx := context.Background()

	// Two calls with process_script — cache must NOT short-circuit.
	_, _, err := h.Handle(ctx, nil, handlers.ReadFileInput{
		Path: file, ProcessScript: "cat",
	})
	require.NoError(t, err)

	_, _, err = h.Handle(ctx, nil, handlers.ReadFileInput{
		Path: file, ProcessScript: "cat",
	})
	require.NoError(t, err)

	// No output should have a SourceHash set (process_script skips hashing).
	for _, o := range ms.saved {
		require.Empty(t, o.SourceHash, "process_script outputs must not store a source hash")
	}
}

func TestReadFile_LegacyCache_NoHash_Revalidates(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "foo.go")
	require.NoError(t, os.WriteFile(file, []byte("package foo // legacy\n"), 0644))

	ms := &readFileMock{}
	// Inject a pre-v0.5.2 cached output (empty SourceHash).
	ms.saved = append(ms.saved, &store.Output{
		OutputID:    "out_legacy_001",
		ProjectPath: "/proj",
		Command:     "[read_file] " + file,
		FullOutput:  "old content",
		SourceHash:  "", // empty → must not be used as a cache hit
		CreatedAt:   time.Now(),
		RefreshedAt: time.Now(),
	})

	h := handlers.NewReadFileHandler(largeCfg(), &mockSandbox{}, ms, "/proj", tmp)
	ctx := context.Background()

	_, out, err := h.Handle(ctx, nil, handlers.ReadFileInput{Path: file})
	require.NoError(t, err)
	// Empty SourceHash guard must prevent returning the legacy cached output.
	require.NotEqual(t, "out_legacy_001", out.OutputID,
		"legacy cache (empty hash) must trigger re-read, not cache hit")
}

func TestReadFile_FileDeletedBetweenReads_NoStaleReturn(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "foo.go")
	require.NoError(t, os.WriteFile(file, []byte("package foo // will be deleted\n"), 0644))

	ms := &readFileMock{}
	h := handlers.NewReadFileHandler(largeCfg(), &mockSandbox{}, ms, "/proj", tmp)
	ctx := context.Background()

	// First read — stores with hash.
	_, _, err := h.Handle(ctx, nil, handlers.ReadFileInput{Path: file})
	require.NoError(t, err)

	// Delete the file.
	require.NoError(t, os.Remove(file))

	// Second read — file is gone; must error, not silently return stale cache.
	_, _, err = h.Handle(ctx, nil, handlers.ReadFileInput{Path: file})
	require.Error(t, err, "deleted file must return an error, not cached content")
}
