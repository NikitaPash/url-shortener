package event

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/NikitaPash/url-shortener/internal/telemetry"
)

var tracer = otel.Tracer("event/producer")

const (
	// Async writer batching: a background goroutine accumulates messages and
	// flushes them in batches, so WriteMessages returns immediately and a redirect
	// never blocks on Kafka. Delivery results arrive later via the Completion hook.
	batchSize    = 500
	batchTimeout = 100 * time.Millisecond
	writeTimeout = 10 * time.Second

	// maxInFlight bounds how many published-but-not-yet-acknowledged events may be
	// buffered in memory. Beyond this (broker down or slow) new events are dropped
	// and counted — bounded backpressure instead of unbounded memory growth.
	maxInFlight = 50_000
)

// Producer publishes click events to Kafka via an async writer. The redirect path
// is never blocked; an in-flight counter caps memory use when Kafka is unavailable.
type Producer struct {
	writer   *kafka.Writer
	metrics  *telemetry.Metrics
	inFlight atomic.Int64
}

func NewProducer(brokers, topic string, metrics *telemetry.Metrics) *Producer {
	if metrics == nil {
		metrics = telemetry.NewNoopMetrics()
	}

	p := &Producer{metrics: metrics}

	p.writer = &kafka.Writer{
		Addr:     kafka.TCP(brokers),
		Topic:    topic,
		Balancer: &kafka.LeastBytes{},

		BatchSize:    batchSize,
		BatchTimeout: batchTimeout,
		WriteTimeout: writeTimeout,
		RequiredAcks: kafka.RequireOne,

		Async: true,
		Completion: func(messages []kafka.Message, err error) {
			p.inFlight.Add(-int64(len(messages)))
			if err != nil {
				p.metrics.KafkaFailed.Add(context.Background(), int64(len(messages)))
				slog.Error("kafka async publish failed", "error", err, "count", len(messages))
				return
			}
			p.metrics.KafkaPublish.Add(context.Background(), int64(len(messages)))
		},

		ErrorLogger: kafka.LoggerFunc(func(msg string, _ ...any) {
			slog.Error("kafka writer error", "msg", msg)
		}),
	}

	slog.Info("kafka producer created (async)",
		"brokers", brokers, "topic", topic,
		"batch_size", batchSize, "max_in_flight", maxInFlight)
	return p
}

// PublishClickAsync enqueues a click event for asynchronous publishing. It never
// blocks the redirect. When too many events are already awaiting acknowledgment
// the event is dropped and counted.
func (p *Producer) PublishClickAsync(ctx context.Context, evt ClickEvent) {
	if p == nil {
		return
	}

	if p.inFlight.Load() >= maxInFlight {
		p.metrics.ClicksDropped.Add(ctx, 1)
		slog.Warn("click in-flight limit reached — dropping event", "short_id", evt.ShortID)
		return
	}

	payload, err := json.Marshal(evt)
	if err != nil {
		slog.Error("failed to marshal click event", "error", err, "short_id", evt.ShortID)
		return
	}

	spanCtx, span := tracer.Start(ctx, "kafka.PublishClick")
	headers := injectTraceContext(spanCtx)
	span.End()

	msg := kafka.Message{
		Key:     []byte(evt.ShortID),
		Value:   payload,
		Headers: headers,
	}

	p.inFlight.Add(1)
	if err := p.writer.WriteMessages(ctx, msg); err != nil {
		// In async mode an error here means the message was never enqueued
		p.inFlight.Add(-1)
		p.metrics.KafkaFailed.Add(ctx, 1)
		slog.Error("failed to enqueue click event", "error", err, "short_id", evt.ShortID)
	}
}

// injectTraceContext serializes the current W3C trace context into Kafka message headers.
func injectTraceContext(ctx context.Context) []kafka.Header {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	headers := make([]kafka.Header, 0, len(carrier))
	for k, v := range carrier {
		headers = append(headers, kafka.Header{Key: k, Value: []byte(v)})
	}
	return headers
}

// Close flushes any buffered events and closes the writer. Must be called during
// graceful shutdown; kafka-go's Close blocks until pending writes complete.
func (p *Producer) Close() error {
	if p == nil {
		return nil
	}
	slog.Info("closing kafka producer — flushing pending events")
	return p.writer.Close()
}
