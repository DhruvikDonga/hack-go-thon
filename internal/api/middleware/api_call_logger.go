package middleware

import (
	"context"
	"time"

	dbclient "hack-go-thon/internal/db_client"
	pgstore "hack-go-thon/internal/store/pg_store"
	"hack-go-thon/pkg/log"

	"github.com/gin-gonic/gin"
)

// APICallLogger returns a Gin middleware that records API call metrics into PostgreSQL.
func APICallLogger(db *dbclient.PostgresDatabase) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "OPTIONS" || db == nil {
			c.Next()
			return
		}

		start := time.Now()
		c.Next()

		latency := time.Since(start).Milliseconds()
		statusCode := c.Writer.Status()
		endpoint := c.FullPath()
		if endpoint == "" {
			endpoint = c.Request.URL.Path
		}
		method := c.Request.Method
		clientIP := c.ClientIP()
		userID := c.GetString("user_id")

		// Asynchronous database write so user request latency is never impacted
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			err := pgstore.InsertAPICall(ctx, db, &pgstore.APICallModel{
				UserID:             userID,
				APIEndPoint:        endpoint,
				HTTPMethod:         method,
				ResponseStatusCode: statusCode,
				LatencyMs:          latency,
				ClientIP:           clientIP,
			})
			if err != nil {
				log.Warn("Failed to persist API call log in database", "error", err.Error())
			}
		}()
	}
}
