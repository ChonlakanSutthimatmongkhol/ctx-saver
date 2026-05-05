# Migration Guide — v0.5.x → v0.6.0

## Overview

v0.6.0 is a backwards-compatible optimization release. No schema changes were made
(`currentSchemaVersion` remains 5). Existing databases open without modification.

---

## Breaking-ish change: `auto_index_threshold_bytes` default raised to 32 KB

### What changed

| Version | Default `auto_index_threshold_bytes` |
|---------|---------------------------------------|
| v0.5.x  | 5 120 (5 KB)                         |
| v0.6.0  | 32 768 (32 KB)                        |

### What this means

Files and command outputs **smaller than the threshold** are returned **inline**
(`direct_output` field) instead of being stored with an `output_id` and summarized.

With the old 5 KB default, typical Go source files (200–500 lines, ~6–20 KB) were
stored via `output_id`, requiring a round-trip `ctx_get_full` call to read the
content. With 32 KB, most Go/Python source files are now returned inline directly,
saving one turn per file read.

Large outputs (build logs, test output, spec dumps) still follow the summary path
and return an `output_id` as before.

### Who is affected

- **Users without an explicit `auto_index_threshold_bytes` in their config** will
  see the new behavior automatically. Medium-size files now return inline.
- **Users with an explicit value** are unaffected. Your configured value is used
  as-is.

### How to keep the old behavior

Add this to `~/.config/ctx-saver/config.yaml` or `.ctx-saver.yaml` in your project:

```yaml
summary:
  auto_index_threshold_bytes: 5120  # restore v0.5.x default
```

### How to go even more aggressive (fewer round-trips)

```yaml
summary:
  auto_index_threshold_bytes: 65536  # 64 KB inline threshold
```

---

## New features

### `ctx_purge` tool

Clears cached outputs and session events for the current project. Useful when
switching to a new feature context or resetting noisy cache entries.

By default, decision notes (`ctx_note` entries) are **preserved**. Set `all=true`
to also delete notes.

Requires `confirm="yes"` to execute (safety check against accidental invocation).

### `--fields=signatures` on `ctx_read_file`

Returns only function/type/const declarations with original line numbers instead
of full file content. Reduces output by 80–90% for code-heavy files.

Supported: Go (full), Python (~95%), Dart (basic regex; see tool description for
known limits).

### `freshness_policy` in `ctx_session_init` response

The freshness policy (how to interpret `stale_level` values) is now returned once
per session in `ctx_session_init` instead of being repeated in every retrieval
tool description. This reduces per-turn token overhead by ~400–600 tokens.

---

## Removed

- Verbose `CACHE FRESHNESS POLICY:` blocks from `ctx_search`, `ctx_list_outputs`,
  `ctx_get_full`, `ctx_outline`, and `ctx_get_section` tool descriptions.
  Each now contains a single reference line pointing to `ctx_session_init`.
