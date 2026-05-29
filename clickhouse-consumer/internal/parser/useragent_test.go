package parser_test

import (
	"testing"

	"github.com/NikitaPash/clickhouse-consumer/internal/parser"
)

func TestParseUserAgent(t *testing.T) {
	tests := []struct {
		name    string
		ua      string
		device  string
		browser string
		isBot   bool
	}{
		{
			name:    "Chrome desktop",
			ua:      "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", //nolint:lll // user-agent fixture must be a single line
			device:  "desktop",
			browser: "Chrome",
			isBot:   false,
		},
		{
			name:    "iPhone Safari",
			ua:      "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1", //nolint:lll // user-agent fixture must be a single line
			device:  "mobile",
			browser: "Safari",
			isBot:   false,
		},
		{
			name:    "Googlebot",
			ua:      "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			device:  "bot",
			browser: "bot",
			isBot:   true,
		},
		{
			name:    "Empty string",
			ua:      "",
			device:  "unknown",
			browser: "unknown",
			isBot:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parser.ParseUserAgent(tt.ua)
			if result.Device != tt.device {
				t.Errorf("Device: got %q, want %q", result.Device, tt.device)
			}
			if result.Browser != tt.browser {
				t.Errorf("Browser: got %q, want %q", result.Browser, tt.browser)
			}
			if result.IsBot != tt.isBot {
				t.Errorf("IsBot: got %v, want %v", result.IsBot, tt.isBot)
			}
		})
	}
}
