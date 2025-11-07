//go:build !sqlite

package floxy

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// useSQLite is false when sqlite build tag is not set (default: use PostgreSQL)
var useSQLite = false

// setupTestStore creates a PostgreSQL store using testcontainers
func setupTestStore(t *testing.T) (Store, TxManager, func()) {
	t.Helper()

	ctx := context.Background()

	// Start PostgreSQL container
	postgresContainer, err := postgres.Run(ctx,
		"postgres:17-alpine",
		postgres.WithDatabase("floxy"),
		postgres.WithUsername("user"),
		postgres.WithPassword("password"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second)),
	)
	if err != nil {
		t.Fatalf("Failed to start postgres container: %v", err)
	}

	// Get connection string
	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = postgresContainer.Terminate(ctx)
		t.Fatalf("Failed to get connection string: %v", err)
	}

	// Wait a bit for PostgreSQL to be fully ready
	time.Sleep(2 * time.Second)

	// Create connection pool with retry
	var pool *pgxpool.Pool
	for i := 0; i < 5; i++ {
		pool, err = pgxpool.New(ctx, connStr)
		if err == nil {
			break
		}
		if i < 4 {
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}
	if err != nil {
		_ = postgresContainer.Terminate(ctx)
		t.Fatalf("Failed to create connection pool: %v", err)
	}

	// Run migrations
	if err := RunMigrations(ctx, pool); err != nil {
		pool.Close()
		_ = postgresContainer.Terminate(ctx)
		t.Fatalf("Failed to run migrations: %v", err)
	}

	store := NewStore(pool)
	txManager := NewTxManager(pool)

	cleanup := func() {
		pool.Close()
		_ = postgresContainer.Terminate(context.Background())
	}

	return store, txManager, cleanup
}
