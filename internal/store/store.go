package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultMaxConns = 10

// Store reads and writes jobs.
type Store struct {
	pool *pgxpool.Pool
}

// Open connects to Postgres.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = defaultMaxConns
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return New(pool), nil
}

// New wraps an existing pool. Close closes that pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Close closes the connection pool.
func (s *Store) Close() {
	s.pool.Close()
}

// Ping checks that the database accepts a query.
func (s *Store) Ping(ctx context.Context) error {
	if err := s.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

var (
	// ErrNotFound means the job id does not exist.
	ErrNotFound = errors.New("job not found")
	// ErrConflict means the job is not in a state that allows the operation.
	ErrConflict = errors.New("invalid job state transition")
	// ErrIdempotencyMismatch means the key was reused with a different request.
	ErrIdempotencyMismatch = errors.New("idempotency key reused with a different request")
)
