//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/NikitaPash/url-shortener/internal/event"
)

func sampleEvent(shortID string) event.ClickEvent {
	return event.ClickEvent{
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		ShortID:   shortID,
		UserID:    "user-" + shortID,
		IP:        "203.0.113.9",
		UserAgent: "curl/8.4.0",
		Referrer:  "https://ref.example/",
		Country:   "DE",
	}
}

// §6.4.1 — a published click event lands on the topic keyed by short_id and
// deserialises to the full 7-field ClickEvent (producer↔consumer schema contract).
func TestProducer_PublishesParseableClickEvent(t *testing.T) {
	requireKafka(t)

	topic := uniqueID("it_prod")
	createTopic(t, topic)

	p := event.NewProducer(kafkaBroker, topic, newMetrics(t))
	ev := sampleEvent(uniqueID("sid"))
	p.PublishClickAsync(context.Background(), ev)
	if err := p.Close(); err != nil { // Close flushes pending async writes
		t.Fatalf("producer close: %v", err)
	}

	msg := readOneMessage(t, topic, 20*time.Second)

	if string(msg.Key) != ev.ShortID {
		t.Errorf("message key = %q, want %q", msg.Key, ev.ShortID)
	}

	var got event.ClickEvent
	if err := json.Unmarshal(msg.Value, &got); err != nil {
		t.Fatalf("unmarshal click event: %v (payload %q)", err, msg.Value)
	}
	if got.ShortID != ev.ShortID || got.UserID != ev.UserID || got.IP != ev.IP ||
		got.UserAgent != ev.UserAgent || got.Referrer != ev.Referrer || got.Country != ev.Country {
		t.Errorf("decoded event mismatch:\n got  %+v\n want %+v", got, ev)
	}
	if !got.Timestamp.Equal(ev.Timestamp) {
		t.Errorf("timestamp = %s, want %s", got.Timestamp, ev.Timestamp)
	}
}

// §6.4.4 — the producer injects W3C trace context into Kafka headers, so the
// consumer can join the same trace across the queue.
func TestProducer_InjectsTraceContext(t *testing.T) {
	requireKafka(t)

	topic := uniqueID("it_prod")
	createTopic(t, topic)

	p := event.NewProducer(kafkaBroker, topic, newMetrics(t))

	// Publish within a span, as the redirect handler does.
	ctxSpan, span := otel.Tracer("test-redirect").Start(context.Background(), "test.Redirect")
	wantTraceID := span.SpanContext().TraceID().String()
	p.PublishClickAsync(ctxSpan, sampleEvent(uniqueID("sid")))
	span.End()
	if err := p.Close(); err != nil {
		t.Fatalf("producer close: %v", err)
	}

	msg := readOneMessage(t, topic, 20*time.Second)

	var traceparent string
	for _, h := range msg.Headers {
		if h.Key == "traceparent" {
			traceparent = string(h.Value)
		}
	}
	if traceparent == "" {
		t.Fatal("no traceparent header on the Kafka message — trace context was not injected")
	}
	// traceparent format: 00-<trace_id>-<span_id>-<flags>; trace_id must match.
	if !strings.Contains(traceparent, wantTraceID) {
		t.Errorf("traceparent %q does not carry trace_id %q", traceparent, wantTraceID)
	}
}

// §6.4.2 — PublishClickAsync must never block the redirect path, even when the
// broker is unreachable (the writer is async).
func TestProducer_PublishDoesNotBlockOnDeadBroker(t *testing.T) {
	// Self-contained: a refused address, no harness required.
	p := event.NewProducer("127.0.0.1:1", uniqueID("dead"), newMetrics(t))
	go p.Close() // flush would block on the dead broker; don't wait on it

	start := time.Now()
	p.PublishClickAsync(context.Background(), sampleEvent("x"))
	elapsed := time.Since(start)

	if elapsed > 500*time.Millisecond {
		t.Errorf("PublishClickAsync blocked %s on a dead broker; the redirect path must never block on Kafka", elapsed)
	}
}

// NOTE: §6.4.3 (events dropped beyond the in-flight cap) is covered
// deterministically as a white-box unit test —
// event.TestPublishClickAsync_DropsWhenAtMaxInFlight — which sets the in-flight
// counter to the cap directly. It cannot be reproduced reliably here: a non-Kafka
// endpoint can't answer the metadata request, so kafka-go rejects writes
// synchronously and the in-flight counter never climbs to the cap.
