package pgstore

import (
	"context"
	"errors"
	dbclient "hack-go-thon/internal/db_client"
	"time"
)

type UploadedFileModel struct {
	ID            string    `json:"id"`
	OriginalName  string    `json:"original_name"`
	StoredName    string    `json:"stored_name"`
	SizeBytes     int64     `json:"size_bytes"`
	SizeFormatted string    `json:"size_formatted"`
	MimeType      string    `json:"mime_type"`
	Category      string    `json:"category"`
	Description   string    `json:"description"`
	SHA256        string    `json:"sha256"`
	URL           string    `json:"url"`
	DownloadURL   string    `json:"download_url"`
	UploadedAt    time.Time `json:"uploaded_at"`
}

func InitUploadedFilesSchema(ctx context.Context, db *dbclient.PostgresDatabase) error {
	if db == nil || db.Client == nil {
		return errors.New("db client is nil")
	}

	query := `
	CREATE TABLE IF NOT EXISTS uploaded_files (
		id VARCHAR(100) PRIMARY KEY,
		original_name VARCHAR(255) NOT NULL,
		stored_name VARCHAR(255) NOT NULL UNIQUE,
		size_bytes BIGINT NOT NULL,
		size_formatted VARCHAR(50) NOT NULL,
		mime_type VARCHAR(100) NOT NULL,
		category VARCHAR(100) DEFAULT '',
		description TEXT DEFAULT '',
		sha256 VARCHAR(64) NOT NULL,
		url VARCHAR(500) NOT NULL,
		download_url VARCHAR(500) NOT NULL,
		uploaded_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	`
	_, err := db.Client.ExecContext(ctx, query)
	return err
}

func InsertUploadedFile(ctx context.Context, db *dbclient.PostgresDatabase, file *UploadedFileModel) error {
	if db == nil || db.Client == nil {
		return errors.New("db client is nil")
	}

	query := `
		INSERT INTO uploaded_files (
			id, original_name, stored_name, size_bytes, size_formatted, mime_type, 
			category, description, sha256, url, download_url, uploaded_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12);
	`
	_, err := db.Client.ExecContext(ctx, query,
		file.ID, file.OriginalName, file.StoredName, file.SizeBytes, file.SizeFormatted,
		file.MimeType, file.Category, file.Description, file.SHA256, file.URL, file.DownloadURL, file.UploadedAt,
	)
	return err
}

func GetUploadedFileByStoredName(ctx context.Context, db *dbclient.PostgresDatabase, storedName string) (*UploadedFileModel, error) {
	if db == nil || db.Client == nil {
		return nil, errors.New("db client is nil")
	}

	query := `SELECT id, original_name, stored_name, size_bytes, size_formatted, mime_type, category, description, sha256, url, download_url, uploaded_at FROM uploaded_files WHERE stored_name = $1`
	row := db.Client.QueryRowContext(ctx, query, storedName)

	var f UploadedFileModel
	err := row.Scan(&f.ID, &f.OriginalName, &f.StoredName, &f.SizeBytes, &f.SizeFormatted, &f.MimeType, &f.Category, &f.Description, &f.SHA256, &f.URL, &f.DownloadURL, &f.UploadedAt)
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func ListUploadedFiles(ctx context.Context, db *dbclient.PostgresDatabase, category string) ([]*UploadedFileModel, error) {
	if db == nil || db.Client == nil {
		return nil, errors.New("db client is nil")
	}

	var query string
	var args []interface{}

	if category != "" {
		query = `SELECT id, original_name, stored_name, size_bytes, size_formatted, mime_type, category, description, sha256, url, download_url, uploaded_at FROM uploaded_files WHERE category = $1 ORDER BY uploaded_at DESC`
		args = append(args, category)
	} else {
		query = `SELECT id, original_name, stored_name, size_bytes, size_formatted, mime_type, category, description, sha256, url, download_url, uploaded_at FROM uploaded_files ORDER BY uploaded_at DESC`
	}

	rows, err := db.Client.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []*UploadedFileModel
	for rows.Next() {
		var f UploadedFileModel
		if err := rows.Scan(&f.ID, &f.OriginalName, &f.StoredName, &f.SizeBytes, &f.SizeFormatted, &f.MimeType, &f.Category, &f.Description, &f.SHA256, &f.URL, &f.DownloadURL, &f.UploadedAt); err != nil {
			return nil, err
		}
		files = append(files, &f)
	}
	return files, nil
}

func DeleteUploadedFile(ctx context.Context, db *dbclient.PostgresDatabase, storedName string) error {
	if db == nil || db.Client == nil {
		return errors.New("db client is nil")
	}
	query := `DELETE FROM uploaded_files WHERE stored_name = $1`
	_, err := db.Client.ExecContext(ctx, query, storedName)
	return err
}
