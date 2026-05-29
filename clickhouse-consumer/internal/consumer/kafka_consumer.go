package consumer

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/segmentio/kafka-go"
)

// ClickEvent mirrors the schema published by the shortener API.
// Deliberately a separate struct — the consumer and producer are independent services.
type ClickEvent struct {
	Timestamp time.Time `json:"timestamp"`
	ShortID   string    `json:"short_id"`
	UserID    string    `json:"user_id"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	Referrer  string    `json:"referrer"`
	Country   string    `json:"country"`
}

// MessageReader is the Kafka consumption behavior the consumer loop depends on.
// *KafkaConsumer satisfies this interface.
type MessageReader interface {
	FetchMessage(ctx context.Context) (kafka.Message, *ClickEvent, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

// kafkaReader is the kafka.Reader subset KafkaConsumer uses, enabling fakes in tests.
type kafkaReader interface {
	FetchMessage(ctx context.Context) (kafka.Message, error)
	CommitMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

type KafkaConsumer struct {
	reader kafkaReader
}

func NewKafkaConsumer(brokers, topic, groupID string) *KafkaConsumer {
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:        []string{brokers},
		Topic:          topic,
		GroupID:        groupID,
		MinBytes:       1,
		MaxBytes:       10e6,
		MaxWait:        1 * time.Second,
		StartOffset:    kafka.FirstOffset,
		CommitInterval: 0, // manual commit only
	})

	slog.Info("kafka consumer created",
		"brokers", brokers,
		"topic", topic,
		"group", groupID,
	)

	return &KafkaConsumer{reader: reader}
}

// FetchMessage reads the next message from Kafka without committing the offset.
// Returns (msg, nil, nil) for malformed messages so the caller can skip them.
func (c *KafkaConsumer) FetchMessage(ctx context.Context) (kafka.Message, *ClickEvent, error) {
	msg, err := c.reader.FetchMessage(ctx)
	if err != nil {
		return msg, nil, err
	}

	var event ClickEvent
	if err := json.Unmarshal(msg.Value, &event); err != nil {
		slog.Error("failed to unmarshal click event",
			"error", err,
			"offset", msg.Offset,
			"partition", msg.Partition,
		)
		// Return message with nil event so caller commits the offset and skips it.
		return msg, nil, nil
	}

	return msg, &event, nil
}

// CommitMessages commits offsets. Call only after successful ClickHouse insert.
func (c *KafkaConsumer) CommitMessages(ctx context.Context, msgs ...kafka.Message) error {
	return c.reader.CommitMessages(ctx, msgs...)
}

func (c *KafkaConsumer) Close() error {
	return c.reader.Close()
}
