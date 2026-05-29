package telemetry

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// Metrics holds the application-level counters. It is constructed once at
// startup and injected into the components that record measurements, avoiding
// global mutable state (which is harder to test and easy to leave uninitialized).
type Metrics struct {
	Redirect      metric.Int64Counter
	CacheHit      metric.Int64Counter
	CacheMiss     metric.Int64Counter
	KafkaPublish  metric.Int64Counter
	KafkaFailed   metric.Int64Counter
	ClicksDropped metric.Int64Counter
}

// NewMetrics creates the counters from the global meter provider.
// Must be called after Setup() so the provider is configured.
func NewMetrics() (*Metrics, error) {
	meter := otel.Meter("go-api")
	m := &Metrics{}
	var err error

	if m.Redirect, err = meter.Int64Counter("shortener.redirects.total",
		metric.WithDescription("Total redirect requests")); err != nil {
		return nil, err
	}
	if m.CacheHit, err = meter.Int64Counter("shortener.cache.hits",
		metric.WithDescription("Redis cache hits on link resolution")); err != nil {
		return nil, err
	}
	if m.CacheMiss, err = meter.Int64Counter("shortener.cache.misses",
		metric.WithDescription("Redis cache misses on link resolution")); err != nil {
		return nil, err
	}
	if m.KafkaPublish, err = meter.Int64Counter("shortener.kafka.published",
		metric.WithDescription("Click events published to Kafka")); err != nil {
		return nil, err
	}
	if m.KafkaFailed, err = meter.Int64Counter("shortener.kafka.failed",
		metric.WithDescription("Click events that failed to publish to Kafka")); err != nil {
		return nil, err
	}
	if m.ClicksDropped, err = meter.Int64Counter("shortener.clicks.dropped",
		metric.WithDescription("Click events dropped because the publish queue was full")); err != nil {
		return nil, err
	}
	return m, nil
}

// NewNoopMetrics returns a Metrics backed by no-op counters. Useful in tests
// that exercise handlers/services without a configured meter provider.
func NewNoopMetrics() *Metrics {
	meter := noop.NewMeterProvider().Meter("noop")
	counter := func() metric.Int64Counter {
		c, _ := meter.Int64Counter("noop")
		return c
	}
	return &Metrics{
		Redirect:      counter(),
		CacheHit:      counter(),
		CacheMiss:     counter(),
		KafkaPublish:  counter(),
		KafkaFailed:   counter(),
		ClicksDropped: counter(),
	}
}
