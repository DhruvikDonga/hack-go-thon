package pgstore

import (
	"context"
	"database/sql"
	"errors"
	dbclient "hack-go-thon/internal/db_client"
	"time"
)

// APIKeyModel represents an API key record in PostgreSQL.
type APIKeyModel struct {
	ID        int64     `json:"id"`
	UserID    string    `json:"user_id"`
	Name      string    `json:"name"`
	APISecret string    `json:"api_secret"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// InitAPIKeySchema ensures the api_keys table is created.
func InitAPIKeySchema(ctx context.Context, db *dbclient.PostgresDatabase) error {
	if db == nil || db.Client == nil {
		return errors.New("db client is nil")
	}

	query := `
	CREATE TABLE IF NOT EXISTS api_keys (
		id BIGSERIAL PRIMARY KEY,
		user_id VARCHAR(64) NOT NULL,
		name VARCHAR(100) NOT NULL,
		api_secret VARCHAR(255) UNIQUE NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	`
	_, err := db.Client.ExecContext(ctx, query)
	return err
}

// CreateAPIKey inserts a new API key record.
func CreateAPIKey(ctx context.Context, db *dbclient.PostgresDatabase, key *APIKeyModel) (int64, error) {
	if db == nil || db.Client == nil {
		return 0, errors.New("db client is nil")
	}

	query := `
		INSERT INTO api_keys (user_id, name, api_secret, created_at, updated_at)
		VALUES ($1, $2, $3, NOW(), NOW())
		RETURNING id;
	`
	err := db.Client.QueryRowContext(ctx, query, key.UserID, key.Name, key.APISecret).Scan(&key.ID)
	return key.ID, err
}

// GetAPIKeyBySecret retrieves an API key by its secret token.
func GetAPIKeyBySecret(ctx context.Context, db *dbclient.PostgresDatabase, secret string) (*APIKeyModel, error) {
	if db == nil || db.Client == nil {
		return nil, errors.New("db client is nil")
	}

	query := `
		SELECT id, user_id, name, api_secret, created_at, updated_at
		FROM api_keys
		WHERE api_secret = $1
		LIMIT 1;
	`

	var key APIKeyModel
	err := db.Client.QueryRowContext(ctx, query, secret).Scan(
		&key.ID,
		&key.UserID,
		&key.Name,
		&key.APISecret,
		&key.CreatedAt,
		&key.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &key, nil
}
