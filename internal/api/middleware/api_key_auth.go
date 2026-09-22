package middleware

import (
	dbclient "hack-go-thon/internal/db_client"
	pgstore "hack-go-thon/internal/store/pg_store"
	"hack-go-thon/pkg/apperrors"
	"hack-go-thon/pkg/response"

	"github.com/gin-gonic/gin"
)

// HeaderAPIKey defines the standard HTTP header for API keys.
const HeaderAPIKey = "X-API-Key"

// APIKeyAuth validates API keys against PostgreSQL (or an optional fallback master key).
func APIKeyAuth(db *dbclient.PostgresDatabase, fallbackMasterKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}

		apiKey := c.GetHeader(HeaderAPIKey)
		if apiKey == "" {
			response.AbortWithError(c, apperrors.NewUnauthorized("API key missing in header '"+HeaderAPIKey+"'"))
			return
		}

		// Check fallback master key first if configured
		if fallbackMasterKey != "" && apiKey == fallbackMasterKey {
			c.Set("user_id", "admin")
			c.Set("api_key_name", "master_key")
			c.Next()
			return
		}

		// Verify against PostgreSQL database if available
		if db != nil {
			key, err := pgstore.GetAPIKeyBySecret(c.Request.Context(), db, apiKey)
			if err != nil {
				response.AbortWithError(c, apperrors.NewInternal("Failed to verify API key", err))
				return
			}
			if key == nil {
				response.AbortWithError(c, apperrors.NewUnauthorized("Invalid API key"))
				return
			}

			c.Set("user_id", key.UserID)
			c.Set("api_key_id", key.ID)
			c.Set("api_key_name", key.Name)
			c.Next()
			return
		}

		// If DB is not configured and master key didn't match
		response.AbortWithError(c, apperrors.NewUnauthorized("Invalid API key"))
	}
}
