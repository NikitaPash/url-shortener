//go:build integration

package integration

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/NikitaPash/clickhouse-consumer/internal/config"
	"github.com/NikitaPash/clickhouse-consumer/internal/consumer"
	"github.com/NikitaPash/clickhouse-consumer/internal/writer"
)

const (
	uaDesktop = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	uaMobile  = "Mozilla/5.0 (iPhone; CPU iPhone OS 16_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/16.0 Mobile/15E148 Safari/604.1"
	uaBot     = "Googlebot/2.1 (+http://www.google.com/bot.html)"
)

func clickEvent(shortID, userAgent string) consumer.ClickEvent {
	return consumer.ClickEvent{
		Timestamp: time.Now().UTC(),
		ShortID:   shortID,
		UserID:    "user-" + shortID,
		IP:        "203.0.113.7",
		UserAgent: userAgent,
		Referrer:  "https://news.example.com/",
		Country:   "UA",
	}
}

// --- writer decorators (wrap the REAL ClickHouse writer) ---

type countingWriter struct {
	inner writer.BatchWriter
	calls atomic.Int64
	rows  atomic.Int64
}

func (c *countingWriter) WriteBatch(ctx context.Context, rows []writer.ClickRow) error {
	if err := c.inner.WriteBatch(ctx, rows); err != nil {
		return err
	}
	c.calls.Add(1)
	c.rows.Add(int64(len(rows)))
	return nil
}
func (c *countingWriter) Close() error { return c.inner.Close() }

type flakyWriter struct {
	inner     writer.BatchWriter
	failsLeft atomic.Int64
}

func (f *flakyWriter) WriteBatch(ctx context.Context, rows []writer.ClickRow) error {
	if f.failsLeft.Add(-1) >= 0 {
		return errors.New("simulated transient ClickHouse failure")
	}
	return f.inner.WriteBatch(ctx, rows)
}
func (f *flakyWriter) Close() error { return f.inner.Close() }

// fetchSignalReader wraps a real reader and closes `ready` once `want` events have
// been fetched, so a test can deterministically wait for the consumer to have
// buffered them before triggering shutdown — instead of guessing with a sleep
// (Kafka consumer-group join can take several seconds on the first fetch).
type fetchSignalReader struct {
	inner consumer.MessageReader
	want  int64
	got   atomic.Int64
	ready chan struct{}
	once  sync.Once
}

func (r *fetchSignalReader) FetchMessage(ctx context.Context) (kafka.Message, *consumer.ClickEvent, error) {
	msg, ev, err := r.inner.FetchMessage(ctx)
	if err == nil && ev != nil && r.got.Add(1) >= r.want {
		r.once.Do(func() { close(r.ready) })
	}
	return msg, ev, err
}
func (r *fetchSignalReader) CommitMessages(ctx context.Context, msgs ...kafka.Message) error {
	return r.inner.CommitMessages(ctx, msgs...)
}
func (r *fetchSignalReader) Close() error { return r.inner.Close() }

// §6.5.1 — round-trip through real Kafka + ClickHouse, with User-Agent enrichment
// into device / browser / is_bot performed by the consumer.
func TestPipeline_RoundTripAndEnrichment(t *testing.T) {
	requireHarness(t)

	topic := uniqueID("it_topic")
	createTopic(t, topic)
	cons := newConsumer(t, topic)
	defer cons.Close()
	w := newWriter(t)

	base := uniqueID("sid")
	desktop, mobile, bot := base+"_d", base+"_m", base+"_b"
	publishEvent(t, topic, clickEvent(desktop, uaDesktop))
	publishEvent(t, topic, clickEvent(mobile, uaMobile))
	publishEvent(t, topic, clickEvent(bot, uaBot))

	cfg := &config.Config{BatchSize: 100, FlushInterval: time.Second}
	cancel, done := runPipeline(cfg, cons, w)

	pollUntil(t, 20*time.Second, "all 3 enriched clicks present in ClickHouse", func() bool {
		return countClicks(t, desktop) == 1 && countClicks(t, mobile) == 1 && countClicks(t, bot) == 1
	})
	stopPipeline(t, cancel, done)

	dDev, dBrowser, dBot := queryEnrichment(t, desktop)
	if dDev != "desktop" || dBot != 0 {
		t.Errorf("desktop: device=%q is_bot=%d, want device=desktop is_bot=0", dDev, dBot)
	}
	if dBrowser == "" || dBrowser == "unknown" {
		t.Errorf("desktop: browser=%q, want a recognised browser", dBrowser)
	}

	mDev, _, mBot := queryEnrichment(t, mobile)
	if mDev != "mobile" || mBot != 0 {
		t.Errorf("mobile: device=%q is_bot=%d, want device=mobile is_bot=0", mDev, mBot)
	}

	bDev, bBrowser, bBot := queryEnrichment(t, bot)
	if bDev != "bot" || bBrowser != "bot" || bBot != 1 {
		t.Errorf("bot: device=%q browser=%q is_bot=%d, want device=bot browser=bot is_bot=1", bDev, bBrowser, bBot)
	}
}

// §6.5.2 — reaching the configured batch size triggers exactly one batched insert.
func TestPipeline_BatchSizeFlush(t *testing.T) {
	requireHarness(t)

	topic := uniqueID("it_topic")
	createTopic(t, topic)
	cons := newConsumer(t, topic)
	defer cons.Close()
	cw := &countingWriter{inner: newWriter(t)}

	base := uniqueID("sid")
	const n = 5
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = base + "_" + string(rune('0'+i))
		publishEvent(t, topic, clickEvent(ids[i], uaDesktop))
	}

	// FlushInterval far in the future so the ONLY flush trigger is batch size.
	cfg := &config.Config{BatchSize: n, FlushInterval: 30 * time.Second}
	cancel, done := runPipeline(cfg, cons, cw)

	pollUntil(t, 20*time.Second, "all 5 clicks present", func() bool {
		for _, id := range ids {
			if countClicks(t, id) != 1 {
				return false
			}
		}
		return true
	})
	stopPipeline(t, cancel, done)

	if got := cw.calls.Load(); got != 1 {
		t.Errorf("WriteBatch calls = %d, want 1 (single batched insert)", got)
	}
	if got := cw.rows.Load(); got != n {
		t.Errorf("rows written = %d, want %d", got, n)
	}
}

// §6.5.3 — at-least-once: a failed insert is NOT committed and is retried from the
// in-memory buffer on the next flush; the row still lands exactly once, no loss.
func TestPipeline_AtLeastOnce_RetryAfterInsertFailure(t *testing.T) {
	requireHarness(t)

	topic := uniqueID("it_topic")
	createTopic(t, topic)
	cons := newConsumer(t, topic)
	defer cons.Close()
	fw := &flakyWriter{inner: newWriter(t)}
	fw.failsLeft.Store(1) // fail the first WriteBatch, succeed thereafter

	sid := uniqueID("sid")
	publishEvent(t, topic, clickEvent(sid, uaDesktop))

	cfg := &config.Config{BatchSize: 1, FlushInterval: time.Second}
	cancel, done := runPipeline(cfg, cons, fw)

	pollUntil(t, 20*time.Second, "click present after retry", func() bool {
		return countClicks(t, sid) == 1
	})
	stopPipeline(t, cancel, done)

	if got := countClicks(t, sid); got != 1 {
		t.Errorf("clicks for %s = %d, want exactly 1 (no loss, no duplicate)", sid, got)
	}
}

// §6.5.4 — a malformed message is skipped (offset advances) while a valid message
// in the same stream is still inserted.
func TestPipeline_SkipsMalformedMessage(t *testing.T) {
	requireHarness(t)

	topic := uniqueID("it_topic")
	createTopic(t, topic)
	cons := newConsumer(t, topic)
	defer cons.Close()
	w := newWriter(t)

	publishRaw(t, topic, "bad", []byte("{not valid json")) // malformed
	sid := uniqueID("sid")
	publishEvent(t, topic, clickEvent(sid, uaDesktop)) // valid

	cfg := &config.Config{BatchSize: 100, FlushInterval: time.Second}
	cancel, done := runPipeline(cfg, cons, w)

	pollUntil(t, 20*time.Second, "valid click present despite preceding malformed message", func() bool {
		return countClicks(t, sid) == 1
	})
	stopPipeline(t, cancel, done)

	if got := countClicks(t, sid); got != 1 {
		t.Errorf("clicks for %s = %d, want 1", sid, got)
	}
}

// §6.5.5 — the W3C trace context injected into Kafka headers by the producer is
// extracted by the consumer, so the clickhouse.BatchInsert span shares the
// producer's trace_id across the Kafka boundary.
func TestPipeline_TraceContextCrossesKafka(t *testing.T) {
	requireHarness(t)

	spanExporter.Reset()

	// Simulate the producer: start a span and inject its context into headers.
	ctxSpan, span := otel.Tracer("test-producer").Start(context.Background(), "test.PublishClick")
	wantTrace := span.SpanContext().TraceID()
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctxSpan, carrier)
	span.End()

	headers := make([]kafka.Header, 0, len(carrier))
	for k, v := range carrier {
		headers = append(headers, kafka.Header{Key: k, Value: []byte(v)})
	}

	topic := uniqueID("it_topic")
	createTopic(t, topic)
	cons := newConsumer(t, topic)
	defer cons.Close()
	w := newWriter(t)

	sid := uniqueID("sid")
	publishEvent(t, topic, clickEvent(sid, uaDesktop), headers...)

	cfg := &config.Config{BatchSize: 100, FlushInterval: time.Second}
	cancel, done := runPipeline(cfg, cons, w)
	pollUntil(t, 20*time.Second, "click present", func() bool {
		return countClicks(t, sid) == 1
	})
	stopPipeline(t, cancel, done)

	found := false
	for _, s := range spanExporter.GetSpans() {
		if s.Name == "clickhouse.BatchInsert" && s.SpanContext.TraceID() == wantTrace {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("no clickhouse.BatchInsert span with trace_id %s — trace context did not cross Kafka", wantTrace)
	}
}

// §6.5.6 — on shutdown, rows buffered below the batch size (and before the flush
// ticker fires) are still flushed and committed.
func TestPipeline_ShutdownFlush(t *testing.T) {
	requireHarness(t)

	topic := uniqueID("it_topic")
	createTopic(t, topic)
	cons := newConsumer(t, topic)
	defer cons.Close()
	w := newWriter(t)

	base := uniqueID("sid")
	a, b := base+"_a", base+"_b"
	publishEvent(t, topic, clickEvent(a, uaDesktop))
	publishEvent(t, topic, clickEvent(b, uaMobile))

	// Neither batch size (100) nor flush ticker (30s) will fire on their own, so a
	// flush can only happen on shutdown.
	reader := &fetchSignalReader{inner: cons, want: 2, ready: make(chan struct{})}
	cfg := &config.Config{BatchSize: 100, FlushInterval: 30 * time.Second}
	cancel, done := runPipeline(cfg, reader, w)

	// Wait until both messages are fetched (and thus buffered) before shutting down.
	select {
	case <-reader.ready:
	case <-time.After(25 * time.Second):
		stopPipeline(t, cancel, done)
		t.Fatal("consumer did not fetch both messages before shutdown")
	}
	stopPipeline(t, cancel, done) // cancel → final flush of the buffered rows

	pollUntil(t, 10*time.Second, "both clicks flushed on shutdown", func() bool {
		return countClicks(t, a) == 1 && countClicks(t, b) == 1
	})
}
