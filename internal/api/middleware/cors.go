package middleware

import (
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// CORS returns a configured CORS middleware for Gin.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	config := cors.DefaultConfig()

	if len(allowedOrigins) == 1 && allowedOrigins[0] == "*" {
		config.AllowAllOrigins = true
	} else if len(allowedOrigins) > 0 {
		config.AllowOrigins = allowedOrigins
	} else {
		config.AllowAllOrigins = true
	}

	config.AllowMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	config.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization", "X-Request-ID", "X-API-Key"}
	config.ExposeHeaders = []string{"Content-Length", "X-Request-ID"}
	config.AllowCredentials = true

	return cors.New(config)
}
