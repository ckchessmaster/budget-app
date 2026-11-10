package db

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var MigrationFS embed.FS

type Migrator interface {
	Up() error
	Down() error
	Version() (uint, bool, error)
}

type pgxMigrator struct {
	m *migrate.Migrate
}

func NewMigrator(databaseURL string) (Migrator, error) {
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		databaseURL = "postgres://postgres:postgres@localhost:5432/budgetdb?sslmode=disable"
	}

	// golang-migrate pgx/v5 driver expects pgx5:// scheme
	dbURL := databaseURL
	if strings.HasPrefix(dbURL, "postgres://") {
		dbURL = "pgx5://" + strings.TrimPrefix(dbURL, "postgres://")
	} else if strings.HasPrefix(dbURL, "postgresql://") {
		dbURL = "pgx5://" + strings.TrimPrefix(dbURL, "postgresql://")
	}

	// Add lock timeout if not specified (abort after 15s if lock cannot be acquired)
	if !strings.Contains(dbURL, "lock_timeout=") {
		sep := "?"
		if strings.Contains(dbURL, "?") {
			sep = "&"
		}
		dbURL = fmt.Sprintf("%s%slock_timeout=15000", dbURL, sep)
	}

	sourceDriver, err := iofs.New(MigrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to create migration source driver: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", sourceDriver, dbURL)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize migrator: %w", err)
	}

	// Set lock timeout on migrate instance if supported
	m.LockTimeout = 15 * time.Second

	return &pgxMigrator{m: m}, nil
}

func (p *pgxMigrator) Up() error {
	errChan := make(chan error, 1)
	go func() {
		err := p.m.Up()
		if err != nil && !errors.Is(err, migrate.ErrNoChange) {
			errChan <- err
			return
		}
		errChan <- nil
	}()

	select {
	case err := <-errChan:
		return err
	case <-time.After(15 * time.Second):
		return errors.New("migration lock acquisition timed out after 15s")
	}
}

func (p *pgxMigrator) Down() error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		err := p.m.Down()
		if err != nil && !errors.Is(err, migrate.ErrNoChange) {
			errChan <- err
			return
		}
		errChan <- nil
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return errors.New("migration down timed out after 15s")
	}
}

func (p *pgxMigrator) Version() (uint, bool, error) {
	return p.m.Version()
}
