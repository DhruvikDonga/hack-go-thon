package middleware

import (
	"errors"
	"strings"
	"time"

	"hack-go-thon/pkg/apperrors"
	"hack-go-thon/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// JWTClaims defines standard payload claims for the framework.
type JWTClaims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

// GenerateToken issues a signed HMAC-SHA256 JWT token for testing and authentication.
func GenerateToken(secret, userID, email, role string, ttl time.Duration) (string, error) {
	if secret == "" {
		return "", errors.New("jwt secret cannot be empty")
	}

	claims := JWTClaims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// JWTAuth returns a Gin middleware validating Bearer JWTs.
func JWTAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}

		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			response.AbortWithError(c, apperrors.NewUnauthorized("Authorization header missing"))
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			response.AbortWithError(c, apperrors.NewUnauthorized("Invalid Authorization header format, expected 'Bearer <token>'"))
			return
		}

		tokenString := parts[1]

		token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (any, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return []byte(secret), nil
		})

		if err != nil || !token.Valid {
			response.AbortWithError(c, apperrors.NewUnauthorized("Invalid or expired JWT token"))
			return
		}

		claims, ok := token.Claims.(*JWTClaims)
		if !ok {
			response.AbortWithError(c, apperrors.NewUnauthorized("Invalid JWT claims payload"))
			return
		}

		// Inject verified claims into context
		c.Set("user_id", claims.UserID)
		c.Set("email", claims.Email)
		c.Set("role", claims.Role)
		c.Set("jwt_claims", claims)

		c.Next()
	}
}
