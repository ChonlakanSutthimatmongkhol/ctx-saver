# Hotfix Spec: Source File Cache Invalidation (v0.5.2)

**Type:** 🔴 Bug fix (silent corruption)
**Priority:** Critical (blocks safe usage on source code editing)
**Estimated effort:** 1 day
**Target version:** v0.5.2
**Spec version:** 2 (verified against actual codebase)

## Changelog from spec v1

- Migration version corrected: **v5** (not v6) — current `currentSchemaVersion = 4`
- `findCachedOutputForPath` updated: 2-step lookup (`FindRecentSameCommand` returns `*OutputMeta`, then `Get(id)` to fetch full `*Output` with `SourceHash`)
- mockStore signature for `FindRecentSameCommand` corrected to return `*OutputMeta`
- Confirmed: `recordToolCall`, `truncatePreview`, `Output.SourceKind/RefreshedAt/TTLSeconds` all already exist (no need to add)

---

## Problem statement

Phase 7 introduced TTL-based cache freshness for `ctx_read_file` outputs. The TTL works well for command outputs (acli, kubectl, git log) where time-based staleness makes sense.

**But TTL is wrong for source files.**

Source code files become stale **the moment the file is edited on disk** — regardless of how recently we cached it. A 30-minute-old cache is invalid if the user edited the file 5 minutes ago.

### Reproducible bug scenario

```
14:00 — User runs prompt: "อ่าน foo.go แล้ว explain"
        AI: ctx_read_file path=foo.go
        ctx-saver: caches output as out_abc with refreshed_at=14:00
        Result: AI explains foo.go correctly

14:30 — User edits foo.go in editor (adds new function `BarV2`)
        File on disk now contains BarV2

14:45 — User runs prompt: "เพิ่ม test สำหรับ BarV2"
        AI: ctx_read_file path=foo.go
        Phase 7 freshness: cache age = 45min, TTL = 1h → "fresh"
        ctx-saver: returns CACHED out_abc (without BarV2!)
        AI: "ไม่เห็น function BarV2 — ต้องสร้างก่อนไหมครับ?"
        OR worse: AI patches based on old code → patch fails or corrupts
```

### Severity: silent corruption

This bug is dangerous because:
- ✅ Cache "looks valid" (within TTL)
- ✅ AI response sounds plausible
- ❌ AI is working on outdated code
- ❌ User doesn't see anything wrong until patch fails or behavior diverges
- ❌ When patch DOES apply but code-on-disk has changed → corruption

**Trust impact:** One incident of "AI edited code based on stale cache" undermines confidence in `ctx_read_file` for all source code work, pushing users back to native `readFile` and erasing Phase 6 adherence gains.

---

## Root cause

Phase 7 freshness assumes **time** correlates with **change probability**:

```go
// Current logic (simplified):
age := time.Since(output.RefreshedAt)
if age <= ttl {
    return cached  // ← BUG: doesn't check if file actually changed
}
```

For files, the correct check is **content-based**:

```go
// Correct logic:
currentHash := hashFile(path)
if cached.SourceHash == currentHash {
    return cached  // file unchanged → cache valid
}
// file changed → invalidate, re-read, update
```

Time-based TTL is still correct for **commands** (kubectl, acli) because their "source" (cluster state, Confluence page) is not on local disk — we can't hash it.

---

## Solution

Add **content hash invalidation** for `ctx_read_file` outputs (file-backed sources only).

**Behavior change:**

| Source type | Stale check | Why |
|---|---|---|
| `ctx_read_file` (file path) | Hash mismatch on disk → stale | File contents are ground truth |
| `ctx_execute` (command) | TTL (Phase 7) | Source not local; rely on time heuristic |

Both checks are independent and can coexist.

**Hash algorithm:** SHA-256 (cheap for typical source files; well under 100ms for 50KB).

---

## Files to modify

```
MODIFY:
  internal/store/store.go              — add SourceHash field to Output
  internal/store/sqlite.go             — Save/Get/Update propagate SourceHash
  internal/store/migrations.go         — add migration for source_hash column
  internal/handlers/read_file.go       — compute & store hash on initial read;
                                         check hash on cache hit before return
  internal/handlers/refresh.go         — recompute hash on refresh (Phase 7 path)
  cmd/ctx-saver/main.go                — bump serverVersion to "0.5.2"

NEW:
  internal/freshness/filehash.go       — fileSHA256 helper
  internal/handlers/read_file_test.go  — add cache invalidation tests (extend existing if present)
```

No changes needed to `ctx_execute` or other handlers — bug is specific to file-backed reads.

---

## Schema change

### Migration

**File:** `internal/store/migrations.go` — add new migration

```go
{
    Version: 5, // currentSchemaVersion is 4 in main; this hotfix bumps it to 5
    Description: "Add source_hash column for file-backed cache invalidation",
    Statements: []string{
        `ALTER TABLE outputs ADD COLUMN source_hash TEXT NOT NULL DEFAULT ''`,
        // No backfill needed — empty hash means "unknown, treat as needs revalidation"
        // No index needed — we look up by output_id (already indexed)
    },
},
```

**Migration safety:**
- Adds column with default `''`
- Existing outputs (pre-v0.5.2) have `source_hash = ''`
- Code treats `''` as "unknown hash" → triggers revalidation on next access (acceptable; one-time cost)

### Output struct

**File:** `internal/store/store.go`

```go
type Output struct {
    OutputID    string
    ProjectPath string
    Command     string
    SizeBytes   int64
    LineCount   int
    DurationMs  int64
    FullOutput  string
    CreatedAt   time.Time
    
    // Phase 7
    SourceKind  string
    RefreshedAt time.Time
    TTLSeconds  int
    
    // v0.5.2 — content-based invalidation for file-backed sources
    SourceHash  string  // SHA-256 hex; empty for non-file sources or pre-v0.5.2 rows
}
```

---

## SQLite implementation

**File:** `internal/store/sqlite.go`

### Update Save

Add `source_hash` to the INSERT statement:

```go
_, err := s.db.ExecContext(ctx, `
    INSERT INTO outputs
        (output_id, project_path, command, size_bytes, line_count, duration_ms,
         full_output, created_at, source_kind, refreshed_at, ttl_seconds, source_hash)
    VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
    out.OutputID, out.ProjectPath, out.Command,
    out.SizeBytes, out.LineCount, out.DurationMs,
    out.FullOutput, out.CreatedAt.Unix(),
    out.SourceKind, out.RefreshedAt.Unix(), out.TTLSeconds,
    out.SourceHash, // ← NEW
)
```

### Update Get / List

Add `source_hash` to all `SELECT` statements that return Output rows:

```go
SELECT output_id, project_path, command, size_bytes, line_count, duration_ms,
       full_output, created_at, source_kind, refreshed_at, ttl_seconds,
       source_hash  -- ← NEW
FROM outputs
WHERE ...
```

And to all corresponding `Scan(...)` calls.

### Update UpdateRefreshed (Phase 7 helper)

If `UpdateRefreshed` exists (from Phase 7), include `source_hash` in the UPDATE:

```go
UPDATE outputs
SET full_output = ?, size_bytes = ?, line_count = ?,
    duration_ms = ?, refreshed_at = ?,
    source_hash = ?  -- ← NEW
WHERE output_id = ?
```

---

## File hash helper

**File:** `internal/freshness/filehash.go` (new)

```go
package freshness

import (
    "crypto/sha256"
    "encoding/hex"
    "io"
    "os"
)

// FileSHA256 returns the hex-encoded SHA-256 of the file's contents.
// Returns empty string if the file cannot be read (caller decides how to handle).
//
// Streaming-based to avoid loading large files entirely into memory.
func FileSHA256(path string) (string, error) {
    f, err := os.Open(path)
    if err != nil {
        return "", err
    }
    defer f.Close()
    
    h := sha256.New()
    if _, err := io.Copy(h, f); err != nil {
        return "", err
    }
    return hex.EncodeToString(h.Sum(nil)), nil
}
```

**Why streaming `io.Copy`:** typical source files are < 100KB but can be larger (generated code, vendored deps); streaming keeps memory bounded.

**Why SHA-256:** standard, fast (~500MB/s on modern CPUs), no collision concerns at our scale.

---

## Read handler integration

**File:** `internal/handlers/read_file.go`

### Step 1: Helper to find existing cache for a path

Add a helper at the top of the package or in `read_file.go`:

```go
// findCachedOutputForPath returns the most recent cached output for a given
// absolute file path, or nil if none exists.
// Match by exact command string used previously (we record `[read_file] <abspath>`).
//
// IMPORTANT: FindRecentSameCommand returns *OutputMeta (lightweight); we need
// the full Output (with FullOutput + SourceHash) to do the cache check.
// So this is a 2-step lookup: meta -> Get(id).
func (h *ReadFileHandler) findCachedOutputForPath(ctx context.Context, absPath string) (*store.Output, error) {
    command := "[read_file] " + absPath
    meta, err := h.st.FindRecentSameCommand(ctx, h.projectPath, command, 24*time.Hour)
    if err != nil {
        return nil, err
    }
    if meta == nil {
        return nil, nil
    }
    // Fetch the full Output (includes FullOutput + SourceHash) for the hash check.
    return h.st.Get(ctx, meta.OutputID)
}
```

**Note:** This 2-step lookup (`FindRecentSameCommand` → `Get`) is required because `FindRecentSameCommand` returns `*OutputMeta` which doesn't include `FullOutput` or `SourceHash`. The window of 24h is conservative — covers the active editing window without bloating cache.

### Step 2: Check hash on cache hit, before returning cached

In `Handle`, after computing `absPath` and BEFORE running `os.ReadFile`:

```go
// Check for an existing cached output for this file path.
cached, err := h.findCachedOutputForPath(ctx, absPath)
if err != nil {
    slog.Warn("read_file: cache lookup failed (proceeding without cache)",
        "path", absPath, "error", err)
    cached = nil // proceed as if no cache
}

if cached != nil && cached.SourceHash != "" && input.ProcessScript == "" {
    // We have a cached output AND a hash to compare AND no transformation script.
    // (process_script outputs depend on script + file content, so we don't
    //  short-circuit them here — let the existing flow re-run.)
    
    currentHash, hashErr := freshness.FileSHA256(absPath)
    if hashErr != nil {
        // File unreadable now (deleted/permissions). Don't return stale cache.
        slog.Warn("read_file: cannot hash file, falling through to read",
            "path", absPath, "error", hashErr)
    } else if currentHash == cached.SourceHash {
        // CACHE HIT: file unchanged since cache. Return existing output_id.
        recordToolCall(
            ctx, h.st, h.projectPath, "ctx_read_file",
            absPath, "", "read (cache-hit): "+absPath,
        )
        return nil, ReadFileOutput{
            OutputID: cached.OutputID,
            Summary:  truncatePreview(cached.FullOutput, 1024), // re-summarize from cache
            Stats: OutputStats{
                LineCount:  cached.LineCount,
                Bytes:      cached.SizeBytes,
                DurationMs: 0, // no work done
            },
            SearchHint: fmt.Sprintf(
                "Returning cached content (file unchanged since %s). Use ctx_get_full %s for the full file.",
                cached.RefreshedAt.Format(time.RFC3339),
                cached.OutputID,
            ),
            Path: absPath,
        }, nil
    }
    // Hash mismatch → fall through and re-read
}

// (existing read logic continues below)
```

### Step 3: Compute hash on every fresh read

After successfully reading the file (either direct or via process_script), compute the hash and store it:

```go
// After successful read, before saving to store:
sourceHash := ""
if input.ProcessScript == "" {
    // Only meaningful for direct file reads — script transforms make hash ambiguous.
    if h, herr := freshness.FileSHA256(absPath); herr == nil {
        sourceHash = h
    }
}

// In the existing Save call, populate SourceHash:
out := &store.Output{
    // ... existing fields
    SourceKind:  "file:" + filepath.Ext(absPath), // or whatever Phase 7 uses
    SourceHash:  sourceHash,
    RefreshedAt: time.Now(),
}
```

**Important:** Only set hash for direct reads (no process_script). With a process_script, the cached output is the script's output, not the file's content — hashing the file alone wouldn't tell us whether to invalidate.

---

## Tests

**File:** `internal/handlers/read_file_test.go` (extend if exists, otherwise create)

### Test 1: Cache hit — file unchanged

```go
func TestReadFile_CacheHit_FileUnchanged(t *testing.T) {
    // Setup
    tmp := t.TempDir()
    file := filepath.Join(tmp, "foo.go")
    require.NoError(t, os.WriteFile(file, []byte("package foo\n"), 0644))
    
    mock := newMockStore()
    h := NewReadFileHandler(testConfig(), nil, mock, "/proj", tmp)
    
    // First call → reads from disk
    _, out1, err := h.Handle(ctx, nil, ReadFileInput{Path: file})
    require.NoError(t, err)
    require.NotEmpty(t, out1.OutputID)
    
    // Second call (file unchanged) → should return same output_id, durationMs = 0
    _, out2, err := h.Handle(ctx, nil, ReadFileInput{Path: file})
    require.NoError(t, err)
    require.Equal(t, out1.OutputID, out2.OutputID, "should return cached output")
    require.Equal(t, int64(0), out2.Stats.DurationMs, "no work done on cache hit")
    require.Contains(t, out2.SearchHint, "cached")
}
```

### Test 2: Cache miss — file changed

```go
func TestReadFile_CacheMiss_FileChanged(t *testing.T) {
    tmp := t.TempDir()
    file := filepath.Join(tmp, "foo.go")
    require.NoError(t, os.WriteFile(file, []byte("v1"), 0644))
    
    mock := newMockStore()
    h := NewReadFileHandler(testConfig(), nil, mock, "/proj", tmp)
    
    _, out1, _ := h.Handle(ctx, nil, ReadFileInput{Path: file})
    
    // Modify the file
    require.NoError(t, os.WriteFile(file, []byte("v2 with new content"), 0644))
    
    _, out2, err := h.Handle(ctx, nil, ReadFileInput{Path: file})
    require.NoError(t, err)
    require.NotEqual(t, out1.OutputID, out2.OutputID,
        "file changed should produce a new output_id (or at least a fresh read)")
    require.Greater(t, out2.Stats.Bytes, int64(0))
}
```

### Test 3: No hash on process_script reads

```go
func TestReadFile_ProcessScript_AlwaysReads(t *testing.T) {
    tmp := t.TempDir()
    file := filepath.Join(tmp, "data.json")
    require.NoError(t, os.WriteFile(file, []byte(`{"x":1}`), 0644))
    
    mock := newMockStore()
    h := NewReadFileHandler(testConfig(), mockSandbox(), mock, "/proj", tmp)
    
    // First call with script
    _, _, err := h.Handle(ctx, nil, ReadFileInput{
        Path: file,
        ProcessScript: "jq .x",
    })
    require.NoError(t, err)
    
    // Second call with same script + unchanged file → no cache short-circuit
    // (because process_script may have changed externally; we don't track it)
    // Should still execute (sandbox.Execute called twice).
    _, _, err = h.Handle(ctx, nil, ReadFileInput{
        Path: file,
        ProcessScript: "jq .x",
    })
    require.NoError(t, err)
    
    require.Equal(t, 2, mock.sandboxCallCount, "process_script reads should not be cache-bypassed")
}
```

### Test 4: Pre-v0.5.2 cache (empty hash) — revalidates

```go
func TestReadFile_LegacyCache_NoHash_Revalidates(t *testing.T) {
    tmp := t.TempDir()
    file := filepath.Join(tmp, "foo.go")
    require.NoError(t, os.WriteFile(file, []byte("legacy"), 0644))
    
    mock := newMockStore()
    // Manually inject an "old" output with empty SourceHash
    mock.savedOutputs = append(mock.savedOutputs, &store.Output{
        OutputID:    "out_legacy",
        ProjectPath: "/proj",
        Command:     "[read_file] " + file,
        FullOutput:  "old content",
        SourceHash:  "", // pre-v0.5.2
    })
    
    h := NewReadFileHandler(testConfig(), nil, mock, "/proj", tmp)
    
    _, out, err := h.Handle(ctx, nil, ReadFileInput{Path: file})
    require.NoError(t, err)
    // Should NOT return out_legacy as a cache hit (empty hash → re-read)
    require.NotEqual(t, "out_legacy", out.OutputID)
}
```

### Test 5: File deleted between reads

```go
func TestReadFile_FileDeletedBetweenReads_NoStaleReturn(t *testing.T) {
    tmp := t.TempDir()
    file := filepath.Join(tmp, "foo.go")
    require.NoError(t, os.WriteFile(file, []byte("v1"), 0644))
    
    mock := newMockStore()
    h := NewReadFileHandler(testConfig(), nil, mock, "/proj", tmp)
    
    _, _, err := h.Handle(ctx, nil, ReadFileInput{Path: file})
    require.NoError(t, err)
    
    // Delete file
    require.NoError(t, os.Remove(file))
    
    // Second call should error (file gone), NOT silently return cache
    _, _, err = h.Handle(ctx, nil, ReadFileInput{Path: file})
    require.Error(t, err, "deleted file should not return stale cache")
}
```

### Test 6: FileSHA256 helper

**File:** `internal/freshness/filehash_test.go` (new)

```go
func TestFileSHA256(t *testing.T) {
    tmp := t.TempDir()
    file := filepath.Join(tmp, "x.txt")
    require.NoError(t, os.WriteFile(file, []byte("hello"), 0644))
    
    hash, err := FileSHA256(file)
    require.NoError(t, err)
    require.Equal(t,
        "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
        hash)
}

func TestFileSHA256_NonExistent(t *testing.T) {
    _, err := FileSHA256("/nonexistent/path")
    require.Error(t, err)
}
```

### Mock store update

In existing test mock (`internal/handlers/handlers_test.go` or local helper), ensure the mock implements `FindRecentSameCommand` returning `*store.OutputMeta` (note: NOT `*store.Output` — see Step 1 above for why):

```go
func (m *mockStore) FindRecentSameCommand(_ context.Context, projectPath, command string, _ time.Duration) (*store.OutputMeta, error) {
    for i := len(m.savedOutputs) - 1; i >= 0; i-- {
        o := m.savedOutputs[i]
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

// Get is then called by findCachedOutputForPath to fetch the full Output.
// The existing mockStore.Get should already exist; ensure it returns the
// stored Output (including SourceHash) when found.
```
```

---

## Bump version

**File:** `cmd/ctx-saver/main.go`

```go
const serverVersion = "0.5.2"
```

---

## Documentation

### CLAUDE.md / README — short note

Add to the freshness section:

```markdown
### Cache invalidation for file reads

Starting in v0.5.2, `ctx_read_file` invalidates its cache automatically when 
the file content changes on disk (SHA-256 comparison). You don't need to 
manually refresh after editing — subsequent reads will detect the change 
and re-read.

This is independent of TTL: even within the configured TTL, a modified file 
will produce a fresh read.

Note: this applies only to direct file reads. Reads with `process_script` 
always re-execute (the script's output may depend on factors beyond the file).
```

### CHANGELOG

```markdown
## v0.5.2 — 2026-04-XX

Critical bug fix: source file cache invalidation.

Previously, ctx_read_file could return cached file content even after the 
file had been edited on disk, as long as the cache was within TTL. This 
caused silent code corruption when AI tools edited based on outdated content.

Fixed by adding SHA-256 hash comparison: cache is invalidated immediately 
when file content changes, regardless of TTL.

No action required for users; existing caches will revalidate on next access.
```

---

## Acceptance criteria

- [ ] Migration v5 runs cleanly on fresh DB
- [ ] Migration v5 runs cleanly on existing v0.5.1 DB (no data loss)
- [ ] First `ctx_read_file` call computes and stores SHA-256
- [ ] Subsequent call on unchanged file → returns cached output_id, `DurationMs == 0`
- [ ] Subsequent call after file edit → returns new output_id with current content
- [ ] `process_script` reads always execute (no cache bypass)
- [ ] Pre-v0.5.2 outputs (empty source_hash) trigger revalidation
- [ ] Deleted file does not return stale cache (errors out cleanly)
- [ ] `FileSHA256` returns deterministic hex of expected length (64 chars)
- [ ] All existing read_file tests still pass
- [ ] `make lint test` passes clean
- [ ] `serverVersion` bumped to `0.5.2`
- [ ] No regression in Phase 7 freshness for non-file commands (kubectl/acli still TTL-based)

---

## Manual verification

After `make install`, run this test:

```bash
# Setup
cd /Users/chonlakan/Desktop/testing/ctx-saver-fixtures
DB=".ctx-saver/outputs.db"
rm -f "$DB"

# 1. Create test file
mkdir -p /tmp/v052-test
echo 'package foo' > /tmp/v052-test/foo.go
echo 'func Bar() {}' >> /tmp/v052-test/foo.go

# In Copilot Chat (Agent mode):
# Prompt: "ใช้ ctx_read_file อ่าน /tmp/v052-test/foo.go"
# (wait for response)

# 2. Verify hash stored
sqlite3 "$DB" "SELECT output_id, source_hash FROM outputs WHERE command LIKE '%foo.go%';"
# Expected: source_hash is a 64-char hex string, not empty

# 3. Modify file
echo 'func BarV2() {}' >> /tmp/v052-test/foo.go

# In Copilot Chat:
# Prompt: "ใช้ ctx_read_file อ่าน /tmp/v052-test/foo.go อีกครั้ง — มี function ใหม่ไหม?"

# 4. Verify new output_id
sqlite3 "$DB" "SELECT output_id, source_hash FROM outputs WHERE command LIKE '%foo.go%' ORDER BY created_at;"
# Expected: 2 rows, different source_hash, different output_id (or refreshed_at updated)

# 5. Verify AI sees BarV2
# AI response should mention BarV2

# Without v0.5.2 fix, AI would say "ไม่เห็น BarV2" because cache is "still fresh"
```

---

## Non-goals (do NOT do in this fix)

- ❌ Don't add hash for `ctx_execute` outputs (commands have no local source)
- ❌ Don't add `Last-Modified`-style mtime check (hash is more accurate)
- ❌ Don't track external file dependencies (e.g. process_script files)
- ❌ Don't add cache warming or pre-hashing
- ❌ Don't add file watcher / inotify (poll-on-access is enough)
- ❌ Don't change the SourceKind format from Phase 7
- ❌ Don't refactor the read_file flow beyond the hash check insertion

---

## Hand-off context for Copilot

This is a focused **bug fix**, not a feature. The bug is silent: AI editing source code based on a cached version that's older than the file on disk. The fix is small (one new helper, one column, one logic branch in read_file) but high-impact.

Reuse existing patterns:
- `FindRecentSameCommand` from Phase 5 (dedup)
- `recordToolCall` from v0.4.2 (session_events)
- `Output.SourceKind` / `RefreshedAt` from Phase 7

Don't add new infrastructure. Don't refactor. Just plug the leak.

After the fix, the file-read flow is:

```
ctx_read_file(path) →
  ├─ findCachedOutputForPath(absPath)
  ├─ if cached and direct-read and hash matches disk → return cached (no work)
  └─ otherwise → read disk, compute hash, save with hash, return fresh
```

Test thoroughly with the 6 test cases above. The "process_script" exception is important — don't accidentally short-circuit those.

## Verification checklist (before merging)

- [ ] All 6 unit tests pass
- [ ] Manual reproduction steps succeed (file edit → AI sees new content)
- [ ] Phase 7 tests for non-file commands still pass (TTL behavior preserved)
- [ ] No new dependencies added
- [ ] `gofmt`, `go vet`, `make test` all clean
- [ ] Coverage of `internal/freshness/filehash.go` is 100% (only 2 test cases needed)
