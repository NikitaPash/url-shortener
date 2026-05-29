package writer

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// BatchWriter is the insert behavior the consumer loop depends on.
// *ClickHouseWriter satisfies this interface.
type BatchWriter interface {
	WriteBatch(ctx context.Context, rows []ClickRow) error
	Close() error
}

// chConn is the connection subset WriteBatch uses, narrower than driver.Conn,
// so tests can inject a lightweight fake instead of a full driver.Conn mock.
type chConn interface {
	PrepareBatch(ctx context.Context, query string, opts ...driver.PrepareBatchOption) (driver.Batch, error)
	Ping(ctx context.Context) error
	Close() error
}

type ClickRow struct {
	Timestamp time.Time
	ShortID   string
	UserID    string
	IP        string
	UserAgent string
	Referrer  string
	Country   string
	Device    string
	Browser   string
	IsBot     uint8
}

type ClickHouseWriter struct {
	conn chConn
}

func NewClickHouseWriter(addr, database, user, password string) (*ClickHouseWriter, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Database: database,
			Username: user,
			Password: password,
		},
		Settings: clickhouse.Settings{
			"max_execution_time": 60,
		},
		DialTimeout:     5 * time.Second,
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: 10 * time.Minute,
	})
	if err != nil {
		return nil, fmt.Errorf("clickhouse open: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Ping(pingCtx); err != nil {
		return nil, fmt.Errorf("clickhouse ping: %w", err)
	}

	slog.Info("clickhouse connected", "addr", addr, "database", database)
	return &ClickHouseWriter{conn: conn}, nil
}

// WriteBatch inserts rows into ClickHouse in a single batch.
// Caller must NOT commit Kafka offsets if this returns an error.
func (w *ClickHouseWriter) WriteBatch(ctx context.Context, rows []ClickRow) error {
	if len(rows) == 0 {
		return nil
	}

	batch, err := w.conn.PrepareBatch(ctx,
		"INSERT INTO clicks (timestamp, short_id, user_id, ip, user_agent, referrer, country, device, browser, is_bot)",
	)
	if err != nil {
		return fmt.Errorf("prepare batch: %w", err)
	}

	for _, row := range rows {
		if err := batch.Append(
			row.Timestamp,
			row.ShortID,
			row.UserID,
			row.IP,
			row.UserAgent,
			row.Referrer,
			row.Country,
			row.Device,
			row.Browser,
			row.IsBot,
		); err != nil {
			return fmt.Errorf("append row: %w", err)
		}
	}

	if err := batch.Send(); err != nil {
		return fmt.Errorf("send batch: %w", err)
	}

	slog.Debug("batch inserted", "rows", len(rows))
	return nil
}

func (w *ClickHouseWriter) Close() error {
	return w.conn.Close()
}
