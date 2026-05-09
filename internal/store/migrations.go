package store

import (
	"database/sql"
	"fmt"
)

const currentSchemaVersion = 7

// runMigrations applies any pending schema migrations to db.
// It is idempotent and safe to call on every server start.
func runMigrations(db *sql.DB) error {
	// Ensure the version-tracking table exists.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		version INTEGER NOT NULL PRIMARY KEY
	)`); err != nil {
		return fmt.Errorf("creating schema_version table: %w", err)
	}

	var version int
	err := db.QueryRow(`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&version)
	if err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	for v := version + 1; v <= currentSchemaVersion; v++ {
		if err := applyMigration(db, v); err != nil {
			return fmt.Errorf("applying migration v%d: %w", v, err)
		}
	}
	return nil
}

func applyMigration(db *sql.DB, version int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	switch version {
	case 7:
		if err := migration7(tx); err != nil {
			return err
		}
	case 1:
		if err := migration1(tx); err != nil {
			return err
		}
	case 2:
		if err := migration2(tx); err != nil {
			return err
		}
	case 3:
		if err := migration3(tx); err != nil {
			return err
		}
	case 4:
		if err := migration4(tx); err != nil {
			return err
		}
	case 5:
		if err := migration5(tx); err != nil {
			return err
		}
	case 6:
		if err := migration6(tx); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown migration version %d", version)
	}

	if _, err := tx.Exec(`INSERT INTO schema_version(version) VALUES (?)`, version); err != nil {
		return fmt.Errorf("recording schema version %d: %w", version, err)
	}
	return tx.Commit()
}

// migration1 creates the initial outputs table, FTS5 virtual table, and indexes.
func migration1(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE outputs (
			output_id    TEXT PRIMARY KEY,
			command      TEXT    NOT NULL,
			intent       TEXT    NOT NULL DEFAULT '',
			full_output  TEXT    NOT NULL,
			size_bytes   INTEGER NOT NULL,
			line_count   INTEGER NOT NULL,
			exit_code    INTEGER NOT NULL,
			duration_ms  INTEGER NOT NULL,
			created_at   INTEGER NOT NULL,
			project_path TEXT    NOT NULL
		)`,

		// Each row in outputs_fts represents one line of one output.
		// output_id and line_no are stored but not tokenised (UNINDEXED).
		`CREATE VIRTUAL TABLE outputs_fts USING fts5(
			output_id UNINDEXED,
			line_no   UNINDEXED,
			content,
			tokenize = 'porter unicode61'
		)`,

		`CREATE INDEX idx_outputs_created_at   ON outputs(created_at)`,
		`CREATE INDEX idx_outputs_project_path ON outputs(project_path)`,
	}

	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("executing %q: %w", stmt[:min(40, len(stmt))], err)
		}
	}
	return nil
}

// migration3 adds cache freshness metadata columns to the outputs table.
func migration3(tx *sql.Tx) error {
	stmts := []string{
		`ALTER TABLE outputs ADD COLUMN source_kind TEXT NOT NULL DEFAULT 'unknown'`,
		`ALTER TABLE outputs ADD COLUMN refreshed_at INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE outputs ADD COLUMN ttl_seconds INTEGER NOT NULL DEFAULT 0`,
		// Backfill: treat existing rows as refreshed when they were created.
		`UPDATE outputs SET refreshed_at = created_at WHERE refreshed_at = 0`,
		`CREATE INDEX idx_outputs_refreshed ON outputs(project_path, refreshed_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("executing %q: %w", stmt[:min(40, len(stmt))], err)
		}
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// migration4 creates the decisions table for the ctx_note decision log feature.
func migration4(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE decisions (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			decision_id  TEXT    NOT NULL UNIQUE,
			session_id   TEXT    NOT NULL DEFAULT '',
			project_path TEXT    NOT NULL,
			text         TEXT    NOT NULL,
			tags         TEXT    NOT NULL DEFAULT '',
			links_to     TEXT    NOT NULL DEFAULT '',
			importance   TEXT    NOT NULL DEFAULT 'normal',
			created_at   INTEGER NOT NULL
		)`,
		`CREATE INDEX idx_decisions_project_created
			ON decisions(project_path, created_at DESC)`,
		`CREATE INDEX idx_decisions_session
			ON decisions(session_id, created_at DESC)`,
		`CREATE INDEX idx_decisions_importance
			ON decisions(project_path, importance, created_at DESC)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("executing %q: %w", stmt[:min(40, len(stmt))], err)
		}
	}
	return nil
}

// migration5 adds the source_hash column for file-backed cache invalidation (v0.5.2).
func migration5(tx *sql.Tx) error {
	stmts := []string{
		`ALTER TABLE outputs ADD COLUMN source_hash TEXT NOT NULL DEFAULT ''`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("executing %q: %w", stmt[:min(40, len(stmt))], err)
		}
	}
	return nil
}

// migration6 adds a composite index on (project_path, created_at) to speed up
// the command-sequence self-join in KnowledgeStats.
func migration6(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_outputs_project_created
		ON outputs(project_path, created_at)`)
	if err != nil {
		return fmt.Errorf("creating composite index: %w", err)
	}
	return nil
}

// migration7 adds a UNIQUE constraint on session_events to prevent duplicate
// records when a hook insert is retried.  Because SQLite cannot add a UNIQUE
// constraint to an existing table via ALTER TABLE, we recreate the table with
// the constraint and copy existing rows.
func migration7(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE session_events_new (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id   TEXT    NOT NULL,
			project_path TEXT    NOT NULL,
			event_type   TEXT    NOT NULL,
			tool_name    TEXT    NOT NULL DEFAULT '',
			tool_input   TEXT    NOT NULL DEFAULT '',
			tool_output  TEXT    NOT NULL DEFAULT '',
			summary      TEXT    NOT NULL DEFAULT '',
			created_at   INTEGER NOT NULL,
			UNIQUE (session_id, event_type, tool_name, tool_input, summary, created_at)
		)`,
		`INSERT OR IGNORE INTO session_events_new
			(id, session_id, project_path, event_type, tool_name,
			 tool_input, tool_output, summary, created_at)
		SELECT id, session_id, project_path, event_type, tool_name,
			   tool_input, tool_output, summary, created_at
		FROM session_events`,
		`DROP TABLE session_events`,
		`ALTER TABLE session_events_new RENAME TO session_events`,
		`CREATE INDEX idx_session_events_session ON session_events(session_id, created_at)`,
		`CREATE INDEX idx_session_events_project ON session_events(project_path, created_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("migration7: %w", err)
		}
	}
	return nil
}

// migration2 creates the session_events table for hook-based session tracking.
func migration2(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE session_events (
			id           INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id   TEXT    NOT NULL,
			project_path TEXT    NOT NULL,
			event_type   TEXT    NOT NULL,
			tool_name    TEXT    NOT NULL DEFAULT '',
			tool_input   TEXT    NOT NULL DEFAULT '',
			tool_output  TEXT    NOT NULL DEFAULT '',
			summary      TEXT    NOT NULL DEFAULT '',
			created_at   INTEGER NOT NULL
		)`,
		`CREATE INDEX idx_session_events_session  ON session_events(session_id, created_at)`,
		`CREATE INDEX idx_session_events_project  ON session_events(project_path, created_at)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			return fmt.Errorf("executing %q: %w", stmt[:min(40, len(stmt))], err)
		}
	}
	return nil
}
