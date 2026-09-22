package middleware

import (
	"time"

	"hack-go-thon/pkg/log"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const HeaderRequestID = "X-Request-ID"

// APILogger logs incoming HTTP requests using Zap and injects a unique X-Request-ID.
func APILogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Do not log OPTIONS preflight requests
		if c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}

		start := time.Now()
		path := c.Request.URL.Path
		rawQuery := c.Request.URL.RawQuery

		// Extract or generate Request ID
		requestID := c.GetHeader(HeaderRequestID)
		if requestID == "" {
			requestID = uuid.New().String()
		}
		c.Header(HeaderRequestID, requestID)
		c.Set("request_id", requestID)

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		clientIP := c.ClientIP()
		method := c.Request.Method

		if rawQuery != "" {
			path = path + "?" + rawQuery
		}

		logFields := []any{
			"status", status,
			"method", method,
			"path", path,
			"ip", clientIP,
			"latency", latency.String(),
			"request_id", requestID,
		}

		if len(c.Errors) > 0 {
			logFields = append(logFields, "errors", c.Errors.String())
		}

		if status >= 500 {
			log.Error("HTTP request failed", logFields...)
		} else if status >= 400 {
			log.Warn("HTTP request warning", logFields...)
		} else {
			log.Info("HTTP request handled", logFields...)
		}
	}
}
