package analytics

import (
	"context"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

type LinkStats struct {
	TotalClicks    uint64       `json:"total_clicks"`
	UniqueVisitors uint64       `json:"unique_visitors"`
	BotClicks      uint64       `json:"bot_clicks"`
	AvgPerDay      float64      `json:"avg_per_day"`
	ClicksOverTime []DayCount   `json:"clicks_over_time"`
	PreviousPeriod []DayCount   `json:"previous_period"`
	Countries      []LabelCount `json:"countries"`
	Devices        []LabelCount `json:"devices"`
	Browsers       []LabelCount `json:"browsers"`
	Referrers      []LabelCount `json:"referrers"`
	PeakHours      []HourCount  `json:"peak_hours"`
}

type DayCount struct {
	Date   string `json:"date"`
	Clicks uint64 `json:"clicks"`
}

type HourCount struct {
	Hour   uint8  `json:"hour"`
	Clicks uint64 `json:"clicks"`
}

type LabelCount struct {
	Label  string `json:"label"`
	Clicks uint64 `json:"clicks"`
}

type ClickHouseReader struct {
	conn driver.Conn
}

func NewClickHouseReader(addr, database, user, password string) (*ClickHouseReader, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Database: database,
			Username: user,
			Password: password,
		},
		Protocol: clickhouse.Native,
	})
	if err != nil {
		return nil, err
	}
	return &ClickHouseReader{conn: conn}, nil
}

func (r *ClickHouseReader) Ping(ctx context.Context) error {
	return r.conn.Ping(ctx)
}

func (r *ClickHouseReader) Close() error {
	return r.conn.Close()
}

// botCond appends an IS_BOT filter when bots should be excluded.
// Safe to interpolate: value is controlled by Go code, never user input.
func botCond(excludeBots bool) string {
	if excludeBots {
		return " AND is_bot = 0"
	}
	return ""
}

func (r *ClickHouseReader) GetLinkStats(ctx context.Context, shortID string, from, to time.Time, excludeBots bool) (*LinkStats, error) {
	days := int(to.Sub(from).Hours() / 24)
	stats := &LinkStats{}
	var err error

	stats.TotalClicks, err = r.queryTotal(ctx, shortID, from, to, excludeBots)
	if err != nil {
		return nil, err
	}

	if days > 0 {
		stats.AvgPerDay = float64(stats.TotalClicks) / float64(days)
	}

	stats.UniqueVisitors, err = r.queryUnique(ctx, shortID, from, to, excludeBots)
	if err != nil {
		return nil, err
	}

	// Always count bots separately so the UI can show how many were excluded.
	stats.BotClicks, err = r.queryBotCount(ctx, shortID, from, to)
	if err != nil {
		return nil, err
	}

	stats.ClicksOverTime, err = r.queryOverTime(ctx, shortID, from, to, excludeBots)
	if err != nil {
		return nil, err
	}

	duration := to.Sub(from)
	stats.PreviousPeriod, err = r.queryOverTime(ctx, shortID, from.Add(-duration), from, excludeBots)
	if err != nil {
		return nil, err
	}

	stats.Countries, err = r.queryLabels(ctx, shortID, from, to, "country", 8, excludeBots)
	if err != nil {
		return nil, err
	}

	stats.Devices, err = r.queryLabels(ctx, shortID, from, to, "device", 10, excludeBots)
	if err != nil {
		return nil, err
	}

	// Browsers exclude bots (bot browser values are not meaningful).
	stats.Browsers, err = r.queryLabels(ctx, shortID, from, to, "browser", 8, true)
	if err != nil {
		return nil, err
	}

	stats.Referrers, err = r.queryReferrers(ctx, shortID, from, to, 8, excludeBots)
	if err != nil {
		return nil, err
	}

	stats.PeakHours, err = r.queryPeakHours(ctx, shortID, from, to, excludeBots)
	if err != nil {
		return nil, err
	}

	return stats, nil
}

func (r *ClickHouseReader) queryTotal(ctx context.Context, shortID string, from, to time.Time, excludeBots bool) (uint64, error) {
	row := r.conn.QueryRow(ctx,
		`SELECT count() FROM shortener.clicks WHERE short_id = ? AND timestamp >= ? AND timestamp < ?`+botCond(excludeBots),
		shortID, from, to,
	)
	var n uint64
	return n, row.Scan(&n)
}

func (r *ClickHouseReader) queryUnique(ctx context.Context, shortID string, from, to time.Time, excludeBots bool) (uint64, error) {
	row := r.conn.QueryRow(ctx,
		`SELECT uniq(ip) FROM shortener.clicks WHERE short_id = ? AND timestamp >= ? AND timestamp < ?`+botCond(excludeBots),
		shortID, from, to,
	)
	var n uint64
	return n, row.Scan(&n)
}

func (r *ClickHouseReader) queryBotCount(ctx context.Context, shortID string, from, to time.Time) (uint64, error) {
	row := r.conn.QueryRow(ctx,
		`SELECT count() FROM shortener.clicks WHERE short_id = ? AND timestamp >= ? AND timestamp < ? AND is_bot = 1`,
		shortID, from, to,
	)
	var n uint64
	return n, row.Scan(&n)
}

func (r *ClickHouseReader) queryOverTime(ctx context.Context, shortID string, from, to time.Time, excludeBots bool) ([]DayCount, error) {
	rows, err := r.conn.Query(ctx,
		`SELECT toDate(timestamp) AS day, count() AS clicks
		 FROM shortener.clicks
		 WHERE short_id = ? AND timestamp >= ? AND timestamp < ?`+botCond(excludeBots)+`
		 GROUP BY day
		 ORDER BY day ASC`,
		shortID, from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byDay := make(map[string]uint64)
	for rows.Next() {
		var day time.Time
		var clicks uint64
		if err := rows.Scan(&day, &clicks); err != nil {
			return nil, err
		}
		byDay[day.Format("2006-01-02")] = clicks
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	days := int(to.Sub(from).Hours() / 24)
	result := make([]DayCount, days)
	for i := range result {
		d := from.AddDate(0, 0, i).Format("2006-01-02")
		result[i] = DayCount{Date: d, Clicks: byDay[d]}
	}
	return result, nil
}

func (r *ClickHouseReader) queryPeakHours(ctx context.Context, shortID string, from, to time.Time, excludeBots bool) ([]HourCount, error) {
	rows, err := r.conn.Query(ctx,
		`SELECT toHour(timestamp) AS hour, count() AS clicks
		 FROM shortener.clicks
		 WHERE short_id = ? AND timestamp >= ? AND timestamp < ?`+botCond(excludeBots)+`
		 GROUP BY hour
		 ORDER BY hour ASC`,
		shortID, from, to,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	byHour := make(map[uint8]uint64)
	for rows.Next() {
		var h uint8
		var clicks uint64
		if err := rows.Scan(&h, &clicks); err != nil {
			return nil, err
		}
		byHour[h] = clicks
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	result := make([]HourCount, 24)
	for i := range result {
		result[i] = HourCount{Hour: uint8(i), Clicks: byHour[uint8(i)]}
	}
	return result, nil
}

// queryLabels is safe despite string interpolation: column is always a hardcoded Go string, never user input.
func (r *ClickHouseReader) queryLabels(ctx context.Context, shortID string, from, to time.Time, column string, limit int, excludeBots bool) ([]LabelCount, error) {
	query := `SELECT if(` + column + ` = '', '(Unknown)', ` + column + `) AS label, count() AS clicks
	          FROM shortener.clicks
	          WHERE short_id = ? AND timestamp >= ? AND timestamp < ?` + botCond(excludeBots) + `
	          GROUP BY label
	          ORDER BY clicks DESC
	          LIMIT ?`
	rows, err := r.conn.Query(ctx, query, shortID, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]LabelCount, 0)
	for rows.Next() {
		var lc LabelCount
		if err := rows.Scan(&lc.Label, &lc.Clicks); err != nil {
			return nil, err
		}
		result = append(result, lc)
	}
	return result, rows.Err()
}

func (r *ClickHouseReader) queryReferrers(ctx context.Context, shortID string, from, to time.Time, limit int, excludeBots bool) ([]LabelCount, error) {
	rows, err := r.conn.Query(ctx,
		`SELECT if(referrer = '', '(Direct)', referrer) AS label, count() AS clicks
		 FROM shortener.clicks
		 WHERE short_id = ? AND timestamp >= ? AND timestamp < ?`+botCond(excludeBots)+`
		 GROUP BY label
		 ORDER BY clicks DESC
		 LIMIT ?`,
		shortID, from, to, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]LabelCount, 0)
	for rows.Next() {
		var lc LabelCount
		if err := rows.Scan(&lc.Label, &lc.Clicks); err != nil {
			return nil, err
		}
		result = append(result, lc)
	}
	return result, rows.Err()
}
