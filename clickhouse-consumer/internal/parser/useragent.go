package parser

import "github.com/mssola/useragent"

type ParsedUA struct {
	Device  string
	Browser string
	IsBot   bool
}

func ParseUserAgent(rawUA string) ParsedUA {
	if rawUA == "" {
		return ParsedUA{Device: "unknown", Browser: "unknown", IsBot: false}
	}

	ua := useragent.New(rawUA)

	if ua.Bot() {
		return ParsedUA{Device: "bot", Browser: "bot", IsBot: true}
	}

	name, _ := ua.Browser()
	browser := name
	if browser == "" {
		browser = "unknown"
	}

	device := "desktop"
	if ua.Mobile() {
		device = "mobile"
	}

	return ParsedUA{Device: device, Browser: browser, IsBot: false}
}
