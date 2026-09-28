package store

import (
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

// Intent is every call this service makes to the executor, trade or not:
// order_type is "market", "limit", "cancel", "deposit", "authorize" or "revoke".
type Intent struct {
	Key       string `json:"intent_key"`
	UserID    string `json:"user_id"`
	MarketID  string `json:"market_id"`
	Venue     string `json:"venue"`
	Side      string `json:"side"`
	OrderType string `json:"order_type"`
	// what the order can spend, in raw atoms of the token it spends. the
	// delegation caps are denominated in this and nothing else; see policy/units.go
	Size string `json:"size"`
	// maker only: raw tick integer
	LimitPrice string `json:"limit_price,omitempty"`
	// maker only: book units
	Quantity        string `json:"quantity,omitempty"`
	Tif             string `json:"tif,omitempty"`
	PostOnly        bool   `json:"post_only,omitempty"`
	Token           string `json:"token,omitempty"`
	SlotIdx         *int   `json:"slot_idx,omitempty"`
	ExpectedOrderID string `json:"expected_order_id,omitempty"`
	// perpl open orders only, hundredths: 500 = 5x
	Leverage int `json:"leverage,omitempty"`
	// the venue's id for this order once it rests; empty until then
	OrderID        string    `json:"order_id,omitempty"`
	MaxSlippageBps int       `json:"max_slippage_bps"`
	ChainID        int64     `json:"chain_id"`
	State          string    `json:"state"`
	CreatedAt      time.Time `json:"created_at"`
}

// KuruAccount is the AccountCore state, cached so a read costs no executor
// round trip. KuruUserID 0 means the address has never deposited and cannot
// trade: every Kuru call names the account by that id.
type KuruAccount struct {
	UserID           string `json:"user_id"`
	ChainID          int64  `json:"chain_id"`
	KuruUserID       int64  `json:"kuru_user_id"`
	SignerAuthorized bool   `json:"signer_authorized"`
	AuthNonce        string `json:"auth_nonce"`
	AccountCore      string `json:"account_core"`
	SignerExpiry     int64  `json:"signer_expiry"`
	// the executor's backend signer; empty when the owner's own wallet signs
	Signer    string    `json:"signer"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Execution struct {
	IntentKey    string     `json:"intent_key"`
	TxHash       string     `json:"tx_hash,omitempty"`
	Confirmation string     `json:"confirmation"`
	Error        string     `json:"error,omitempty"`
	SubmittedAt  time.Time  `json:"submitted_at"`
	SettledAt    *time.Time `json:"settled_at,omitempty"`
}
