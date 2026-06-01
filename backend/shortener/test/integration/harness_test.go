//go:build integration

// Package integration holds level-L2 integration tests for the Go API's Kafka
// producer (internal/event), run against the broker brought up by
// docker-compose.test.yml. Excluded from the default unit pass; run only with
// `-tags=integration` (see `make test-integration`).
package integration

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/NikitaPash/url-shortener/internal/telemetry"
)

var (
	kafkaBroker string
	harnessErr  error
	idCounter   atomic.Int64
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func TestMain(m *testing.M) {
	kafkaBroker = env("KAFKA_BROKERS", "localhost:29092")

	// Trace: an always-sample provider + the W3C propagator so PublishClickAsync
	// injects a real traceparent header (the default no-op propagator injects none).
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample())))
	otel.SetTextMapPropagator(propagation.TraceContext{})

	harnessErr = checkKafka()

	os.Exit(m.Run())
}

func checkKafka() error {
	c, err := net.DialTimeout("tcp", kafkaBroker, 3*time.Second)
	if err != nil {
		return fmt.Errorf("kafka unreachable at %s: %w", kafkaBroker, err)
	}
	_ = c.Close()
	return nil
}

// requireKafka skips tests that need a real broker when the harness is down.
// The dead-broker and in-flight-cap tests are self-contained and do NOT call it.
func requireKafka(t *testing.T) {
	t.Helper()
	if harnessErr != nil {
		t.Skipf("integration harness unavailable (start it with `docker compose -f docker-compose.test.yml up -d --wait`): %v", harnessErr)
	}
}

func uniqueID(prefix string) string {
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), idCounter.Add(1))
}

// newMetrics returns no-op metrics: the producer requires a non-nil *Metrics, but
// these tests assert on Kafka / trace behaviour, not on counter values. (The
// in-flight-cap drop counter is covered by a unit test in package event.)
func newMetrics(t *testing.T) *telemetry.Metrics {
	t.Helper()
	return telemetry.NewNoopMetrics()
}

func createTopic(t *testing.T, name string) {
	t.Helper()
	client := &kafka.Client{Addr: kafka.TCP(kafkaBroker)}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	resp, err := client.CreateTopics(ctx, &kafka.CreateTopicsRequest{
		Topics: []kafka.TopicConfig{{Topic: name, NumPartitions: 1, ReplicationFactor: 1}},
	})
	if err != nil {
		t.Fatalf("create topic %s: %v", name, err)
	}
	if e := resp.Errors[name]; e != nil {
		t.Fatalf("create topic %s: %v", name, e)
	}
}

// readOneMessage reads the first message from a topic with a fresh consumer group.
func readOneMessage(t *testing.T, topic string, timeout time.Duration) kafka.Message {
	t.Helper()
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     []string{kafkaBroker},
		Topic:       topic,
		GroupID:     uniqueID("grp"),
		StartOffset: kafka.FirstOffset,
		MinBytes:    1,
		MaxBytes:    10e6,
	})
	defer r.Close()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	msg, err := r.ReadMessage(ctx)
	if err != nil {
		t.Fatalf("read message from %s: %v", topic, err)
	}
	return msg
}
