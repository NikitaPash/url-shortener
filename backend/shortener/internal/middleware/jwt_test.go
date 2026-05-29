package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/NikitaPash/url-shortener/internal/middleware"
)

// okHandler is a simple handler that returns 200 to confirm the middleware let the request through.
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

const testSecret = "test-secret"

func makeToken(secret string, claims jwt.MapClaims) string {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, _ := token.SignedString([]byte(secret))
	return signed
}

// jwtMiddleware wraps okHandler with JWTAuth. Passes nil for RedisCache — safe as long as
// test tokens contain no jti claim (skipping the denylist check).
func jwtMiddleware() http.Handler {
	return middleware.JWTAuth([]byte(testSecret), nil)(okHandler)
}

func TestJWTMissingHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	jwtMiddleware().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", w.Code)
	}
}

func TestJWTInvalidFormat(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "NotBearer token")
	w := httptest.NewRecorder()
	jwtMiddleware().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", w.Code)
	}
}

func TestJWTWrongSigningKey(t *testing.T) {
	token := makeToken("different-secret", jwt.MapClaims{"sub": "user1"})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	jwtMiddleware().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", w.Code)
	}
}

func TestJWTExpiredToken(t *testing.T) {
	token := makeToken(testSecret, jwt.MapClaims{
		"sub": "user1",
		"exp": time.Now().Add(-time.Hour).Unix(),
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	jwtMiddleware().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", w.Code)
	}
}

func TestJWTValidToken(t *testing.T) {
	// No jti claim — middleware skips the Redis denylist check, so nil cache is safe.
	token := makeToken(testSecret, jwt.MapClaims{
		"sub": "user-abc",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	jwtMiddleware().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}
