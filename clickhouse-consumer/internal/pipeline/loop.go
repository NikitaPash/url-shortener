// Package pipeline contains the consumer's fetch→buffer→flush loop, extracted
// from package main so it can be driven directly by integration tests against a
// real Kafka + ClickHouse, not only by unit tests with fakes.
package pipeline

import (
	"context"
	"log/slog"
	"time"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"

	"github.com/NikitaPash/clickhouse-consumer/internal/config"
	"github.com/NikitaPash/clickhouse-consumer/internal/consumer"
	"github.com/NikitaPash/clickhouse-consumer/internal/parser"
	"github.com/NikitaPash/clickhouse-consumer/internal/writer"
)

var tracer = otel.Tracer("clickhouse-consumer")

const (
	// fetchTimeout bounds a single Kafka poll so the loop can check the flush
	// ticker and shutdown signal between fetches when the topic is idle.
	fetchTimeout = 500 * time.Millisecond
	// commitTimeout bounds the offset commit. It is derived from a background
	// context so the final flush can still commit after the loop's context is
	// canceled on shutdown.
	commitTimeout = 5 * time.Second
)

// Run consumes messages from Kafka, buffers them, and flushes batches to
// ClickHouse until ctx is canceled. It is the production consume loop; the
// composition root (cmd/consumer) wires the real reader + writer, while tests
// may inject fakes or real clients via the MessageReader / BatchWriter interfaces.
func Run(
	ctx context.Context,
	cfg *config.Config,
	kafkaConsumer consumer.MessageReader,
	chWriter writer.BatchWriter,
) {
	var (
		buffer   []writer.ClickRow
		messages []kafka.Message
		ticker   = time.NewTicker(cfg.FlushInterval)
	)
	defer ticker.Stop()

	flush := func() {
		if len(messages) == 0 {
			return
		}

		// Malformed messages carry no row but their offsets must still advance, so
		// the insert is skipped (not the commit) when the buffer holds no rows.
		if len(buffer) > 0 {
			slog.Info("flushing batch", "size", len(buffer))

			// Extract trace context from the first Kafka message so this span is linked
			// to the Go API's kafka.PublishClick span — the cross-service trace join.
			flushCtx := extractTraceContext(context.Background(), messages[0])
			flushCtx, span := tracer.Start(flushCtx, "clickhouse.BatchInsert")
			span.SetAttributes(attribute.Int("batch.size", len(buffer)))

			if err := chWriter.WriteBatch(flushCtx, buffer); err != nil {
				span.RecordError(err)
				span.End()
				slog.Error("batch insert failed — will retry on next flush",
					"error", err,
					"batch_size", len(buffer),
				)
				return
			}
			span.End()
		}

		// Commit with a background-derived context: ctx may already be canceled
		// during the final flush, which would otherwise abort the commit and cause
		// the batch to be re-processed on restart.
		commitCtx, cancel := context.WithTimeout(context.Background(), commitTimeout)
		err := kafkaConsumer.CommitMessages(commitCtx, messages...)
		cancel()
		if err != nil {
			slog.Error("failed to commit offsets — messages may be re-processed", "error", err)
		}

		buffer = buffer[:0]
		messages = messages[:0]
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("shutting down — flushing remaining buffer", "size", len(buffer))
			flush()
			return

		case <-ticker.C:
			flush()

		default:
			fetchCtx, fetchCancel := context.WithTimeout(ctx, fetchTimeout)
			msg, event, err := kafkaConsumer.FetchMessage(fetchCtx)
			fetchCancel()

			if err != nil {
				if ctx.Err() != nil {
					flush()
					return
				}
				continue
			}

			messages = append(messages, msg)

			if event != nil {
				parsed := parser.ParseUserAgent(event.UserAgent)
				buffer = append(buffer, writer.ClickRow{
					Timestamp: event.Timestamp,
					ShortID:   event.ShortID,
					UserID:    event.UserID,
					IP:        event.IP,
					UserAgent: event.UserAgent,
					Referrer:  event.Referrer,
					Country:   event.Country,
					Device:    parsed.Device,
					Browser:   parsed.Browser,
					IsBot:     boolToUint8(parsed.IsBot),
				})
			}

			if len(buffer) >= cfg.BatchSize {
				flush()
			}
		}
	}
}

// extractTraceContext rebuilds W3C trace context from Kafka message headers,
// linking this consumer span to the producer's span in Jaeger.
func extractTraceContext(ctx context.Context, msg kafka.Message) context.Context {
	carrier := propagation.MapCarrier{}
	for _, h := range msg.Headers {
		carrier[h.Key] = string(h.Value)
	}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

func boolToUint8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}
