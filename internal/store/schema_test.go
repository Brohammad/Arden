package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Brohammad/Arden/internal/testutil"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	pool, cleanup, err := testutil.StartPostgres(ctx)
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "postgres: %v\n", err)
		os.Exit(1)
	}
	testPool = pool
	code := m.Run()
	cleanup()
	os.Exit(code)
}

func TestSchemaInvariants(t *testing.T) {
	ctx := context.Background()
	reset(t)

	t.Run("reject insert running", func(t *testing.T) {
		_, err := testPool.Exec(ctx, `INSERT INTO jobs (payload, status) VALUES ('{}', 'running')`)
		if err == nil || !contains(err, "new jobs must be pending") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("reject pending to succeeded", func(t *testing.T) {
		id := insertPending(t, 5)
		_, err := testPool.Exec(ctx, `UPDATE jobs SET status = 'succeeded' WHERE id = $1::uuid`, id)
		if err == nil || !contains(err, "invalid job status transition") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("reject running without lease", func(t *testing.T) {
		id := insertPending(t, 5)
		_, err := testPool.Exec(ctx, `
			UPDATE jobs
			SET status = 'running', attempt = 1, fencing_token = 1
			WHERE id = $1::uuid`, id)
		if err == nil || !contains(err, "jobs_lease_consistency") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("claim retry exhaustion and replay", func(t *testing.T) {
		id := insertPending(t, 1)
		workerID := insertWorker(t)

		_, err := testPool.Exec(ctx, `
			UPDATE jobs
			SET status = 'running',
			    attempt = 1,
			    fencing_token = 1,
			    lease_owner = $2::uuid,
			    lease_expires_at = now() + interval '30 seconds'
			WHERE id = $1::uuid`, id, workerID)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}

		_, err = testPool.Exec(ctx, `
			UPDATE jobs
			SET status = 'pending', lease_owner = NULL, lease_expires_at = NULL
			WHERE id = $1::uuid`, id)
		if err == nil || !contains(err, "attempts exhausted") {
			t.Fatalf("exhausted retry error = %v", err)
		}

		_, err = testPool.Exec(ctx, `
			UPDATE jobs
			SET status = 'dead', lease_owner = NULL, lease_expires_at = NULL
			WHERE id = $1::uuid`, id)
		if err != nil {
			t.Fatalf("dead letter: %v", err)
		}

		_, err = testPool.Exec(ctx, `UPDATE jobs SET status = 'pending', attempt = 0 WHERE id = $1::uuid`, id)
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
	})
}

func insertPending(t *testing.T, maxAttempts int) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO jobs (payload, max_attempts)
		VALUES ('{"n":1}', $1)
		RETURNING id::text`, maxAttempts).Scan(&id)
	if err != nil {
		t.Fatalf("insert job: %v", err)
	}
	return id
}

func insertWorker(t *testing.T) string {
	t.Helper()
	var id string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO workers (hostname)
		VALUES ('worker-a')
		RETURNING id::text`).Scan(&id)
	if err != nil {
		t.Fatalf("insert worker: %v", err)
	}
	return id
}

func reset(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	for _, stmt := range []string{
		`DELETE FROM job_attempts`,
		`DELETE FROM jobs`,
		`DELETE FROM workers`,
	} {
		if _, err := testPool.Exec(ctx, stmt); err != nil {
			t.Fatalf("reset %s: %v", stmt, err)
		}
	}
}

func contains(err error, text string) bool {
	return err != nil && strings.Contains(err.Error(), text)
}
