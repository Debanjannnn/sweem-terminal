package ws

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Up is a browser -> server frame.
type Up struct {
	Op     string   `json:"op"`
	Topics []string `json:"topics"`
}

// Down is a server -> browser frame. Type is snapshot, update or error.
type Down struct {
	Topic string          `json:"topic"`
	Type  string          `json:"type"`
	Ts    int64           `json:"ts"`
	Data  json.RawMessage `json:"data"`
}

const (
	TypeSnapshot = "snapshot"
	TypeUpdate   = "update"
	TypeError    = "error"
)

func BookTopic(venue, marketID string) string {
	return "book." + venue + "." + strings.ToLower(marketID)
}

func TickTopic(venue, marketID string) string {
	return "tick." + venue + "." + strings.ToLower(marketID)
}

func CandleTopic(marketID string, resolution int) string {
	return "candle." + strings.ToLower(marketID) + "." + strconv.Itoa(resolution)
}

func IndexTopic(marketID string) string { return "index." + strings.ToLower(marketID) }

// ValidTopic keeps a typo from becoming a subscription that never fires.
func ValidTopic(t string) bool {
	parts := strings.Split(t, ".")
	for _, p := range parts {
		if p == "" {
			return false
		}
	}
	switch parts[0] {
	case "book", "tick":
		return len(parts) == 3
	case "candle":
		if len(parts) != 3 {
			return false
		}
		n, err := strconv.Atoi(parts[2])
		return err == nil && n > 0
	case "index":
		return len(parts) == 2
	}
	return false
}
