package cache

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
)

var tracer = otel.Tracer("cache/redis")

const (
	linkKeyPrefix   = "link:"
	linkDefaultTTL  = 24 * time.Hour
	rateLimitPrefix = "rl:"
	blacklistPrefix = "bl:"
	jwtDenyPrefix   = "jwt:deny:"
)

// Connection-pool tuning. Modest defaults sized for the redirect hot path,
// where Redis lookups must be fast and are safe to fail open.
const (
	poolSize     = 20
	minIdleConns = 5
	readTimeout  = 500 * time.Millisecond
	writeTimeout = 500 * time.Millisecond
	dialTimeout  = 1 * time.Second
	pingTimeout  = 2 * time.Second
)

// CachedLink is the structure stored in Redis for each short link.
// Storing both fields avoids a PostgreSQL lookup on every redirect.
type CachedLink struct {
	OriginalURL string `json:"url"`
	UserID      string `json:"user_id"`
}

type RedisCache struct {
	client *redis.Client
}

func NewRedisCache(addr, password string, db int) (*RedisCache, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		PoolSize:     poolSize,
		MinIdleConns: minIdleConns,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		DialTimeout:  dialTimeout,
	})

	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		slog.Warn("redis not available at startup — cache disabled until reconnect",
			"addr", addr, "error", err)
	} else {
		slog.Info("redis connected", "addr", addr)
	}

	return &RedisCache{client: client}, nil
}

func (c *RedisCache) Close() error {
	return c.client.Close()
}

// Ping verifies connectivity to Redis. Used by the readiness probe.
func (c *RedisCache) Ping(ctx context.Context) error {
	return c.client.Ping(ctx).Err()
}

// GetLink retrieves a cached link. Returns (nil, false) on miss or error.
func (c *RedisCache) GetLink(ctx context.Context, id string) (*CachedLink, bool) {
	ctx, span := tracer.Start(ctx, "redis.GetLink")
	defer span.End()

	val, err := c.client.Get(ctx, linkKeyPrefix+id).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			slog.Warn("redis GET error", "key", linkKeyPrefix+id, "error", err)
		}
		return nil, false
	}

	var link CachedLink
	if err := json.Unmarshal([]byte(val), &link); err != nil {
		slog.Warn("redis unmarshal error", "key", linkKeyPrefix+id, "error", err)
		return nil, false
	}
	return &link, true
}

// SetLink caches a link entry. Errors are logged, not returned.
func (c *RedisCache) SetLink(ctx context.Context, id string, link *CachedLink, ttl time.Duration) {
	ctx, span := tracer.Start(ctx, "redis.SetLink")
	defer span.End()

	if ttl == 0 {
		ttl = linkDefaultTTL
	}
	data, err := json.Marshal(link)
	if err != nil {
		slog.Warn("redis marshal error", "key", linkKeyPrefix+id, "error", err)
		return
	}
	if err := c.client.Set(ctx, linkKeyPrefix+id, data, ttl).Err(); err != nil {
		slog.Warn("redis SET error", "key", linkKeyPrefix+id, "error", err)
	}
}

// DeleteLink removes a cached link entry.
func (c *RedisCache) DeleteLink(ctx context.Context, id string) {
	if err := c.client.Del(ctx, linkKeyPrefix+id).Err(); err != nil {
		slog.Warn("redis DEL error", "key", linkKeyPrefix+id, "error", err)
	}
}

// RateLimitResult holds the outcome of a rate limit check.
type RateLimitResult struct {
	Allowed   bool
	Current   int64
	Limit     int64
	Remaining int64
	ResetAt   time.Time
}

// CheckRateLimit increments the counter for a key. Fails open if Redis is down.
func (c *RedisCache) CheckRateLimit(
	ctx context.Context,
	key string,
	limit int64,
	window time.Duration,
) RateLimitResult {
	redisKey := rateLimitPrefix + key

	pipe := c.client.Pipeline()
	incrCmd := pipe.Incr(ctx, redisKey)
	pipe.Expire(ctx, redisKey, window)
	_, err := pipe.Exec(ctx)

	if err != nil {
		slog.Warn("rate limit check failed — allowing request", "key", key, "error", err)
		return RateLimitResult{Allowed: true, Limit: limit, Remaining: limit}
	}

	current := incrCmd.Val()
	remaining := limit - current
	if remaining < 0 {
		remaining = 0
	}

	return RateLimitResult{
		Allowed:   current <= limit,
		Current:   current,
		Limit:     limit,
		Remaining: remaining,
		ResetAt:   time.Now().Add(window),
	}
}

// IsBlacklisted checks whether an IP is permanently banned. Fails open.
func (c *RedisCache) IsBlacklisted(ctx context.Context, ip string) bool {
	exists, err := c.client.Exists(ctx, blacklistPrefix+ip).Result()
	if err != nil {
		slog.Warn("blacklist check failed — allowing request", "ip", ip, "error", err)
		return false
	}
	return exists > 0
}

// BlacklistIP permanently bans an IP.
func (c *RedisCache) BlacklistIP(ctx context.Context, ip string) {
	if err := c.client.Set(ctx, blacklistPrefix+ip, "1", 0).Err(); err != nil {
		slog.Warn("failed to blacklist IP", "ip", ip, "error", err)
	}
}

// DenyJWT adds a token ID to the denylist until the token's natural expiry.
func (c *RedisCache) DenyJWT(ctx context.Context, jti string, expiration time.Duration) {
	if err := c.client.Set(ctx, jwtDenyPrefix+jti, "1", expiration).Err(); err != nil {
		slog.Warn("failed to deny JWT", "jti", jti, "error", err)
	}
}

// IsJWTDenied checks whether a token has been revoked. Fails closed for security.
func (c *RedisCache) IsJWTDenied(ctx context.Context, jti string) bool {
	exists, err := c.client.Exists(ctx, jwtDenyPrefix+jti).Result()
	if err != nil {
		slog.Warn("JWT denylist check failed — rejecting token for safety", "jti", jti, "error", err)
		return true
	}
	return exists > 0
}
