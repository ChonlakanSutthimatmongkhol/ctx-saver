package store

import (
	"database/sql"
	"fmt"
)

const currentSchemaVersion = 2

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
	case 1:
		if err := migration1(tx); err != nil {
			return err
		}
	case 2:
		if err := migration2(tx); err != nil {
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
