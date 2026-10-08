//go:build ignore

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"hack-go-thon/config"
	dbclient "hack-go-thon/internal/db_client"

	"github.com/brianvoe/gofakeit/v6"
)

func main() {
	count := flag.Int("count", 50, "Number of records to seed")
	flag.Parse()

	// 1. Load config
	cfg := config.Load()

	// 2. Connect to Database
	pgDB, err := dbclient.NewPostgresClient(context.Background(), cfg.PostgresURI, "postgres")
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer pgDB.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Printf("🌱 Seeding database with %d records...\n", *count)
	gofakeit.Seed(0)

	// Example: Seed dummy users
	for i := 0; i < *count; i++ {
		userID := gofakeit.UUID()
		email := gofakeit.Email()
		name := gofakeit.Name()
		phone := gofakeit.Phone()

		query := `
			INSERT INTO users (id, phone_number, created_at, auth_level, metadata)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (phone_number) DO NOTHING;
		`

		// In a real hackathon, we would populate the specific tables for the theme here.
		// For now, we seed a dummy user or just demonstrate the connection.
		_, err := pgDB.Client.ExecContext(ctx, query,
			userID,
			phone, // Use phone or email depending on your schema
			time.Now(),
			gofakeit.Number(1, 10), // Random auth level
			fmt.Sprintf(`{"name": "%s", "email": "%s"}`, name, email),
		)

		if err != nil {
			// Users table might not exist yet if not initialized, handle gracefully
			log.Printf("⚠️  Warning inserting record %d: %v. (Ensure schemas are initialized first)", i, err)
			break
		}
	}

	fmt.Println("✅ Seeding complete! Database is populated with realistic fake data.")
}
