//go:build integration

// Package integration holds level-L2 pipeline integration tests for the consumer.
// They drive the REAL consume loop (internal/pipeline.Run) with a REAL Kafka
// reader and a REAL ClickHouse writer against the infra brought up by
// docker-compose.test.yml. They are excluded from the default unit pass and run
// only with `-tags=integration` (see `make test-integration`).
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/NikitaPash/clickhouse-consumer/internal/config"
	"github.com/NikitaPash/clickhouse-consumer/internal/consumer"
	"github.com/NikitaPash/clickhouse-consumer/internal/pipeline"
	"github.com/NikitaPash/clickhouse-consumer/internal/writer"
)

// Harness connection parameters. Defaults match docker-compose.test.yml; override
// via env to point at a different infra instance.
var (
	kafkaBroker string
	chAddr      string
	chDatabase  string
	chUser      string
	chPassword  string

	chConn       driver.Conn
	spanExporter *tracetest.InMemoryExporter
	harnessErr   error
	idCounter    atomic.Int64
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func TestMain(m *testing.M) {
	kafkaBroker = env("KAFKA_BROKERS", "localhost:29092")
	chAddr = env("CLICKHOUSE_ADDR", "localhost:9000")
	chDatabase = env("CLICKHOUSE_DATABASE", "shortener")
	chUser = env("CLICKHOUSE_USER", "default")
	chPassword = env("CLICKHOUSE_PASSWORD", "testpass")

	// Global telemetry for the trace-join test: an in-memory span exporter on the
	// global tracer provider (set exactly once for the whole package), plus the
	// W3C trace-context propagator the consumer extracts from Kafka headers.
	spanExporter = tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(spanExporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	harnessErr = connectHarness()

	code := m.Run()

	if chConn != nil {
		_ = chConn.Close()
	}
	os.Exit(code)
}

// connectHarness verifies Kafka and ClickHouse are reachable. A non-nil result
// makes every test skip (with a clear reason) rather than fail confusingly when
// the infra harness is not up.
func connectHarness() error {
	c, err := net.DialTimeout("tcp", kafkaBroker, 3*time.Second)
	if err != nil {
		return fmt.Errorf("kafka unreachable at %s: %w", kafkaBroker, err)
	}
	_ = c.Close()

	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{chAddr},
		Auth: clickhouse.Auth{Database: chDatabase, Username: chUser, Password: chPassword},
	})
	if err != nil {
		return fmt.Errorf("clickhouse open: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := conn.Ping(ctx); err != nil {
		return fmt.Errorf("clickhouse ping at %s: %w", chAddr, err)
	}
	chConn = conn
	return nil
}

func requireHarness(t *testing.T) {
	t.Helper()
	if harnessErr != nil {
		t.Skipf("integration harness unavailable (start it with `docker compose -f docker-compose.test.yml up -d --wait`): %v", harnessErr)
	}
}

// uniqueID returns a per-test-unique identifier so suites can share infra without
// data bleed (each test filters ClickHouse by its own short_id / topic).
func uniqueID(prefix string) string {
	return fmt.Sprintf("%s_%d_%d", prefix, time.Now().UnixNano(), idCounter.Add(1))
}

// createTopic creates a fresh single-partition topic (auto-create is disabled).
// One topic per test gives each consumer-group a clean, isolated event stream.
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

// publishRaw writes a single message synchronously (waits for the broker ack).
func publishRaw(t *testing.T, topic, key string, value []byte, headers ...kafka.Header) {
	t.Helper()
	w := &kafka.Writer{
		Addr:         kafka.TCP(kafkaBroker),
		Topic:        topic,
		Balancer:     &kafka.LeastBytes{},
		RequiredAcks: kafka.RequireOne,
		BatchTimeout: 10 * time.Millisecond,
	}
	defer w.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := w.WriteMessages(ctx, kafka.Message{Key: []byte(key), Value: value, Headers: headers}); err != nil {
		t.Fatalf("publish to %s: %v", topic, err)
	}
}

// publishEvent marshals a ClickEvent as the producer would and publishes it.
func publishEvent(t *testing.T, topic string, ev consumer.ClickEvent, headers ...kafka.Header) {
	t.Helper()
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	publishRaw(t, topic, ev.ShortID, b, headers...)
}

// newWriter opens a real ClickHouse writer (the production writer) for a test.
func newWriter(t *testing.T) *writer.ClickHouseWriter {
	t.Helper()
	w, err := writer.NewClickHouseWriter(chAddr, chDatabase, chUser, chPassword)
	if err != nil {
		t.Fatalf("new clickhouse writer: %v", err)
	}
	return w
}

// newConsumer builds a real Kafka consumer bound to a unique group + topic.
func newConsumer(t *testing.T, topic string) *consumer.KafkaConsumer {
	t.Helper()
	return consumer.NewKafkaConsumer(kafkaBroker, topic, uniqueID("grp"))
}

// runPipeline starts pipeline.Run in the background; the returned cancel + wait
// stops it and waits for a clean exit.
func runPipeline(cfg *config.Config, cons consumer.MessageReader, w writer.BatchWriter) (context.CancelFunc, chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		pipeline.Run(ctx, cfg, cons, w)
	}()
	return cancel, done
}

func stopPipeline(t *testing.T, cancel context.CancelFunc, done chan struct{}) {
	t.Helper()
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("pipeline.Run did not exit after cancel")
	}
}

func countClicks(t *testing.T, shortID string) uint64 {
	t.Helper()
	var n uint64
	row := chConn.QueryRow(context.Background(),
		"SELECT count() FROM "+chDatabase+".clicks WHERE short_id = ?", shortID)
	if err := row.Scan(&n); err != nil {
		t.Fatalf("count clicks for %s: %v", shortID, err)
	}
	return n
}

func queryEnrichment(t *testing.T, shortID string) (device, browser string, isBot uint8) {
	t.Helper()
	row := chConn.QueryRow(context.Background(),
		"SELECT device, browser, is_bot FROM "+chDatabase+".clicks WHERE short_id = ? LIMIT 1", shortID)
	if err := row.Scan(&device, &browser, &isBot); err != nil {
		t.Fatalf("query enrichment for %s: %v", shortID, err)
	}
	return device, browser, isBot
}

// pollUntil polls cond every 250ms until it returns true or timeout elapses.
// The pipeline is asynchronous + batched, so assertions must never use fixed
// sleeps — they wait for the row to actually land.
func pollUntil(t *testing.T, timeout time.Duration, desc string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for: %s", timeout, desc)
}
