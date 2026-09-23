package pgstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	dbclient "hack-go-thon/internal/db_client"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// UserModel represents a user record with contact info, credentials, and custom JSON metadata.
type UserModel struct {
	ID           string         `json:"id"`
	Username     string         `json:"username"`
	Email        string         `json:"email"`
	PhoneNumber  string         `json:"phone_number"`
	PasswordHash string         `json:"-"`
	Metadata     map[string]any `json:"metadata"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// HashPassword generates a bcrypt hash of the provided plaintext password.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

// CheckPassword compares a plaintext password against a stored bcrypt hash.
func CheckPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// GetAuthLevel returns the auth_level defined in Metadata, or a fallback default.
func (u *UserModel) GetAuthLevel() any {
	if u.Metadata != nil {
		if lvl, exists := u.Metadata["auth_level"]; exists {
			return lvl
		}
		if lvl, exists := u.Metadata["level"]; exists {
			return lvl
		}
		if role, exists := u.Metadata["role"]; exists {
			return role
		}
	}
	return 1
}

// InitUserSchema creates the users table and corresponding indexes in PostgreSQL.
func InitUserSchema(ctx context.Context, db *dbclient.PostgresDatabase) error {
	if db == nil || db.Client == nil {
		return errors.New("database client is nil")
	}

	query := `
	CREATE TABLE IF NOT EXISTS users (
		id VARCHAR(64) PRIMARY KEY,
		username VARCHAR(100) NOT NULL UNIQUE,
		email VARCHAR(255) NOT NULL UNIQUE,
		phone_number VARCHAR(50) NOT NULL DEFAULT '',
		password_hash VARCHAR(255) NOT NULL,
		metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);
	CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
	CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
	`
	_, err := db.Client.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to init users schema: %w", err)
	}

	// Automatically seed default admin and demo user
	_ = SeedDefaultUsers(ctx, db)
	return nil
}

// SeedDefaultUsers inserts starter admin and demo user accounts if they do not exist.
func SeedDefaultUsers(ctx context.Context, db *dbclient.PostgresDatabase) error {
	if db == nil || db.Client == nil {
		return nil
	}

	// Default Admin
	adminHash, _ := HashPassword("Mp@tel98")
	adminUser := &UserModel{
		ID:           "user_admin_01",
		Username:     "admin",
		Email:        "admin@hack-go-thon.local",
		PhoneNumber:  "9427425572",
		PasswordHash: adminHash,
		Metadata: map[string]any{
			"auth_level":  99,
			"role":        "admin",
			"department":  "core-team",
			"permissions": []string{"admin", "read", "write", "delete"},
		},
	}
	_ = CreateUser(ctx, db, adminUser)

	// Default Demo User
	userHash, _ := HashPassword("user123")
	demoUser := &UserModel{
		ID:           "user_demo_01",
		Username:     "demo_user",
		Email:        "user@hackathon.local",
		PhoneNumber:  "+1-555-0101",
		PasswordHash: userHash,
		Metadata: map[string]any{
			"auth_level":  1,
			"role":        "member",
			"department":  "general",
			"permissions": []string{"read"},
		},
	}
	_ = CreateUser(ctx, db, demoUser)

	return nil
}

// CreateUser persists a new user record into PostgreSQL.
func CreateUser(ctx context.Context, db *dbclient.PostgresDatabase, user *UserModel) error {
	if db == nil || db.Client == nil {
		return errors.New("database client is nil")
	}

	if user.ID == "" {
		user.ID = "user_" + strings.ReplaceAll(uuid.New().String()[:8], "-", "")
	}
	now := time.Now().UTC()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	if user.UpdatedAt.IsZero() {
		user.UpdatedAt = now
	}
	if user.Metadata == nil {
		user.Metadata = make(map[string]any)
	}

	metaJSON, err := json.Marshal(user.Metadata)
	if err != nil {
		metaJSON = []byte("{}")
	}

	query := `
		INSERT INTO users (
			id, username, email, phone_number, password_hash, metadata, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (id) DO NOTHING;
	`
	_, err = db.Client.ExecContext(
		ctx,
		query,
		user.ID,
		strings.ToLower(strings.TrimSpace(user.Username)),
		strings.ToLower(strings.TrimSpace(user.Email)),
		strings.TrimSpace(user.PhoneNumber),
		user.PasswordHash,
		metaJSON,
		user.CreatedAt,
		user.UpdatedAt,
	)
	return err
}

// GetUserByID fetches a user record by its ID.
func GetUserByID(ctx context.Context, db *dbclient.PostgresDatabase, id string) (*UserModel, error) {
	if db == nil || db.Client == nil {
		return nil, errors.New("database client is nil")
	}

	query := `
		SELECT id, username, email, phone_number, password_hash, metadata, created_at, updated_at
		FROM users
		WHERE id = $1;
	`
	row := db.Client.QueryRowContext(ctx, query, id)

	var u UserModel
	var metaBytes []byte
	if err := row.Scan(
		&u.ID,
		&u.Username,
		&u.Email,
		&u.PhoneNumber,
		&u.PasswordHash,
		&metaBytes,
		&u.CreatedAt,
		&u.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if len(metaBytes) > 0 {
		_ = json.Unmarshal(metaBytes, &u.Metadata)
	}
	if u.Metadata == nil {
		u.Metadata = make(map[string]any)
	}
	return &u, nil
}

// GetUserByEmailOrUsername fetches a user record matching either email or username.
func GetUserByEmailOrUsername(ctx context.Context, db *dbclient.PostgresDatabase, identifier string) (*UserModel, error) {
	if db == nil || db.Client == nil {
		return nil, errors.New("database client is nil")
	}

	cleanIdent := strings.ToLower(strings.TrimSpace(identifier))
	query := `
		SELECT id, username, email, phone_number, password_hash, metadata, created_at, updated_at
		FROM users
		WHERE LOWER(email) = $1 OR LOWER(username) = $1
		LIMIT 1;
	`
	row := db.Client.QueryRowContext(ctx, query, cleanIdent)

	var u UserModel
	var metaBytes []byte
	if err := row.Scan(
		&u.ID,
		&u.Username,
		&u.Email,
		&u.PhoneNumber,
		&u.PasswordHash,
		&metaBytes,
		&u.CreatedAt,
		&u.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	if len(metaBytes) > 0 {
		_ = json.Unmarshal(metaBytes, &u.Metadata)
	}
	if u.Metadata == nil {
		u.Metadata = make(map[string]any)
	}
	return &u, nil
}

// ListUsers retrieves all registered users ordered by creation date.
func ListUsers(ctx context.Context, db *dbclient.PostgresDatabase, limit, offset int) ([]*UserModel, error) {
	if db == nil || db.Client == nil {
		return nil, errors.New("database client is nil")
	}

	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT id, username, email, phone_number, password_hash, metadata, created_at, updated_at
		FROM users
		ORDER BY created_at ASC
		LIMIT $1 OFFSET $2;
	`
	rows, err := db.Client.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]*UserModel, 0)
	for rows.Next() {
		var u UserModel
		var metaBytes []byte
		if err := rows.Scan(
			&u.ID,
			&u.Username,
			&u.Email,
			&u.PhoneNumber,
			&u.PasswordHash,
			&metaBytes,
			&u.CreatedAt,
			&u.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if len(metaBytes) > 0 {
			_ = json.Unmarshal(metaBytes, &u.Metadata)
		}
		if u.Metadata == nil {
			u.Metadata = make(map[string]any)
		}
		users = append(users, &u)
	}
	return users, rows.Err()
}

// UpdateUser updates editable user attributes (phone_number, metadata, optional password).
func UpdateUser(ctx context.Context, db *dbclient.PostgresDatabase, user *UserModel) error {
	if db == nil || db.Client == nil {
		return errors.New("database client is nil")
	}

	metaJSON, err := json.Marshal(user.Metadata)
	if err != nil {
		metaJSON = []byte("{}")
	}

	query := `
		UPDATE users
		SET phone_number = $1,
		    metadata = $2,
		    updated_at = NOW()
		WHERE id = $3;
	`
	_, err = db.Client.ExecContext(ctx, query, user.PhoneNumber, metaJSON, user.ID)
	return err
}

// DeleteUser deletes a user by ID.
func DeleteUser(ctx context.Context, db *dbclient.PostgresDatabase, id string) error {
	if db == nil || db.Client == nil {
		return errors.New("database client is nil")
	}

	query := `DELETE FROM users WHERE id = $1;`
	_, err := db.Client.ExecContext(ctx, query, id)
	return err
}
