package db

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"strings"
	"time"
)

const recapSHA256HexLength = 64

// Recap lifecycle states.
const (
	RecapStatusQueued    = "queued"
	RecapStatusRunning   = "running"
	RecapStatusCompleted = "completed"
	RecapStatusFailed    = "failed"
	RecapStatusCancelled = "cancelled"
)

// RecapArtifact identifies one atomically written local-memory artifact.
type RecapArtifact struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// RecapArtifacts is the manifest grouped by artifact type.
type RecapArtifacts struct {
	Sessions      RecapArtifact `json:"sessions"`
	Questions     RecapArtifact `json:"questions"`
	ThingsToAvoid RecapArtifact `json:"things_to_avoid"`
}

// RecapRecord is the durable reservation and completion ledger for one source
// message. Source sequences identify the immutable window; times are metadata.
type RecapRecord struct {
	ID                 string
	SessionID          string
	SourceStartSeq     int
	SourceEndSeq       int
	SourceStartTime    int64
	SourceEndTime      int64
	SourceEndMessageID string
	Model              string
	Artifacts          RecapArtifacts
	Status             string
	Attempts           int
	Error              string
	TimeCreated        int64
	TimeStarted        *int64
	TimeFinished       *int64
}

const recapRecordColumns = `id, session_id, source_start_seq, source_end_seq,
source_start_time, source_end_time, source_end_message_id, model, artifacts_json,
status, attempts, error, time_created, time_started, time_finished`

// Validate checks the manifest boundary before it enters durable state.
func (a RecapArtifacts) Validate() error {
	for name, artifact := range map[string]RecapArtifact{
		"sessions":        a.Sessions,
		"questions":       a.Questions,
		"things_to_avoid": a.ThingsToAvoid,
	} {
		if artifact.Path == "" && artifact.SHA256 == "" {
			continue
		}
		if artifact.Path == "" || artifact.SHA256 == "" {
			return fmt.Errorf("db: recap artifact %s requires path and sha256", name)
		}
		clean := path.Clean(artifact.Path)
		if path.IsAbs(artifact.Path) || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("db: recap artifact %s path escapes workspace", name)
		}
		if len(artifact.SHA256) != recapSHA256HexLength {
			return fmt.Errorf("db: recap artifact %s sha256 must be 64 hex characters", name)
		}
		if _, err := hex.DecodeString(artifact.SHA256); err != nil {
			return fmt.Errorf("db: recap artifact %s sha256: %w", name, err)
		}
	}
	return nil
}

func (r RecapRecord) validate() error {
	if r.SessionID == "" {
		return fmt.Errorf("db: reserve recap: empty session id")
	}
	if r.SourceEndMessageID == "" {
		return fmt.Errorf("db: reserve recap: empty source end message id")
	}
	if r.SourceStartSeq < 0 || r.SourceEndSeq < r.SourceStartSeq {
		return fmt.Errorf("db: reserve recap: invalid source sequence range")
	}
	if strings.TrimSpace(r.Model) == "" {
		return fmt.Errorf("db: reserve recap: empty model")
	}
	return r.Artifacts.Validate()
}

// ReserveRecap atomically reserves one source window. The unique session and
// end-message key makes overlapping or retried reservations harmless. The
// bool is true only when this call inserted a new record.
func (s *Store) ReserveRecap(ctx context.Context, r RecapRecord) (RecapRecord, bool, error) {
	if err := r.validate(); err != nil {
		return RecapRecord{}, false, err
	}
	if r.ID == "" {
		r.ID = NewID("rec_")
	}
	r.Model = strings.TrimSpace(r.Model)
	r.Status = RecapStatusQueued
	r.Attempts = 0
	r.Error = ""
	now := time.Now().UnixMilli()
	if r.TimeCreated == 0 {
		r.TimeCreated = now
	}
	artifactsJSON, err := json.Marshal(r.Artifacts)
	if err != nil {
		return RecapRecord{}, false, fmt.Errorf("db: encode recap artifacts: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO recap_records (`+recapRecordColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(session_id, source_end_message_id) DO NOTHING`,
		r.ID, r.SessionID, r.SourceStartSeq, r.SourceEndSeq, r.SourceStartTime,
		r.SourceEndTime, r.SourceEndMessageID, r.Model, string(artifactsJSON), r.Status,
		r.Attempts, nil, r.TimeCreated, nil, nil)
	if err != nil {
		return RecapRecord{}, false, fmt.Errorf("db: reserve recap: %w", err)
	}
	inserted, err := result.RowsAffected()
	if err != nil {
		return RecapRecord{}, false, fmt.Errorf("db: reserve recap rows affected: %w", err)
	}
	if inserted == 1 {
		return r, true, nil
	}
	existing, err := s.getRecapBySource(ctx, r.SessionID, r.SourceEndMessageID)
	if err != nil {
		return RecapRecord{}, false, err
	}
	return existing, false, nil
}

// GetRecap returns one durable record by id.
func (s *Store) GetRecap(ctx context.Context, id string) (RecapRecord, error) {
	if id == "" {
		return RecapRecord{}, fmt.Errorf("db: get recap: empty id")
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+recapRecordColumns+` FROM recap_records WHERE id = ?`, id)
	r, err := scanRecapRecord(row)
	if err != nil {
		return RecapRecord{}, fmt.Errorf("db: get recap: %w", err)
	}
	return r, nil
}

// ClaimRecap moves a queued reservation to running and counts the attempt.
func (s *Store) ClaimRecap(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("db: claim recap: empty id")
	}
	now := time.Now().UnixMilli()
	result, err := s.db.ExecContext(ctx, `UPDATE recap_records
SET status = ?, attempts = attempts + 1, time_started = ?, time_finished = NULL, error = NULL
WHERE id = ? AND status = ?`, RecapStatusRunning, now, id, RecapStatusQueued)
	if err != nil {
		return fmt.Errorf("db: claim recap: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("db: claim recap: record %q is not queued", id)
	}
	return nil
}

// RequeueRecap makes an unfinished or failed record eligible for one more
// worker attempt. It is used during process restart recovery and explicit
// retry of a deduplicated failed source window.
func (s *Store) RequeueRecap(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("db: requeue recap: empty id")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE recap_records
SET status = ?, error = NULL, time_started = NULL, time_finished = NULL
WHERE id = ? AND status IN (?, ?, ?)`, RecapStatusQueued, id,
		RecapStatusFailed, RecapStatusRunning, RecapStatusQueued)
	if err != nil {
		return fmt.Errorf("db: requeue recap: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("db: requeue recap: record %q is not retryable", id)
	}
	return nil
}

// CompleteRecap records the validated artifact manifest after all files land.
func (s *Store) CompleteRecap(ctx context.Context, id string, artifacts RecapArtifacts) error {
	if id == "" {
		return fmt.Errorf("db: complete recap: empty id")
	}
	if err := artifacts.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(artifacts)
	if err != nil {
		return fmt.Errorf("db: encode completed recap artifacts: %w", err)
	}
	result, err := s.db.ExecContext(ctx, `UPDATE recap_records
SET status = ?, artifacts_json = ?, error = NULL, time_finished = ?
WHERE id = ? AND status = ?`, RecapStatusCompleted, string(raw), time.Now().UnixMilli(), id, RecapStatusRunning)
	if err != nil {
		return fmt.Errorf("db: complete recap: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("db: complete recap: record %q is not running", id)
	}
	return nil
}

// FailRecap records a worker failure and its completion timestamp.
func (s *Store) FailRecap(ctx context.Context, id, failure string) error {
	if id == "" {
		return fmt.Errorf("db: fail recap: empty id")
	}
	failure = strings.TrimSpace(failure)
	if failure == "" {
		return fmt.Errorf("db: fail recap: empty error")
	}
	now := time.Now().UnixMilli()
	result, err := s.db.ExecContext(ctx, `UPDATE recap_records
SET status = ?, error = ?, time_finished = ?, time_started = COALESCE(time_started, ?),
attempts = CASE WHEN status = ? THEN attempts + 1 ELSE attempts END
WHERE id = ? AND status IN (?, ?)`, RecapStatusFailed, failure, now, now, RecapStatusQueued,
		id, RecapStatusQueued, RecapStatusRunning)
	if err != nil {
		return fmt.Errorf("db: fail recap: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("db: fail recap: record %q is not open", id)
	}
	return nil
}

// CancelRecap prevents a queued or running worker from completing a record.
func (s *Store) CancelRecap(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("db: cancel recap: empty id")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE recap_records
SET status = ?, time_finished = ?
WHERE id = ? AND status IN (?, ?)`, RecapStatusCancelled, time.Now().UnixMilli(), id,
		RecapStatusQueued, RecapStatusRunning)
	if err != nil {
		return fmt.Errorf("db: cancel recap: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return fmt.Errorf("db: cancel recap: record %q is not open", id)
	}
	return nil
}

// ListOpenRecaps returns queued and running records for crash recovery.
func (s *Store) ListOpenRecaps(ctx context.Context) ([]RecapRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+recapRecordColumns+` FROM recap_records
WHERE status IN (?, ?) ORDER BY time_created ASC, id ASC`, RecapStatusQueued, RecapStatusRunning)
	if err != nil {
		return nil, fmt.Errorf("db: list open recaps: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanRecapRecords(rows)
}

// ListRecapsAfter returns records whose source window ends after seq, oldest
// source first. It is useful for avoiding duplicate recall work after resume.
func (s *Store) ListRecapsAfter(ctx context.Context, sessionID string, seq int) ([]RecapRecord, error) {
	if sessionID == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+recapRecordColumns+` FROM recap_records
WHERE session_id = ? AND source_end_seq > ? ORDER BY source_end_seq ASC, id ASC`, sessionID, seq)
	if err != nil {
		return nil, fmt.Errorf("db: list recaps after: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanRecapRecords(rows)
}

// ListRecaps returns the newest recap records for one session. A non-positive
// limit uses the store's normal bounded UI page size.
func (s *Store) ListRecaps(ctx context.Context, sessionID string, limit int) ([]RecapRecord, error) {
	if sessionID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+recapRecordColumns+` FROM recap_records
WHERE session_id = ? ORDER BY source_end_seq DESC, time_created DESC, id DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("db: list recaps: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanRecapRecords(rows)
}

func (s *Store) getRecapBySource(ctx context.Context, sessionID, messageID string) (RecapRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+recapRecordColumns+` FROM recap_records
WHERE session_id = ? AND source_end_message_id = ?`, sessionID, messageID)
	r, err := scanRecapRecord(row)
	if err != nil {
		return RecapRecord{}, fmt.Errorf("db: get recap by source: %w", err)
	}
	return r, nil
}

type recapScannable interface {
	Scan(dest ...any) error
}

func scanRecapRecord(row recapScannable) (RecapRecord, error) {
	var r RecapRecord
	var raw, errText sql.NullString
	var started, finished sql.NullInt64
	if err := row.Scan(&r.ID, &r.SessionID, &r.SourceStartSeq, &r.SourceEndSeq,
		&r.SourceStartTime, &r.SourceEndTime, &r.SourceEndMessageID, &r.Model, &raw,
		&r.Status, &r.Attempts, &errText, &r.TimeCreated, &started, &finished); err != nil {
		return RecapRecord{}, err
	}
	if err := json.Unmarshal([]byte(raw.String), &r.Artifacts); err != nil {
		return RecapRecord{}, fmt.Errorf("decode recap artifacts: %w", err)
	}
	if err := r.Artifacts.Validate(); err != nil {
		return RecapRecord{}, err
	}
	r.Error = errText.String
	if started.Valid {
		r.TimeStarted = &started.Int64
	}
	if finished.Valid {
		r.TimeFinished = &finished.Int64
	}
	return r, nil
}

func scanRecapRecords(rows *sql.Rows) ([]RecapRecord, error) {
	var out []RecapRecord
	for rows.Next() {
		r, err := scanRecapRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("db: scan recap: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list recaps: %w", err)
	}
	return out, nil
}
