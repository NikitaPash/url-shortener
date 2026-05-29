package writer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2/lib/column"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// --- mock chConn & driver.Batch ---

type mockBatch struct {
	appendErr error
	sendErr   error
	rows      [][]any
}

func (m *mockBatch) Append(v ...any) error {
	if m.appendErr != nil {
		return m.appendErr
	}
	m.rows = append(m.rows, v)
	return nil
}
func (m *mockBatch) AppendStruct(_ any) error        { return nil }
func (m *mockBatch) Column(_ int) driver.BatchColumn { return nil }
func (m *mockBatch) Flush() error                    { return nil }
func (m *mockBatch) Send() error                     { return m.sendErr }
func (m *mockBatch) Abort() error                    { return nil }
func (m *mockBatch) Rows() int                       { return len(m.rows) }
func (m *mockBatch) IsSent() bool                    { return m.sendErr == nil }
func (m *mockBatch) Columns() []column.Interface     { return nil }
func (m *mockBatch) Close() error                    { return nil }

type mockConn struct {
	prepareFn func(context.Context, string, ...driver.PrepareBatchOption) (driver.Batch, error)
}

func (m *mockConn) PrepareBatch(ctx context.Context, query string, opts ...driver.PrepareBatchOption) (driver.Batch, error) {
	return m.prepareFn(ctx, query, opts...)
}
func (m *mockConn) Ping(_ context.Context) error { return nil }
func (m *mockConn) Close() error                 { return nil }

// --- tests ---

func TestWriteBatch_EmptyRows_IsNoop(t *testing.T) {
	w := &ClickHouseWriter{conn: nil} // conn never called for empty slice
	if err := w.WriteBatch(context.Background(), nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := w.WriteBatch(context.Background(), []ClickRow{}); err != nil {
		t.Fatalf("unexpected error for empty slice: %v", err)
	}
}

func TestWriteBatch_Success(t *testing.T) {
	batch := &mockBatch{}
	conn := &mockConn{
		prepareFn: func(_ context.Context, _ string, _ ...driver.PrepareBatchOption) (driver.Batch, error) {
			return batch, nil
		},
	}
	w := &ClickHouseWriter{conn: conn}

	rows := []ClickRow{
		{
			Timestamp: time.Now(),
			ShortID:   "abc",
			UserID:    "u1",
			IP:        "1.2.3.4",
			Device:    "desktop",
			IsBot:     0,
		},
		{
			Timestamp: time.Now(),
			ShortID:   "def",
			UserID:    "u2",
			IP:        "5.6.7.8",
			Device:    "mobile",
			IsBot:     0,
		},
	}

	if err := w.WriteBatch(context.Background(), rows); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(batch.rows) != 2 {
		t.Errorf("appended %d rows, want 2", len(batch.rows))
	}
}

func TestWriteBatch_PrepareBatchError_Propagated(t *testing.T) {
	prepErr := errors.New("prepare failed")
	conn := &mockConn{
		prepareFn: func(_ context.Context, _ string, _ ...driver.PrepareBatchOption) (driver.Batch, error) {
			return nil, prepErr
		},
	}
	w := &ClickHouseWriter{conn: conn}

	err := w.WriteBatch(context.Background(), []ClickRow{{ShortID: "x"}})
	if !errors.Is(err, prepErr) {
		t.Errorf("err = %v, want wrapped %v", err, prepErr)
	}
}

func TestWriteBatch_AppendError_Propagated(t *testing.T) {
	appendErr := errors.New("append failed")
	batch := &mockBatch{appendErr: appendErr}
	conn := &mockConn{
		prepareFn: func(_ context.Context, _ string, _ ...driver.PrepareBatchOption) (driver.Batch, error) {
			return batch, nil
		},
	}
	w := &ClickHouseWriter{conn: conn}

	err := w.WriteBatch(context.Background(), []ClickRow{{ShortID: "x"}})
	if !errors.Is(err, appendErr) {
		t.Errorf("err = %v, want wrapped %v", err, appendErr)
	}
}

func TestWriteBatch_SendError_Propagated(t *testing.T) {
	sendErr := errors.New("send failed")
	batch := &mockBatch{sendErr: sendErr}
	conn := &mockConn{
		prepareFn: func(_ context.Context, _ string, _ ...driver.PrepareBatchOption) (driver.Batch, error) {
			return batch, nil
		},
	}
	w := &ClickHouseWriter{conn: conn}

	err := w.WriteBatch(context.Background(), []ClickRow{{ShortID: "x"}})
	if !errors.Is(err, sendErr) {
		t.Errorf("err = %v, want wrapped %v", err, sendErr)
	}
}

func TestWriteBatch_AllFieldsAppended(t *testing.T) {
	batch := &mockBatch{}
	conn := &mockConn{
		prepareFn: func(_ context.Context, _ string, _ ...driver.PrepareBatchOption) (driver.Batch, error) {
			return batch, nil
		},
	}
	w := &ClickHouseWriter{conn: conn}

	ts := time.Now()
	row := ClickRow{
		Timestamp: ts,
		ShortID:   "sid",
		UserID:    "uid",
		IP:        "10.0.0.1",
		UserAgent: "Mozilla/5.0",
		Referrer:  "https://ref.example.com",
		Country:   "PL",
		Device:    "desktop",
		Browser:   "Chrome",
		IsBot:     0,
	}

	if err := w.WriteBatch(context.Background(), []ClickRow{row}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(batch.rows) != 1 || len(batch.rows[0]) != 10 {
		t.Errorf("Append received %d row(s) with %d field(s), want 1 row × 10 fields",
			len(batch.rows), func() int {
				if len(batch.rows) > 0 {
					return len(batch.rows[0])
				}
				return 0
			}())
	}
}
