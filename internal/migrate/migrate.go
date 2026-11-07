package migrate

import (
	"errors"
	"fmt"

	"github.com/Brohammad/Arden/db"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// Up applies all pending migrations.
func Up(databaseURL string) error {
	return run(databaseURL, func(m *migrate.Migrate) error { return m.Up() })
}

// Down rolls back every applied migration.
func Down(databaseURL string) error {
	return run(databaseURL, func(m *migrate.Migrate) error { return m.Down() })
}

func run(databaseURL string, apply func(*migrate.Migrate) error) error {
	src, err := iofs.New(db.Migrations, "migrations")
	if err != nil {
		return fmt.Errorf("open migrations: %w", err)
	}

	cfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse database url: %w", err)
	}
	// The migration file contains functions, so it must run as one script.
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	sqlDB := stdlib.OpenDB(*cfg)
	driver, err := postgres.WithInstance(sqlDB, &postgres.Config{})
	if err != nil {
		sqlDB.Close()
		return fmt.Errorf("open migrate driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return fmt.Errorf("create migrator: %w", err)
	}
	defer m.Close()

	if err := apply(m); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}
