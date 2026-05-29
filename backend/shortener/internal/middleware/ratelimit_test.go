package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/NikitaPash/url-shortener/internal/cache"
	"github.com/NikitaPash/url-shortener/internal/middleware"
)

// --- mock RateLimitStore ---

type mockRateLimitStore struct {
	isBlacklisted  func(ctx context.Context, ip string) bool
	checkRateLimit func(ctx context.Context, key string, limit int64, window time.Duration) cache.RateLimitResult
}

func (m *mockRateLimitStore) IsBlacklisted(ctx context.Context, ip string) bool {
	return m.isBlacklisted(ctx, ip)
}
func (m *mockRateLimitStore) CheckRateLimit(ctx context.Context, key string, limit int64, window time.Duration) cache.RateLimitResult {
	return m.checkRateLimit(ctx, key, limit, window)
}

var okHandlerRL = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
})

func TestRateLimit_Allowed(t *testing.T) {
	store := &mockRateLimitStore{
		isBlacklisted: func(_ context.Context, _ string) bool { return false },
		checkRateLimit: func(_ context.Context, _ string, limit int64, w time.Duration) cache.RateLimitResult {
			return cache.RateLimitResult{
				Allowed:   true,
				Limit:     limit,
				Remaining: limit - 1,
				ResetAt:   time.Now().Add(w),
			}
		},
	}

	cfg := middleware.RateLimiterConfig{Name: "test", Limit: 10, Window: time.Minute}
	h := middleware.RateLimit(store, cfg)(okHandlerRL)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:5000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Header().Get("X-RateLimit-Limit") != "10" {
		t.Errorf("X-RateLimit-Limit = %q, want 10", w.Header().Get("X-RateLimit-Limit"))
	}
	if w.Header().Get("X-RateLimit-Remaining") == "" {
		t.Error("X-RateLimit-Remaining header missing")
	}
	if w.Header().Get("X-RateLimit-Reset") == "" {
		t.Error("X-RateLimit-Reset header missing")
	}
}

func TestRateLimit_Exceeded(t *testing.T) {
	store := &mockRateLimitStore{
		isBlacklisted: func(_ context.Context, _ string) bool { return false },
		checkRateLimit: func(_ context.Context, _ string, limit int64, w time.Duration) cache.RateLimitResult {
			return cache.RateLimitResult{
				Allowed:   false,
				Limit:     limit,
				Remaining: 0,
				ResetAt:   time.Now().Add(w),
			}
		},
	}

	cfg := middleware.RateLimiterConfig{Name: "test", Limit: 5, Window: time.Minute}
	h := middleware.RateLimit(store, cfg)(okHandlerRL)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:5000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("Retry-After header missing on 429 response")
	}
}

func TestRateLimit_BlacklistedIP(t *testing.T) {
	store := &mockRateLimitStore{
		isBlacklisted: func(_ context.Context, _ string) bool { return true },
		checkRateLimit: func(_ context.Context, _ string, _ int64, _ time.Duration) cache.RateLimitResult {
			return cache.RateLimitResult{}
		},
	}

	cfg := middleware.RateLimiterConfig{Name: "test", Limit: 10, Window: time.Minute}
	h := middleware.RateLimit(store, cfg)(okHandlerRL)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "9.9.9.9:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", w.Code)
	}
}

func TestRateLimit_HeadersReflectConfiguredLimit(t *testing.T) {
	const wantLimit int64 = 42
	store := &mockRateLimitStore{
		isBlacklisted: func(_ context.Context, _ string) bool { return false },
		checkRateLimit: func(_ context.Context, _ string, limit int64, w time.Duration) cache.RateLimitResult {
			return cache.RateLimitResult{
				Allowed:   true,
				Limit:     limit,
				Remaining: limit - 3,
				ResetAt:   time.Now().Add(w),
			}
		},
	}

	cfg := middleware.RateLimiterConfig{Name: "api", Limit: wantLimit, Window: time.Minute}
	h := middleware.RateLimit(store, cfg)(okHandlerRL)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "5.5.5.5:9999"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if got := w.Header().Get("X-RateLimit-Limit"); got != "42" {
		t.Errorf("X-RateLimit-Limit = %q, want 42", got)
	}
	if got := w.Header().Get("X-RateLimit-Remaining"); got != "39" {
		t.Errorf("X-RateLimit-Remaining = %q, want 39", got)
	}
}
