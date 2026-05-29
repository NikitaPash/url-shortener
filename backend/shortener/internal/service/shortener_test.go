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

	userID := "00000000-0000-0000-0000-000000000001"
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
