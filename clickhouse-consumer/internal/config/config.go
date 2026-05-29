package config

import (
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	KafkaBrokers string `env:"KAFKA_BROKERS"  envDefault:"localhost:9092"`
	KafkaTopic   string `env:"KAFKA_TOPIC"    envDefault:"link_clicks"`
	KafkaGroupID string `env:"KAFKA_GROUP_ID" envDefault:"clickhouse-writers"`

	ClickHouseAddr     string `env:"CLICKHOUSE_ADDR"     envDefault:"localhost:9000"`
	ClickHouseDatabase string `env:"CLICKHOUSE_DATABASE" envDefault:"shortener"`
	ClickHouseUser     string `env:"CLICKHOUSE_USER"     envDefault:"default"`
	ClickHousePassword string `env:"CLICKHOUSE_PASSWORD" envDefault:""`

	BatchSize     int           `env:"BATCH_SIZE"     envDefault:"1000"`
	FlushInterval time.Duration `env:"FLUSH_INTERVAL" envDefault:"5s"`

	JaegerEndpoint string `env:"JAEGER_ENDPOINT" envDefault:"localhost:4318"`
	MetricsPort    int    `env:"METRICS_PORT"    envDefault:"9094"`
}

func Load() (*Config, error) {
	cfg := &Config{}
	if err := env.Parse(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}
