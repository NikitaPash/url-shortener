package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/NikitaPash/url-shortener/internal/domain"
	"github.com/NikitaPash/url-shortener/internal/service"
	"github.com/NikitaPash/url-shortener/internal/storage/postgres"
)

// --- fakes ---

type fakeUserRepo struct {
	createFn      func(context.Context, string, string) (*domain.User, error)
	getByEmailFn  func(context.Context, string) (*domain.User, error)
	ensureAdminFn func(context.Context, string, string) error
}

func (r *fakeUserRepo) Create(ctx context.Context, email, hash string) (*domain.User, error) {
	return r.createFn(ctx, email, hash)
}
func (r *fakeUserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	return r.getByEmailFn(ctx, email)
}
func (r *fakeUserRepo) EnsureAdmin(ctx context.Context, email, hash string) error {
	return r.ensureAdminFn(ctx, email, hash)
}

type fakeDenylistUnit struct{ denied []string }

func (d *fakeDenylistUnit) DenyJWT(_ context.Context, jti string, _ time.Duration) {
	d.denied = append(d.denied, jti)
}

func hashPw(t *testing.T, pw string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return string(h)
}

// --- Register ---

func TestAuthService_Register(t *testing.T) {
	tests := []struct {
		name    string
		repoFn  func(context.Context, string, string) (*domain.User, error)
		wantErr error
	}{
		{
			name: "success creates user",
			repoFn: func(_ context.Context, email, _ string) (*domain.User, error) {
				return &domain.User{ID: "u1", Email: email}, nil
			},
		},
		{
			name: "duplicate email returns ErrEmailTaken",
			repoFn: func(_ context.Context, _, _ string) (*domain.User, error) {
				return nil, postgres.ErrEmailTaken
			},
			wantErr: postgres.ErrEmailTaken,
		},
		{
			name: "repo error propagated",
			repoFn: func(_ context.Context, _, _ string) (*domain.User, error) {
				return nil, errors.New("db timeout")
			},
			wantErr: errors.New("db timeout"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := service.NewAuthService(
				&fakeUserRepo{createFn: tt.repoFn},
				&fakeDenylistUnit{},
				"secret", time.Hour,
			)
			user, err := svc.Register(context.Background(), "a@b.com", "password123")
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if user == nil {
				t.Fatal("expected non-nil user")
			}
		})
	}
}

// --- Login ---

func TestAuthService_Login(t *testing.T) {
	const secret = "test-secret"
	const correctPw = "correct-password"

	tests := []struct {
		name       string
		password   string
		repoResult func(*testing.T) (*domain.User, error)
		wantErr    error
		checkToken bool
	}{
		{
			name:     "valid credentials return signed JWT",
			password: correctPw,
			repoResult: func(t *testing.T) (*domain.User, error) {
				return &domain.User{ID: "u1", Email: "a@b.com", PasswordHash: hashPw(t, correctPw)}, nil
			},
			checkToken: true,
		},
		{
			name:     "user not found maps to ErrInvalidCredentials",
			password: "whatever",
			repoResult: func(*testing.T) (*domain.User, error) {
				return nil, postgres.ErrUserNotFound
			},
			wantErr: service.ErrInvalidCredentials,
		},
		{
			name:     "wrong password maps to ErrInvalidCredentials",
			password: "wrong",
			repoResult: func(t *testing.T) (*domain.User, error) {
				return &domain.User{ID: "u1", PasswordHash: hashPw(t, correctPw)}, nil
			},
			wantErr: service.ErrInvalidCredentials,
		},
		{
			name:     "unexpected repo error is propagated",
			password: "whatever",
			repoResult: func(*testing.T) (*domain.User, error) {
				return nil, errors.New("db connection refused")
			},
			wantErr: errors.New("any non-nil"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user, repoErr := tt.repoResult(t)
			svc := service.NewAuthService(
				&fakeUserRepo{
					getByEmailFn: func(_ context.Context, _ string) (*domain.User, error) {
						return user, repoErr
					},
				},
				&fakeDenylistUnit{},
				secret, 24*time.Hour,
			)

			tok, err := svc.Login(context.Background(), "a@b.com", tt.password)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error, got token %q", tok)
				}
				if errors.Is(tt.wantErr, service.ErrInvalidCredentials) {
					if !errors.Is(err, service.ErrInvalidCredentials) {
						t.Fatalf("got %v, want ErrInvalidCredentials", err)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.checkToken {
				parsed, parseErr := jwt.Parse(tok, func(*jwt.Token) (interface{}, error) {
					return []byte(secret), nil
				})
				if parseErr != nil {
					t.Fatalf("invalid JWT: %v", parseErr)
				}
				claims := parsed.Claims.(jwt.MapClaims)
				if claims["sub"] != "u1" {
					t.Errorf("sub = %v, want u1", claims["sub"])
				}
				if _, ok := claims["jti"]; !ok {
					t.Error("JWT missing jti claim")
				}
				if _, ok := claims["is_admin"]; !ok {
					t.Error("JWT missing is_admin claim")
				}
			}
		})
	}
}

// --- Logout ---

func TestAuthService_Logout(t *testing.T) {
	const secret = "test-secret"

	sign := func(claims jwt.MapClaims) string {
		t, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
		return t
	}

	tests := []struct {
		name       string
		token      string
		wantErr    bool
		wantDenied bool
	}{
		{
			name: "future-expiry token with jti → denylist called",
			token: sign(jwt.MapClaims{
				"sub": "u1",
				"jti": "jti-abc",
				"exp": float64(time.Now().Add(time.Hour).Unix()),
			}),
			wantDenied: true,
		},
		{
			name: "already-expired token → no-op, no error",
			token: sign(jwt.MapClaims{
				"sub": "u1",
				"jti": "jti-old",
				"exp": float64(time.Now().Add(-time.Hour).Unix()),
			}),
			wantDenied: false,
		},
		{
			name: "token without jti claim → error",
			token: sign(jwt.MapClaims{
				"sub": "u1",
				"exp": float64(time.Now().Add(time.Hour).Unix()),
			}),
			wantErr: true,
		},
		{
			name:    "garbage string → error",
			token:   "not.a.jwt",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dl := &fakeDenylistUnit{}
			svc := service.NewAuthService(&fakeUserRepo{}, dl, secret, time.Hour)

			err := svc.Logout(context.Background(), tt.token)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantDenied != (len(dl.denied) > 0) {
				t.Errorf("wantDenied=%v but denied=%v", tt.wantDenied, dl.denied)
			}
		})
	}
}

// --- SeedAdmin ---

func TestAuthService_SeedAdmin(t *testing.T) {
	tests := []struct {
		name    string
		repoErr error
		wantErr bool
	}{
		{name: "success", repoErr: nil, wantErr: false},
		{name: "repo error propagated", repoErr: errors.New("db error"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotEmail string
			svc := service.NewAuthService(
				&fakeUserRepo{
					ensureAdminFn: func(_ context.Context, email, _ string) error {
						gotEmail = email
						return tt.repoErr
					},
				},
				&fakeDenylistUnit{}, "secret", time.Hour,
			)

			err := svc.SeedAdmin(context.Background(), "admin@example.com", "adminpass123")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotEmail != "admin@example.com" {
				t.Errorf("email = %q, want admin@example.com", gotEmail)
			}
		})
	}
}
