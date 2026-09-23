package handler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"hack-go-thon/internal/api/middleware"
	dbclient "hack-go-thon/internal/db_client"
	pgstore "hack-go-thon/internal/store/pg_store"
	"hack-go-thon/pkg/apperrors"
	"hack-go-thon/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// UserHandler handles user authentication, registration, RBAC token issuing, and profile management.
type UserHandler struct {
	db            *dbclient.PostgresDatabase
	jwtSecret     string
	tokenTTL      time.Duration
	mu            sync.RWMutex
	fallbackUsers map[string]*pgstore.UserModel
}

// NewUserHandler initializes UserHandler with database client and in-memory fallback.
func NewUserHandler(db *dbclient.PostgresDatabase, jwtSecret string, tokenTTL time.Duration) *UserHandler {
	if tokenTTL <= 0 {
		tokenTTL = 24 * time.Hour
	}
	if jwtSecret == "" {
		jwtSecret = "default-hackathon-jwt-secret-key-12345"
	}

	h := &UserHandler{
		db:            db,
		jwtSecret:     jwtSecret,
		tokenTTL:      tokenTTL,
		fallbackUsers: make(map[string]*pgstore.UserModel),
	}

	h.seedFallbackUsers()
	return h
}

func (h *UserHandler) seedFallbackUsers() {
	h.mu.Lock()
	defer h.mu.Unlock()

	adminHash, _ := pgstore.HashPassword("Mp@tel98")
	userHash, _ := pgstore.HashPassword("user123")
	staffHash, _ := pgstore.HashPassword("Staff@123")
	now := time.Now().UTC()

	admin := &pgstore.UserModel{
		ID:           "user_admin_01",
		Username:     "admin",
		Email:        "admin@hack-go-thon.local",
		PhoneNumber:  "9427425572",
		PasswordHash: adminHash,
		Metadata: map[string]any{
			"auth_level":  99,
			"role":        "admin",
			"department":  "core-team",
			"permissions": []any{"admin", "read", "write", "delete"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	demo := &pgstore.UserModel{
		ID:           "user_demo_01",
		Username:     "demo_user",
		Email:        "user@hackathon.local",
		PhoneNumber:  "+1-555-0101",
		PasswordHash: userHash,
		Metadata: map[string]any{
			"auth_level":  1,
			"role":        "member",
			"department":  "general",
			"permissions": []any{"read"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	staff := &pgstore.UserModel{
		ID:           "user_staff_10",
		Username:     "staff_user",
		Email:        "staff@hack-go-thon.local",
		PhoneNumber:  "9427425570",
		PasswordHash: staffHash,
		Metadata: map[string]any{
			"auth_level":  10,
			"role":        "staff",
			"department":  "operations",
			"permissions": []any{"read"},
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	h.fallbackUsers[admin.ID] = admin
	h.fallbackUsers[demo.ID] = demo
	h.fallbackUsers[staff.ID] = staff
}

// RegisterUserRequest defines payload for registering a user.
type RegisterUserRequest struct {
	Username    string         `json:"username" binding:"required,min=2,max=100"`
	Email       string         `json:"email" binding:"required,email"`
	PhoneNumber string         `json:"phone_number"`
	Password    string         `json:"password" binding:"required,min=4"`
	Metadata    map[string]any `json:"metadata"`
}

// LoginRequest defines payload for authenticating a user.
type LoginRequest struct {
	Identifier string `json:"identifier"`
	Username   string `json:"username"`
	Email      string `json:"email"`
	Password   string `json:"password" binding:"required"`
}

// UpdateUserRequest defines payload for updating a user profile.
type UpdateUserRequest struct {
	PhoneNumber *string        `json:"phone_number"`
	Password    *string        `json:"password"`
	Metadata    map[string]any `json:"metadata"`
}

// GenerateTokenRequest defines optional overrides when generating testing tokens.
type GenerateTokenRequest struct {
	TTLHours int `json:"ttl_hours"`
}

// Register godoc
// @Summary Register a new user
// @Description Creates a new user record with password hashing and custom JSON metadata, returning a signed JWT token.
// @Tags auth
// @Accept json
// @Produce json
// @Param request body RegisterUserRequest true "Registration Payload"
// @Success 201 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 409 {object} response.Response
// @Router /api/v1/auth/register [post]
func (h *UserHandler) Register(c *gin.Context) {
	var req RegisterUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperrors.NewBadRequest("Invalid registration payload: "+err.Error()))
		return
	}

	username := strings.ToLower(strings.TrimSpace(req.Username))
	email := strings.ToLower(strings.TrimSpace(req.Email))

	hash, err := pgstore.HashPassword(req.Password)
	if err != nil {
		response.Error(c, apperrors.NewInternal("Failed to secure user password", err))
		return
	}

	meta := req.Metadata
	if meta == nil {
		meta = make(map[string]any)
	}
	if _, ok := meta["auth_level"]; !ok {
		meta["auth_level"] = 1
	}
	if _, ok := meta["role"]; !ok {
		meta["role"] = "member"
	}

	user := &pgstore.UserModel{
		ID:           "user_" + strings.ReplaceAll(uuid.New().String()[:8], "-", ""),
		Username:     username,
		Email:        email,
		PhoneNumber:  strings.TrimSpace(req.PhoneNumber),
		PasswordHash: hash,
		Metadata:     meta,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	if h.db != nil && h.db.Client != nil {
		existing, err := pgstore.GetUserByEmailOrUsername(c.Request.Context(), h.db, username)
		if err == nil && existing != nil {
			response.Error(c, apperrors.NewConflict("A user with this username already exists"))
			return
		}
		existing, err = pgstore.GetUserByEmailOrUsername(c.Request.Context(), h.db, email)
		if err == nil && existing != nil {
			response.Error(c, apperrors.NewConflict("A user with this email address already exists"))
			return
		}

		if err := pgstore.CreateUser(c.Request.Context(), h.db, user); err != nil {
			response.Error(c, apperrors.NewInternal("Failed to persist user", err))
			return
		}
	} else {
		// In-memory fallback
		h.mu.Lock()
		for _, u := range h.fallbackUsers {
			if strings.EqualFold(u.Username, username) {
				h.mu.Unlock()
				response.Error(c, apperrors.NewConflict("A user with this username already exists"))
				return
			}
			if strings.EqualFold(u.Email, email) {
				h.mu.Unlock()
				response.Error(c, apperrors.NewConflict("A user with this email address already exists"))
				return
			}
		}
		h.fallbackUsers[user.ID] = user
		h.mu.Unlock()
	}

	token, err := middleware.GenerateUserToken(h.jwtSecret, user, h.tokenTTL)
	if err != nil {
		response.Error(c, apperrors.NewInternal("Failed to issue JWT token", err))
		return
	}

	response.Created(c, gin.H{
		"user":       user,
		"token":      token,
		"auth_level": user.GetAuthLevel(),
	})
}

// Login godoc
// @Summary User login
// @Description Authenticates via email/username and password, returning user data and a signed JWT claims token.
// @Tags auth
// @Accept json
// @Produce json
// @Param request body LoginRequest true "Login Payload"
// @Success 200 {object} response.Response
// @Failure 400 {object} response.Response
// @Failure 401 {object} response.Response
// @Router /api/v1/auth/login [post]
func (h *UserHandler) Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperrors.NewBadRequest("Invalid login payload: "+err.Error()))
		return
	}

	ident := req.Identifier
	if ident == "" {
		ident = req.Username
	}
	if ident == "" {
		ident = req.Email
	}
	ident = strings.TrimSpace(ident)
	if ident == "" {
		response.Error(c, apperrors.NewBadRequest("Username or email identifier is required"))
		return
	}

	var user *pgstore.UserModel
	if h.db != nil && h.db.Client != nil {
		u, err := pgstore.GetUserByEmailOrUsername(c.Request.Context(), h.db, ident)
		if err != nil {
			response.Error(c, apperrors.NewInternal("Failed to query user database", err))
			return
		}
		user = u
	} else {
		// In-memory fallback
		h.mu.RLock()
		for _, u := range h.fallbackUsers {
			if strings.EqualFold(u.Username, ident) || strings.EqualFold(u.Email, ident) {
				user = u
				break
			}
		}
		h.mu.RUnlock()
	}

	if user == nil || !pgstore.CheckPassword(user.PasswordHash, req.Password) {
		response.Error(c, apperrors.NewUnauthorized("Invalid credentials"))
		return
	}

	token, err := middleware.GenerateUserToken(h.jwtSecret, user, h.tokenTTL)
	if err != nil {
		response.Error(c, apperrors.NewInternal("Failed to issue JWT token", err))
		return
	}

	response.OK(c, gin.H{
		"token":      token,
		"user":       user,
		"auth_level": user.GetAuthLevel(),
		"expires_in": int(h.tokenTTL.Seconds()),
	})
}

// Me godoc
// @Summary Current user profile
// @Description Returns current user details extracted from JWT token and storage.
// @Tags auth
// @Security BearerAuth
// @Produce json
// @Success 200 {object} response.Response
// @Failure 401 {object} response.Response
// @Router /api/v1/auth/me [get]
func (h *UserHandler) Me(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		response.Error(c, apperrors.NewUnauthorized("Authentication required"))
		return
	}

	var user *pgstore.UserModel
	if h.db != nil && h.db.Client != nil {
		u, _ := pgstore.GetUserByID(c.Request.Context(), h.db, userID)
		user = u
	} else {
		h.mu.RLock()
		user = h.fallbackUsers[userID]
		h.mu.RUnlock()
	}

	claims, _ := c.Get("jwt_claims")

	response.OK(c, gin.H{
		"user_id":      c.GetString("user_id"),
		"username":     c.GetString("username"),
		"email":        c.GetString("email"),
		"phone_number": c.GetString("phone_number"),
		"role":         c.GetString("role"),
		"auth_level":   c.MustGet("auth_level"),
		"metadata":     c.MustGet("metadata"),
		"claims":       claims,
		"user":         user,
	})
}

// ListUsers godoc
// @Summary List all users
// @Description Returns list of registered users.
// @Tags users
// @Produce json
// @Success 200 {object} response.Response
// @Router /api/v1/users [get]
func (h *UserHandler) ListUsers(c *gin.Context) {
	if h.db != nil && h.db.Client != nil {
		users, err := pgstore.ListUsers(c.Request.Context(), h.db, 100, 0)
		if err != nil {
			response.Error(c, apperrors.NewInternal("Failed to list users", err))
			return
		}
		response.OK(c, users)
		return
	}

	// In-memory fallback
	h.mu.RLock()
	defer h.mu.RUnlock()
	users := make([]*pgstore.UserModel, 0, len(h.fallbackUsers))
	for _, u := range h.fallbackUsers {
		users = append(users, u)
	}
	response.OK(c, users)
}

// checkCanModifyUsers verifies that if an authentication token is provided in the request
// (context, Bearer header, or query param), the user has an auth_level strictly above 10.
// Users with auth_level 10 (e.g. staff/viewer) have view-only access and cannot create, update, or delete users.
func (h *UserHandler) checkCanModifyUsers(c *gin.Context) (bool, error) {
	// If context has auth_level set from middleware
	if val, exists := c.Get("auth_level"); exists && val != nil {
		lvl := 0
		switch v := val.(type) {
		case int:
			lvl = v
		case int64:
			lvl = int(v)
		case float64:
			lvl = int(v)
		case string:
			if strings.EqualFold(v, "admin") || strings.EqualFold(v, "superadmin") {
				lvl = 99
			} else if n, err := strconv.Atoi(v); err == nil {
				lvl = n
			}
		}
		if lvl <= 10 {
			return false, fmt.Errorf("forbidden: user auth level %d has view-only permissions and cannot update or modify users (auth level above 10 required)", lvl)
		}
		return true, nil
	}

	// Check if Bearer token or query param token was passed in request
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

	if tokenString != "" {
		valid, claims, err := middleware.VerifyTokenAuthLevel(h.jwtSecret, tokenString, 11, 99)
		if !valid {
			if err != nil {
				return false, fmt.Errorf("forbidden: %w", err)
			}
			return false, fmt.Errorf("forbidden: user auth level %v has view-only permissions (auth level above 10 required)", claims.AuthLevel)
		}
	}

	return true, nil
}

// CreateUser godoc
// @Summary Create a user (Admin/API)
// @Description Creates a new user with full attribute and metadata control.
// @Tags users
// @Accept json
// @Produce json
// @Param request body RegisterUserRequest true "Create User Payload"
// @Success 201 {object} response.Response
// @Router /api/v1/users [post]
func (h *UserHandler) CreateUser(c *gin.Context) {
	if ok, err := h.checkCanModifyUsers(c); !ok {
		response.Error(c, apperrors.NewForbidden(err.Error()))
		return
	}
	h.Register(c)
}

// GetUser godoc
// @Summary Get user by ID
// @Description Retrieves a user by their unique user_id.
// @Tags users
// @Produce json
// @Param id path string true "User ID"
// @Success 200 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /api/v1/users/{id} [get]
func (h *UserHandler) GetUser(c *gin.Context) {
	id := c.Param("id")

	var user *pgstore.UserModel
	if h.db != nil && h.db.Client != nil {
		u, err := pgstore.GetUserByID(c.Request.Context(), h.db, id)
		if err != nil {
			response.Error(c, apperrors.NewInternal("Failed to query user", err))
			return
		}
		user = u
	} else {
		h.mu.RLock()
		user = h.fallbackUsers[id]
		h.mu.RUnlock()
	}

	if user == nil {
		response.Error(c, apperrors.NewNotFound("User not found"))
		return
	}

	response.OK(c, user)
}

// UpdateUser godoc
// @Summary Update user profile
// @Description Updates user metadata, phone number, and optional password.
// @Tags users
// @Accept json
// @Produce json
// @Param id path string true "User ID"
// @Param request body UpdateUserRequest true "Update User Payload"
// @Success 200 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /api/v1/users/{id} [put]
func (h *UserHandler) UpdateUser(c *gin.Context) {
	if ok, err := h.checkCanModifyUsers(c); !ok {
		response.Error(c, apperrors.NewForbidden(err.Error()))
		return
	}

	id := c.Param("id")

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, apperrors.NewBadRequest("Invalid update payload: "+err.Error()))
		return
	}

	var user *pgstore.UserModel
	if h.db != nil && h.db.Client != nil {
		u, err := pgstore.GetUserByID(c.Request.Context(), h.db, id)
		if err != nil {
			response.Error(c, apperrors.NewInternal("Failed to query user", err))
			return
		}
		user = u
	} else {
		h.mu.RLock()
		user = h.fallbackUsers[id]
		h.mu.RUnlock()
	}

	if user == nil {
		response.Error(c, apperrors.NewNotFound("User not found"))
		return
	}

	if req.PhoneNumber != nil {
		user.PhoneNumber = strings.TrimSpace(*req.PhoneNumber)
	}
	if req.Password != nil && strings.TrimSpace(*req.Password) != "" {
		hash, err := pgstore.HashPassword(*req.Password)
		if err != nil {
			response.Error(c, apperrors.NewInternal("Failed to hash password", err))
			return
		}
		user.PasswordHash = hash
	}
	if req.Metadata != nil {
		user.Metadata = req.Metadata
	}
	user.UpdatedAt = time.Now().UTC()

	if h.db != nil && h.db.Client != nil {
		if err := pgstore.UpdateUser(c.Request.Context(), h.db, user); err != nil {
			response.Error(c, apperrors.NewInternal("Failed to update user", err))
			return
		}
	} else {
		h.mu.Lock()
		h.fallbackUsers[id] = user
		h.mu.Unlock()
	}

	response.OK(c, user)
}

// DeleteUser godoc
// @Summary Delete user by ID
// @Description Deletes a user record.
// @Tags users
// @Produce json
// @Param id path string true "User ID"
// @Success 200 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /api/v1/users/{id} [delete]
func (h *UserHandler) DeleteUser(c *gin.Context) {
	if ok, err := h.checkCanModifyUsers(c); !ok {
		response.Error(c, apperrors.NewForbidden(err.Error()))
		return
	}

	id := c.Param("id")

	if h.db != nil && h.db.Client != nil {
		if err := pgstore.DeleteUser(c.Request.Context(), h.db, id); err != nil {
			response.Error(c, apperrors.NewInternal("Failed to delete user", err))
			return
		}
	} else {
		h.mu.Lock()
		if _, exists := h.fallbackUsers[id]; !exists {
			h.mu.Unlock()
			response.Error(c, apperrors.NewNotFound("User not found"))
			return
		}
		delete(h.fallbackUsers, id)
		h.mu.Unlock()
	}

	response.OK(c, gin.H{"status": "deleted", "id": id})
}

// GenerateUserToken godoc
// @Summary Instant JWT Token Generator for User
// @Description Generates a signed JWT with full user metadata and auth_level for quick testing and RBAC inspection.
// @Tags users
// @Produce json
// @Param id path string true "User ID"
// @Success 200 {object} response.Response
// @Failure 404 {object} response.Response
// @Router /api/v1/users/{id}/token [post]
func (h *UserHandler) GenerateUserToken(c *gin.Context) {
	id := c.Param("id")

	var user *pgstore.UserModel
	if h.db != nil && h.db.Client != nil {
		u, err := pgstore.GetUserByID(c.Request.Context(), h.db, id)
		if err != nil {
			response.Error(c, apperrors.NewInternal("Failed to query user", err))
			return
		}
		user = u
	} else {
		h.mu.RLock()
		user = h.fallbackUsers[id]
		h.mu.RUnlock()
	}

	if user == nil {
		response.Error(c, apperrors.NewNotFound("User not found"))
		return
	}

	ttl := h.tokenTTL
	var req GenerateTokenRequest
	if err := c.ShouldBindJSON(&req); err == nil && req.TTLHours > 0 {
		ttl = time.Duration(req.TTLHours) * time.Hour
	}

	token, err := middleware.GenerateUserToken(h.jwtSecret, user, ttl)
	if err != nil {
		response.Error(c, apperrors.NewInternal("Failed to generate token", err))
		return
	}

	response.OK(c, gin.H{
		"token":        token,
		"user_id":      user.ID,
		"username":     user.Username,
		"email":        user.Email,
		"phone_number": user.PhoneNumber,
		"auth_level":   user.GetAuthLevel(),
		"metadata":     user.Metadata,
		"expires_in":   int(ttl.Seconds()),
	})
}

// FindUser helper retrieves a user by ID from DB or fallback
func (h *UserHandler) FindUser(ctx gin.Context, id string) (*pgstore.UserModel, error) {
	if h.db != nil && h.db.Client != nil {
		return pgstore.GetUserByID(ctx.Request.Context(), h.db, id)
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	u, ok := h.fallbackUsers[id]
	if !ok {
		return nil, errors.New("user not found")
	}
	return u, nil
}
