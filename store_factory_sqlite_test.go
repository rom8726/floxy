//go:build sqlite
// +build sqlite

package floxy

import (
	"testing"
)

// useSQLite is true when sqlite build tag is set
var useSQLite = true

// setupTestStore creates a SQLite in-memory store
func setupTestStore(t *testing.T) (Store, TxManager, func()) {
	t.Helper()

	store, err := NewSQLiteInMemoryStore()
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
