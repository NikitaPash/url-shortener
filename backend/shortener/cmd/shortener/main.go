package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/NikitaPash/url-shortener/internal/analytics"
	"github.com/NikitaPash/url-shortener/internal/cache"
	"github.com/NikitaPash/url-shortener/internal/config"
	"github.com/NikitaPash/url-shortener/internal/event"
	"github.com/NikitaPash/url-shortener/internal/geo"
	"github.com/NikitaPash/url-shortener/internal/handler"
	"github.com/NikitaPash/url-shortener/internal/middleware"
	"github.com/NikitaPash/url-shortener/internal/service"
	"github.com/NikitaPash/url-shortener/internal/storage/postgres"
	"github.com/NikitaPash/url-shortener/internal/telemetry"
)

const (
	readTimeout      = 5 * time.Second
	writeTimeout     = 10 * time.Second
	idleTimeout      = 120 * time.Second
	shutdownTimeout  = 10 * time.Second
	readinessTimeout = 2 * time.Second
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

// run wires up dependencies and serves until a shutdown signal arrives.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx := context.Background()

	telemetryShutdown, err := telemetry.Setup(ctx, "go-api", cfg.JaegerEndpoint, cfg.MetricsPort)
	if err != nil {
		return fmt.Errorf("init telemetry: %w", err)
	}
	defer telemetryShutdown()

	metrics, err := telemetry.NewMetrics()
	if err != nil {
		return fmt.Errorf("init metrics: %w", err)
	}

	if cfg.RunMigrations {
		if err := runMigrations(cfg.DatabaseURL); err != nil {
			return fmt.Errorf("run migrations: %w", err)
		}
	}

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL, cfg.DBMaxConns, cfg.DBMinConns)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	redisCache, err := cache.NewRedisCache(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		return fmt.Errorf("create redis client: %w", err)
	}
	defer func() {
		if err := redisCache.Close(); err != nil {
			slog.Warn("redis close failed", "error", err)
		}
	}()

	// GeoIP gracefully degrades to empty country if database file is absent.
	geoResolver := geo.NewResolver(cfg.GeoIPDBPath)
	defer geoResolver.Close()

	// Kafka producer gracefully logs errors if Kafka is unreachable.
	kafkaProducer := event.NewProducer(cfg.KafkaBrokers, cfg.KafkaTopic, metrics)
	defer func() {
		if err := kafkaProducer.Close(); err != nil {
			slog.Warn("kafka producer close failed", "error", err)
		}
	}()

	userRepo := postgres.NewUserRepo(pool)
	linkRepo := postgres.NewLinkRepo(pool)

	authService := service.NewAuthService(userRepo, redisCache, cfg.JWTSecret, cfg.JWTExpiry)
	shortenerService := service.NewShortenerService(linkRepo, redisCache, metrics)

	// Seed the analytics admin from env, if configured. This is the only account
	// the Python agent allows to run analytics queries.
	if cfg.AdminEmail != "" && cfg.AdminPassword != "" {
		if err := authService.SeedAdmin(ctx, cfg.AdminEmail, cfg.AdminPassword); err != nil {
			return fmt.Errorf("seed admin user: %w", err)
		}
		slog.Info("admin user ensured", "email", cfg.AdminEmail)
	}

	// ClickHouse analytics reader.
	chReader, err := analytics.NewClickHouseReader(
		cfg.ClickHouseAddr,
		cfg.ClickHouseDatabase,
		cfg.ClickHouseAnalystUser,
		cfg.ClickHouseAnalystPassword,
	)
	if err != nil {
		slog.Warn("clickhouse analytics unavailable", "error", err)
		chReader = nil
	}
	if chReader != nil {
		defer func() {
			if err := chReader.Close(); err != nil {
				slog.Warn("clickhouse close failed", "error", err)
			}
		}()
	}

	authHandler := handler.NewAuthHandler(authService)
	linkHandler := handler.NewLinkHandler(shortenerService, cfg.BaseURL)
	redirectHandler := handler.NewRedirectHandler(shortenerService, kafkaProducer, geoResolver, metrics)
	analyticsHandler := handler.NewLinkAnalyticsHandler(shortenerService, chReader)
	manageHandler := handler.NewLinkManageHandler(shortenerService)

	r := chi.NewRouter()
	r.Use(otelhttp.NewMiddleware("go-api")) // Creates a span for every HTTP request.
	r.Use(chimiddleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestLogger)
	r.Use(chimiddleware.Recoverer)

	// Liveness — no auth, no rate limit, no tracing overhead.
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Readiness — verifies critical dependencies are reachable.
	r.Get("/readyz", func(w http.ResponseWriter, req *http.Request) {
		checkCtx, cancel := context.WithTimeout(req.Context(), readinessTimeout)
		defer cancel()
		if err := pool.Ping(checkCtx); err != nil {
			http.Error(w, `{"error":"database not ready"}`, http.StatusServiceUnavailable)
			return
		}
		if err := redisCache.Ping(checkCtx); err != nil {
			http.Error(w, `{"error":"redis not ready"}`, http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	// Auth routes — strict limit per IP.
	r.Group(func(r chi.Router) {
		r.Use(middleware.RateLimit(redisCache, middleware.RateLimiterConfig{
			Name: "auth", Limit: cfg.RateLimitAuth, Window: time.Minute,
		}))
		r.Post("/auth/register", authHandler.Register)
		r.Post("/auth/login", authHandler.Login)
	})

	// Redirect route — generous limit per IP.
	r.Group(func(r chi.Router) {
		r.Use(middleware.RateLimit(redisCache, middleware.RateLimiterConfig{
			Name: "redirect", Limit: cfg.RateLimitRedirect, Window: time.Minute,
		}))
		r.Get("/{id}", redirectHandler.Redirect)
	})

	// Protected routes — JWT required, moderate limit per IP.
	r.Group(func(r chi.Router) {
		r.Use(middleware.JWTAuth([]byte(cfg.JWTSecret), redisCache))
		r.Use(middleware.RateLimit(redisCache, middleware.RateLimiterConfig{
			Name: "api", Limit: cfg.RateLimitAPI, Window: time.Minute,
		}))
		r.Post("/api/shorten", linkHandler.Shorten)
		r.Get("/api/links", linkHandler.ListLinks)
		r.Patch("/api/links/{id}", manageHandler.SetActive)
		r.Delete("/api/links/{id}", manageHandler.Delete)
		r.Get("/api/links/{id}/analytics", analyticsHandler.GetLinkAnalytics)
		r.Post("/auth/logout", authHandler.Logout)
	})

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      r,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	// Trigger graceful shutdown when an interrupt/terminate signal arrives.
	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-sigCtx.Done()
		slog.Info("shutting down server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("graceful shutdown failed", "error", err)
		}
	}()

	slog.Info("server starting", "port", cfg.Port)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("listen and serve: %w", err)
	}
	slog.Info("server stopped")
	return nil
}

// runMigrations applies all up migrations and releases the migrator's own
// database connection before returning.
func runMigrations(databaseURL string) error {
	m, err := migrate.New("file://migrations", databaseURL)
	if err != nil {
		return fmt.Errorf("init migrator: %w", err)
	}
	defer func() {
		if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
			slog.Warn("migrator close failed", "source_error", srcErr, "db_error", dbErr)
		}
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	slog.Info("database migrations applied")
	return nil
}
