package privy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

var (
	ErrNotOwned = errors.New("wallet_not_owned")
	ErrNoSigner = errors.New("signer_missing")
	ErrMismatch = errors.New("wallet_address_mismatch")
	ErrNotFound = errors.New("wallet_not_found")
)

type Client struct {
	Base, AppID, AppSecret, QuorumID string
	HTTP                             *http.Client
}

func New(appID, appSecret, quorumID string) *Client {
	return &Client{Base: "https://api.privy.io", AppID: appID, AppSecret: appSecret, QuorumID: quorumID,
		HTTP: &http.Client{Timeout: 10 * time.Second}}
}

// VerifyDelegation checks the wallet is one of did's linked accounts and our key quorum is an additional signer on it.
func (c *Client) VerifyDelegation(ctx context.Context, did, walletID, address string) error {
	var user struct {
		LinkedAccounts []struct{ Type, Address string } `json:"linked_accounts"`
	}
	if err := c.get(ctx, "/v1/users/"+url.PathEscape(did), &user); err != nil {
		return err
	}
	if !slices.ContainsFunc(user.LinkedAccounts, func(a struct{ Type, Address string }) bool {
		return a.Type == "wallet" && strings.EqualFold(a.Address, address)
	}) {
		return ErrNotOwned
	}
	var w struct {
		Address           string `json:"address"`
		AdditionalSigners []struct {
			SignerID string `json:"signer_id"`
		} `json:"additional_signers"`
	}
	if err := c.get(ctx, "/v1/wallets/"+url.PathEscape(walletID), &w); err != nil {
		return err
	}
	if !strings.EqualFold(w.Address, address) {
		return ErrMismatch
	}
	for _, s := range w.AdditionalSigners {
		if s.SignerID == c.QuorumID {
			return nil
		}
	}
	return ErrNoSigner
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.AppID, c.AppSecret)
	req.Header.Set("privy-app-id", c.AppID)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("privy %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("privy GET %s: status %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
