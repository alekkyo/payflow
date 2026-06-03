package main

import (
	"errors"
	"log/slog"
	"os"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		slog.Error("DATABASE_URL not set")
		os.Exit(1)
	}

	// golang-migrate expects the pgx5:// scheme for the pgx/v5 driver.
	migrateURL := "pgx5://" + databaseURL[len("postgres://"):]

	m, err := migrate.New("file://migrations", migrateURL)
	if err != nil {
		slog.Error("creating migrator", "error", err)
		os.Exit(1)
	}
	defer m.Close()

	direction := "up"
	if len(os.Args) > 1 {
		direction = os.Args[1]
	}

	switch direction {
	case "up":
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			slog.Error("running migrations up", "error", err)
			os.Exit(1)
		}
		slog.Info("migrations applied")
	case "down":
		if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			slog.Error("running migrations down", "error", err)
			os.Exit(1)
		}
		slog.Info("migrations rolled back")
	default:
		slog.Error("unknown direction; use 'up' or 'down'", "direction", direction)
		os.Exit(1)
	}
}
