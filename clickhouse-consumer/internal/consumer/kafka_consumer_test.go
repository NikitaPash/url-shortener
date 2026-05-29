package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
)

// --- fake kafkaReader ---

type fakeKafkaReader struct {
	msgs []kafka.Message
	idx  int
	err  error
}

func (f *fakeKafkaReader) FetchMessage(_ context.Context) (kafka.Message, error) {
	if f.err != nil {
		return kafka.Message{}, f.err
	}
	if f.idx >= len(f.msgs) {
		return kafka.Message{}, errors.New("no more messages")
	}
	m := f.msgs[f.idx]
	f.idx++
	return m, nil
}

func (f *fakeKafkaReader) CommitMessages(_ context.Context, _ ...kafka.Message) error { return nil }
func (f *fakeKafkaReader) Close() error                                               { return nil }

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// --- tests ---

func TestFetchMessage_ValidEvent(t *testing.T) {
	evt := ClickEvent{
		Timestamp: time.Now().UTC().Truncate(time.Millisecond),
		ShortID:   "abc123",
		UserID:    "user-uuid",
		IP:        "1.2.3.4",
		Country:   "UA",
	}

	c := &KafkaConsumer{reader: &fakeKafkaReader{
		msgs: []kafka.Message{{Value: mustMarshal(t, evt)}},
	}}

	_, got, err := c.FetchMessage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil ClickEvent")
	}
	if got.ShortID != evt.ShortID {
		t.Errorf("ShortID = %q, want %q", got.ShortID, evt.ShortID)
	}
	if got.Country != evt.Country {
		t.Errorf("Country = %q, want %q", got.Country, evt.Country)
	}
}

func TestFetchMessage_MalformedJSON_ReturnsNilEvent(t *testing.T) {
	c := &KafkaConsumer{reader: &fakeKafkaReader{
		msgs: []kafka.Message{{Value: []byte("this-is-not-json"), Offset: 42}},
	}}

	msg, got, err := c.FetchMessage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil event for malformed payload, got %+v", got)
	}
	// The message itself must be returned so the caller can commit the offset.
	if msg.Offset != 42 {
		t.Errorf("offset = %d, want 42", msg.Offset)
	}
}

func TestFetchMessage_ReaderError_PropagatedAsError(t *testing.T) {
	sentinelErr := errors.New("kafka broker unavailable")
	c := &KafkaConsumer{reader: &fakeKafkaReader{err: sentinelErr}}

	_, _, err := c.FetchMessage(context.Background())
	if !errors.Is(err, sentinelErr) {
		t.Errorf("err = %v, want %v", err, sentinelErr)
	}
}

func TestFetchMessage_AllFields_Unmarshalled(t *testing.T) {
	ts := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	evt := ClickEvent{
		Timestamp: ts,
		ShortID:   "short1",
		UserID:    "uid-abc",
		IP:        "192.0.2.1",
		UserAgent: "Mozilla/5.0",
		Referrer:  "https://referrer.example.com",
		Country:   "DE",
	}

	c := &KafkaConsumer{reader: &fakeKafkaReader{
		msgs: []kafka.Message{{Value: mustMarshal(t, evt)}},
	}}

	_, got, err := c.FetchMessage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.UserAgent != "Mozilla/5.0" {
		t.Errorf("UserAgent = %q", got.UserAgent)
	}
	if got.Referrer != "https://referrer.example.com" {
		t.Errorf("Referrer = %q", got.Referrer)
	}
	if !got.Timestamp.Equal(ts) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, ts)
	}
}
