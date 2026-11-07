package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Brohammad/Arden/internal/job"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const jobColumns = `
	id::text,
	queue,
	payload,
	status,
	priority,
	scheduled_at,
	attempt,
	max_attempts,
	lease_owner::text,
	lease_expires_at,
	fencing_token,
	idempotency_key,
	last_error,
	result,
	schedule_id::text,
	occurrence_at,
	created_at,
	updated_at`

// ListFilter selects a page of jobs ordered by created_at DESC, id DESC.
type ListFilter struct {
	Status          *job.Status
	Queue           *string
	CursorCreatedAt *time.Time
	CursorID        *uuid.UUID
	Limit           int
}

// Page is one list result. HasMore means another row exists after Jobs.
type Page struct {
	Jobs    []job.Job
	HasMore bool
}

// CreateJob inserts a job. created is false when an idempotency key replayed an existing row.
func (s *Store) CreateJob(ctx context.Context, sub job.Submission) (job.Job, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return job.Job{}, false, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		INSERT INTO jobs (queue, payload, priority, scheduled_at, max_attempts, idempotency_key)
		VALUES ($1, $2::jsonb, $3, COALESCE($4::timestamptz, now()), $5, $6)
		ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
		RETURNING `+jobColumns,
		sub.Queue,
		string(sub.Payload),
		sub.Priority,
		sub.ScheduledAt,
		sub.MaxAttempts,
		sub.IdempotencyKey,
	)

	createdJob, err := scanJob(row)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return job.Job{}, false, fmt.Errorf("commit insert: %w", err)
		}
		return createdJob, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return job.Job{}, false, fmt.Errorf("insert job: %w", err)
	}
	if sub.IdempotencyKey == nil {
		return job.Job{}, false, fmt.Errorf("insert job: no row returned")
	}

	existing, err := scanJob(tx.QueryRow(ctx, `
		SELECT `+jobColumns+`
		FROM jobs
		WHERE idempotency_key = $1
		  AND queue = $2
		  AND priority = $3
		  AND max_attempts = $4
		  AND payload = $5::jsonb
		  AND (NOT $6::bool OR scheduled_at = $7::timestamptz)`,
		*sub.IdempotencyKey,
		sub.Queue,
		sub.Priority,
		sub.MaxAttempts,
		string(sub.Payload),
		sub.ScheduledAtWasSet,
		sub.ScheduledAt,
	))
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return job.Job{}, false, fmt.Errorf("commit replay: %w", err)
		}
		return existing, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return job.Job{}, false, fmt.Errorf("load idempotent job: %w", err)
	}

	var found bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM jobs WHERE idempotency_key = $1)`, *sub.IdempotencyKey).Scan(&found); err != nil {
		return job.Job{}, false, fmt.Errorf("check idempotency key: %w", err)
	}
	if !found {
		return job.Job{}, false, fmt.Errorf("idempotency conflict did not resolve")
	}
	return job.Job{}, false, ErrIdempotencyMismatch
}

// GetJob loads one job by id.
func (s *Store) GetJob(ctx context.Context, id uuid.UUID) (job.Job, error) {
	found, err := scanJob(s.pool.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = $1::uuid`, id.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return job.Job{}, ErrNotFound
	}
	if err != nil {
		return job.Job{}, fmt.Errorf("get job: %w", err)
	}
	return found, nil
}

// ListJobs returns a page of jobs.
func (s *Store) ListJobs(ctx context.Context, filter ListFilter) (Page, error) {
	limit := filter.Limit
	if limit < 1 {
		limit = 50
	}
	var status *string
	if filter.Status != nil {
		value := string(*filter.Status)
		status = &value
	}
	var cursorID *string
	if filter.CursorID != nil {
		value := filter.CursorID.String()
		cursorID = &value
	}

	rows, err := s.pool.Query(ctx, `
		SELECT `+jobColumns+`
		FROM jobs
		WHERE ($1::text IS NULL OR status = $1::job_status)
		  AND ($2::text IS NULL OR queue = $2)
		  AND (
		    $3::timestamptz IS NULL
		    OR (created_at, id) < ($3::timestamptz, $4::uuid)
		  )
		ORDER BY created_at DESC, id DESC
		LIMIT $5`,
		status,
		filter.Queue,
		filter.CursorCreatedAt,
		cursorID,
		limit+1,
	)
	if err != nil {
		return Page{}, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]job.Job, 0, limit)
	for rows.Next() {
		found, err := scanJob(rows)
		if err != nil {
			return Page{}, fmt.Errorf("scan job: %w", err)
		}
		jobs = append(jobs, found)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("list jobs: %w", err)
	}

	page := Page{Jobs: jobs}
	if len(jobs) > limit {
		page.Jobs = jobs[:limit]
		page.HasMore = true
	}
	return page, nil
}

// CancelJob moves a pending job to cancelled.
// Cancelling a job that is already cancelled returns that job.
func (s *Store) CancelJob(ctx context.Context, id uuid.UUID) (job.Job, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return job.Job{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	current, err := scanJob(tx.QueryRow(ctx, `SELECT `+jobColumns+` FROM jobs WHERE id = $1::uuid FOR UPDATE`, id.String()))
	if errors.Is(err, pgx.ErrNoRows) {
		return job.Job{}, ErrNotFound
	}
	if err != nil {
		return job.Job{}, fmt.Errorf("lock job: %w", err)
	}
	if current.Status == job.StatusCancelled {
		return current, nil
	}
	if !job.CanTransition(current.Status, job.StatusCancelled) {
		return job.Job{}, ErrConflict
	}

	updated, err := scanJob(tx.QueryRow(ctx, `
		UPDATE jobs
		SET status = 'cancelled'
		WHERE id = $1::uuid
		RETURNING `+jobColumns, id.String()))
	if err != nil {
		return job.Job{}, fmt.Errorf("cancel job: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return job.Job{}, fmt.Errorf("commit cancel: %w", err)
	}
	return updated, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanJob(row scanner) (job.Job, error) {
	var (
		found      job.Job
		idText     string
		payload    []byte
		status     string
		leaseOwner *string
		result     []byte
		scheduleID *string
	)
	if err := row.Scan(
		&idText,
		&found.Queue,
		&payload,
		&status,
		&found.Priority,
		&found.ScheduledAt,
		&found.Attempt,
		&found.MaxAttempts,
		&leaseOwner,
		&found.LeaseExpiresAt,
		&found.FencingToken,
		&found.IdempotencyKey,
		&found.LastError,
		&result,
		&scheduleID,
		&found.OccurrenceAt,
		&found.CreatedAt,
		&found.UpdatedAt,
	); err != nil {
		return job.Job{}, err
	}

	id, err := uuid.Parse(idText)
	if err != nil {
		return job.Job{}, fmt.Errorf("parse id: %w", err)
	}
	found.ID = id
	found.Payload = json.RawMessage(payload)
	found.Status = job.Status(status)
	found.Result = result
	if leaseOwner != nil {
		parsed, err := uuid.Parse(*leaseOwner)
		if err != nil {
			return job.Job{}, fmt.Errorf("parse lease owner: %w", err)
		}
		found.LeaseOwner = &parsed
	}
	if scheduleID != nil {
		parsed, err := uuid.Parse(*scheduleID)
		if err != nil {
			return job.Job{}, fmt.Errorf("parse schedule id: %w", err)
		}
		found.ScheduleID = &parsed
	}
	return found, nil
}
