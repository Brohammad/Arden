package testutil

import (
	"context"
	"fmt"

	"github.com/Brohammad/Arden/internal/migrate"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

// StartPostgres launches Postgres 17, applies migrations, and returns a pool.
// cleanup terminates the container and closes the pool.
func StartPostgres(ctx context.Context) (*pgxpool.Pool, func(), error) {
	container, err := postgres.Run(ctx, "postgres:17-alpine",
		postgres.WithDatabase("arden"),
		postgres.WithUsername("arden"),
		postgres.WithPassword("arden"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("start postgres: %w", err)
	}
	terminate := func() {
		_ = container.Terminate(context.Background())
	}

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		terminate()
		return nil, nil, fmt.Errorf("connection string: %w", err)
	}
	if err := migrate.Up(connStr); err != nil {
		terminate()
		return nil, nil, fmt.Errorf("migrate: %w", err)
	}
	if err := migrate.Up(connStr); err != nil {
		terminate()
		return nil, nil, fmt.Errorf("migrate again: %w", err)
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		terminate()
		return nil, nil, fmt.Errorf("pool: %w", err)
	}
	return pool, func() {
		pool.Close()
		terminate()
	}, nil
}
