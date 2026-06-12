package store

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
)

const (
	evictionBatchSize  = 25
	recentOutputWindow = time.Hour
)

func (s *SQLiteStore) touchOutput(outputID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := s.db.ExecContext(
		ctx,
		`UPDATE outputs SET last_accessed_at = ? WHERE output_id = ?`,
		time.Now().Unix(),
		outputID,
	); err != nil {
		slog.Debug("touching output access time failed", "output_id", outputID, "error", err)
	}
}

func (s *SQLiteStore) enforceConfiguredSize() error {
	if s.maxDBSizeBytes <= 0 || s.dbFile == "" || s.readOnly {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return s.enforceSizeLimit(ctx)
}

func (s *SQLiteStore) enforceSizeLimit(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(PASSIVE)`); err != nil {
		return fmt.Errorf("checkpointing before size enforcement: %w", err)
	}
	initialSize, err := databaseFileSize(s.dbFile)
	if err != nil {
		return err
	}
	if initialSize <= s.maxDBSizeBytes {
		return nil
	}

	target := s.maxDBSizeBytes * 9 / 10
	totalDeleted := 0
	for {
		currentSize, sizeErr := databaseFileSize(s.dbFile)
		if sizeErr != nil {
			return sizeErr
		}
		if currentSize <= target {
			break
		}

		ids, queryErr := s.evictionCandidates(ctx, time.Now().Add(-recentOutputWindow), evictionBatchSize)
		if queryErr != nil {
			return queryErr
		}
		if len(ids) == 0 {
			slog.Warn(
				"database remains above configured size because all outputs are recent",
				"size_bytes", currentSize,
				"target_bytes", target,
			)
			break
		}
		if deleteErr := s.deleteOutputBatch(ctx, ids); deleteErr != nil {
			return deleteErr
		}
		totalDeleted += len(ids)
		if _, vacuumErr := s.db.ExecContext(ctx, `PRAGMA incremental_vacuum`); vacuumErr != nil {
			return fmt.Errorf("running incremental vacuum: %w", vacuumErr)
		}
		if _, checkpointErr := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); checkpointErr != nil {
			return fmt.Errorf("checkpointing after eviction: %w", checkpointErr)
		}
	}

	if totalDeleted > 0 {
		finalSize, _ := databaseFileSize(s.dbFile)
		slog.Info(
			"evicted cached outputs to enforce database size",
			"outputs", totalDeleted,
			"bytes_reclaimed", initialSize-finalSize,
			"size_bytes", finalSize,
		)
	}
	return nil
}

func (s *SQLiteStore) evictionCandidates(ctx context.Context, cutoff time.Time, limit int) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT output_id
		FROM outputs
		WHERE last_accessed_at < ? AND refreshed_at < ?
		ORDER BY last_accessed_at ASC, created_at ASC
		LIMIT ?`,
		cutoff.Unix(), cutoff.Unix(), limit,
	)
	if err != nil {
		return nil, fmt.Errorf("querying eviction candidates: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scanning eviction candidate: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating eviction candidates: %w", err)
	}
	return ids, nil
}

func (s *SQLiteStore) deleteOutputBatch(ctx context.Context, ids []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning eviction transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM outputs_fts WHERE output_id = ?`, id); err != nil {
			return fmt.Errorf("deleting FTS rows for evicted output %s: %w", id, err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM outputs WHERE output_id = ?`, id); err != nil {
			return fmt.Errorf("deleting evicted output %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing output eviction: %w", err)
	}
	return nil
}

func databaseFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("stating database file %s: %w", path, err)
	}
	return info.Size(), nil
}
