package middleware

import (
	"fmt"
	"net/http/httputil"
	"runtime/debug"

	"hack-go-thon/pkg/apperrors"
	"hack-go-thon/pkg/log"
	"hack-go-thon/pkg/response"

	"github.com/gin-gonic/gin"
)

// Recovery recovers from any panics, logs stack traces via Zap, and writes a standard error JSON response.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				httpRequest, _ := httputil.DumpRequest(c.Request, false)
				stack := debug.Stack()

				log.Error("panic recovered",
					"error", fmt.Sprintf("%v", r),
					"request", string(httpRequest),
					"stack", string(stack),
				)

				response.AbortWithError(c, apperrors.NewInternal("Internal server error occurred"))
			}
		}()
		c.Next()
	}
}
