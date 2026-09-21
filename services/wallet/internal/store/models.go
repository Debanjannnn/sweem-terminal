package store

import (
	"encoding/json"
	"time"
)

// Every amount is a base-unit integer string, matching numeric(78,0). No float
// ever touches a size or a price.

type User struct {
	ID            string     `json:"id"`
	WalletAddress string     `json:"wallet_address"`
	PrivyWalletID string     `json:"privy_wallet_id,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	FirstFundedAt *time.Time `json:"first_funded_at,omitempty"`
}

type Intent struct {
	Key            string    `json:"intent_key"`
	UserID         string    `json:"user_id"`
	MarketID       string    `json:"market_id"`
	Venue          string    `json:"venue"`
	Side           string    `json:"side"`
	OrderType      string    `json:"order_type"`
	Size           string    `json:"size"`
	LimitPrice     string    `json:"limit_price,omitempty"`
	MaxSlippageBps int       `json:"max_slippage_bps"`
	ChainID        int64     `json:"chain_id"`
	State          string    `json:"state"`
	CreatedAt      time.Time `json:"created_at"`
}

type Execution struct {
	IntentKey    string     `json:"intent_key"`
	TxHash       string     `json:"tx_hash,omitempty"`
	Confirmation string     `json:"confirmation"`
	Error        string     `json:"error,omitempty"`
	SubmittedAt  time.Time  `json:"submitted_at"`
	SettledAt    *time.Time `json:"settled_at,omitempty"`
}

type Schedule struct {
	ID             string          `json:"id"`
	UserID         string          `json:"user_id"`
	Kind           string          `json:"kind"`
	MarketID       string          `json:"market_id"`
	Venue          string          `json:"venue"`
	Side           string          `json:"side"`
	Total          string          `json:"total"`
	SliceCount     int             `json:"slice_count"`
	WindowSeconds  int64           `json:"window_seconds"`
	CadenceSeconds int64           `json:"cadence_seconds"`
	Levels         json.RawMessage `json:"levels,omitempty"`
	State          string          `json:"state"`
	IdempotencyKey string          `json:"idempotency_key"`
	CreatedAt      time.Time       `json:"created_at"`
	Slices         []Slice         `json:"slices,omitempty"`
}

type Slice struct {
	ScheduleID string    `json:"schedule_id"`
	Index      int       `json:"slice_index"`
	DueAt      time.Time `json:"due_at"`
	Amount     string    `json:"amount"`
	State      string    `json:"state"`
	IntentKey  string    `json:"intent_key,omitempty"`
	SkipReason string    `json:"skip_reason,omitempty"`
	// grid levels re-arm and fire again on the same index; twap/dca stay at 0
	Generation int `json:"generation"`
}
