# Migration Guide: v0.4.2 → v0.5.0 (Cache Freshness Policy)

## What changed

v0.5.0 adds Phase 7: a 4-layer cache freshness system.

| Layer | What it does |
|---|---|
| Provenance display | Every retrieval response now includes a `freshness` field with `stale_level`, `age_human`, `source_kind`, and `refresh_hint` |
| TTL-based auto-refresh | Outputs from sources with `auto_refresh: true` are silently re-executed on retrieval when stale |
| AI heuristic | Tool descriptions instruct the AI to call `ctx_execute` when the user asks for "ล่าสุด/current/latest/now" |
| Critical confirmation gate | Outputs older than 7 days set `user_confirmation_required: true` — the AI must surface this to the user before proceeding |

## Schema migration

The SQLite database schema is automatically migrated from v2 → v3 on first startup. Three columns are added to the `outputs` table:

```sql
ALTER TABLE outputs ADD COLUMN source_kind  TEXT    NOT NULL DEFAULT 'unknown';
ALTER TABLE outputs ADD COLUMN refreshed_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE outputs ADD COLUMN ttl_seconds  INTEGER NOT NULL DEFAULT 0;
```

Existing rows are backfilled: `refreshed_at` is set to `created_at`. No data is lost.

## New response fields

All retrieval tools (`ctx_get_full`, `ctx_get_section`, `ctx_search`, `ctx_list_outputs`, `ctx_outline`) now include:

```json
{
  "freshness": {
    "source_kind": "shell:acli",
    "cached_at": "2026-04-19T10:00:00Z",
    "age_seconds": 604800,
    "age_human": "7d ago",
    "stale_level": "critical",
    "refresh_hint": "🛑 STOP — this output is over 7 days old..."
  },
  "user_confirmation_required": true,
  "user_confirmation_prompt": "This output is over 7 days old..."
}
```

The `accept_stale: true` input parameter bypasses the confirmation gate on any retrieval tool.

## How to disable

Add to `~/.config/ctx-saver/config.yaml` or `.ctx-saver.yaml`:

```yaml
freshness:
  enabled: false
```

With `enabled: false` the Resolver always returns `use_cache`, no auto-refresh occurs, and `user_confirmation_required` is never set. Freshness metadata (`stale_level`, `age_human`) is still included in responses for informational purposes.

## Default TTL values

| Source | TTL | Auto-refresh |
|---|---|---|
| `shell:acli` | 5 min | yes |
| `shell:jira` | 5 min | yes |
| `shell:kubectl` | 1 min | yes |
| `shell:docker` | 1 min | yes |
| `shell:git` | 2 min | no |
| `shell:flutter` | 10 min | no |
| `shell:go` | 10 min | no |
| `shell:npm` | 10 min | no |
| everything else | 60 min (default) | no |

## Rollback

Simply downgrade to v0.4.2. The extra columns have `DEFAULT` values and are ignored by the old binary. No schema rollback needed.
