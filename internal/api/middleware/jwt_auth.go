package middleware

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	pgstore "hack-go-thon/internal/store/pg_store"
	"hack-go-thon/pkg/apperrors"
	"hack-go-thon/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

// JWTClaims defines standard payload claims for the framework including custom metadata and auth level.
type JWTClaims struct {
	UserID      string         `json:"user_id"`
	Username    string         `json:"username,omitempty"`
	Email       string         `json:"email"`
	PhoneNumber string         `json:"phone_number,omitempty"`
	Role        string         `json:"role,omitempty"`
	AuthLevel   any            `json:"auth_level,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	jwt.RegisteredClaims
}

// GenerateToken issues a signed HMAC-SHA256 JWT token for testing and authentication.
func GenerateToken(secret, userID, email, role string, ttl time.Duration) (string, error) {
	return GenerateTokenWithMetadata(secret, userID, "", email, "", role, 1, nil, ttl)
}

// GenerateTokenWithMetadata issues a signed HMAC-SHA256 JWT token with full metadata and auth level.
func GenerateTokenWithMetadata(secret, userID, username, email, phoneNumber, role string, authLevel any, metadata map[string]any, ttl time.Duration) (string, error) {
	if secret == "" {
		return "", errors.New("jwt secret cannot be empty")
	}

	claims := JWTClaims{
		UserID:      userID,
		Username:    username,
		Email:       email,
		PhoneNumber: phoneNumber,
		Role:        role,
		AuthLevel:   authLevel,
		Metadata:    metadata,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// GenerateUserToken issues a signed JWT token populated directly from a UserModel.
func GenerateUserToken(secret string, user *pgstore.UserModel, ttl time.Duration) (string, error) {
	if user == nil {
		return "", errors.New("user cannot be nil")
	}
	role := "member"
	if r, ok := user.Metadata["role"].(string); ok && r != "" {
		role = r
	}
	return GenerateTokenWithMetadata(
		secret,
		user.ID,
		user.Username,
		user.Email,
		user.PhoneNumber,
		role,
		user.GetAuthLevel(),
		user.Metadata,
		ttl,
	)
}

// JWTAuth returns a Gin middleware validating Bearer JWTs (header or query param).
func JWTAuth(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}

		var tokenString string
		authHeader := c.GetHeader("Authorization")
		if authHeader != "" {
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
				tokenString = parts[1]
			}
		}

		if tokenString == "" {
			tokenString = c.Query("token")
		}
		if tokenString == "" {
			tokenString = c.Query("access_token")
		}

		if tokenString == "" {
			response.AbortWithError(c, apperrors.NewUnauthorized("Authorization required: missing Bearer token or token query parameter"))
			return
		}

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
		c.Set("username", claims.Username)
		c.Set("email", claims.Email)
		c.Set("phone_number", claims.PhoneNumber)
		c.Set("role", claims.Role)
		c.Set("auth_level", claims.AuthLevel)
		c.Set("metadata", claims.Metadata)
		c.Set("jwt_claims", claims)

		c.Next()
	}
}

// RequireAuthLevelRange returns a middleware that verifies the authenticated user's auth_level is between minLevel and maxLevel (inclusive).
func RequireAuthLevelRange(minLevel, maxLevel int) gin.HandlerFunc {
	return func(c *gin.Context) {
		val, exists := c.Get("auth_level")
		if !exists || val == nil {
			response.AbortWithError(c, apperrors.NewForbidden("Insufficient authorization level"))
			return
		}

		userLevel := 0
		switch v := val.(type) {
		case int:
			userLevel = v
		case int64:
			userLevel = int(v)
		case float64:
			userLevel = int(v)
		case string:
			if strings.EqualFold(v, "admin") || strings.EqualFold(v, "superadmin") {
				userLevel = 99
			} else if n, err := strconv.Atoi(v); err == nil {
				userLevel = n
			}
		default:
			response.AbortWithError(c, apperrors.NewForbidden("Invalid authorization level"))
			return
		}

		if userLevel < minLevel || (maxLevel > 0 && userLevel > maxLevel) {
			if maxLevel > 0 {
				response.AbortWithError(c, apperrors.NewForbidden(fmt.Sprintf("Forbidden: required auth level %d to %d, current is %d", minLevel, maxLevel, userLevel)))
			} else {
				response.AbortWithError(c, apperrors.NewForbidden(fmt.Sprintf("Forbidden: required auth level >= %d, current is %d", minLevel, userLevel)))
			}
			return
		}

		c.Next()
	}
}

// RequireAuthLevel returns a middleware that verifies the authenticated user meets or exceeds minLevel.
func RequireAuthLevel(minLevel int) gin.HandlerFunc {
	return RequireAuthLevelRange(minLevel, 0)
}

// VerifyTokenAuthLevel validates a JWT string and confirms whether the auth_level satisfies minLevel and maxLevel.
func VerifyTokenAuthLevel(secret, tokenString string, minLevel, maxLevel int) (bool, *JWTClaims, error) {
	if secret == "" || tokenString == "" {
		return false, nil, errors.New("missing token or secret")
	}

	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})

	if err != nil || !token.Valid {
		return false, nil, errors.New("invalid or expired token")
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok {
		return false, nil, errors.New("invalid claims payload")
	}

	userLevel := 0
	switch v := claims.AuthLevel.(type) {
	case int:
		userLevel = v
	case int64:
		userLevel = int(v)
	case float64:
		userLevel = int(v)
	case string:
		if strings.EqualFold(v, "admin") || strings.EqualFold(v, "superadmin") {
			userLevel = 99
		} else if n, err := strconv.Atoi(v); err == nil {
			userLevel = n
		}
	default:
		userLevel = 1
	}

	if userLevel < minLevel || (maxLevel > 0 && userLevel > maxLevel) {
		if maxLevel > 0 {
			return false, claims, fmt.Errorf("insufficient auth level: required %d to %d, current is %d", minLevel, maxLevel, userLevel)
		}
		return false, claims, fmt.Errorf("insufficient auth level: required >= %d, current is %d", minLevel, userLevel)
	}

	return true, claims, nil
}

// RequireRole returns a middleware requiring the user's role to match any of the allowed roles.
func RequireRole(allowedRoles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role := c.GetString("role")
		for _, r := range allowedRoles {
			if strings.EqualFold(role, r) {
				c.Next()
				return
			}
		}
		response.AbortWithError(c, apperrors.NewForbidden("Forbidden: insufficient role permissions"))
	}
}
