package pipeline

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"

	"github.com/NikitaPash/clickhouse-consumer/internal/config"
	"github.com/NikitaPash/clickhouse-consumer/internal/consumer"
	"github.com/NikitaPash/clickhouse-consumer/internal/writer"
)

// --- fakes ---

type fakeReader struct {
	fetchFn  func(context.Context) (kafka.Message, *consumer.ClickEvent, error)
	commitFn func(context.Context, ...kafka.Message) error
}

func (f *fakeReader) FetchMessage(ctx context.Context) (kafka.Message, *consumer.ClickEvent, error) {
	return f.fetchFn(ctx)
}
func (f *fakeReader) CommitMessages(ctx context.Context, msgs ...kafka.Message) error {
	if f.commitFn != nil {
		return f.commitFn(ctx, msgs...)
	}
	return nil
}
func (f *fakeReader) Close() error { return nil }

type fakeBatchWriter struct {
	written  []writer.ClickRow
	writeErr error
}

func (f *fakeBatchWriter) WriteBatch(_ context.Context, rows []writer.ClickRow) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.written = append(f.written, rows...)
	return nil
}
func (f *fakeBatchWriter) Close() error { return nil }

func testCfg(batchSize int) *config.Config {
	return &config.Config{BatchSize: batchSize, FlushInterval: 10 * time.Second}
}

// --- tests ---

// TestRun_FlushOnBatchSize verifies that the loop flushes and commits
// when the buffer reaches the configured batch size.
func TestRun_FlushOnBatchSize(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	evt := &consumer.ClickEvent{ShortID: "abc", UserID: "u1", Timestamp: time.Now()}

	callCount := 0
	committed := false
	bw := &fakeBatchWriter{}

	reader := &fakeReader{
		fetchFn: func(fetchCtx context.Context) (kafka.Message, *consumer.ClickEvent, error) {
			if callCount < 3 {
				callCount++
				return kafka.Message{Offset: int64(callCount)}, evt, nil
			}
			// After batch is flushed+committed (cancel called), unblock.
			<-fetchCtx.Done()
			return kafka.Message{}, nil, fetchCtx.Err()
		},
		commitFn: func(_ context.Context, _ ...kafka.Message) error {
			committed = true
			cancel() // stop the loop
			return nil
		},
	}

	Run(ctx, testCfg(3), reader, bw)

	if !committed {
		t.Error("expected CommitMessages to be called after batch size reached")
	}
	if len(bw.written) != 3 {
		t.Errorf("written rows = %d, want 3", len(bw.written))
	}
}

// TestRun_SkipsMalformedEvent verifies that a nil event (malformed JSON in
// FetchMessage) does not add a row to the ClickHouse batch, but the Kafka offset
// is still committed so processing advances.
func TestRun_SkipsMalformedEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	callCount := 0
	committed := false
	bw := &fakeBatchWriter{}

	reader := &fakeReader{
		fetchFn: func(fetchCtx context.Context) (kafka.Message, *consumer.ClickEvent, error) {
			callCount++
			if callCount == 1 {
				// Malformed → nil event, message returned for offset tracking.
				return kafka.Message{Offset: 1}, nil, nil
			}
			// Trigger shutdown so the final flush commits the malformed msg offset.
			cancel()
			<-fetchCtx.Done()
			return kafka.Message{}, nil, fetchCtx.Err()
		},
		commitFn: func(_ context.Context, _ ...kafka.Message) error {
			committed = true
			return nil
		},
	}

	Run(ctx, testCfg(1000), reader, bw)

	if len(bw.written) != 0 {
		t.Errorf("expected 0 CH rows for malformed event, got %d", len(bw.written))
	}
	if !committed {
		t.Error("expected offset to be committed even for malformed event")
	}
}

// TestRun_ShutdownFlushesRemainingRows verifies that events buffered between
// ticker intervals are written and committed when the context is canceled.
func TestRun_ShutdownFlushesRemainingRows(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	evt := &consumer.ClickEvent{ShortID: "xyz", UserID: "u2", Timestamp: time.Now()}
	callCount := 0
	committed := false
	bw := &fakeBatchWriter{}

	reader := &fakeReader{
		fetchFn: func(fetchCtx context.Context) (kafka.Message, *consumer.ClickEvent, error) {
			if callCount < 2 {
				callCount++
				return kafka.Message{Offset: int64(callCount)}, evt, nil
			}
			cancel() // cancel while 2 rows are buffered (batchSize=100)
			<-fetchCtx.Done()
			return kafka.Message{}, nil, fetchCtx.Err()
		},
		commitFn: func(_ context.Context, _ ...kafka.Message) error {
			committed = true
			return nil
		},
	}

	Run(ctx, testCfg(100 /* larger than 2 */), reader, bw)

	if len(bw.written) != 2 {
		t.Errorf("written rows = %d, want 2 after shutdown flush", len(bw.written))
	}
	if !committed {
		t.Error("expected CommitMessages on shutdown flush")
	}
}

// TestRun_WriteError_DoesNotCommit verifies that if WriteBatch fails the Kafka
// offsets are NOT committed (at-least-once delivery guarantee).
func TestRun_WriteError_DoesNotCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	evt := &consumer.ClickEvent{ShortID: "err", UserID: "u3", Timestamp: time.Now()}
	callCount := 0
	committed := false

	bw := &fakeBatchWriter{writeErr: errWriteFailed}

	reader := &fakeReader{
		fetchFn: func(fetchCtx context.Context) (kafka.Message, *consumer.ClickEvent, error) {
			if callCount == 0 {
				callCount++
				return kafka.Message{Offset: 1}, evt, nil
			}
			cancel()
			<-fetchCtx.Done()
			return kafka.Message{}, nil, fetchCtx.Err()
		},
		commitFn: func(_ context.Context, _ ...kafka.Message) error {
			committed = true
			return nil
		},
	}

	Run(ctx, testCfg(1), reader, bw)

	if committed {
		t.Error("expected NO commit when WriteBatch fails (at-least-once guarantee)")
	}
}

var errWriteFailed = fmt.Errorf("CH insert error")
