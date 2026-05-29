package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/NikitaPash/url-shortener/internal/domain"
	"github.com/NikitaPash/url-shortener/internal/handler"
	"github.com/NikitaPash/url-shortener/internal/service"
	"github.com/NikitaPash/url-shortener/internal/storage/postgres"
)

// --- shared fakes used across auth + link handler tests ---

type stubUserRepo struct {
	createFn      func(context.Context, string, string) (*domain.User, error)
	getByEmailFn  func(context.Context, string) (*domain.User, error)
	ensureAdminFn func(context.Context, string, string) error
}

func (r *stubUserRepo) Create(ctx context.Context, email, hash string) (*domain.User, error) {
	if r.createFn != nil {
		return r.createFn(ctx, email, hash)
	}
	return &domain.User{ID: "u1", Email: email}, nil
}
func (r *stubUserRepo) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	if r.getByEmailFn != nil {
		return r.getByEmailFn(ctx, email)
	}
	return nil, postgres.ErrUserNotFound
}
func (r *stubUserRepo) EnsureAdmin(ctx context.Context, email, hash string) error {
	if r.ensureAdminFn != nil {
		return r.ensureAdminFn(ctx, email, hash)
	}
	return nil
}

type stubDenylist struct{ denied []string }

func (d *stubDenylist) DenyJWT(_ context.Context, jti string, _ time.Duration) {
	d.denied = append(d.denied, jti)
}

func newAuthHandler(repo *stubUserRepo, dl *stubDenylist) *handler.AuthHandler {
	svc := service.NewAuthService(repo, dl, "test-secret", 24*time.Hour)
	return handler.NewAuthHandler(svc)
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewBuffer(b)
}

// --- Register ---

func TestAuthHandler_Register(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		repoCreate func(context.Context, string, string) (*domain.User, error)
		wantStatus int
	}{
		{
			name:       "valid input returns 201",
			body:       `{"email":"new@example.com","password":"pass1234"}`,
			wantStatus: http.StatusCreated,
		},
		{
			name:       "invalid JSON returns 400",
			body:       `not-json`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid email returns 400",
			body:       `{"email":"not-an-email","password":"pass1234"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "password too short returns 400",
			body:       `{"email":"a@b.com","password":"short"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "password over 72 bytes returns 400",
			body:       `{"email":"a@b.com","password":"` + strings.Repeat("x", 73) + `"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "duplicate email returns 409",
			body: `{"email":"dup@example.com","password":"pass1234"}`,
			repoCreate: func(_ context.Context, _, _ string) (*domain.User, error) {
				return nil, postgres.ErrEmailTaken
			},
			wantStatus: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubUserRepo{createFn: tt.repoCreate}
			h := newAuthHandler(repo, &stubDenylist{})

			req := httptest.NewRequest(http.MethodPost, "/auth/register", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.Register(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", w.Code, tt.wantStatus, w.Body.String())
			}
		})
	}
}

// --- Login ---

func TestAuthHandler_Login(t *testing.T) {
	hashOf := func(t *testing.T, pw string) string {
		t.Helper()
		h, _ := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.MinCost)
		return string(h)
	}

	tests := []struct {
		name         string
		body         string
		repoGetEmail func(*testing.T) func(context.Context, string) (*domain.User, error)
		wantStatus   int
		checkToken   bool
	}{
		{
			name: "valid credentials return 200 with token",
			body: `{"email":"a@b.com","password":"correctpass"}`,
			repoGetEmail: func(t *testing.T) func(context.Context, string) (*domain.User, error) {
				return func(_ context.Context, _ string) (*domain.User, error) {
					return &domain.User{ID: "u1", Email: "a@b.com", PasswordHash: hashOf(t, "correctpass")}, nil
				}
			},
			wantStatus: http.StatusOK,
			checkToken: true,
		},
		{
			name:       "invalid JSON returns 400",
			body:       `{bad-json`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "wrong password returns 401",
			body: `{"email":"a@b.com","password":"wrong"}`,
			repoGetEmail: func(t *testing.T) func(context.Context, string) (*domain.User, error) {
				return func(_ context.Context, _ string) (*domain.User, error) {
					return &domain.User{ID: "u1", PasswordHash: hashOf(t, "correctpass")}, nil
				}
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "unknown email returns 401",
			body: `{"email":"ghost@b.com","password":"whatever"}`,
			repoGetEmail: func(*testing.T) func(context.Context, string) (*domain.User, error) {
				return func(_ context.Context, _ string) (*domain.User, error) {
					return nil, postgres.ErrUserNotFound
				}
			},
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var getEmailFn func(context.Context, string) (*domain.User, error)
			if tt.repoGetEmail != nil {
				getEmailFn = tt.repoGetEmail(t)
			}
			repo := &stubUserRepo{getByEmailFn: getEmailFn}
			h := newAuthHandler(repo, &stubDenylist{})

			req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.Login(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.checkToken {
				var resp map[string]string
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode response: %v", err)
				}
				tok := resp["token"]
				if tok == "" {
					t.Fatal("expected non-empty token in response")
				}
				// Verify token is a valid JWT signed with our secret
				parsed, err := jwt.Parse(tok, func(*jwt.Token) (interface{}, error) {
					return []byte("test-secret"), nil
				})
				if err != nil || !parsed.Valid {
					t.Errorf("invalid JWT: %v", err)
				}
			}
		})
	}
}

// --- Logout ---

func TestAuthHandler_Logout(t *testing.T) {
	signToken := func(claims jwt.MapClaims) string {
		t, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret"))
		return t
	}

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
		wantDenied bool
	}{
		{
			name: "valid token with jti → 200 and token denied",
			authHeader: "Bearer " + signToken(jwt.MapClaims{
				"sub": "u1",
				"jti": "jti-xyz",
				"exp": float64(time.Now().Add(time.Hour).Unix()),
			}),
			wantStatus: http.StatusOK,
			wantDenied: true,
		},
		{
			name:       "missing Authorization header → 400",
			authHeader: "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "malformed header (no space) → 400",
			authHeader: "BearerNoSpace",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dl := &stubDenylist{}
			h := newAuthHandler(&stubUserRepo{}, dl)

			req := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()

			h.Logout(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.wantDenied && len(dl.denied) == 0 {
				t.Error("expected DenyJWT to be called, but it was not")
			}
		})
	}
}
