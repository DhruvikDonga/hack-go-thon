package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hack-go-thon/internal/api/middleware"

	"github.com/gin-gonic/gin"
)

func setupUserTestRouter() (*gin.Engine, *UserHandler) {
	gin.SetMode(gin.TestMode)
	secret := "test-secret-key-12345"
	h := NewUserHandler(nil, secret, time.Hour)

	r := gin.New()
	auth := r.Group("/api/v1/auth")
	{
		auth.POST("/register", h.Register)
		auth.POST("/login", h.Login)
		auth.GET("/me", middleware.JWTAuth(secret), h.Me)
	}

	users := r.Group("/api/v1/users")
	{
		users.GET("", h.ListUsers)
		users.POST("", h.CreateUser)
		users.GET("/:id", h.GetUser)
		users.PUT("/:id", h.UpdateUser)
		users.DELETE("/:id", h.DeleteUser)
		users.POST("/:id/token", h.GenerateUserToken)
	}

	return r, h
}

func TestUserHandler_SeedUsers(t *testing.T) {
	r, _ := setupUserTestRouter()

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/users", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var resp struct {
		Success bool `json:"success"`
		Data    []struct {
			ID       string         `json:"id"`
			Username string         `json:"username"`
			Metadata map[string]any `json:"metadata"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Data) < 2 {
		t.Fatalf("expected at least 2 seeded users, got %d", len(resp.Data))
	}
}

func TestUserHandler_RegisterAndLogin(t *testing.T) {
	r, _ := setupUserTestRouter()

	// 1. Register
	regPayload := RegisterUserRequest{
		Username:    "hack_coder",
		Email:       "coder@hackathon.local",
		PhoneNumber: "+1-555-9988",
		Password:    "secretpassword123",
		Metadata: map[string]any{
			"auth_level": 50,
			"role":       "lead-dev",
			"skills":     []any{"go", "webrtc", "rag"},
		},
	}
	body, _ := json.Marshal(regPayload)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var regResp struct {
		Success bool `json:"success"`
		Data    struct {
			Token     string `json:"token"`
			AuthLevel any    `json:"auth_level"`
		} `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &regResp)
	if regResp.Data.Token == "" {
		t.Fatalf("expected token in register response")
	}

	// 2. Duplicate registration should fail
	wDup := httptest.NewRecorder()
	reqDup := httptest.NewRequest("POST", "/api/v1/auth/register", bytes.NewReader(body))
	reqDup.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wDup, reqDup)

	if wDup.Code != http.StatusConflict {
		t.Fatalf("expected 409 Conflict for duplicate registration, got %d", wDup.Code)
	}

	// 3. Login with correct password
	loginPayload := LoginRequest{
		Identifier: "hack_coder",
		Password:   "secretpassword123",
	}
	loginBody, _ := json.Marshal(loginPayload)

	wLogin := httptest.NewRecorder()
	reqLogin := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBody))
	reqLogin.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wLogin, reqLogin)

	if wLogin.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on login, got %d: %s", wLogin.Code, wLogin.Body.String())
	}

	var loginResp struct {
		Data struct {
			Token     string `json:"token"`
			AuthLevel any    `json:"auth_level"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wLogin.Body.Bytes(), &loginResp)

	// 4. Test /me endpoint using the login token
	wMe := httptest.NewRecorder()
	reqMe := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	reqMe.Header.Set("Authorization", "Bearer "+loginResp.Data.Token)
	r.ServeHTTP(wMe, reqMe)

	if wMe.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on /me, got %d: %s", wMe.Code, wMe.Body.String())
	}

	var meResp struct {
		Data struct {
			Username    string         `json:"username"`
			PhoneNumber string         `json:"phone_number"`
			AuthLevel   any            `json:"auth_level"`
			Metadata    map[string]any `json:"metadata"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wMe.Body.Bytes(), &meResp)

	if meResp.Data.Username != "hack_coder" {
		t.Fatalf("expected username hack_coder, got %s", meResp.Data.Username)
	}
	if meResp.Data.PhoneNumber != "+1-555-9988" {
		t.Fatalf("expected phone +1-555-9988, got %s", meResp.Data.PhoneNumber)
	}
}

func TestUserHandler_CRUDAndTokenGen(t *testing.T) {
	r, _ := setupUserTestRouter()

	// 1. Get seeded user
	wGet := httptest.NewRecorder()
	reqGet := httptest.NewRequest("GET", "/api/v1/users/user_admin_01", nil)
	r.ServeHTTP(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for admin user, got %d", wGet.Code)
	}

	// 2. Instant Token Generation
	wTok := httptest.NewRecorder()
	reqTok := httptest.NewRequest("POST", "/api/v1/users/user_admin_01/token", bytes.NewReader([]byte(`{"ttl_hours": 12}`)))
	reqTok.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wTok, reqTok)
	if wTok.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for instant token gen, got %d: %s", wTok.Code, wTok.Body.String())
	}

	// 3. Update User
	phone := "+1-999-0000"
	upPayload := UpdateUserRequest{
		PhoneNumber: &phone,
		Metadata: map[string]any{
			"auth_level": 100,
			"role":       "superadmin",
		},
	}
	upBody, _ := json.Marshal(upPayload)

	wUp := httptest.NewRecorder()
	reqUp := httptest.NewRequest("PUT", "/api/v1/users/user_admin_01", bytes.NewReader(upBody))
	reqUp.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wUp, reqUp)
	if wUp.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on update, got %d", wUp.Code)
	}

	// 4. Delete User
	wDel := httptest.NewRecorder()
	reqDel := httptest.NewRequest("DELETE", "/api/v1/users/user_demo_01", nil)
	r.ServeHTTP(wDel, reqDel)
	if wDel.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on delete, got %d", wDel.Code)
	}

	// 5. Verify deleted user not found
	wCheck := httptest.NewRecorder()
	reqCheck := httptest.NewRequest("GET", "/api/v1/users/user_demo_01", nil)
	r.ServeHTTP(wCheck, reqCheck)
	if wCheck.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on deleted user, got %d", wCheck.Code)
	}
}

func TestUserHandler_Level10ViewOnlyRestrictions(t *testing.T) {
	r, _ := setupUserTestRouter()

	// 1. Verify staff_user exists and can log in
	loginPayload := LoginRequest{
		Identifier: "staff_user",
		Password:   "Staff@123",
	}
	loginBody, _ := json.Marshal(loginPayload)

	wLogin := httptest.NewRecorder()
	reqLogin := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(loginBody))
	reqLogin.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wLogin, reqLogin)

	if wLogin.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on staff login, got %d: %s", wLogin.Code, wLogin.Body.String())
	}

	var loginResp struct {
		Data struct {
			Token     string `json:"token"`
			AuthLevel any    `json:"auth_level"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wLogin.Body.Bytes(), &loginResp)
	staffToken := loginResp.Data.Token
	if staffToken == "" {
		t.Fatalf("expected valid token for staff user")
	}

	// 2. Staff user can view users list (GET /api/v1/users) -> 200 OK
	wList := httptest.NewRecorder()
	reqList := httptest.NewRequest("GET", "/api/v1/users", nil)
	reqList.Header.Set("Authorization", "Bearer "+staffToken)
	r.ServeHTTP(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for staff viewing users list, got %d", wList.Code)
	}

	// 3. Staff user CANNOT update a user (PUT /api/v1/users/:id) -> 403 Forbidden
	newPhone := "+1-888-7777"
	upPayload := UpdateUserRequest{PhoneNumber: &newPhone}
	upBody, _ := json.Marshal(upPayload)

	wUp := httptest.NewRecorder()
	reqUp := httptest.NewRequest("PUT", "/api/v1/users/user_admin_01", bytes.NewReader(upBody))
	reqUp.Header.Set("Content-Type", "application/json")
	reqUp.Header.Set("Authorization", "Bearer "+staffToken)
	r.ServeHTTP(wUp, reqUp)

	if wUp.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden when level 10 attempts to update user, got %d: %s", wUp.Code, wUp.Body.String())
	}

	// 4. Staff user CANNOT delete a user (DELETE /api/v1/users/:id) -> 403 Forbidden
	wDel := httptest.NewRecorder()
	reqDel := httptest.NewRequest("DELETE", "/api/v1/users/user_demo_01", nil)
	reqDel.Header.Set("Authorization", "Bearer "+staffToken)
	r.ServeHTTP(wDel, reqDel)

	if wDel.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden when level 10 attempts to delete user, got %d: %s", wDel.Code, wDel.Body.String())
	}

	// 5. Staff user CANNOT create a new user (POST /api/v1/users) -> 403 Forbidden
	createPayload := RegisterUserRequest{
		Username: "malicious_user",
		Email:    "evil@local.test",
		Password: "password123",
	}
	createBody, _ := json.Marshal(createPayload)

	wCreate := httptest.NewRecorder()
	reqCreate := httptest.NewRequest("POST", "/api/v1/users", bytes.NewReader(createBody))
	reqCreate.Header.Set("Content-Type", "application/json")
	reqCreate.Header.Set("Authorization", "Bearer "+staffToken)
	r.ServeHTTP(wCreate, reqCreate)

	if wCreate.Code != http.StatusForbidden {
		t.Fatalf("expected 403 Forbidden when level 10 attempts to create user, got %d: %s", wCreate.Code, wCreate.Body.String())
	}

	// 6. Admin user (Level 99) CAN update users -> 200 OK
	adminLoginPayload := LoginRequest{
		Identifier: "admin",
		Password:   "Mp@tel98",
	}
	adminLoginBody, _ := json.Marshal(adminLoginPayload)
	wAdminLogin := httptest.NewRecorder()
	reqAdminLogin := httptest.NewRequest("POST", "/api/v1/auth/login", bytes.NewReader(adminLoginBody))
	reqAdminLogin.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wAdminLogin, reqAdminLogin)

	var adminLoginResp struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(wAdminLogin.Body.Bytes(), &adminLoginResp)
	adminToken := adminLoginResp.Data.Token

	wAdminUp := httptest.NewRecorder()
	reqAdminUp := httptest.NewRequest("PUT", "/api/v1/users/user_staff_10", bytes.NewReader(upBody))
	reqAdminUp.Header.Set("Content-Type", "application/json")
	reqAdminUp.Header.Set("Authorization", "Bearer "+adminToken)
	r.ServeHTTP(wAdminUp, reqAdminUp)

	if wAdminUp.Code != http.StatusOK {
		t.Fatalf("expected 200 OK when admin updates user, got %d: %s", wAdminUp.Code, wAdminUp.Body.String())
	}
}
