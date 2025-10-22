package floxy

import (
	"context"
	"embed"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// RunMigrations executes all migration files in order
func RunMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	// Get all SQL files from migrations directory
	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations directory: %w", err)
	}

	// Sort files by name to ensure correct order
	var files []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, entry.Name())
		}
	}

	// Execute each migration file
	for _, file := range files {
		content, err := migrationFiles.ReadFile("migrations/" + file)
		if err != nil {
			return fmt.Errorf("read migration file %s: %w", file, err)
		}

		_, err = pool.Exec(ctx, string(content))
		if err != nil {
			return fmt.Errorf("execute migration %s: %w", file, err)
		}
	}

	return nil
}
