package dbclient

import (
	"context"
	"testing"
)

func TestPostgresDatabase(t *testing.T) {
	t.Run("Nil Client Health", func(t *testing.T) {
		var db *PostgresDatabase
		if err := db.Health(context.Background()); err == nil {
			t.Errorf("expected error for nil database")
		}
	})

	t.Run("Name and Check", func(t *testing.T) {
		db := &PostgresDatabase{database: "testdb"}
		if db.Name() != "postgres" {
			t.Errorf("expected name 'postgres', got '%s'", db.Name())
		}
		if err := db.Check(context.Background()); err == nil {
			t.Errorf("expected error for nil client inside struct")
		}
	})
}
