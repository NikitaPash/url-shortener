package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/NikitaPash/url-shortener/internal/middleware"
)

// mockDenylist implements middleware.DenylistChecker.
type mockDenylist struct {
	denied map[string]bool
}

func (m *mockDenylist) IsJWTDenied(_ context.Context, jti string) bool {
	return m.denied[jti]
}

func signWithJTI(secret, jti string) string {
	claims := jwt.MapClaims{
		"sub": "user-abc",
		"jti": jti,
		"exp": time.Now().Add(time.Hour).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, _ := tok.SignedString([]byte(secret))
	return s
}

func TestJWTAuth_RevokedToken_Returns401(t *testing.T) {
	const jti = "revoked-jti-001"
	dl := &mockDenylist{denied: map[string]bool{jti: true}}
	h := middleware.JWTAuth([]byte(testSecret), dl)(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+signWithJTI(testSecret, jti))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("revoked token: got %d, want 401", w.Code)
	}
}

func TestJWTAuth_ValidTokenNotDenylisted_Returns200(t *testing.T) {
	const jti = "live-jti-001"
	dl := &mockDenylist{denied: map[string]bool{}} // nothing denied
	h := middleware.JWTAuth([]byte(testSecret), dl)(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+signWithJTI(testSecret, jti))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("valid token: got %d, want 200", w.Code)
	}
}

func TestJWTAuth_NilDenylist_SkipsDenylistCheck(t *testing.T) {
	// Existing tests already pass nil, but this makes the intent explicit.
	// A token WITH jti must still succeed when the denylist is nil (no Redis).
	h := middleware.JWTAuth([]byte(testSecret), nil)(okHandler)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+signWithJTI(testSecret, "any-jti"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("nil denylist: got %d, want 200", w.Code)
	}
}
