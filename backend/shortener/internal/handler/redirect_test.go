package handler_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/NikitaPash/url-shortener/internal/event"
	"github.com/NikitaPash/url-shortener/internal/geo"
	"github.com/NikitaPash/url-shortener/internal/handler"
	"github.com/NikitaPash/url-shortener/internal/service"
	"github.com/NikitaPash/url-shortener/internal/storage/postgres"
)

type mockResolver struct {
	resolve func(ctx context.Context, id string) (*service.ResolvedLink, error)
}

func (m *mockResolver) Resolve(ctx context.Context, id string) (*service.ResolvedLink, error) {
	return m.resolve(ctx, id)
}

func newTestRouter(resolver handler.LinkResolver) *chi.Mux {
	// nil producer and nil metrics — both are handled gracefully (no-op).
	h := handler.NewRedirectHandler(resolver, (*event.Producer)(nil), geo.NewResolver(""), nil)
	r := chi.NewRouter()
	r.Get("/{id}", h.Redirect)
	return r
}

func TestRedirectFound(t *testing.T) {
	resolver := &mockResolver{
		resolve: func(_ context.Context, _ string) (*service.ResolvedLink, error) {
			return &service.ResolvedLink{OriginalURL: "https://example.com", UserID: "u1"}, nil
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/abc123", nil)
	w := httptest.NewRecorder()
	newTestRouter(resolver).ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusFound)
	}
	if loc := w.Header().Get("Location"); loc != "https://example.com" {
		t.Errorf("Location: got %q, want %q", loc, "https://example.com")
	}
}

func TestRedirectNotFound(t *testing.T) {
	resolver := &mockResolver{
		resolve: func(_ context.Context, _ string) (*service.ResolvedLink, error) {
			return nil, postgres.ErrLinkNotFound
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	w := httptest.NewRecorder()
	newTestRouter(resolver).ServeHTTP(w, req)

	// Not-found links now redirect to the frontend 404 page instead of returning JSON.
	if w.Code != http.StatusFound {
		t.Errorf("status: got %d, want %d (redirect to 404 page)", w.Code, http.StatusFound)
	}
	if loc := w.Header().Get("Location"); loc != "/app/not-found" {
		t.Errorf("Location: got %q, want %q", loc, "/app/not-found")
	}
}

func TestRedirectInternalError(t *testing.T) {
	resolver := &mockResolver{
		resolve: func(_ context.Context, _ string) (*service.ResolvedLink, error) {
			return nil, errors.New("db timeout")
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/abc123", nil)
	w := httptest.NewRecorder()
	newTestRouter(resolver).ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusInternalServerError)
	}
}
