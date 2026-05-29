package geo

import (
	"log/slog"
	"net"

	"github.com/oschwald/maxminddb-golang"
)

// Resolver performs IP-to-country lookups using a local MaxMind database.
type Resolver struct {
	db *maxminddb.Reader
}

type countryRecord struct {
	Country struct {
		ISOCode string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
}

// NewResolver opens the MaxMind database file. Returns a stub resolver if the
// file is missing — the system continues operating with an empty country field.
func NewResolver(dbPath string) *Resolver {
	db, err := maxminddb.Open(dbPath)
	if err != nil {
		slog.Warn("GeoIP database not found — country will be empty",
			"path", dbPath, "error", err)
		return &Resolver{db: nil}
	}
	slog.Info("GeoIP database loaded", "path", dbPath)
	return &Resolver{db: db}
}

// Country resolves an IP string to its ISO 3166-1 alpha-2 country code.
// Returns "" for private IPs, invalid addresses, or when the database is not loaded.
func (r *Resolver) Country(ipStr string) string {
	if r.db == nil {
		return ""
	}

	// Strip port if present (r.RemoteAddr is "ip:port").
	host, _, err := net.SplitHostPort(ipStr)
	if err != nil {
		host = ipStr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return ""
	}

	var record countryRecord
	if err := r.db.Lookup(ip, &record); err != nil {
		return ""
	}

	return record.Country.ISOCode
}

// Close releases the memory-mapped database file.
func (r *Resolver) Close() {
	if r.db != nil {
		if err := r.db.Close(); err != nil {
			slog.Warn("failed to close GeoIP database", "error", err)
		}
	}
}
