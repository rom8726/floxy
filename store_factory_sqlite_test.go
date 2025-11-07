//go:build sqlite

package floxy

import (
	"testing"
)

// useSQLite is true when sqlite build tag is set
var useSQLite = true

// setupTestStore creates a SQLite in-memory store
func setupTestStore(t *testing.T) (Store, TxManager, func()) {
	t.Helper()

	// Create a temporary file for the database
	tmpFile := t.TempDir() + "/test.db"

	store, err := NewSQLiteStore(tmpFile)
	if err != nil {
		t.Fatalf("Failed to create SQLite store: %v", err)
	}

	txManager := NewMemoryTxManager()

	cleanup := func() {
		// SQLite in-memory store doesn't need explicit cleanup
		// The database is automatically destroyed when the connection is closed
	}

	return store, txManager, cleanup
}
