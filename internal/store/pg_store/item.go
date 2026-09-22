package pgstore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	dbclient "hack-go-thon/internal/db_client"
)

// ItemModel represents a persistent item record in PostgreSQL.
type ItemModel struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// InitSchema automatically ensures the required tables exist.
func InitSchema(ctx context.Context, db *dbclient.PostgresDatabase) error {
	if db == nil || db.Client == nil {
		return errors.New("db client is nil")
	}

	query := `
	CREATE TABLE IF NOT EXISTS items (
		id VARCHAR(64) PRIMARY KEY,
		title VARCHAR(255) NOT NULL,
		description TEXT,
		status VARCHAR(50) NOT NULL DEFAULT 'active',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	`
	_, err := db.Client.ExecContext(ctx, query)
	return err
}

// CreateItem inserts a new item record into the database.
func CreateItem(ctx context.Context, db *dbclient.PostgresDatabase, item *ItemModel) error {
	if db == nil || db.Client == nil {
		return errors.New("db client is nil")
	}

	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = time.Now().UTC()
	}
	if item.Status == "" {
		item.Status = "active"
	}

	query := `
		INSERT INTO items (
			id, title, description, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6);
	`

	_, err := db.Client.ExecContext(
		ctx,
		query,
		item.ID,
		item.Title,
		item.Description,
		item.Status,
		item.CreatedAt,
		item.UpdatedAt,
	)

	return err
}

// GetItemByID retrieves an item by its primary key ID.
func GetItemByID(ctx context.Context, db *dbclient.PostgresDatabase, id string) (*ItemModel, error) {
	if db == nil || db.Client == nil {
		return nil, errors.New("db client is nil")
	}

	query := `
		SELECT id, title, description, status, created_at, updated_at
		FROM items
		WHERE id = $1
		LIMIT 1;
	`

	var item ItemModel
	err := db.Client.QueryRowContext(ctx, query, id).Scan(
		&item.ID,
		&item.Title,
		&item.Description,
		&item.Status,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // Not found
		}
		return nil, err
	}

	return &item, nil
}

// ListItems fetches items with pagination.
func ListItems(ctx context.Context, db *dbclient.PostgresDatabase, limit, offset int) ([]ItemModel, error) {
	if db == nil || db.Client == nil {
		return nil, errors.New("db client is nil")
	}

	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT id, title, description, status, created_at, updated_at
		FROM items
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2;
	`

	rows, err := db.Client.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]ItemModel, 0)
	for rows.Next() {
		var item ItemModel
		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.Description,
			&item.Status,
			&item.CreatedAt,
			&item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

// DeleteItem removes an item record by ID.
func DeleteItem(ctx context.Context, db *dbclient.PostgresDatabase, id string) error {
	if db == nil || db.Client == nil {
		return errors.New("db client is nil")
	}

	query := `DELETE FROM items WHERE id = $1;`
	_, err := db.Client.ExecContext(ctx, query, id)
	return err
}
