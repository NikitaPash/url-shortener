package service

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"time"

	gonanoid "github.com/matoous/go-nanoid/v2"

	"github.com/NikitaPash/url-shortener/internal/cache"
	"github.com/NikitaPash/url-shortener/internal/domain"
	"github.com/NikitaPash/url-shortener/internal/storage/postgres"
	"github.com/NikitaPash/url-shortener/internal/telemetry"
)

var (
	ErrInvalidAlias = errors.New("alias must be 3–32 characters, lowercase letters, digits, and hyphens only; cannot start or end with a hyphen")
	ErrForbidden    = errors.New("resource belongs to another user")
)

var aliasRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$`)

func validateAlias(alias string) error {
	if len(alias) < 3 || len(alias) > 32 {
		return ErrInvalidAlias
	}
	if !aliasRe.MatchString(alias) {
		return ErrInvalidAlias
	}
	return nil
}

const (
	defaultIDLength = 8
	// expiredCacheTTL keeps an already-expired link out of the cache almost
	// immediately while still avoiding a zero TTL (which means "no expiry").
	expiredCacheTTL = 1 * time.Second
)

// LinkRepository is the persistence behavior ShortenerService depends on.
type LinkRepository interface {
	Create(ctx context.Context, link *domain.Link) error
	GetByUserID(ctx context.Context, userID string, limit, offset int) ([]domain.Link, int64, error)
	GetByID(ctx context.Context, id string) (*domain.Link, error)
	SetActive(ctx context.Context, id, userID string, active bool) error
	Delete(ctx context.Context, id, userID string) error
}

// LinkCache is the caching behavior used on the resolve hot path.
type LinkCache interface {
	GetLink(ctx context.Context, id string) (*cache.CachedLink, bool)
	SetLink(ctx context.Context, id string, link *cache.CachedLink, ttl time.Duration)
	DeleteLink(ctx context.Context, id string)
}

// ResolvedLink contains everything the redirect handler needs from one call:
// the target URL for the 302 and the user_id for the analytics event.
type ResolvedLink struct {
	OriginalURL string
	UserID      string
}

type ShortenerService struct {
	linkRepo  LinkRepository
	linkCache LinkCache
	metrics   *telemetry.Metrics
}

func NewShortenerService(
	repo LinkRepository,
	linkCache LinkCache,
	metrics *telemetry.Metrics,
) *ShortenerService {
	if metrics == nil {
		metrics = telemetry.NewNoopMetrics()
	}
	return &ShortenerService{
		linkRepo:  repo,
		linkCache: linkCache,
		metrics:   metrics,
	}
}

func (s *ShortenerService) Shorten(
	ctx context.Context,
	userID, originalURL string,
	expiresAt *time.Time,
	customAlias string,
) (*domain.Link, error) {
	var id string
	if customAlias != "" {
		if err := validateAlias(customAlias); err != nil {
			return nil, err
		}
		id = customAlias
	} else {
		generated, err := gonanoid.New(defaultIDLength)
		if err != nil {
			return nil, err
		}
		id = generated
	}

	link := &domain.Link{
		ID:          id,
		UserID:      userID,
		OriginalURL: originalURL,
		ExpiresAt:   expiresAt,
	}

	if err := s.linkRepo.Create(ctx, link); err != nil {
		return nil, err
	}

	if s.linkCache != nil {
		s.linkCache.SetLink(ctx, link.ID, &cache.CachedLink{
			OriginalURL: link.OriginalURL,
			UserID:      link.UserID,
		}, cacheTTL(expiresAt))
	}

	return link, nil
}

func (s *ShortenerService) GetLink(ctx context.Context, id string) (*domain.Link, error) {
	return s.linkRepo.GetByID(ctx, id)
}

func (s *ShortenerService) ListLinks(
	ctx context.Context,
	userID string,
	limit, offset int,
) ([]domain.Link, int64, error) {
	return s.linkRepo.GetByUserID(ctx, userID, limit, offset)
}

// SetLinkActive activates or deactivates a link. Deactivating evicts it from the
// redirect cache so the next redirect hits the DB and returns 404 immediately.
func (s *ShortenerService) SetLinkActive(ctx context.Context, id, userID string, active bool) error {
	link, err := s.linkRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if link.UserID != userID {
		return ErrForbidden
	}
	if err := s.linkRepo.SetActive(ctx, id, userID, active); err != nil {
		return err
	}
	if !active && s.linkCache != nil {
		s.linkCache.DeleteLink(ctx, id)
	}
	return nil
}

// DeleteLink hard-deletes a link and removes it from the redirect cache.
func (s *ShortenerService) DeleteLink(ctx context.Context, id, userID string) error {
	link, err := s.linkRepo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if link.UserID != userID {
		return ErrForbidden
	}
	if err := s.linkRepo.Delete(ctx, id, userID); err != nil {
		return err
	}
	if s.linkCache != nil {
		s.linkCache.DeleteLink(ctx, id)
	}
	return nil
}

// Resolve looks up a short link by ID, returning the target URL and owning user.
// Inactive links resolve to ErrLinkNotFound so the redirect handler returns 404.
func (s *ShortenerService) Resolve(ctx context.Context, id string) (*ResolvedLink, error) {
	// 1. Check Redis cache (only active links are cached).
	if s.linkCache != nil {
		if cached, ok := s.linkCache.GetLink(ctx, id); ok {
			slog.Debug("cache hit", "id", id)
			s.metrics.CacheHit.Add(ctx, 1)
			return &ResolvedLink{OriginalURL: cached.OriginalURL, UserID: cached.UserID}, nil
		}
		slog.Debug("cache miss", "id", id)
		s.metrics.CacheMiss.Add(ctx, 1)
	}

	// 2. Query PostgreSQL.
	link, err := s.linkRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// 3. Inactive links are treated as not found on the redirect path.
	if !link.IsActive {
		return nil, postgres.ErrLinkNotFound
	}

	// 4. Lazy-populate cache (only for active links).
	if s.linkCache != nil {
		s.linkCache.SetLink(ctx, link.ID, &cache.CachedLink{
			OriginalURL: link.OriginalURL,
			UserID:      link.UserID,
		}, cacheTTL(link.ExpiresAt))
	}

	return &ResolvedLink{OriginalURL: link.OriginalURL, UserID: link.UserID}, nil
}

func cacheTTL(expiresAt *time.Time) time.Duration {
	if expiresAt == nil {
		return 0
	}
	remaining := time.Until(*expiresAt)
	if remaining <= 0 {
		return expiredCacheTTL
	}
	return remaining
}
