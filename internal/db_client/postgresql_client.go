package dbclient

import (
	"context"
	"database/sql"
	"errors"
	"time"

	_ "github.com/lib/pq"

	"hack-go-thon/pkg/log"
)

// PostgresDatabase wraps an sql.DB connection pool for PostgreSQL.
type PostgresDatabase struct {
	Client   *sql.DB
	database string
}

// NewPostgresClient initializes a connection pool to PostgreSQL.
func NewPostgresClient(ctx context.Context, uri, dbName string) (*PostgresDatabase, error) {
	client, err := sql.Open("postgres", uri)
	if err != nil {
		log.Error("Failed to open postgres connection", "error", err.Error())
		return nil, err
	}

	// Connection pool tuning
	client.SetMaxOpenConns(25)
	client.SetMaxIdleConns(5)
	client.SetConnMaxLifetime(5 * time.Minute)

	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := client.PingContext(pingCtx); err != nil {
		log.Warn("PostgreSQL initial ping failed (will retry on request)", "error", err.Error())
	} else {
		log.Info("PostgreSQL connection established successfully", "database", dbName)
	}

	return &PostgresDatabase{
		Client:   client,
		database: dbName,
	}, nil
}

// Name implements the handler.Checker interface.
func (p *PostgresDatabase) Name() string {
	return "postgres"
}

// Check implements the handler.Checker interface for health probes.
func (p *PostgresDatabase) Check(ctx context.Context) error {
	return p.Health(ctx)
}

// Health verifies the PostgreSQL database connection is alive.
func (p *PostgresDatabase) Health(ctx context.Context) error {
	if p == nil || p.Client == nil {
		return errors.New("postgres client is nil")
	}
	return p.Client.PingContext(ctx)
}

// Close closes the underlying database connection pool.
func (p *PostgresDatabase) Close() error {
	if p != nil && p.Client != nil {
		return p.Client.Close()
	}
	return nil
}
