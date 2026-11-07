package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Brohammad/Arden/internal/api"
	"github.com/Brohammad/Arden/internal/config"
	"github.com/Brohammad/Arden/internal/migrate"
	"github.com/Brohammad/Arden/internal/store"
)

const usageText = `usage: arden <command>

  arden api
  arden migrate up
  arden migrate down

Environment:
  DATABASE_URL   Postgres connection string (required)
  ADDR           API listen address (default :8080)
`

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log, os.Args[1:]); err != nil {
		if errors.Is(err, errUsage) {
			fmt.Fprint(os.Stderr, usageText)
			os.Exit(2)
		}
		log.Error("arden stopped", "error", err)
		os.Exit(1)
	}
}

var errUsage = errors.New("usage")

func run(log *slog.Logger, args []string) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "api":
		if len(args) != 1 {
			return errUsage
		}
		return runAPI(log)
	case "migrate":
		if len(args) != 2 {
			return errUsage
		}
		return runMigrate(log, args[1])
	default:
		return errUsage
	}
}

func runAPI(log *slog.Logger) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	st, err := store.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           api.NewHandler(log, st),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Info("api listening", "addr", cfg.Addr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func runMigrate(log *slog.Logger, direction string) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	switch direction {
	case "up":
		if err := migrate.Up(cfg.DatabaseURL); err != nil {
			return err
		}
	case "down":
		if err := migrate.Down(cfg.DatabaseURL); err != nil {
			return err
		}
	default:
		return errUsage
	}
	log.Info("migrations applied", "direction", direction)
	return nil
}
