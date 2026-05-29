package geo_test

import (
	"testing"

	"github.com/NikitaPash/url-shortener/internal/geo"
)

// TestNewResolver_MissingFile verifies the resolver degrades gracefully when
// the database file doesn't exist — it must not panic and must return a stub
// that always returns an empty country code.
func TestNewResolver_MissingFile(t *testing.T) {
	r := geo.NewResolver("/nonexistent/path/GeoLite2-Country.mmdb")
	if r == nil {
		t.Fatal("NewResolver returned nil for missing file")
	}
	if code := r.Country("8.8.8.8"); code != "" {
		t.Errorf("stub resolver returned %q, want empty string", code)
	}
}

func TestResolver_Country(t *testing.T) {
	// All tests use the stub resolver (no .mmdb file), so the expected result is
	// always "" — these cases exercise the input-normalisation paths in Country().
	r := geo.NewResolver("")

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "host:port stripped before lookup",
			input: "203.0.113.5:54321",
			want:  "",
		},
		{
			name:  "bare IP without port",
			input: "203.0.113.5",
			want:  "",
		},
		{
			name:  "invalid IP returns empty",
			input: "not-an-ip",
			want:  "",
		},
		{
			name:  "empty string returns empty",
			input: "",
			want:  "",
		},
		{
			name:  "IPv6 localhost returns empty",
			input: "[::1]:8080",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.Country(tt.input); got != tt.want {
				t.Errorf("Country(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
