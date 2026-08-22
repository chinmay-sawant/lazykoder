package db

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// Memory update lifecycle states.
const (
	MemoryUpdateStatusQueued    = "queued"
	MemoryUpdateStatusRunning   = "running"
	MemoryUpdateStatusCompleted = "completed"
	MemoryUpdateStatusFailed    = "failed"
)

// MemoryUpdate is the durable idempotency ledger for one project memory
// refresh. It deliberately does not foreign-key the source session because
// memories.md belongs to the project and survives session deletion.
type MemoryUpdate struct {
	ID                 string
	Workdir            string
	SourceSessionID    string
	SourceEndSeq       int
	SourceEndMessageID string
	Model              string
	Status             string
	Attempts           int
	SHA256             string
	Error              string
	TimeCreated        int64
	TimeStarted        *int64
	TimeFinished       *int64
}

const memoryUpdateColumns = `id, workdir, source_session_id, source_end_seq,
source_end_message_id, model, status, attempts, sha256, error, time_created,
time_started, time_finished`

func (u MemoryUpdate) validate() error {
	if strings.TrimSpace(u.Workdir) == "" {
		return fmt.Errorf("db: reserve memory update: empty workdir")
	}
	if strings.TrimSpace(u.SourceSessionID) == "" {
		return fmt.Errorf("db: reserve memory update: empty source session id")
	}
	if u.SourceEndSeq <= 0 {
		return fmt.Errorf("db: reserve memory update: invalid source sequence")
	}
	if strings.TrimSpace(u.SourceEndMessageID) == "" {
		return fmt.Errorf("db: reserve memory update: empty source end message id")
	}
	if strings.TrimSpace(u.Model) == "" {
		return fmt.Errorf("db: reserve memory update: empty model")
	}
	return nil
}

func validateMemoryDigest(value string) error {
	if value == "" {
		return nil
	}
	if len(value) != recapSHA256HexLength {
		return fmt.Errorf("db: memory update sha256 must be 64 hex characters")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return fmt.Errorf("db: memory update sha256: %w", err)
	}
	return nil
}

// ReserveMemoryUpdate reserves one project memory source anchor. Replayed
// completion events return the existing row instead of creating another job.
func (s *Store) ReserveMemoryUpdate(ctx context.Context, update MemoryUpdate) (MemoryUpdate, bool, error) {
	if err := update.validate(); err != nil {
		return MemoryUpdate{}, false, err
	}
	if update.ID == "" {
		update.ID = NewID("mem_")
	}
	update.Status = MemoryUpdateStatusQueued
	update.Model = strings.TrimSpace(update.Model)
	update.Attempts = 0
	update.SHA256 = ""
	update.Error = ""
	if update.TimeCreated == 0 {
		update.TimeCreated = time.Now().UnixMilli()
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO memory_updates (`+memoryUpdateColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(workdir, source_session_id, source_end_message_id) DO NOTHING`,
		update.ID, update.Workdir, update.SourceSessionID, update.SourceEndSeq,
		update.SourceEndMessageID, update.Model, update.Status, update.Attempts, nil,
		nil, update.TimeCreated, nil, nil)
	if err != nil {
		return MemoryUpdate{}, false, fmt.Errorf("db: reserve memory update: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return MemoryUpdate{}, false, fmt.Errorf("db: reserve memory update rows affected: %w", err)
	}
	if inserted == 1 {
		return update, true, nil
	}
	existing, err := s.getMemoryUpdateBySource(ctx, update.Workdir, update.SourceSessionID, update.SourceEndMessageID)
	if err != nil {
		return MemoryUpdate{}, false, err
	}
	return existing, false, nil
}

// GetMemoryUpdate returns one memory update by id.
func (s *Store) GetMemoryUpdate(ctx context.Context, id string) (MemoryUpdate, error) {
	if strings.TrimSpace(id) == "" {
		return MemoryUpdate{}, fmt.Errorf("db: get memory update: empty id")
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+memoryUpdateColumns+` FROM memory_updates WHERE id = ?`, id)
	update, err := scanMemoryUpdate(row)
	if err != nil {
		return MemoryUpdate{}, fmt.Errorf("db: get memory update: %w", err)
	}
	return update, nil
}

// ClaimMemoryUpdate moves a queued update to running and counts the attempt.
func (s *Store) ClaimMemoryUpdate(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("db: claim memory update: empty id")
	}
	now := time.Now().UnixMilli()
	result, err := s.db.ExecContext(ctx, `UPDATE memory_updates
SET status = ?, attempts = attempts + 1, time_started = ?, time_finished = NULL,
sha256 = NULL, error = NULL
WHERE id = ? AND status = ?`, MemoryUpdateStatusRunning, now, id, MemoryUpdateStatusQueued)
	if err != nil {
		return fmt.Errorf("db: claim memory update: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("db: claim memory update: record %q is not queued", id)
	}
	return nil
}

// RequeueMemoryUpdate makes an interrupted or failed update retryable.
func (s *Store) RequeueMemoryUpdate(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("db: requeue memory update: empty id")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE memory_updates
SET status = ?, time_started = NULL, time_finished = NULL, sha256 = NULL, error = NULL
WHERE id = ? AND status IN (?, ?, ?)`, MemoryUpdateStatusQueued, id,
		MemoryUpdateStatusQueued, MemoryUpdateStatusRunning, MemoryUpdateStatusFailed)
	if err != nil {
		return fmt.Errorf("db: requeue memory update: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("db: requeue memory update: record %q is not retryable", id)
	}
	return nil
}

// CompleteMemoryUpdate records the digest after memories.md is atomically
// replaced.
func (s *Store) CompleteMemoryUpdate(ctx context.Context, id, digest string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("db: complete memory update: empty id")
	}
	if err := validateMemoryDigest(digest); err != nil {
		return err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE memory_updates
SET status = ?, sha256 = ?, error = NULL, time_finished = ?
WHERE id = ? AND status = ?`, MemoryUpdateStatusCompleted, digest,
		time.Now().UnixMilli(), id, MemoryUpdateStatusRunning)
	if err != nil {
		return fmt.Errorf("db: complete memory update: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("db: complete memory update: record %q is not running", id)
	}
	return nil
}

// FailMemoryUpdate records a retryable worker failure.
func (s *Store) FailMemoryUpdate(ctx context.Context, id, failure string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("db: fail memory update: empty id")
	}
	failure = strings.TrimSpace(failure)
	if failure == "" {
		return fmt.Errorf("db: fail memory update: empty error")
	}
	now := time.Now().UnixMilli()
	result, err := s.db.ExecContext(ctx, `UPDATE memory_updates
SET status = ?, error = ?, time_finished = ?, time_started = COALESCE(time_started, ?)
WHERE id = ? AND status IN (?, ?)`, MemoryUpdateStatusFailed, failure, now, now, id,
		MemoryUpdateStatusQueued, MemoryUpdateStatusRunning)
	if err != nil {
		return fmt.Errorf("db: fail memory update: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("db: fail memory update: record %q is not open", id)
	}
	return nil
}

// ListOpenMemoryUpdates returns queued and running updates for recovery.
func (s *Store) ListOpenMemoryUpdates(ctx context.Context, workdir string) ([]MemoryUpdate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+memoryUpdateColumns+` FROM memory_updates
WHERE workdir = ? AND status IN (?, ?) ORDER BY source_end_seq ASC, time_created ASC, id ASC`,
		workdir, MemoryUpdateStatusQueued, MemoryUpdateStatusRunning)
	if err != nil {
		return nil, fmt.Errorf("db: list open memory updates: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanMemoryUpdates(rows)
}

// ListMemoryUpdatesForRecovery returns open updates plus failures caused only
// by an insufficient context window. Those failures can become valid after a
// later session message and are safe to retry without retrying provider or
// validation failures on every launch.
func (s *Store) ListMemoryUpdatesForRecovery(ctx context.Context, workdir string) ([]MemoryUpdate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+memoryUpdateColumns+` FROM memory_updates
WHERE workdir = ? AND (status IN (?, ?) OR (status = ? AND error LIKE ?))
ORDER BY source_end_seq ASC, time_created ASC, id ASC`, workdir,
		MemoryUpdateStatusQueued, MemoryUpdateStatusRunning, MemoryUpdateStatusFailed,
		"recap: fewer than four complete messages%")
	if err != nil {
		return nil, fmt.Errorf("db: list memory updates for recovery: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanMemoryUpdates(rows)
}

func (s *Store) getMemoryUpdateBySource(ctx context.Context, workdir, sessionID, messageID string) (MemoryUpdate, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+memoryUpdateColumns+` FROM memory_updates
WHERE workdir = ? AND source_session_id = ? AND source_end_message_id = ?`, workdir, sessionID, messageID)
	update, err := scanMemoryUpdate(row)
	if err != nil {
		return MemoryUpdate{}, fmt.Errorf("db: get memory update by source: %w", err)
	}
	return update, nil
}

type memoryUpdateScannable interface {
	Scan(dest ...any) error
}

func scanMemoryUpdate(row memoryUpdateScannable) (MemoryUpdate, error) {
	var update MemoryUpdate
	var sha, errText sql.NullString
	var started, finished sql.NullInt64
	if err := row.Scan(&update.ID, &update.Workdir, &update.SourceSessionID,
		&update.SourceEndSeq, &update.SourceEndMessageID, &update.Model, &update.Status,
		&update.Attempts, &sha, &errText, &update.TimeCreated, &started, &finished); err != nil {
		return MemoryUpdate{}, err
	}
	update.SHA256 = sha.String
	update.Error = errText.String
	if started.Valid {
		update.TimeStarted = &started.Int64
	}
	if finished.Valid {
		update.TimeFinished = &finished.Int64
	}
	return update, nil
}

func scanMemoryUpdates(rows *sql.Rows) ([]MemoryUpdate, error) {
	updates := make([]MemoryUpdate, 0)
	for rows.Next() {
		update, err := scanMemoryUpdate(rows)
		if err != nil {
			return nil, fmt.Errorf("db: scan memory update: %w", err)
		}
		updates = append(updates, update)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list memory updates: %w", err)
	}
	return updates, nil
}
