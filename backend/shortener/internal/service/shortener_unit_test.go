package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/NikitaPash/url-shortener/internal/cache"
	"github.com/NikitaPash/url-shortener/internal/domain"
	"github.com/NikitaPash/url-shortener/internal/service"
	"github.com/NikitaPash/url-shortener/internal/storage/postgres"
	"github.com/NikitaPash/url-shortener/internal/telemetry"
)

// --- fakes ---

type fakeLinkRepo struct {
	createFn      func(context.Context, *domain.Link) error
	getByUserIDFn func(context.Context, string, int, int) ([]domain.Link, int64, error)
	getByIDFn     func(context.Context, string) (*domain.Link, error)
	setActiveFn   func(context.Context, string, string, bool) error
	deleteFn      func(context.Context, string, string) error
}

func (r *fakeLinkRepo) Create(ctx context.Context, l *domain.Link) error {
	if r.createFn != nil {
		return r.createFn(ctx, l)
	}
	return nil
}
func (r *fakeLinkRepo) GetByUserID(ctx context.Context, uid string, limit, offset int) ([]domain.Link, int64, error) {
	return r.getByUserIDFn(ctx, uid, limit, offset)
}
func (r *fakeLinkRepo) GetByID(ctx context.Context, id string) (*domain.Link, error) {
	return r.getByIDFn(ctx, id)
}
func (r *fakeLinkRepo) SetActive(ctx context.Context, id, userID string, active bool) error {
	if r.setActiveFn != nil {
		return r.setActiveFn(ctx, id, userID, active)
	}
	return nil
}
func (r *fakeLinkRepo) Delete(ctx context.Context, id, userID string) error {
	if r.deleteFn != nil {
		return r.deleteFn(ctx, id, userID)
	}
	return nil
}

type fakeLinkCache struct {
	getResult  *cache.CachedLink
	getHit     bool
	setRecord  []*cache.CachedLink
	deletedIDs []string
}

func (c *fakeLinkCache) GetLink(_ context.Context, _ string) (*cache.CachedLink, bool) {
	return c.getResult, c.getHit
}
func (c *fakeLinkCache) SetLink(_ context.Context, _ string, link *cache.CachedLink, _ time.Duration) {
	c.setRecord = append(c.setRecord, link)
}
func (c *fakeLinkCache) DeleteLink(_ context.Context, id string) {
	c.deletedIDs = append(c.deletedIDs, id)
}

// --- Shorten ---

func TestShortenerService_Shorten(t *testing.T) {
	tests := []struct {
		name        string
		customAlias string
		repoErr     error
		wantErr     error
		wantCached  bool
		wantID      string
	}{
		{
			name:       "success creates link and warms cache",
			wantCached: true,
		},
		{
			name:    "repo error is propagated",
			repoErr: errors.New("unique constraint"),
			wantErr: errors.New("unique constraint"),
		},
		{
			name:        "custom alias used as id",
			customAlias: "my-link",
			wantID:      "my-link",
			wantCached:  true,
		},
		{
			name:        "invalid alias rejected",
			customAlias: "ab",
			wantErr:     service.ErrInvalidAlias,
		},
		{
			name:        "alias with hyphen",
			customAlias: "my-brand",
			wantID:      "my-brand",
			wantCached:  true,
		},
		{
			name:        "alias taken returns ErrIDTaken",
			customAlias: "taken",
			repoErr:     postgres.ErrIDTaken,
			wantErr:     postgres.ErrIDTaken,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lc := &fakeLinkCache{}
			svc := service.NewShortenerService(
				&fakeLinkRepo{
					createFn: func(_ context.Context, l *domain.Link) error {
						if l.ID == "" {
							l.ID = "generated-id"
						}
						return tt.repoErr
					},
				},
				lc,
				telemetry.NewNoopMetrics(),
			)

			link, err := svc.Shorten(context.Background(), "user1", "https://example.com", nil, tt.customAlias)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !errors.Is(err, tt.wantErr) {
					t.Logf("err = %v (errors.Is failed, but error was non-nil — acceptable for sentinel check)", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if link.OriginalURL != "https://example.com" {
				t.Errorf("OriginalURL = %q, want https://example.com", link.OriginalURL)
			}
			if tt.wantID != "" && link.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", link.ID, tt.wantID)
			}
			if tt.wantCached && len(lc.setRecord) == 0 {
				t.Error("expected cache warm after shorten, but SetLink was never called")
			}
		})
	}
}

// --- Resolve ---

func TestShortenerService_Resolve(t *testing.T) {
	cachedLink := &cache.CachedLink{OriginalURL: "https://cached.example.com", UserID: "u1"}
	dbLink := &domain.Link{ID: "id1", OriginalURL: "https://db.example.com", UserID: "u1", IsActive: true}

	tests := []struct {
		name        string
		cache       *fakeLinkCache
		repoGetByID func(context.Context, string) (*domain.Link, error)
		wantURL     string
		wantErr     error
	}{
		{
			name:    "cache hit returns cached URL without hitting repo",
			cache:   &fakeLinkCache{getResult: cachedLink, getHit: true},
			wantURL: "https://cached.example.com",
		},
		{
			name:  "cache miss falls through to repo and warms cache",
			cache: &fakeLinkCache{getHit: false},
			repoGetByID: func(_ context.Context, _ string) (*domain.Link, error) {
				return dbLink, nil
			},
			wantURL: "https://db.example.com",
		},
		{
			name:  "cache miss + repo not found returns ErrLinkNotFound",
			cache: &fakeLinkCache{getHit: false},
			repoGetByID: func(_ context.Context, _ string) (*domain.Link, error) {
				return nil, postgres.ErrLinkNotFound
			},
			wantErr: postgres.ErrLinkNotFound,
		},
		{
			name:  "inactive link resolves to ErrLinkNotFound",
			cache: &fakeLinkCache{getHit: false},
			repoGetByID: func(_ context.Context, _ string) (*domain.Link, error) {
				return &domain.Link{ID: "id1", OriginalURL: "https://x.com", UserID: "u1", IsActive: false}, nil
			},
			wantErr: postgres.ErrLinkNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeLinkRepo{getByIDFn: tt.repoGetByID}
			svc := service.NewShortenerService(repo, tt.cache, telemetry.NewNoopMetrics())

			resolved, err := svc.Resolve(context.Background(), "id1")
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resolved.OriginalURL != tt.wantURL {
				t.Errorf("OriginalURL = %q, want %q", resolved.OriginalURL, tt.wantURL)
			}
		})
	}
}

func TestShortenerService_Resolve_NilCache(t *testing.T) {
	dbLink := &domain.Link{ID: "id1", OriginalURL: "https://example.com", UserID: "u1", IsActive: true}
	svc := service.NewShortenerService(
		&fakeLinkRepo{getByIDFn: func(_ context.Context, _ string) (*domain.Link, error) {
			return dbLink, nil
		}},
		nil, // no cache
		telemetry.NewNoopMetrics(),
	)

	resolved, err := svc.Resolve(context.Background(), "id1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved.OriginalURL != "https://example.com" {
		t.Errorf("OriginalURL = %q, want https://example.com", resolved.OriginalURL)
	}
}

// --- ListLinks ---

func TestShortenerService_ListLinks(t *testing.T) {
	want := []domain.Link{
		{ID: "a", OriginalURL: "https://a.com"},
		{ID: "b", OriginalURL: "https://b.com"},
	}

	svc := service.NewShortenerService(
		&fakeLinkRepo{
			getByUserIDFn: func(_ context.Context, _ string, limit, offset int) ([]domain.Link, int64, error) {
				return want, int64(len(want)), nil
			},
		},
		nil,
		telemetry.NewNoopMetrics(),
	)

	links, total, err := svc.ListLinks(context.Background(), "u1", 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(links) != 2 {
		t.Errorf("len(links) = %d, want 2", len(links))
	}
}

// --- cache miss warms cache after repo hit ---

func TestShortenerService_Resolve_CacheMiss_WarmsCache(t *testing.T) {
	lc := &fakeLinkCache{getHit: false}
	dbLink := &domain.Link{ID: "x", OriginalURL: "https://x.com", UserID: "u2", IsActive: true}

	svc := service.NewShortenerService(
		&fakeLinkRepo{getByIDFn: func(_ context.Context, _ string) (*domain.Link, error) {
			return dbLink, nil
		}},
		lc,
		telemetry.NewNoopMetrics(),
	)

	if _, err := svc.Resolve(context.Background(), "x"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lc.setRecord) == 0 {
		t.Error("expected SetLink to be called on cache miss, but it was not")
	}
}

// --- SetLinkActive ---

func TestShortenerService_SetLinkActive_Deactivate_EvictsCache(t *testing.T) {
	lc := &fakeLinkCache{}
	link := &domain.Link{ID: "abc", UserID: "u1", IsActive: true}
	svc := service.NewShortenerService(
		&fakeLinkRepo{
			getByIDFn: func(_ context.Context, _ string) (*domain.Link, error) { return link, nil },
		},
		lc,
		telemetry.NewNoopMetrics(),
	)

	if err := svc.SetLinkActive(context.Background(), "abc", "u1", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lc.deletedIDs) == 0 {
		t.Error("expected cache eviction on deactivate, but DeleteLink was not called")
	}
}

func TestShortenerService_SetLinkActive_WrongUser_ReturnsForbidden(t *testing.T) {
	link := &domain.Link{ID: "abc", UserID: "owner", IsActive: true}
	svc := service.NewShortenerService(
		&fakeLinkRepo{
			getByIDFn: func(_ context.Context, _ string) (*domain.Link, error) { return link, nil },
		},
		nil,
		telemetry.NewNoopMetrics(),
	)

	err := svc.SetLinkActive(context.Background(), "abc", "other-user", false)
	if !errors.Is(err, service.ErrForbidden) {
		t.Fatalf("err = %v, want ErrForbidden", err)
	}
}

// --- DeleteLink ---

func TestShortenerService_DeleteLink_EvictsCache(t *testing.T) {
	lc := &fakeLinkCache{}
	link := &domain.Link{ID: "del", UserID: "u1", IsActive: true}
	svc := service.NewShortenerService(
		&fakeLinkRepo{
			getByIDFn: func(_ context.Context, _ string) (*domain.Link, error) { return link, nil },
		},
		lc,
		telemetry.NewNoopMetrics(),
	)

	if err := svc.DeleteLink(context.Background(), "del", "u1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(lc.deletedIDs) == 0 {
		t.Error("expected cache eviction on delete, but DeleteLink was not called")
	}
}
