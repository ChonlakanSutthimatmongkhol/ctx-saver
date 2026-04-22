package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // register the "sqlite" database driver
)

// SQLiteStore implements Store using a per-project SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore opens (or creates) the SQLite database for the given project path
// inside dataDir, runs pending migrations, and cleans up old outputs.
func NewSQLiteStore(dataDir, projectPath string) (*SQLiteStore, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("creating data directory %s: %w", dataDir, err)
	}

	dbFile := dbPath(dataDir, projectPath)

	// Open with WAL mode for better concurrent read performance.
	db, err := sql.Open("sqlite", dbFile+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", dbFile, err)
	}

	// Single writer to avoid SQLITE_BUSY on concurrent MCP calls.
	db.SetMaxOpenConns(1)

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	// Restrict permissions to owner only after migrations have created the file.
	if err := os.Chmod(dbFile, 0600); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting database file permissions: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// dbPath returns the SQLite file path for a project, derived from a SHA-256 of projectPath.
func dbPath(dataDir, projectPath string) string {
	h := sha256.Sum256([]byte(projectPath))
	name := hex.EncodeToString(h[:8]) + ".db" // 16 hex chars
	return filepath.Join(dataDir, name)
}

// Save stores an Output and inserts each line into the FTS index.
func (s *SQLiteStore) Save(ctx context.Context, output *Output) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	_, err = tx.ExecContext(ctx, `
		INSERT INTO outputs
			(output_id, command, intent, full_output, size_bytes, line_count,
			 exit_code, duration_ms, created_at, project_path)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		output.OutputID,
		output.Command,
		output.Intent,
		output.FullOutput,
		output.SizeBytes,
		output.LineCount,
		output.ExitCode,
		output.DurationMs,
		output.CreatedAt.Unix(),
		output.ProjectPath,
	)
	if err != nil {
		return fmt.Errorf("inserting output: %w", err)
	}

	// Index each non-empty line into the FTS table.
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO outputs_fts(output_id, line_no, content) VALUES (?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("preparing FTS insert: %w", err)
	}
	defer stmt.Close()

	for i, line := range strings.Split(output.FullOutput, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, output.OutputID, i+1, line); err != nil {
			return fmt.Errorf("inserting FTS row (line %d): %w", i+1, err)
		}
	}

	return tx.Commit()
}

// Get retrieves a single Output by ID.
func (s *SQLiteStore) Get(ctx context.Context, id string) (*Output, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT output_id, command, intent, full_output, size_bytes, line_count,
		       exit_code, duration_ms, created_at, project_path
		FROM outputs WHERE output_id = ?`, id)

	var o Output
	var createdAt int64
	err := row.Scan(
		&o.OutputID, &o.Command, &o.Intent, &o.FullOutput,
		&o.SizeBytes, &o.LineCount, &o.ExitCode, &o.DurationMs,
		&createdAt, &o.ProjectPath,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("output %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("scanning output row: %w", err)
	}
	o.CreatedAt = time.Unix(createdAt, 0)
	return &o, nil
}

// List returns lightweight metadata for outputs belonging to projectPath.
func (s *SQLiteStore) List(ctx context.Context, projectPath string, limit int) ([]*OutputMeta, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT output_id, command, created_at, size_bytes, line_count
		FROM outputs
		WHERE project_path = ?
		ORDER BY created_at DESC
		LIMIT ?`, projectPath, limit)
	if err != nil {
		return nil, fmt.Errorf("listing outputs: %w", err)
	}
	defer rows.Close()

	var metas []*OutputMeta
	for rows.Next() {
		var m OutputMeta
		var createdAt int64
		if err := rows.Scan(&m.OutputID, &m.Command, &createdAt, &m.SizeBytes, &m.LineCount); err != nil {
			return nil, fmt.Errorf("scanning output meta: %w", err)
		}
		m.CreatedAt = time.Unix(createdAt, 0)
		metas = append(metas, &m)
	}
	return metas, rows.Err()
}

// Search runs a single FTS5 query and returns up to maxResults matches.
// If outputID is non-empty, results are filtered to that output only.
func (s *SQLiteStore) Search(ctx context.Context, query, outputID string, maxResults int) ([]*Match, error) {
	if maxResults <= 0 {
		maxResults = 5
	}

	// Use a generous fetch limit so we have enough rows to filter when outputID is set.
	fetchLimit := maxResults
	if outputID != "" {
		fetchLimit = maxResults * 20
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT output_id, line_no,
		       snippet(outputs_fts, 2, '[[', ']]', '...', 20) AS snip,
		       rank
		FROM outputs_fts
		WHERE outputs_fts MATCH ?
		ORDER BY rank
		LIMIT ?`, query, fetchLimit)
	if err != nil {
		return nil, fmt.Errorf("FTS search for %q: %w", query, err)
	}
	defer rows.Close()

	var matches []*Match
	for rows.Next() {
		var m Match
		var rank float64
		if err := rows.Scan(&m.OutputID, &m.Line, &m.Snippet, &rank); err != nil {
			return nil, fmt.Errorf("scanning search row: %w", err)
		}
		m.Score = -rank // FTS5 rank is negative; flip so higher = more relevant
		if outputID != "" && m.OutputID != outputID {
			continue
		}
		matches = append(matches, &m)
		if len(matches) >= maxResults {
			break
		}
	}
	return matches, rows.Err()
}

// Cleanup deletes outputs (and their FTS rows) older than retentionDays.
func (s *SQLiteStore) Cleanup(ctx context.Context, projectPath string, retentionDays int) error {
	cutoff := time.Now().AddDate(0, 0, -retentionDays).Unix()

	// Collect IDs to delete so we can remove FTS rows too.
	rows, err := s.db.QueryContext(ctx,
		`SELECT output_id FROM outputs WHERE project_path = ? AND created_at < ?`,
		projectPath, cutoff)
	if err != nil {
		return fmt.Errorf("querying stale outputs: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("scanning output id: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	if len(ids) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM outputs WHERE output_id = ?`, id); err != nil {
			return fmt.Errorf("deleting output %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM outputs_fts WHERE output_id = ?`, id); err != nil {
			return fmt.Errorf("deleting FTS rows for %s: %w", id, err)
		}
	}
	return tx.Commit()
}

// Close releases the database connection.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
