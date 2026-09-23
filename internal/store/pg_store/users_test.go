package pgstore

import (
	"context"
	"testing"
)

func TestUserModel_PasswordHashing(t *testing.T) {
	rawPassword := "SecurePass123!"
	hashed, err := HashPassword(rawPassword)
	if err != nil {
		t.Fatalf("failed to hash password: %v", err)
	}

	if hashed == rawPassword {
		t.Fatalf("hashed password must not match raw password")
	}

	if !CheckPassword(hashed, rawPassword) {
		t.Fatalf("expected valid password verification")
	}

	if CheckPassword(hashed, "WrongPass") {
		t.Fatalf("expected failed verification on invalid password")
	}
}

func TestUserModel_GetAuthLevel(t *testing.T) {
	u1 := UserModel{
		Metadata: map[string]any{
			"auth_level": 99,
		},
	}
	if u1.GetAuthLevel() != 99 {
		t.Errorf("expected auth_level 99, got %v", u1.GetAuthLevel())
	}

	u2 := UserModel{
		Metadata: map[string]any{
			"role": "admin",
		},
	}
	if u2.GetAuthLevel() != "admin" {
		t.Errorf("expected role 'admin', got %v", u2.GetAuthLevel())
	}

	u3 := UserModel{
		Metadata: nil,
	}
	if u3.GetAuthLevel() != 1 {
		t.Errorf("expected default auth_level 1, got %v", u3.GetAuthLevel())
	}
}

func TestUserStore_NilDBErrors(t *testing.T) {
	ctx := context.Background()

	if err := InitUserSchema(ctx, nil); err == nil {
		t.Errorf("expected error with nil db client")
	}

	if err := CreateUser(ctx, nil, &UserModel{}); err == nil {
		t.Errorf("expected error with nil db client")
	}

	if _, err := GetUserByID(ctx, nil, "123"); err == nil {
		t.Errorf("expected error with nil db client")
	}

	if _, err := GetUserByEmailOrUsername(ctx, nil, "test"); err == nil {
		t.Errorf("expected error with nil db client")
	}

	if _, err := ListUsers(ctx, nil, 10, 0); err == nil {
		t.Errorf("expected error with nil db client")
	}

	if err := UpdateUser(ctx, nil, &UserModel{}); err == nil {
		t.Errorf("expected error with nil db client")
	}

	if err := DeleteUser(ctx, nil, "123"); err == nil {
		t.Errorf("expected error with nil db client")
	}
}
