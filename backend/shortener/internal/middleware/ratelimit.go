package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/NikitaPash/url-shortener/internal/cache"
)

type RateLimiterConfig struct {
	// Name identifies the limiter bucket. It must be stable per route group —
	// using the request path would give the redirect route ("/{id}") a separate
	// bucket per short link, defeating the intended per-IP limit.
	Name   string
	Limit  int64
	Window time.Duration
}

// RateLimitStore is the cache behavior RateLimit depends on.
// *cache.RedisCache satisfies this interface.
type RateLimitStore interface {
	IsBlacklisted(ctx context.Context, ip string) bool
	CheckRateLimit(ctx context.Context, key string, limit int64, window time.Duration) cache.RateLimitResult
}

func RateLimit(store RateLimitStore, cfg RateLimiterConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr

			if store.IsBlacklisted(r.Context(), ip) {
				http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
				return
			}

			key := fmt.Sprintf("%s:%s", cfg.Name, ip)
			result := store.CheckRateLimit(r.Context(), key, cfg.Limit, cfg.Window)

			w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(result.Limit, 10))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(result.ResetAt.Unix(), 10))

			if !result.Allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(cfg.Window.Seconds())))
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
