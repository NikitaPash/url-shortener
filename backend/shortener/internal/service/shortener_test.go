package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/NikitaPash/url-shortener/internal/service"
	"github.com/NikitaPash/url-shortener/internal/storage/postgres"
)

func TestShortenAndResolve(t *testing.T) {
	if testPool == nil {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()
	linkRepo := postgres.NewLinkRepo(testPool)
	// nil cache: cache misses degrade gracefully.
	shortenerSvc := service.NewShortenerService(linkRepo, nil, nil)

	// links.user_id is a NOT NULL FK to users(id), so seed an owner row first;
	// otherwise Shorten fails with a foreign-key violation (SQLSTATE 23503).
	userID := "00000000-0000-0000-0000-000000000001"
	if _, err := testPool.Exec(ctx,
		`INSERT INTO users (id, email, password_hash) VALUES ($1, $2, 'x')
		 ON CONFLICT (id) DO NOTHING`,
		userID, fmt.Sprintf("shorten-owner-%d@example.com", time.Now().UnixNano()),
	); err != nil {
		t.Fatalf("seed owner: %v", err)
	}
	t.Cleanup(func() {
		// ON DELETE CASCADE removes the owned link too.
		_, _ = testPool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID)
	})

	origURL := fmt.Sprintf("https://example.com/%d", time.Now().UnixNano())

	link, err := shortenerSvc.Shorten(ctx, userID, origURL, nil, "")
	if err != nil {
		t.Fatalf("Shorten: %v", err)
	}
	if link.ID == "" {
		t.Fatal("Shorten returned empty ID")
	}
	if link.OriginalURL != origURL {
		t.Errorf("OriginalURL: got %q, want %q", link.OriginalURL, origURL)
	}

	resolved, err := shortenerSvc.Resolve(ctx, link.ID)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved.OriginalURL != origURL {
		t.Errorf("Resolved URL: got %q, want %q", resolved.OriginalURL, origURL)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, "DELETE FROM links WHERE id = $1", link.ID)
	})
}

func TestResolveNotFound(t *testing.T) {
	if testPool == nil {
		t.Skip("DATABASE_URL not set — skipping integration test")
	}

	ctx := context.Background()
	linkRepo := postgres.NewLinkRepo(testPool)
	shortenerSvc := service.NewShortenerService(linkRepo, nil, nil)

	_, err := shortenerSvc.Resolve(ctx, "nonexistent123")
	if !errors.Is(err, postgres.ErrLinkNotFound) {
		t.Errorf("Resolve non-existent: got %v, want ErrLinkNotFound", err)
	}
}
