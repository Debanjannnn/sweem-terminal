// Package client talks to wallet and market-data. scheduler holds no privy
// credentials: every delegation read and every dispatch goes through wallet.
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/httpx"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/privyauth"
	"github.com/sweem-terminal/sweem-terminal/services/scheduler/internal/delegation"
)

type Wallet struct {
	BaseURL string
	Secret  string
	HTTP    *http.Client
}

func NewWallet(baseURL, secret string) *Wallet {
	return &Wallet{BaseURL: baseURL, Secret: secret, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

type Dispatch struct {
	ScheduleID string `json:"schedule_id"`
	SliceIndex int    `json:"slice_index"`
	State      string `json:"state"`
	IntentKey  string `json:"intent_key"`
	TxHash     string `json:"tx_hash"`
	Error      string `json:"error"`
	Replayed   bool   `json:"replayed"`
}

// DispatchSlice is a trigger, not an authorization: wallet re-checks
// delegation, caps, policy and balance, and it owns the slice's state.
func (w *Wallet) DispatchSlice(ctx context.Context, did, scheduleID string, index int) (Dispatch, error) {
	var out Dispatch
	path := "/v1/internal/slices/" + url.PathEscape(scheduleID) + "/" + strconv.Itoa(index) + "/dispatch"
	err := w.do(ctx, http.MethodPost, path, did, &out)
	return out, err
}

func (w *Wallet) Delegation(ctx context.Context, did string) (delegation.Status, error) {
	var st delegation.Status
	err := w.do(ctx, http.MethodGet, "/v1/delegation?user="+url.QueryEscape(did), did, &st)
	return st, err
}

func (w *Wallet) do(ctx context.Context, method, path, did string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, w.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(privyauth.HeaderInternalSecret, w.Secret)
	req.Header.Set(privyauth.HeaderActingUser, did)

	resp, err := w.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("call wallet %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	var env struct {
		Data  json.RawMessage `json:"data"`
		Error *httpx.Err      `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil && resp.StatusCode < 300 {
		return fmt.Errorf("decode wallet response for %s: %w", path, err)
	}
	if out != nil && len(env.Data) > 0 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			return fmt.Errorf("decode wallet payload for %s: %w", path, err)
		}
	}
	if resp.StatusCode >= 300 {
		return walletError(path, resp.StatusCode, env.Error)
	}
	return nil
}

// wallet answers a missing or dead delegation with a code, never a special
// http status, so the codes are what we key on.
func walletError(path string, status int, e *httpx.Err) error {
	if e == nil {
		return fmt.Errorf("wallet %s returned %d", path, status)
	}
	switch e.Code {
	case "no_delegation", "delegation_revoked", "delegation_expired":
		return fmt.Errorf("%w: wallet says %s", delegation.ErrRevoked, e.Message)
	}
	return fmt.Errorf("wallet %s returned %d %s: %s", path, status, e.Code, e.Message)
}
