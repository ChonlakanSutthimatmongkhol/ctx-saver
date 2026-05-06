package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // register the "sqlite" database driver
)

// SQLiteStore implements Store using a per-project SQLite database.
type SQLiteStore struct {
	db   *sql.DB // write connection — single writer
	roDB *sql.DB // read-only connection — for analytics/background queries
}

// NewReadOnlySQLiteStore opens an existing database in read-only mode.
// It skips migrations (DB must already exist at the correct schema version).
// This is safe to call while another process holds a write connection because
// SQLite WAL read-only connections never block on writers.
// Returns an error if the database file does not exist.
func NewReadOnlySQLiteStore(dataDir, projectPath string) (*SQLiteStore, error) {
	dbFile := dbPath(dataDir, projectPath)
	if _, err := os.Stat(dbFile); err != nil {
		return nil, fmt.Errorf("database not found at %s (run ctx-saver server first): %w", dbFile, err)
	}

	// file: URI with mode=ro + immutable=1 — opens read-only without acquiring
	// any OS-level file lock (no fcntl/flock). SQLite assumes the file won't
	// change while open. This prevents the process from entering kernel
	// uninterruptible sleep (UE state) when another process holds the write lock.
	dsn := "file://" + dbFile + "?mode=ro&immutable=1"
	roDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening read-only database %s: %w", dbFile, err)
	}
	roDB.SetMaxOpenConns(4)
	roDB.SetMaxIdleConns(2)

	// Return a store with db == roDB so all paths work; callers must not write.
	return &SQLiteStore{db: roDB, roDB: roDB}, nil
}

// inside dataDir, runs pending migrations, and cleans up old outputs.
func NewSQLiteStore(dataDir, projectPath string) (*SQLiteStore, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("creating data directory %s: %w", dataDir, err)
	}

	dbFile := dbPath(dataDir, projectPath)

	// Open with WAL mode for better concurrent read performance.
	// Note: modernc.org/sqlite ignores _busy_timeout in the DSN.
	// We set it via PRAGMA after the first connection is established.
	db, err := sql.Open("sqlite", dbFile+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("opening database %s: %w", dbFile, err)
	}

	// Single writer to avoid SQLITE_BUSY on concurrent MCP calls.
	db.SetMaxOpenConns(1)

	// Set busy_timeout via PRAGMA so modernc.org/sqlite actually honours it.
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		db.Close()
		return nil, fmt.Errorf("setting busy_timeout: %w", err)
	}

	// Read-only connection for background analytics queries (KnowledgeStats).
	roDB, err := sql.Open("sqlite", dbFile+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("opening read-only database %s: %w", dbFile, err)
	}
	roDB.SetMaxOpenConns(4)
	roDB.SetMaxIdleConns(2)
	if _, err := roDB.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		roDB.Close()
		db.Close()
		return nil, fmt.Errorf("setting roDB busy_timeout: %w", err)
	}

	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	// Restrict permissions to owner only after migrations have created the file.
	if err := os.Chmod(dbFile, 0600); err != nil {
		roDB.Close()
		db.Close()
		return nil, fmt.Errorf("setting database file permissions: %w", err)
	}

	return &SQLiteStore{db: db, roDB: roDB}, nil
}

// dbPath returns the SQLite file path.
// When dataDir is already project-specific (the default .ctx-saver/ layout) the
// file is simply outputs.db.  A central shared store (absolute dataDir shared
// across projects) uses a SHA-256-derived name to avoid collisions.
func dbPath(dataDir, projectPath string) string {
	// Heuristic: if the directory itself encodes the project (i.e. its basename is
	// ".ctx-saver"), use a human-readable filename.
	if filepath.Base(dataDir) == ".ctx-saver" {
		return filepath.Join(dataDir, "outputs.db")
	}
	h := sha256.Sum256([]byte(projectPath))
	name := hex.EncodeToString(h[:8]) + ".db" // 16 hex chars
	return filepath.Join(dataDir, name)
}

// ClassifySource returns a stable source_kind string from a stored command string.
// Commands are stored in the format "[lang] code", e.g. "[shell] acli page view 123".
// Examples: "[shell] acli page view" → "shell:acli", "[python] import os" → "python".
func ClassifySource(command string) string {
	// Strip the "[lang] " prefix inserted by the execute handler.
	lang := "shell"
	rest := command
	if strings.HasPrefix(command, "[") {
		end := strings.Index(command, "]")
		if end > 0 {
			lang = command[1:end]
			rest = strings.TrimSpace(command[end+1:])
		}
	}
	if lang != "shell" && lang != "" {
		return lang
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "shell:other"
	}
	return "shell:" + fields[0]
}

// Save stores an Output and inserts each line into the FTS index.
func (s *SQLiteStore) Save(ctx context.Context, output *Output) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	sourceKind := output.SourceKind
	if sourceKind == "" {
		sourceKind = ClassifySource(output.Command)
	}
	refreshedAt := output.RefreshedAt
	if refreshedAt.IsZero() {
		refreshedAt = output.CreatedAt
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO outputs
			(output_id, command, intent, full_output, size_bytes, line_count,
			 exit_code, duration_ms, created_at, project_path,
			 source_kind, refreshed_at, ttl_seconds, source_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
		sourceKind,
		refreshedAt.Unix(),
		output.TTLSeconds,
		output.SourceHash,
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
		       exit_code, duration_ms, created_at, project_path,
		       source_kind, refreshed_at, ttl_seconds, source_hash
		FROM outputs WHERE output_id = ?`, id)

	var o Output
	var createdAt, refreshedAt int64
	err := row.Scan(
		&o.OutputID, &o.Command, &o.Intent, &o.FullOutput,
		&o.SizeBytes, &o.LineCount, &o.ExitCode, &o.DurationMs,
		&createdAt, &o.ProjectPath,
		&o.SourceKind, &refreshedAt, &o.TTLSeconds, &o.SourceHash,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("output %q not found", id)
	}
	if err != nil {
		return nil, fmt.Errorf("scanning output row: %w", err)
	}
	o.CreatedAt = time.Unix(createdAt, 0)
	o.RefreshedAt = time.Unix(refreshedAt, 0)
	return &o, nil
}

// UpdateRefreshed updates an existing output's content and refreshed_at in-place,
// preserving the original output_id. Also re-indexes FTS rows for the output.
func (s *SQLiteStore) UpdateRefreshed(ctx context.Context, output *Output) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	_, err = tx.ExecContext(ctx, `
		UPDATE outputs
		SET full_output = ?, size_bytes = ?, line_count = ?,
		    refreshed_at = ?, duration_ms = ?, source_hash = ?
		WHERE output_id = ?`,
		output.FullOutput,
		output.SizeBytes,
		output.LineCount,
		output.RefreshedAt.Unix(),
		output.DurationMs,
		output.SourceHash,
		output.OutputID,
	)
	if err != nil {
		return fmt.Errorf("updating output: %w", err)
	}

	// Re-index FTS: delete old rows, insert new ones.
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM outputs_fts WHERE output_id = ?`, output.OutputID); err != nil {
		return fmt.Errorf("deleting stale FTS rows: %w", err)
	}

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

// List returns lightweight metadata for outputs belonging to projectPath.
func (s *SQLiteStore) List(ctx context.Context, projectPath string, limit int) ([]*OutputMeta, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT output_id, command, created_at, size_bytes, line_count,
		       source_kind, refreshed_at, ttl_seconds
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
		var createdAt, refreshedAt int64
		if err := rows.Scan(&m.OutputID, &m.Command, &createdAt, &m.SizeBytes, &m.LineCount,
			&m.SourceKind, &refreshedAt, &m.TTLSeconds); err != nil {
			return nil, fmt.Errorf("scanning output meta: %w", err)
		}
		m.CreatedAt = time.Unix(createdAt, 0)
		m.RefreshedAt = time.Unix(refreshedAt, 0)
		metas = append(metas, &m)
	}
	return metas, rows.Err()
}

// Search runs a FTS5 query with automatic phrase-escaping and LIKE fallback.
// If FTS5 returns a syntax error the query is retried with LIKE; the Mode field
// on each returned Match reflects which backend served it.
func (s *SQLiteStore) Search(ctx context.Context, query, outputID string, maxResults int) ([]*Match, error) {
	ftsQuery := BuildFTS5PhraseQuery(query)
	matches, err := s.searchFTS5(ctx, ftsQuery, outputID, maxResults)
	if err == nil {
		for _, m := range matches {
			m.Mode = "fts5"
		}
		return matches, nil
	}
	if !IsFTS5SyntaxError(err) {
		return nil, err
	}
	slog.Warn("FTS5 failed, falling back to LIKE", "query", query, "error", err)
	return s.SearchLike(ctx, query, outputID, maxResults)
}

// searchFTS5 is the raw FTS5 MATCH implementation.
func (s *SQLiteStore) searchFTS5(ctx context.Context, query, outputID string, maxResults int) ([]*Match, error) {
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

// SearchLike performs a LIKE-based line scan over the FTS content column.
// Slower than FTS5 but tolerant of any input (no syntax errors).
// Returns up to maxResults matches.
func (s *SQLiteStore) SearchLike(ctx context.Context, query, outputID string, maxResults int) ([]*Match, error) {
	if maxResults <= 0 {
		maxResults = 5
	}

	sqlQuery := `
		SELECT output_id, line_no,
		       content AS snip,
		       1.0 AS rank
		FROM outputs_fts
		WHERE content LIKE ? ESCAPE '\'`
	args := []any{"%" + escapeLikePattern(query) + "%"}

	if outputID != "" {
		sqlQuery += ` AND output_id = ?`
		args = append(args, outputID)
	}
	sqlQuery += ` LIMIT ?`
	args = append(args, maxResults)

	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("LIKE search for %q: %w", query, err)
	}
	defer rows.Close()

	var matches []*Match
	for rows.Next() {
		var m Match
		var rank float64
		if err := rows.Scan(&m.OutputID, &m.Line, &m.Snippet, &rank); err != nil {
			return nil, fmt.Errorf("scanning LIKE row: %w", err)
		}
		m.Score = rank
		m.Mode = "like_fallback"
		matches = append(matches, &m)
	}
	return matches, rows.Err()
}

// escapeLikePattern escapes %, _, and \ for safe LIKE usage with ESCAPE '\'.
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
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

// SaveSessionEvent persists one hook lifecycle event.
func (s *SQLiteStore) SaveSessionEvent(ctx context.Context, event *SessionEvent) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO session_events
			(session_id, project_path, event_type, tool_name, tool_input,
			 tool_output, summary, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.SessionID,
		event.ProjectPath,
		event.EventType,
		event.ToolName,
		event.ToolInput,
		event.ToolOutput,
		event.Summary,
		event.CreatedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("inserting session event: %w", err)
	}
	return nil
}

// ListSessionEvents returns recent events for a given session (oldest first).
func (s *SQLiteStore) ListSessionEvents(ctx context.Context, sessionID string, limit int) ([]*SessionEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, project_path, event_type, tool_name,
		       tool_input, tool_output, summary, created_at
		FROM session_events
		WHERE session_id = ?
		ORDER BY created_at ASC
		LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("listing session events: %w", err)
	}
	defer rows.Close()
	return scanSessionEvents(rows)
}

// ListProjectSessionEvents returns recent events across all sessions for
// a project (oldest first), for SessionStart context restoration.
func (s *SQLiteStore) ListProjectSessionEvents(ctx context.Context, projectPath string, limit int) ([]*SessionEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, session_id, project_path, event_type, tool_name,
		       tool_input, tool_output, summary, created_at
		FROM session_events
		WHERE project_path = ?
		ORDER BY created_at DESC
		LIMIT ?`, projectPath, limit)
	if err != nil {
		return nil, fmt.Errorf("listing project session events: %w", err)
	}
	defer rows.Close()
	events, err := scanSessionEvents(rows)
	if err != nil {
		return nil, err
	}
	// Reverse to oldest-first for context presentation.
	for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
		events[i], events[j] = events[j], events[i]
	}
	return events, nil
}

func scanSessionEvents(rows *sql.Rows) ([]*SessionEvent, error) {
	var events []*SessionEvent
	for rows.Next() {
		var e SessionEvent
		var createdAt int64
		if err := rows.Scan(
			&e.ID, &e.SessionID, &e.ProjectPath, &e.EventType,
			&e.ToolName, &e.ToolInput, &e.ToolOutput, &e.Summary, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scanning session event: %w", err)
		}
		e.CreatedAt = time.Unix(createdAt, 0)
		events = append(events, &e)
	}
	return events, rows.Err()
}

// GetStats returns aggregate statistics for outputs and session events
// belonging to projectPath created at or after since.
// A zero since means no time filter.
func (s *SQLiteStore) GetStats(ctx context.Context, projectPath string, since time.Time) (*Stats, error) {
	sinceUnix := int64(0)
	if !since.IsZero() {
		sinceUnix = since.Unix()
	}

	stats := &Stats{}

	// --- outputs aggregates ---
	row := s.db.QueryRowContext(ctx, `
		SELECT
			COUNT(*),
			COALESCE(SUM(size_bytes), 0),
			COALESCE(MAX(size_bytes), 0),
			CAST(COALESCE(AVG(duration_ms), 0) AS INTEGER)
		FROM outputs
		WHERE project_path = ? AND (? = 0 OR created_at >= ?)`,
		projectPath, sinceUnix, sinceUnix)
	if err := row.Scan(&stats.OutputsStored, &stats.RawBytes, &stats.LargestBytes, &stats.AvgDurationMs); err != nil {
		return nil, fmt.Errorf("scanning output aggregates: %w", err)
	}

	// --- top commands ---
	rows, err := s.db.QueryContext(ctx, `
		SELECT command, COUNT(*) AS cnt, COALESCE(SUM(size_bytes), 0)
		FROM outputs
		WHERE project_path = ? AND (? = 0 OR created_at >= ?)
		GROUP BY command
		ORDER BY cnt DESC
		LIMIT 5`,
		projectPath, sinceUnix, sinceUnix)
	if err != nil {
		return nil, fmt.Errorf("querying top commands: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c CommandStat
		if err := rows.Scan(&c.Command, &c.Count, &c.TotalBytes); err != nil {
			return nil, fmt.Errorf("scanning command stat: %w", err)
		}
		stats.TopCommands = append(stats.TopCommands, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating command stats: %w", err)
	}

	// --- largest outputs ---
	lrows, err := s.db.QueryContext(ctx, `
		SELECT output_id, command, size_bytes, line_count
		FROM outputs
		WHERE project_path = ? AND (? = 0 OR created_at >= ?)
		ORDER BY size_bytes DESC
		LIMIT 3`,
		projectPath, sinceUnix, sinceUnix)
	if err != nil {
		return nil, fmt.Errorf("querying largest outputs: %w", err)
	}
	defer lrows.Close()
	for lrows.Next() {
		m := &OutputMeta{}
		if err := lrows.Scan(&m.OutputID, &m.Command, &m.SizeBytes, &m.LineCount); err != nil {
			return nil, fmt.Errorf("scanning largest output: %w", err)
		}
		stats.LargestOutputs = append(stats.LargestOutputs, m)
	}
	if err := lrows.Err(); err != nil {
		return nil, fmt.Errorf("iterating largest outputs: %w", err)
	}

	// --- session event counts ---
	erow := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM session_events
		WHERE project_path = ? AND (? = 0 OR created_at >= ?)`,
		projectPath, sinceUnix, sinceUnix)
	if err := erow.Scan(&stats.EventsCaptured); err != nil {
		return nil, fmt.Errorf("scanning event count: %w", err)
	}

	drow := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM session_events
		WHERE project_path = ? AND (? = 0 OR created_at >= ?) AND summary LIKE '%deny%'`,
		projectPath, sinceUnix, sinceUnix)
	if err := drow.Scan(&stats.DangerousBlocked); err != nil {
		return nil, fmt.Errorf("scanning dangerous-blocked count: %w", err)
	}

	rrow := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM session_events
		WHERE project_path = ? AND (? = 0 OR created_at >= ?) AND summary LIKE '%redirect%'`,
		projectPath, sinceUnix, sinceUnix)
	if err := rrow.Scan(&stats.RedirectedToMCP); err != nil {
		return nil, fmt.Errorf("scanning redirected count: %w", err)
	}

	// --- tool usage adherence counts ---
	// Count posttooluse events per tool_name so we can compute adherence_score.
	trows, err := s.db.QueryContext(ctx, `
		SELECT tool_name, COUNT(*)
		FROM session_events
		WHERE project_path = ?
		  AND (? = 0 OR created_at >= ?)
		  AND event_type = 'posttooluse'
		GROUP BY tool_name`,
		projectPath, sinceUnix, sinceUnix)
	if err != nil {
		return nil, fmt.Errorf("querying tool usage counts: %w", err)
	}
	defer trows.Close()
	for trows.Next() {
		var name string
		var n int
		if err := trows.Scan(&name, &n); err != nil {
			return nil, fmt.Errorf("scanning tool usage row: %w", err)
		}
		lower := strings.ToLower(name)
		switch {
		case strings.Contains(lower, "terminal") ||
			strings.Contains(lower, "bash") ||
			strings.Contains(lower, "shell"):
			stats.NativeShellCount += n
		case strings.Contains(lower, "readfile") || lower == "read":
			stats.NativeReadCount += n
		case name == "ctx_execute":
			stats.CtxExecuteCount += n
		case name == "ctx_read_file":
			stats.CtxReadFileCount += n
		}
	}
	if err := trows.Err(); err != nil {
		return nil, fmt.Errorf("iterating tool usage rows: %w", err)
	}

	return stats, nil
}

// FindRecentSameCommand returns the most recent output for the same normalised
// command within the given window. Returns nil, nil if no match is found.
func (s *SQLiteStore) FindRecentSameCommand(ctx context.Context, projectPath, command string, within time.Duration) (*OutputMeta, error) {
	normalized := NormalizeCommand(command)
	threshold := time.Now().Add(-within).Unix()

	row := s.db.QueryRowContext(ctx, `
		SELECT output_id, command, size_bytes, line_count, created_at
		FROM outputs
		WHERE project_path = ?
		  AND command = ?
		  AND created_at >= ?
		ORDER BY created_at DESC
		LIMIT 1`,
		projectPath, normalized, threshold)

	var meta OutputMeta
	var createdAt int64
	err := row.Scan(&meta.OutputID, &meta.Command, &meta.SizeBytes, &meta.LineCount, &createdAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding recent command: %w", err)
	}
	meta.CreatedAt = time.Unix(createdAt, 0)
	return &meta, nil
}

// NormalizeCommand trims and collapses whitespace so semantically-identical
// commands match. Does NOT lowercase (case matters for some commands).
func NormalizeCommand(cmd string) string {
	return strings.Join(strings.Fields(cmd), " ")
}

// SaveDecision implements Store.
func (s *SQLiteStore) SaveDecision(ctx context.Context, d *Decision) error {
	if d.DecisionID == "" {
		d.DecisionID = newDecisionID()
	}
	if d.Importance == "" {
		d.Importance = ImportanceNormal
	}
	if d.CreatedAt.IsZero() {
		d.CreatedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO decisions
			(decision_id, session_id, project_path, text, tags, links_to, importance, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		d.DecisionID,
		d.SessionID,
		d.ProjectPath,
		d.Text,
		strings.Join(d.Tags, ","),
		strings.Join(d.LinksTo, ","),
		d.Importance,
		d.CreatedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("inserting decision: %w", err)
	}
	return nil
}

// ListDecisions implements Store.
func (s *SQLiteStore) ListDecisions(ctx context.Context, opts ListDecisionsOptions) ([]*Decision, error) {
	if opts.Limit <= 0 {
		opts.Limit = 50
	}
	if opts.Limit > 200 {
		opts.Limit = 200
	}

	clauses := []string{"project_path = ?"}
	args := []any{opts.ProjectPath}

	switch opts.Scope {
	case "session":
		if opts.SessionID != "" {
			clauses = append(clauses, "session_id = ?")
			args = append(args, opts.SessionID)
		}
	case "today":
		midnight := time.Now().Truncate(24 * time.Hour).Unix()
		clauses = append(clauses, "created_at >= ?")
		args = append(args, midnight)
	case "7d":
		weekAgo := time.Now().Add(-7 * 24 * time.Hour).Unix()
		clauses = append(clauses, "created_at >= ?")
		args = append(args, weekAgo)
	case "", "all":
		// no filter
	default:
		return nil, fmt.Errorf("invalid scope %q", opts.Scope)
	}

	switch opts.MinImportance {
	case "high":
		clauses = append(clauses, "importance = 'high'")
	case "normal":
		clauses = append(clauses, "importance IN ('normal', 'high')")
	case "", "low":
		// no filter
	default:
		return nil, fmt.Errorf("invalid min_importance %q", opts.MinImportance)
	}

	if len(opts.Tags) > 0 {
		tagClauses := make([]string, 0, len(opts.Tags))
		for _, tag := range opts.Tags {
			tagClauses = append(tagClauses, "(',' || tags || ',') LIKE ?")
			args = append(args, "%,"+tag+",%")
		}
		clauses = append(clauses, "("+strings.Join(tagClauses, " OR ")+")")
	}

	query := fmt.Sprintf(`
		SELECT id, decision_id, session_id, project_path, text, tags, links_to, importance, created_at
		FROM decisions
		WHERE %s
		ORDER BY created_at DESC
		LIMIT ?
	`, strings.Join(clauses, " AND "))
	args = append(args, opts.Limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing decisions: %w", err)
	}
	defer rows.Close()

	var out []*Decision
	for rows.Next() {
		var (
			d         Decision
			tagsCSV   string
			linksCSV  string
			createdAt int64
		)
		if err := rows.Scan(
			&d.ID, &d.DecisionID, &d.SessionID, &d.ProjectPath,
			&d.Text, &tagsCSV, &linksCSV, &d.Importance, &createdAt,
		); err != nil {
			return nil, fmt.Errorf("scanning decision row: %w", err)
		}
		if tagsCSV != "" {
			d.Tags = strings.Split(tagsCSV, ",")
		}
		if linksCSV != "" {
			d.LinksTo = strings.Split(linksCSV, ",")
		}
		d.CreatedAt = time.Unix(createdAt, 0)
		out = append(out, &d)
	}
	return out, rows.Err()
}

// GetDecision implements Store.
func (s *SQLiteStore) GetDecision(ctx context.Context, decisionID string) (*Decision, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, decision_id, session_id, project_path, text, tags, links_to, importance, created_at
		FROM decisions
		WHERE decision_id = ?
	`, decisionID)

	var (
		d         Decision
		tagsCSV   string
		linksCSV  string
		createdAt int64
	)
	err := row.Scan(
		&d.ID, &d.DecisionID, &d.SessionID, &d.ProjectPath,
		&d.Text, &tagsCSV, &linksCSV, &d.Importance, &createdAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting decision: %w", err)
	}
	if tagsCSV != "" {
		d.Tags = strings.Split(tagsCSV, ",")
	}
	if linksCSV != "" {
		d.LinksTo = strings.Split(linksCSV, ",")
	}
	d.CreatedAt = time.Unix(createdAt, 0)
	return &d, nil
}

// PurgeOutputs deletes all cached outputs (and their FTS index rows) for
// projectPath. Returns the number of rows deleted from the outputs table.
func (s *SQLiteStore) PurgeOutputs(ctx context.Context, projectPath string) (int, error) {
	// Collect IDs first so we can remove FTS rows individually.
	rows, err := s.db.QueryContext(ctx,
		`SELECT output_id FROM outputs WHERE project_path = ?`, projectPath)
	if err != nil {
		return 0, fmt.Errorf("purge outputs: listing ids for %s: %w", projectPath, err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if scanErr := rows.Scan(&id); scanErr != nil {
			rows.Close()
			return 0, fmt.Errorf("purge outputs: scanning id: %w", scanErr)
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("purge outputs: iterating ids: %w", err)
	}

	// Delete FTS rows then main rows in a transaction.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("purge outputs: begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for _, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM outputs_fts WHERE output_id = ?`, id); err != nil {
			return 0, fmt.Errorf("purge outputs: removing fts rows for %s: %w", id, err)
		}
	}
	res, err := tx.ExecContext(ctx,
		`DELETE FROM outputs WHERE project_path = ?`, projectPath)
	if err != nil {
		return 0, fmt.Errorf("purge outputs: deleting outputs for %s: %w", projectPath, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("purge outputs: commit: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// PurgeEvents deletes all session events for projectPath.
// Returns the number of rows deleted.
func (s *SQLiteStore) PurgeEvents(ctx context.Context, projectPath string) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM session_events WHERE project_path = ?`, projectPath)
	if err != nil {
		return 0, fmt.Errorf("purge events: deleting events for %s: %w", projectPath, err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// PurgeNotes deletes all decision notes for projectPath.
// Returns the number of rows deleted.
func (s *SQLiteStore) PurgeNotes(ctx context.Context, projectPath string) (int, error) {
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM decisions WHERE project_path = ?`, projectPath)
	if err != nil {
		return 0, fmt.Errorf("purge notes: deleting notes for %s: %w", projectPath, err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// LastEventTime implements Store.
func (s *SQLiteStore) LastEventTime(ctx context.Context, projectPath string) (time.Time, error) {
	var ts sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		`SELECT MAX(created_at) FROM session_events WHERE project_path = ?`,
		projectPath,
	).Scan(&ts)
	if err != nil {
		return time.Time{}, fmt.Errorf("last event time: %w", err)
	}
	if !ts.Valid {
		return time.Time{}, nil
	}
	return time.Unix(ts.Int64, 0), nil
}

// LastKnowledgeRefresh implements Store by stat-ing project-knowledge.md.
// No database query — avoids any schema change.
func (s *SQLiteStore) LastKnowledgeRefresh(_ context.Context, projectPath string) (time.Time, error) {
	knowledgePath := filepath.Join(projectPath, ".ctx-saver", "project-knowledge.md")
	info, err := os.Stat(knowledgePath)
	if os.IsNotExist(err) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, fmt.Errorf("stat project-knowledge.md: %w", err)
	}
	return info.ModTime(), nil
}

// SessionCountSince implements Store.
func (s *SQLiteStore) SessionCountSince(ctx context.Context, projectPath string, since time.Time) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT session_id) FROM session_events
		 WHERE project_path = ? AND created_at > ?`,
		projectPath,
		since.Unix(),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("session count since: %w", err)
	}
	return count, nil
}

// KnowledgeStats implements Store.
func (s *SQLiteStore) KnowledgeStats(ctx context.Context, projectPath string) (*KnowledgeData, error) {
	data := &KnowledgeData{}

	// Aggregate counts.
	if err := s.roDB.QueryRowContext(ctx,
		`SELECT COUNT(DISTINCT session_id) FROM session_events WHERE project_path = ?`,
		projectPath,
	).Scan(&data.SessionCount); err != nil {
		return nil, fmt.Errorf("knowledge stats: session count: %w", err)
	}
	if err := s.roDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM outputs WHERE project_path = ?`,
		projectPath,
	).Scan(&data.OutputCount); err != nil {
		return nil, fmt.Errorf("knowledge stats: output count: %w", err)
	}
	if err := s.roDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM decisions WHERE project_path = ?`,
		projectPath,
	).Scan(&data.DecisionCount); err != nil {
		return nil, fmt.Errorf("knowledge stats: decision count: %w", err)
	}

	// Top files — read operations with a non-empty source_hash.
	fileRows, err := s.roDB.QueryContext(ctx, `
		SELECT command, COUNT(*) as reads, MAX(source_hash) as last_hash, MAX(refreshed_at) as last_refreshed
		FROM outputs
		WHERE project_path = ?
		  AND command LIKE '%read%'
		  AND source_hash != ''
		GROUP BY command
		ORDER BY reads DESC
		LIMIT 10`,
		projectPath,
	)
	if err != nil {
		return nil, fmt.Errorf("knowledge stats: top files: %w", err)
	}
	defer fileRows.Close()
	for fileRows.Next() {
		var (
			f             FileFreq
			readCount     float64
			lastHash      string
			lastRefreshed int64
		)
		if err := fileRows.Scan(&f.Path, &readCount, &lastHash, &lastRefreshed); err != nil {
			return nil, fmt.Errorf("knowledge stats: scanning file row: %w", err)
		}
		f.ReadCount = int(readCount)
		f.LastChanged = time.Unix(lastRefreshed, 0)
		f.HashStable = time.Since(f.LastChanged) > 3*24*time.Hour
		data.TopFiles = append(data.TopFiles, f)
	}
	if err := fileRows.Err(); err != nil {
		return nil, fmt.Errorf("knowledge stats: iterating file rows: %w", err)
	}

	// Top commands — non-read operations.
	cmdRows, err := s.roDB.QueryContext(ctx, `
		SELECT command, COUNT(*) as runs, AVG(size_bytes) as avg_bytes
		FROM outputs
		WHERE project_path = ?
		  AND command NOT LIKE '%read%'
		GROUP BY command
		ORDER BY runs DESC
		LIMIT 10`,
		projectPath,
	)
	if err != nil {
		return nil, fmt.Errorf("knowledge stats: top commands: %w", err)
	}
	defer cmdRows.Close()
	for cmdRows.Next() {
		var (
			c        CommandFreq
			runCount float64
			avgBytes float64
		)
		if err := cmdRows.Scan(&c.Command, &runCount, &avgBytes); err != nil {
			return nil, fmt.Errorf("knowledge stats: scanning command row: %w", err)
		}
		c.RunCount = int(runCount)
		c.AvgBytes = int64(avgBytes)
		data.TopCommands = append(data.TopCommands, c)
	}
	if err := cmdRows.Err(); err != nil {
		return nil, fmt.Errorf("knowledge stats: iterating command rows: %w", err)
	}

	// Command sequences — co-occurrence within 5-minute window.
	// Use a pre-filtered CTE (recent, max 200 rows) so the self-join is at most
	// 200×200 = 40 000 comparisons regardless of total table size.
	seqRows, err := s.roDB.QueryContext(ctx, `
		WITH recent AS (
			SELECT command, created_at
			FROM outputs
			WHERE project_path = ?
			  AND created_at > strftime('%s', 'now') - 7*24*3600
			ORDER BY created_at DESC
			LIMIT 200
		),
		pairs AS (
			SELECT a.command AS first_cmd, b.command AS second_cmd, COUNT(*) AS co_count
			FROM recent a
			JOIN recent b ON b.created_at > a.created_at
			             AND b.created_at < a.created_at + 300
			GROUP BY first_cmd, second_cmd
			HAVING co_count >= 2
		),
		totals AS (
			SELECT command, COUNT(*) AS total
			FROM recent
			GROUP BY command
		)
		SELECT p.first_cmd, p.second_cmd,
		       CAST(p.co_count * 100.0 / t.total AS REAL) AS pct
		FROM pairs p
		JOIN totals t ON t.command = p.first_cmd
		ORDER BY p.co_count DESC
		LIMIT 5`,
		projectPath,
	)
	if err != nil {
		return nil, fmt.Errorf("knowledge stats: sequences: %w", err)
	}
	defer seqRows.Close()
	for seqRows.Next() {
		var seq CmdSequence
		if err := seqRows.Scan(&seq.First, &seq.Second, &seq.Percent); err != nil {
			return nil, fmt.Errorf("knowledge stats: scanning sequence row: %w", err)
		}
		data.Sequences = append(data.Sequences, seq)
	}
	if err := seqRows.Err(); err != nil {
		return nil, fmt.Errorf("knowledge stats: iterating sequence rows: %w", err)
	}

	// High-importance decisions.
	decRows, err := s.roDB.QueryContext(ctx, `
		SELECT text, tags, created_at
		FROM decisions
		WHERE project_path = ?
		  AND importance = 'high'
		ORDER BY created_at DESC
		LIMIT 10`,
		projectPath,
	)
	if err != nil {
		return nil, fmt.Errorf("knowledge stats: decisions: %w", err)
	}
	defer decRows.Close()
	for decRows.Next() {
		var (
			d         DecisionOut
			tagsCSV   string
			createdAt int64
		)
		if err := decRows.Scan(&d.Text, &tagsCSV, &createdAt); err != nil {
			return nil, fmt.Errorf("knowledge stats: scanning decision row: %w", err)
		}
		if tagsCSV != "" {
			d.Tags = strings.Split(tagsCSV, ",")
		}
		d.CreatedAt = time.Unix(createdAt, 0)
		data.KeyDecisions = append(data.KeyDecisions, d)
	}
	if err := decRows.Err(); err != nil {
		return nil, fmt.Errorf("knowledge stats: iterating decision rows: %w", err)
	}

	return data, nil
}

// Close releases both database connections.
func (s *SQLiteStore) Close() error {
	if err := s.roDB.Close(); err != nil {
		slog.Warn("closing read-only database connection", "error", err)
	}
	return s.db.Close()
}
