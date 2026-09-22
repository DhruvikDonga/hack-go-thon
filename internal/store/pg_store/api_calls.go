package pgstore

import (
	"context"
	"errors"
	dbclient "hack-go-thon/internal/db_client"
	"time"
)

// APICallModel represents an audit log record of an API invocation in PostgreSQL.
type APICallModel struct {
	ID                 int64     `json:"id"`
	UserID             string    `json:"user_id"`
	APIEndPoint        string    `json:"api_end_point"`
	HTTPMethod         string    `json:"http_method"`
	ResponseStatusCode int       `json:"response_status_code"`
	LatencyMs          int64     `json:"latency_ms"`
	ClientIP           string    `json:"client_ip"`
	CreatedAt          time.Time `json:"created_at"`
}

// InitAPICallSchema ensures the api_calls audit table exists.
func InitAPICallSchema(ctx context.Context, db *dbclient.PostgresDatabase) error {
	if db == nil || db.Client == nil {
		return errors.New("db client is nil")
	}

	query := `
	CREATE TABLE IF NOT EXISTS api_calls (
		id BIGSERIAL PRIMARY KEY,
		user_id VARCHAR(64) DEFAULT '',
		api_end_point VARCHAR(255) NOT NULL,
		http_method VARCHAR(10) NOT NULL,
		response_status_code INT NOT NULL,
		latency_ms BIGINT NOT NULL,
		client_ip VARCHAR(50) DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	`
	_, err := db.Client.ExecContext(ctx, query)
	return err
}

// InsertAPICall records an API invocation asynchronously.
func InsertAPICall(ctx context.Context, db *dbclient.PostgresDatabase, call *APICallModel) error {
	if db == nil || db.Client == nil {
		return errors.New("db client is nil")
	}

	query := `
		INSERT INTO api_calls (
			user_id, api_end_point, http_method, response_status_code, latency_ms, client_ip, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW());
	`
	_, err := db.Client.ExecContext(
		ctx,
		query,
		call.UserID,
		call.APIEndPoint,
		call.HTTPMethod,
		call.ResponseStatusCode,
		call.LatencyMs,
		call.ClientIP,
	)
	return err
}
