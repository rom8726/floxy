package floxy

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/url"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/lib/pq"
)

const dbSchema = "workflows"

//go:embed migrations/*.sql
var migrationFiles embed.FS

// RunMigrations executes all migration files in order
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	fmt.Println("[floxy] up migrations...")

	_, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+dbSchema)
	if err != nil {
		return err
	}

	connString := pool.Config().ConnString()
	connStringURL, err := url.Parse(connString)
	if err != nil {
		return fmt.Errorf("parsing connection string: %w", err)
	}

	values := connStringURL.Query()
	values.Set("search_path", dbSchema) // set db schema
	connStringURL.RawQuery = values.Encode()
	connString = connStringURL.String()

	driver, err := iofs.New(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("open migration files: %w", err)
	}

	migrator, err := migrate.NewWithSourceInstance("iofs", driver, connString)
	if err != nil {
		return fmt.Errorf("init migrator: %w", err)
	}

	if err := migrator.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			fmt.Println("[floxy] up migrations: no changes")

			return nil
		}

		return fmt.Errorf("run migrations: %w", err)
	}

	fmt.Println("[floxy] up migrations done")

	return nil
}
