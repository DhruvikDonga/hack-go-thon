package pgstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	dbclient "hack-go-thon/internal/db_client"
)

// WebhookModel represents a webhook subscription persisted in PostgreSQL.
type WebhookModel struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	Events      []string  `json:"events"`
	Secret      string    `json:"secret"`
	Description string    `json:"description"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// WebhookDeliveryLogModel represents an HTTP delivery attempt logged in PostgreSQL.
type WebhookDeliveryLogModel struct {
	ID             string    `json:"id"`
	WebhookID      string    `json:"webhook_id"`
	URL            string    `json:"url"`
	Event          string    `json:"event"`
	StatusCode     int       `json:"status_code"`
	DurationMs     int64     `json:"duration_ms"`
	Success        bool      `json:"success"`
	Error          string    `json:"error"`
	PayloadPreview string    `json:"payload_preview"`
	CreatedAt      time.Time `json:"created_at"`
}

// InitWebhookSchema creates the webhook_subscriptions and webhook_delivery_logs tables and indexes.
func InitWebhookSchema(ctx context.Context, db *dbclient.PostgresDatabase) error {
	if db == nil || db.Client == nil {
		return errors.New("database client is nil")
	}

	query := `
	CREATE TABLE IF NOT EXISTS webhook_subscriptions (
		id VARCHAR(64) PRIMARY KEY,
		url TEXT NOT NULL,
		events JSONB NOT NULL DEFAULT '["*"]'::jsonb,
		secret TEXT NOT NULL DEFAULT '',
		description TEXT NOT NULL DEFAULT '',
		active BOOLEAN NOT NULL DEFAULT TRUE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_webhook_subscriptions_active ON webhook_subscriptions(active);

	CREATE TABLE IF NOT EXISTS webhook_delivery_logs (
		id VARCHAR(64) PRIMARY KEY,
		webhook_id VARCHAR(64) NOT NULL DEFAULT '',
		url TEXT NOT NULL,
		event VARCHAR(100) NOT NULL,
		status_code INT NOT NULL DEFAULT 0,
		duration_ms BIGINT NOT NULL DEFAULT 0,
		success BOOLEAN NOT NULL DEFAULT FALSE,
		error TEXT NOT NULL DEFAULT '',
		payload_preview TEXT NOT NULL DEFAULT '',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_webhook_logs_created_at ON webhook_delivery_logs(created_at DESC);
	`
	_, err := db.Client.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to init webhooks schema: %w", err)
	}
	return nil
}

// CreateWebhookSubscription inserts a new webhook subscription into PostgreSQL.
func CreateWebhookSubscription(ctx context.Context, db *dbclient.PostgresDatabase, sub *WebhookModel) error {
	if db == nil || db.Client == nil {
		return errors.New("database client is nil")
	}

	eventsJSON, err := json.Marshal(sub.Events)
	if err != nil {
		eventsJSON = []byte(`["*"]`)
	}

	now := time.Now().UTC()
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = now
	}
	if sub.UpdatedAt.IsZero() {
		sub.UpdatedAt = now
	}

	query := `
	INSERT INTO webhook_subscriptions (id, url, events, secret, description, active, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err = db.Client.ExecContext(ctx, query,
		sub.ID,
		sub.URL,
		eventsJSON,
		sub.Secret,
		sub.Description,
		sub.Active,
		sub.CreatedAt,
		sub.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to create webhook subscription: %w", err)
	}
	return nil
}

// ListWebhookSubscriptions queries all webhook subscriptions from PostgreSQL.
func ListWebhookSubscriptions(ctx context.Context, db *dbclient.PostgresDatabase) ([]*WebhookModel, error) {
	if db == nil || db.Client == nil {
		return nil, errors.New("database client is nil")
	}

	query := `
	SELECT id, url, events, secret, description, active, created_at, updated_at
	FROM webhook_subscriptions
	ORDER BY created_at DESC
	`
	rows, err := db.Client.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query webhook subscriptions: %w", err)
	}
	defer rows.Close()

	var result []*WebhookModel
	for rows.Next() {
		var sub WebhookModel
		var eventsRaw []byte

		if err := rows.Scan(
			&sub.ID,
			&sub.URL,
			&eventsRaw,
			&sub.Secret,
			&sub.Description,
			&sub.Active,
			&sub.CreatedAt,
			&sub.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan webhook subscription: %w", err)
		}

		if len(eventsRaw) > 0 {
			_ = json.Unmarshal(eventsRaw, &sub.Events)
		}
		if len(sub.Events) == 0 {
			sub.Events = []string{"*"}
		}
		result = append(result, &sub)
	}

	return result, rows.Err()
}

// GetWebhookSubscription retrieves a specific webhook subscription by ID.
func GetWebhookSubscription(ctx context.Context, db *dbclient.PostgresDatabase, id string) (*WebhookModel, error) {
	if db == nil || db.Client == nil {
		return nil, errors.New("database client is nil")
	}

	query := `
	SELECT id, url, events, secret, description, active, created_at, updated_at
	FROM webhook_subscriptions
	WHERE id = $1
	`
	var sub WebhookModel
	var eventsRaw []byte

	err := db.Client.QueryRowContext(ctx, query, id).Scan(
		&sub.ID,
		&sub.URL,
		&eventsRaw,
		&sub.Secret,
		&sub.Description,
		&sub.Active,
		&sub.CreatedAt,
		&sub.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get webhook subscription: %w", err)
	}

	if len(eventsRaw) > 0 {
		_ = json.Unmarshal(eventsRaw, &sub.Events)
	}
	if len(sub.Events) == 0 {
		sub.Events = []string{"*"}
	}
	return &sub, nil
}

// UpdateWebhookSubscription updates an existing webhook subscription in PostgreSQL.
func UpdateWebhookSubscription(ctx context.Context, db *dbclient.PostgresDatabase, sub *WebhookModel) error {
	if db == nil || db.Client == nil {
		return errors.New("database client is nil")
	}

	eventsJSON, err := json.Marshal(sub.Events)
	if err != nil {
		eventsJSON = []byte(`["*"]`)
	}
	sub.UpdatedAt = time.Now().UTC()

	query := `
	UPDATE webhook_subscriptions
	SET url = $2, events = $3, secret = $4, description = $5, active = $6, updated_at = $7
	WHERE id = $1
	`
	res, err := db.Client.ExecContext(ctx, query,
		sub.ID,
		sub.URL,
		eventsJSON,
		sub.Secret,
		sub.Description,
		sub.Active,
		sub.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to update webhook subscription: %w", err)
	}

	rows, _ := res.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteWebhookSubscription deletes a webhook subscription from PostgreSQL.
func DeleteWebhookSubscription(ctx context.Context, db *dbclient.PostgresDatabase, id string) error {
	if db == nil || db.Client == nil {
		return errors.New("database client is nil")
	}

	query := `DELETE FROM webhook_subscriptions WHERE id = $1`
	res, err := db.Client.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete webhook subscription: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// InsertWebhookDeliveryLog inserts an HTTP delivery log record into PostgreSQL.
func InsertWebhookDeliveryLog(ctx context.Context, db *dbclient.PostgresDatabase, log *WebhookDeliveryLogModel) error {
	if db == nil || db.Client == nil {
		return errors.New("database client is nil")
	}

	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now().UTC()
	}

	query := `
	INSERT INTO webhook_delivery_logs (id, webhook_id, url, event, status_code, duration_ms, success, error, payload_preview, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := db.Client.ExecContext(ctx, query,
		log.ID,
		log.WebhookID,
		log.URL,
		log.Event,
		log.StatusCode,
		log.DurationMs,
		log.Success,
		log.Error,
		log.PayloadPreview,
		log.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert webhook delivery log: %w", err)
	}
	return nil
}

// ListWebhookDeliveryLogs returns the most recent delivery logs from PostgreSQL.
func ListWebhookDeliveryLogs(ctx context.Context, db *dbclient.PostgresDatabase, limit int) ([]*WebhookDeliveryLogModel, error) {
	if db == nil || db.Client == nil {
		return nil, errors.New("database client is nil")
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}

	query := `
	SELECT id, webhook_id, url, event, status_code, duration_ms, success, error, payload_preview, created_at
	FROM webhook_delivery_logs
	ORDER BY created_at DESC
	LIMIT $1
	`
	rows, err := db.Client.QueryContext(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query webhook delivery logs: %w", err)
	}
	defer rows.Close()

	var result []*WebhookDeliveryLogModel
	for rows.Next() {
		var l WebhookDeliveryLogModel
		if err := rows.Scan(
			&l.ID,
			&l.WebhookID,
			&l.URL,
			&l.Event,
			&l.StatusCode,
			&l.DurationMs,
			&l.Success,
			&l.Error,
			&l.PayloadPreview,
			&l.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan webhook delivery log: %w", err)
		}
		result = append(result, &l)
	}
	return result, rows.Err()
}
