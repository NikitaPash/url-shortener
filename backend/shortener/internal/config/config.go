package config

import (
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	Port        int           `env:"PORT"                  envDefault:"8080"`
	DatabaseURL string        `env:"DATABASE_URL,required"`
	JWTSecret   string        `env:"JWT_SECRET,required"`
	JWTExpiry   time.Duration `env:"JWT_EXPIRY"            envDefault:"24h"`
	BaseURL     string        `env:"BASE_URL"              envDefault:"http://localhost:8080"`

	// Seeded admin account, is the only account allowed to use analytics.
	AdminEmail    string `env:"ADMIN_EMAIL"    envDefault:""`
	AdminPassword string `env:"ADMIN_PASSWORD" envDefault:""`

	DBMaxConns int32 `env:"DB_MAX_CONNS" envDefault:"10"`
	DBMinConns int32 `env:"DB_MIN_CONNS" envDefault:"2"`

	// RunMigrations applies pending migrations on startup. Set false in
	// multi-replica deployments that migrate via a separate job/init container.
	RunMigrations bool `env:"RUN_MIGRATIONS" envDefault:"true"`

	RedisAddr     string `env:"REDIS_ADDR"     envDefault:"localhost:6379"`
	RedisPassword string `env:"REDIS_PASSWORD" envDefault:""`
	RedisDB       int    `env:"REDIS_DB"       envDefault:"0"`

	GeoIPDBPath  string `env:"GEOIP_DB_PATH" envDefault:"data/GeoLite2-Country.mmdb"`
	KafkaBrokers string `env:"KAFKA_BROKERS" envDefault:"localhost:9092"`
	KafkaTopic   string `env:"KAFKA_TOPIC"   envDefault:"link_clicks"`

	RateLimitAuth     int64 `env:"RATE_LIMIT_AUTH"     envDefault:"10"`
	RateLimitRedirect int64 `env:"RATE_LIMIT_REDIRECT" envDefault:"100"`
	RateLimitAPI      int64 `env:"RATE_LIMIT_API"      envDefault:"30"`

	JaegerEndpoint string `env:"JAEGER_ENDPOINT" envDefault:"localhost:4318"`
	MetricsPort    int    `env:"METRICS_PORT"    envDefault:"9091"`

	// ClickHouse read-only connection for link analytics.
	// Uses the analyst (read-only) user — same credentials as the Python agent.
	ClickHouseAddr            string `env:"CLICKHOUSE_ADDR"             envDefault:"clickhouse:9000"`
	ClickHouseDatabase        string `env:"CLICKHOUSE_DATABASE"         envDefault:"shortener"`
	ClickHouseAnalystUser     string `env:"CLICKHOUSE_ANALYST_USER"     envDefault:"analyst"`
	ClickHouseAnalystPassword string `env:"CLICKHOUSE_ANALYST_PASSWORD" envDefault:""`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
