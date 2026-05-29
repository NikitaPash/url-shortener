package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/NikitaPash/url-shortener/internal/service"
	"github.com/NikitaPash/url-shortener/internal/storage/postgres"
)

func TestRegisterAndLogin(t *testing.T) {
	if testPool == nil {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()
	userRepo := postgres.NewUserRepo(testPool)
	// nil cache: Register and Login do not touch Redis.
	authSvc := service.NewAuthService(userRepo, nil, "test-secret", time.Hour)

	email := fmt.Sprintf("auth-test-%d@example.com", time.Now().UnixNano())

	user, err := authSvc.Register(ctx, email, "password123")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if user.Email != email {
		t.Errorf("email: got %q, want %q", user.Email, email)
	}

	token, err := authSvc.Login(ctx, email, "password123")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if token == "" {
		t.Fatal("Login returned empty token")
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, "DELETE FROM users WHERE email = $1", email)
	})
}

func TestLoginWrongPassword(t *testing.T) {
	if testPool == nil {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()
	userRepo := postgres.NewUserRepo(testPool)
	authSvc := service.NewAuthService(userRepo, nil, "test-secret", time.Hour)

	email := fmt.Sprintf("auth-wrong-%d@example.com", time.Now().UnixNano())

	if _, err := authSvc.Register(ctx, email, "correctpassword"); err != nil {
		t.Fatalf("Register: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, "DELETE FROM users WHERE email = $1", email)
	})

	_, err := authSvc.Login(ctx, email, "wrongpassword")
	if !errors.Is(err, service.ErrInvalidCredentials) {
		t.Errorf("Login with wrong password: got %v, want ErrInvalidCredentials", err)
	}
}

func TestRegisterDuplicateEmail(t *testing.T) {
	if testPool == nil {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()
	userRepo := postgres.NewUserRepo(testPool)
	authSvc := service.NewAuthService(userRepo, nil, "test-secret", time.Hour)

	email := fmt.Sprintf("dup-%d@example.com", time.Now().UnixNano())

	if _, err := authSvc.Register(ctx, email, "pass"); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, "DELETE FROM users WHERE email = $1", email)
	})

	_, err := authSvc.Register(ctx, email, "pass")
	if !errors.Is(err, postgres.ErrEmailTaken) {
		t.Errorf("duplicate Register: got %v, want ErrEmailTaken", err)
	}
}
