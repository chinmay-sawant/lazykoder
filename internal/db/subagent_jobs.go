package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const subagentJobColumns = `id, parent_session_id, parent_part_id, child_session_id, name, role, status,
prompt, description, model, variant, max_steps, timeout_ms, summary, error,
time_created, time_updated, time_started, time_finished`

// UpsertSubagentJob inserts or replaces a durable job row.
func (s *Store) UpsertSubagentJob(ctx context.Context, j SubagentJob) error {
	if j.ID == "" {
		return fmt.Errorf("db: upsert subagent job: empty id")
	}
	if j.ParentSessionID == "" {
		return fmt.Errorf("db: upsert subagent job: empty parent_session_id")
	}
	now := time.Now().UnixMilli()
	if j.TimeCreated == 0 {
		j.TimeCreated = now
	}
	if j.TimeUpdated == 0 {
		j.TimeUpdated = now
	}
	if j.Status == "" {
		j.Status = "queued"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO subagent_jobs (`+subagentJobColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  parent_session_id = excluded.parent_session_id,
  parent_part_id = excluded.parent_part_id,
  child_session_id = excluded.child_session_id,
  name = excluded.name,
  role = excluded.role,
  status = excluded.status,
  prompt = excluded.prompt,
  description = excluded.description,
  model = excluded.model,
  variant = excluded.variant,
  max_steps = excluded.max_steps,
  timeout_ms = excluded.timeout_ms,
  summary = excluded.summary,
  error = excluded.error,
  time_updated = excluded.time_updated,
  time_started = excluded.time_started,
  time_finished = excluded.time_finished
WHERE subagent_jobs.status IN ('queued', 'running')`,
		j.ID, j.ParentSessionID, nullIfEmpty(j.ParentPartID), nullIfEmpty(j.ChildSessionID),
		j.Name, j.Role, j.Status, j.Prompt, j.Description, nullIfEmpty(j.Model), nullIfEmpty(j.Variant),
		j.MaxSteps, j.TimeoutMS, nullIfEmpty(j.Summary), nullIfEmpty(j.Error),
		j.TimeCreated, j.TimeUpdated, nullIfZero(j.TimeStarted), nullIfZero(j.TimeFinished),
	)
	if err != nil {
		return fmt.Errorf("db: upsert subagent job: %w", err)
	}
	return nil
}

// CancelSubagentJob conditionally marks an open durable job as cancelled.
// Terminal rows are immutable so a late recovery or worker write cannot
// resurrect or replace their outcome.
func (s *Store) CancelSubagentJob(ctx context.Context, id string) (bool, error) {
	now := time.Now().UnixMilli()
	result, err := s.db.ExecContext(ctx, `UPDATE subagent_jobs
SET status = 'cancelled', error = 'subagent: cancelled', time_updated = ?, time_finished = ?
WHERE id = ? AND status IN ('queued', 'running')`, now, now, id)
	if err != nil {
		return false, fmt.Errorf("db: cancel subagent job: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("db: count cancelled subagent job: %w", err)
	}
	return changed > 0, nil
}

// GetSubagentJob returns one job by id.
func (s *Store) GetSubagentJob(ctx context.Context, id string) (SubagentJob, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+subagentJobColumns+` FROM subagent_jobs WHERE id = ?`, id)
	j, err := scanSubagentJob(row)
	if err != nil {
		return SubagentJob{}, fmt.Errorf("db: get subagent job: %w", err)
	}
	return j, nil
}

// ListSubagentJobs returns all jobs for a parent session, oldest first.
func (s *Store) ListSubagentJobs(ctx context.Context, parentSessionID string) ([]SubagentJob, error) {
	if parentSessionID == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+subagentJobColumns+` FROM subagent_jobs
WHERE parent_session_id = ?
ORDER BY COALESCE(time_started, time_created) ASC, id ASC`, parentSessionID)
	if err != nil {
		return nil, fmt.Errorf("db: list subagent jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanSubagentJobs(rows)
}

// ListOpenSubagentJobs returns queued/running jobs (for crash recovery).
func (s *Store) ListOpenSubagentJobs(ctx context.Context) ([]SubagentJob, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+subagentJobColumns+` FROM subagent_jobs
WHERE status IN ('queued', 'running')
ORDER BY time_created ASC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("db: list open subagent jobs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanSubagentJobs(rows)
}

type scannable interface {
	Scan(dest ...any) error
}

func scanSubagentJob(row scannable) (SubagentJob, error) {
	var j SubagentJob
	var parentPart, child, model, variant, summary, errText sql.NullString
	var started, finished sql.NullInt64
	if err := row.Scan(
		&j.ID, &j.ParentSessionID, &parentPart, &child, &j.Name, &j.Role, &j.Status,
		&j.Prompt, &j.Description, &model, &variant, &j.MaxSteps, &j.TimeoutMS,
		&summary, &errText, &j.TimeCreated, &j.TimeUpdated, &started, &finished,
	); err != nil {
		return SubagentJob{}, err
	}
	j.ParentPartID = parentPart.String
	j.ChildSessionID = child.String
	j.Model = model.String
	j.Variant = variant.String
	j.Summary = summary.String
	j.Error = errText.String
	if started.Valid {
		j.TimeStarted = started.Int64
	}
	if finished.Valid {
		j.TimeFinished = finished.Int64
	}
	return j, nil
}

func scanSubagentJobs(rows *sql.Rows) ([]SubagentJob, error) {
	var out []SubagentJob
	for rows.Next() {
		j, err := scanSubagentJob(rows)
		if err != nil {
			return nil, fmt.Errorf("db: scan subagent job: %w", err)
		}
		out = append(out, j)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("db: list subagent jobs: %w", err)
	}
	return out, nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullIfZero(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}
