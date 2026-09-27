package executor

import (
	"bytes"
	"encoding/json"
	"testing"
)

// The executor's own request structs, mirrored in Go the way scheduler mirrors
// wallet's intentKey. Every field name and unit is copied from
// executor/src/route.rs; if either side renames or rescales one, this goes red
// instead of a swap going out at 1e8 times the intended size.
type (
	rustRouteRequest struct {
		IntentKey     string `json:"intent_key"`
		PrivyWalletID string `json:"privy_wallet_id"`
		UserWallet    string `json:"user_wallet"`
		MarketID      string `json:"market_id"`
		Venue         string `json:"venue"`
		Side          string `json:"side"`
		// raw atoms of the input token: quote for a buy, base for a sell
		Amount         string `json:"amount"`
		MaxSlippageBps uint32 `json:"max_slippage_bps"`
		ChainID        uint64 `json:"chain_id"`
	}
	rustOrderRequest struct {
		IntentKey     string `json:"intent_key"`
		PrivyWalletID string `json:"privy_wallet_id"`
		UserWallet    string `json:"user_wallet"`
		MarketID      string `json:"market_id"`
		Side          string `json:"side"`
		// raw tick integer, price * price_precision
		Price string `json:"price"`
		// book units, size * size_precision
		Quantity string `json:"quantity"`
		Tif      string `json:"tif"`
		PostOnly bool   `json:"post_only"`
		ChainID  uint64 `json:"chain_id"`
	}
	rustCancelRequest struct {
		IntentKey     string `json:"intent_key"`
		PrivyWalletID string `json:"privy_wallet_id"`
		UserWallet    string `json:"user_wallet"`
		MarketID      string `json:"market_id"`
		SlotIdx       uint8  `json:"slot_idx"`
		// the order the caller believes occupies that slot. slots are reused.
		ExpectedOrderID string `json:"expected_order_id"`
		ChainID         uint64 `json:"chain_id"`
	}
	rustDepositRequest struct {
		IntentKey     string `json:"intent_key"`
		PrivyWalletID string `json:"privy_wallet_id"`
		UserWallet    string `json:"user_wallet"`
		MarketID      string `json:"market_id"`
		// "base" or "quote": a name, never an address
		Token string `json:"token"`
		// raw token atoms
		Amount  string `json:"amount"`
		ChainID uint64 `json:"chain_id"`
	}
	rustSignerRequest struct {
		IntentKey     string `json:"intent_key"`
		PrivyWalletID string `json:"privy_wallet_id"`
		UserWallet    string `json:"user_wallet"`
		// unix seconds; 0 defaults to 30 days. never 0-means-forever.
		Expiry  uint64 `json:"expiry"`
		ChainID uint64 `json:"chain_id"`
	}
	rustAccountRequest struct {
		PrivyWalletID string `json:"privy_wallet_id"`
		UserWallet    string `json:"user_wallet"`
		ChainID       uint64 `json:"chain_id"`
	}
)

// into decodes what wallet would actually put on the wire into the shape the
// executor would actually read, refusing any field the executor does not know.
func into(t *testing.T, sent, dst any) {
	t.Helper()
	body, err := json.Marshal(sent)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		t.Fatalf("%T does not fit %T: %v\nwire: %s", sent, dst, err, body)
	}
}

func TestEveryRequestFitsTheExecutorsShape(t *testing.T) {
	t.Run("route carries the taker amount as raw input atoms", func(t *testing.T) {
		// 25 USDC of quote atoms to buy MON: 6 decimals, no book scaling
		var got rustRouteRequest
		into(t, RouteRequest{IntentKey: "k", PrivyWalletID: "w", UserWallet: "0x11",
			MarketID: "MON-USDC", Venue: "kuru", Side: "buy", Amount: "25000000",
			MaxSlippageBps: 50, ChainID: 10143}, &got)
		if got.Amount != "25000000" || got.MaxSlippageBps != 50 {
			t.Fatalf("route arrived as %+v", got)
		}
	})

	t.Run("order carries ticks and book units, never atoms", func(t *testing.T) {
		// 400 MON @ 0.025 on MON-USDC: price_precision 1e6, size_precision 1e8
		var got rustOrderRequest
		into(t, OrderRequest{IntentKey: "k", PrivyWalletID: "w", UserWallet: "0x11",
			MarketID: "MON-USDC", Side: "buy", Price: "25000", Quantity: "40000000000",
			Tif: "gtc", PostOnly: true, ChainID: 10143}, &got)
		if got.Price != "25000" || got.Quantity != "40000000000" || got.Tif != "gtc" || !got.PostOnly {
			t.Fatalf("order arrived as %+v", got)
		}
		// the raw base atom count of the same order is 4e20: not what is sent
		if got.Quantity == "400000000000000000000" {
			t.Fatal("quantity was sent as raw base atoms, not book units")
		}
	})

	t.Run("cancel binds a slot to an order id", func(t *testing.T) {
		var got rustCancelRequest
		into(t, CancelRequest{IntentKey: "k", PrivyWalletID: "w", UserWallet: "0x11",
			MarketID: "MON-USDC", SlotIdx: 61, ExpectedOrderID: "18446744073709551615",
			ChainID: 10143}, &got)
		if got.SlotIdx != 61 || got.ExpectedOrderID != "18446744073709551615" {
			t.Fatalf("cancel arrived as %+v", got)
		}
	})

	t.Run("deposit names a side of the pair and raw token atoms", func(t *testing.T) {
		var got rustDepositRequest
		into(t, DepositRequest{IntentKey: "k", PrivyWalletID: "w", UserWallet: "0x11",
			MarketID: "MON-USDC", Token: "quote", Amount: "25000000", ChainID: 10143}, &got)
		if got.Token != "quote" || got.Amount != "25000000" {
			t.Fatalf("deposit arrived as %+v", got)
		}
	})

	t.Run("authorize carries a unix second", func(t *testing.T) {
		var got rustSignerRequest
		into(t, SignerRequest{IntentKey: "k", PrivyWalletID: "w", UserWallet: "0x11",
			Expiry: 2000000000, ChainID: 10143}, &got)
		if got.Expiry != 2000000000 {
			t.Fatalf("authorize arrived as %+v", got)
		}
	})

	t.Run("ensure signs nothing, so it carries no intent key", func(t *testing.T) {
		var got rustAccountRequest
		into(t, AccountRequest{PrivyWalletID: "w", UserWallet: "0x11", ChainID: 10143}, &got)
		if got.UserWallet != "0x11" {
			t.Fatalf("ensure arrived as %+v", got)
		}
	})
}

// The executor answers every write with this one shape, and its status is
// truthful: a confirmed transaction is not a resting order.
func TestTheExecutorAnswerIsReadWhole(t *testing.T) {
	const raw = `{"intent_key":"k","tx_hash":"0xabc","status":"not_resting","error":"",
	  "steps":[{"name":"order","status":"confirmed","tx_hash":"0xabc","detail":"MON-USDC buy"}],
	  "info":{"rested":false,"slot_idx":3,"quote_notional":"10000000"}}`
	var got RouteResponse
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "not_resting" || len(got.Steps) != 1 || got.Steps[0].Status != "confirmed" {
		t.Fatalf("answer read as %+v", got)
	}
	var info struct {
		Rested        bool   `json:"rested"`
		SlotIdx       int    `json:"slot_idx"`
		QuoteNotional string `json:"quote_notional"`
	}
	if err := json.Unmarshal(got.Info, &info); err != nil {
		t.Fatal(err)
	}
	// slot_idx is what a later cancel has to name, so it must survive the trip
	if info.SlotIdx != 3 || info.Rested {
		t.Fatalf("info read as %+v", info)
	}
}

// The account read must distinguish "never deposited" from "deposited but not
// delegated": the client shows a different sentence for each.
func TestAccountReadIsTruthfulAboutRegistration(t *testing.T) {
	tests := []struct {
		name             string
		raw              string
		wantRegistered   bool
		wantUserID       int64
		wantAuthNonceStr string
	}{
		{"never deposited", `{"user_id":0,"registered":false,"signer":null,"signer_authorized":false,"auth_nonce":"0","chain_id":10143}`, false, 0, "0"},
		{"funded and delegated", `{"user_id":7,"registered":true,"signer":"0x22","signer_authorized":true,"auth_nonce":"6","chain_id":10143}`, true, 7, "6"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Account
			if err := json.Unmarshal([]byte(tt.raw), &got); err != nil {
				t.Fatal(err)
			}
			if got.Registered != tt.wantRegistered || got.UserID != tt.wantUserID {
				t.Fatalf("account read as %+v", got)
			}
			// the epoch is a uint256 on chain: a string, never a number
			if got.AuthNonce != tt.wantAuthNonceStr {
				t.Fatalf("auth_nonce = %q, want %q", got.AuthNonce, tt.wantAuthNonceStr)
			}
			if (got.UserID == 0) != !got.Registered {
				t.Fatal("registered and user_id disagree: user_id 0 is the unfunded account")
			}
		})
	}
}
