package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NikitaPash/url-shortener/internal/cache"
	"github.com/NikitaPash/url-shortener/internal/domain"
	"github.com/NikitaPash/url-shortener/internal/handler"
	"github.com/NikitaPash/url-shortener/internal/middleware"
	"github.com/NikitaPash/url-shortener/internal/service"
	"github.com/NikitaPash/url-shortener/internal/storage/postgres"
	"github.com/NikitaPash/url-shortener/internal/telemetry"
)

// stubLinkRepo implements service.LinkRepository for handler tests.
type stubLinkRepo struct {
	createFn      func(context.Context, *domain.Link) error
	getByUserIDFn func(context.Context, string, int, int) ([]domain.Link, int64, error)
	getByIDFn     func(context.Context, string) (*domain.Link, error)
}

func (r *stubLinkRepo) Create(ctx context.Context, l *domain.Link) error {
	if r.createFn != nil {
		return r.createFn(ctx, l)
	}
	return nil
}
func (r *stubLinkRepo) GetByUserID(ctx context.Context, uid string, limit, offset int) ([]domain.Link, int64, error) {
	if r.getByUserIDFn != nil {
		return r.getByUserIDFn(ctx, uid, limit, offset)
	}
	return nil, 0, nil
}
func (r *stubLinkRepo) GetByID(ctx context.Context, id string) (*domain.Link, error) {
	if r.getByIDFn != nil {
		return r.getByIDFn(ctx, id)
	}
	return nil, errors.New("not found")
}
func (r *stubLinkRepo) SetActive(_ context.Context, _, _ string, _ bool) error { return nil }
func (r *stubLinkRepo) Delete(_ context.Context, _, _ string) error            { return nil }

// stubLinkCache implements service.LinkCache; all calls are no-ops.
type stubLinkCache struct{}

func (c *stubLinkCache) GetLink(_ context.Context, _ string) (*cache.CachedLink, bool) {
	return nil, false
}
func (c *stubLinkCache) SetLink(_ context.Context, _ string, _ *cache.CachedLink, _ time.Duration) {}
func (c *stubLinkCache) DeleteLink(_ context.Context, _ string)                                    {}

func newLinkHandler(repo *stubLinkRepo) *handler.LinkHandler {
	svc := service.NewShortenerService(repo, &stubLinkCache{}, telemetry.NewNoopMetrics())
	return handler.NewLinkHandler(svc, "https://short.ly")
}

// withUserID injects a user ID into the request context (simulates JWTAuth middleware).
func withUserID(req *http.Request, uid string) *http.Request {
	return req.WithContext(context.WithValue(req.Context(), middleware.UserIDKey, uid))
}

// --- Shorten ---

func TestLinkHandler_Shorten(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		repoErr    error
		wantStatus int
		checkBody  func(*testing.T, map[string]any)
	}{
		{
			name:       "valid URL returns 201 with short_url",
			body:       `{"url":"https://example.com/long-path"}`,
			wantStatus: http.StatusCreated,
			checkBody: func(t *testing.T, m map[string]any) {
				t.Helper()
				if _, ok := m["short_url"]; !ok {
					t.Error("response missing short_url")
				}
				if _, ok := m["id"]; !ok {
					t.Error("response missing id")
				}
				if m["original_url"] != "https://example.com/long-path" {
					t.Errorf("original_url = %v", m["original_url"])
				}
			},
		},
		{
			name:       "invalid JSON returns 400",
			body:       `not-json`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "non-http URL scheme rejected by validation → 400",
			body:       `{"url":"ftp://example.com"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty URL rejected → 400",
			body:       `{"url":""}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "repo error returns 500",
			body:       `{"url":"https://example.com"}`,
			repoErr:    errors.New("db error"),
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "custom alias accepted → 201 with alias as id",
			body:       `{"url":"https://x.com","custom_alias":"my-slug"}`,
			wantStatus: http.StatusCreated,
			checkBody: func(t *testing.T, m map[string]any) {
				t.Helper()
				if m["id"] != "my-slug" {
					t.Errorf("id = %v, want my-slug", m["id"])
				}
			},
		},
		{
			name:       "invalid alias returns 400",
			body:       `{"url":"https://x.com","custom_alias":"ab"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "alias taken returns 409",
			body:       `{"url":"https://x.com","custom_alias":"taken"}`,
			repoErr:    postgres.ErrIDTaken,
			wantStatus: http.StatusConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &stubLinkRepo{createFn: func(_ context.Context, l *domain.Link) error {
				if l.ID == "" {
					l.ID = "abc123"
				}
				return tt.repoErr
			}}
			h := newLinkHandler(repo)

			req := httptest.NewRequest(http.MethodPost, "/api/shorten", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			req = withUserID(req, "user-1")
			w := httptest.NewRecorder()

			h.Shorten(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.checkBody != nil && w.Code == http.StatusCreated {
				var m map[string]any
				if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
					t.Fatalf("decode: %v", err)
				}
				tt.checkBody(t, m)
			}
		})
	}
}

// --- ListLinks ---

func TestLinkHandler_ListLinks(t *testing.T) {
	links := []domain.Link{
		{ID: "a", UserID: "u1", OriginalURL: "https://a.com", CreatedAt: time.Now()},
		{ID: "b", UserID: "u1", OriginalURL: "https://b.com", CreatedAt: time.Now()},
	}

	tests := []struct {
		name       string
		query      string
		wantLimit  int
		wantOffset int
		wantStatus int
		wantTotal  int
	}{
		{
			name:       "defaults applied when no query params",
			query:      "",
			wantLimit:  20,
			wantOffset: 0,
			wantStatus: http.StatusOK,
			wantTotal:  2,
		},
		{
			name:       "explicit limit and offset forwarded to repo",
			query:      "?limit=5&offset=10",
			wantLimit:  5,
			wantOffset: 10,
			wantStatus: http.StatusOK,
		},
		{
			name:       "limit clamped to max 100",
			query:      "?limit=999",
			wantLimit:  100,
			wantStatus: http.StatusOK,
		},
		{
			name:       "negative offset clamped to 0",
			query:      "?offset=-5",
			wantOffset: 0,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedLimit, capturedOffset int
			repo := &stubLinkRepo{
				getByUserIDFn: func(_ context.Context, _ string, limit, offset int) ([]domain.Link, int64, error) {
					capturedLimit = limit
					capturedOffset = offset
					return links, int64(len(links)), nil
				},
			}
			h := newLinkHandler(repo)

			req := httptest.NewRequest(http.MethodGet, "/api/links"+tt.query, nil)
			req = withUserID(req, "u1")
			w := httptest.NewRecorder()

			h.ListLinks(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if tt.wantLimit != 0 && capturedLimit != tt.wantLimit {
				t.Errorf("limit passed to repo = %d, want %d", capturedLimit, tt.wantLimit)
			}
			if tt.wantOffset != 0 && capturedOffset != tt.wantOffset {
				t.Errorf("offset passed to repo = %d, want %d", capturedOffset, tt.wantOffset)
			}
			if tt.wantTotal != 0 {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("decode: %v", err)
				}
				if got := resp["total"]; got != float64(tt.wantTotal) {
					t.Errorf("total = %v, want %d", got, tt.wantTotal)
				}
			}
		})
	}
}
