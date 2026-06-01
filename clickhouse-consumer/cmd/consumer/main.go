package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/NikitaPash/clickhouse-consumer/internal/config"
	"github.com/NikitaPash/clickhouse-consumer/internal/consumer"
	"github.com/NikitaPash/clickhouse-consumer/internal/pipeline"
	"github.com/NikitaPash/clickhouse-consumer/internal/telemetry"
	"github.com/NikitaPash/clickhouse-consumer/internal/writer"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

// run wires up dependencies and consumes until a shutdown signal arrives.
// It returns an error rather than calling os.Exit so that deferred cleanup
// (telemetry, ClickHouse, Kafka) always runs.
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx := context.Background()

	telemetryShutdown, err := telemetry.Setup(ctx, "clickhouse-consumer", cfg.JaegerEndpoint, cfg.MetricsPort)
	if err != nil {
		return fmt.Errorf("init telemetry: %w", err)
	}
	defer telemetryShutdown()

	chWriter, err := writer.NewClickHouseWriter(
		cfg.ClickHouseAddr,
		cfg.ClickHouseDatabase,
		cfg.ClickHouseUser,
		cfg.ClickHousePassword,
	)
	if err != nil {
		return fmt.Errorf("connect clickhouse: %w", err)
	}
	defer func() {
		if err := chWriter.Close(); err != nil {
			slog.Warn("clickhouse close failed", "error", err)
		}
	}()

	kafkaConsumer := consumer.NewKafkaConsumer(
		cfg.KafkaBrokers,
		cfg.KafkaTopic,
		cfg.KafkaGroupID,
	)
	defer func() {
		if err := kafkaConsumer.Close(); err != nil {
			slog.Warn("kafka consumer close failed", "error", err)
		}
	}()

	// Cancel the consume loop's context on interrupt/terminate so it drains and exits.
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pipeline.Run(ctx, cfg, kafkaConsumer, chWriter)

	slog.Info("consumer stopped")
	return nil
}
