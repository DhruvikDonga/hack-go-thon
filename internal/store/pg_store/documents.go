package pgstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	dbclient "hack-go-thon/internal/db_client"
)

// DocumentModel represents a document indexed in PostgreSQL with an embedding vector.
type DocumentModel struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Content   string         `json:"content"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Embedding []float32      `json:"embedding,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// DocumentSearchResult contains a matched document and its cosine similarity score.
type DocumentSearchResult struct {
	ID         string         `json:"id"`
	Title      string         `json:"title"`
	Content    string         `json:"content"`
	Metadata   map[string]any `json:"metadata,omitempty"`
	Similarity float64        `json:"similarity"`
	CreatedAt  time.Time      `json:"created_at"`
}

// InitDocumentSchema enables the pgvector extension and creates the documents table with an HNSW index.
func InitDocumentSchema(ctx context.Context, db *dbclient.PostgresDatabase) error {
	if db == nil || db.Client == nil {
		return errors.New("db client is nil")
	}

	// 1. Enable pgvector extension
	if _, err := db.Client.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS vector;"); err != nil {
		return fmt.Errorf("failed to enable vector extension: %w", err)
	}

	// 2. Create documents table with 1536-dimension vector column
	schemaQuery := `
	CREATE TABLE IF NOT EXISTS documents (
		id VARCHAR(64) PRIMARY KEY,
		title VARCHAR(255) NOT NULL,
		content TEXT NOT NULL,
		metadata JSONB DEFAULT '{}',
		embedding vector(1536),
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	`
	if _, err := db.Client.ExecContext(ctx, schemaQuery); err != nil {
		return fmt.Errorf("failed to create documents table: %w", err)
	}

	// 3. Create HNSW index for high-speed approximate nearest neighbor cosine similarity search
	indexQuery := `
	CREATE INDEX IF NOT EXISTS documents_embedding_hnsw_idx 
	ON documents USING hnsw (embedding vector_cosine_ops);
	`
	if _, err := db.Client.ExecContext(ctx, indexQuery); err != nil {
		// HNSW might fail if vector extension or version doesn't support it, but log non-fatal or return
		return fmt.Errorf("failed to create documents hnsw index: %w", err)
	}

	return nil
}

// FormatVector serializes a float32 slice into the PostgreSQL vector literal string format "[v1,v2,v3...]".
func FormatVector(vec []float32) string {
	if len(vec) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, v := range vec {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(v), 'f', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}

// ParseVector deserializes a PostgreSQL vector literal string "[v1,v2,v3...]" into a float32 slice.
func ParseVector(s string) ([]float32, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if len(s) == 0 {
		return nil, nil
	}

	parts := strings.Split(s, ",")
	vec := make([]float32, len(parts))
	for i, p := range parts {
		val, err := strconv.ParseFloat(strings.TrimSpace(p), 32)
		if err != nil {
			return nil, fmt.Errorf("invalid vector component %q: %w", p, err)
		}
		vec[i] = float32(val)
	}
	return vec, nil
}

// CreateDocument inserts a new document and its vector embedding into PostgreSQL.
func CreateDocument(ctx context.Context, db *dbclient.PostgresDatabase, doc *DocumentModel) error {
	if db == nil || db.Client == nil {
		return errors.New("db client is nil")
	}

	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now().UTC()
	}

	metaBytes, err := json.Marshal(doc.Metadata)
	if err != nil {
		metaBytes = []byte("{}")
	}

	var vecStr any
	if len(doc.Embedding) > 0 {
		vecStr = FormatVector(doc.Embedding)
	} else {
		vecStr = nil
	}

	query := `
		INSERT INTO documents (
			id, title, content, metadata, embedding, created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err = db.Client.ExecContext(
		ctx,
		query,
		doc.ID,
		doc.Title,
		doc.Content,
		string(metaBytes),
		vecStr,
		doc.CreatedAt,
	)
	return err
}

// SearchSimilarDocuments retrieves top-k documents ranked by cosine similarity using the `<=>` distance operator.
func SearchSimilarDocuments(ctx context.Context, db *dbclient.PostgresDatabase, queryEmbedding []float32, limit int) ([]*DocumentSearchResult, error) {
	if db == nil || db.Client == nil {
		return nil, errors.New("db client is nil")
	}
	if limit <= 0 {
		limit = 5
	}
	if len(queryEmbedding) == 0 {
		return nil, errors.New("query embedding cannot be empty")
	}

	vecStr := FormatVector(queryEmbedding)

	query := `
		SELECT id, title, content, COALESCE(metadata, '{}'::jsonb),
		       1 - (embedding <=> $1) AS similarity, created_at
		FROM documents
		WHERE embedding IS NOT NULL
		ORDER BY embedding <=> $1
		LIMIT $2;
	`

	rows, err := db.Client.QueryContext(ctx, query, vecStr, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query similar documents: %w", err)
	}
	defer rows.Close()

	var results []*DocumentSearchResult
	for rows.Next() {
		var item DocumentSearchResult
		var metaRaw []byte
		var sim sql.NullFloat64

		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.Content,
			&metaRaw,
			&sim,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan document row: %w", err)
		}

		if sim.Valid {
			item.Similarity = sim.Float64
		}
		if len(metaRaw) > 0 {
			_ = json.Unmarshal(metaRaw, &item.Metadata)
		}

		results = append(results, &item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	return results, nil
}

// ListDocuments returns a paginated list of stored documents.
func ListDocuments(ctx context.Context, db *dbclient.PostgresDatabase, limit, offset int) ([]*DocumentModel, error) {
	if db == nil || db.Client == nil {
		return nil, errors.New("db client is nil")
	}
	if limit <= 0 {
		limit = 20
	}

	query := `
		SELECT id, title, content, COALESCE(metadata, '{}'::jsonb), created_at
		FROM documents
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2;
	`

	rows, err := db.Client.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("failed to list documents: %w", err)
	}
	defer rows.Close()

	var docs []*DocumentModel
	for rows.Next() {
		var doc DocumentModel
		var metaRaw []byte

		if err := rows.Scan(
			&doc.ID,
			&doc.Title,
			&doc.Content,
			&metaRaw,
			&doc.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan document: %w", err)
		}

		if len(metaRaw) > 0 {
			_ = json.Unmarshal(metaRaw, &doc.Metadata)
		}
		docs = append(docs, &doc)
	}

	return docs, nil
}

// GetDocumentByID retrieves a single document by its ID.
func GetDocumentByID(ctx context.Context, db *dbclient.PostgresDatabase, id string) (*DocumentModel, error) {
	if db == nil || db.Client == nil {
		return nil, errors.New("db client is nil")
	}

	query := `
		SELECT id, title, content, COALESCE(metadata, '{}'::jsonb), created_at
		FROM documents
		WHERE id = $1;
	`

	row := db.Client.QueryRowContext(ctx, query, id)
	var doc DocumentModel
	var metaRaw []byte

	if err := row.Scan(
		&doc.ID,
		&doc.Title,
		&doc.Content,
		&metaRaw,
		&doc.CreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get document: %w", err)
	}

	if len(metaRaw) > 0 {
		_ = json.Unmarshal(metaRaw, &doc.Metadata)
	}
	return &doc, nil
}

// DeleteDocument removes a document from the database.
func DeleteDocument(ctx context.Context, db *dbclient.PostgresDatabase, id string) error {
	if db == nil || db.Client == nil {
		return errors.New("db client is nil")
	}

	query := `DELETE FROM documents WHERE id = $1;`
	_, err := db.Client.ExecContext(ctx, query, id)
	return err
}
