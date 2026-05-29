package event

import "time"

// ClickEvent represents a single link-click event published to Kafka.
type ClickEvent struct {
	Timestamp time.Time `json:"timestamp"`
	ShortID   string    `json:"short_id"`
	UserID    string    `json:"user_id"`
	IP        string    `json:"ip"`
	UserAgent string    `json:"user_agent"`
	Referrer  string    `json:"referrer"`
	Country   string    `json:"country"`
}
