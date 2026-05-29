package service_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NikitaPash/url-shortener/internal/storage/postgres"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL != "" {
		// Migrations are at ../../migrations relative to internal/service/.
		mig, err := migrate.New("file://../../migrations", dbURL)
		if err == nil {
			if upErr := mig.Up(); upErr != nil && !errors.Is(upErr, migrate.ErrNoChange) {
				// Migrations failed; tests will skip when they check testPool == nil.
			} else {
				testPool, _ = postgres.NewPool(context.Background(), dbURL, 5, 1)
			}
		}
	}

	code := m.Run()

	if testPool != nil {
		testPool.Close()
	}

	os.Exit(code)
}
