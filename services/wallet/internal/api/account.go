package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/httpx"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/executor"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/policy"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/store"
)

// long enough that a page of reads costs one executor call, short enough that a
// deposit or a revoke made elsewhere shows up on its own.
// ponytail: a revoke inside this window is not seen, the executor refuses the
// order anyway; subscribe to AccountSignerRevoked if that is ever too slow.
const accountFresh = 30 * time.Second

type accountView struct {
	// a deposit is what mints the Kuru account id, so this is false until one lands
	Registered       bool  `json:"registered"`
	UserID           int64 `json:"user_id"`
	SignerAuthorized bool  `json:"signer_authorized"`
	// unix seconds, 0 when unknown. expiry emits no event on chain and
	// /account/ensure reads the permission bit but not its deadline, so this is
	// the expiry this service asked for.
	// ponytail: a revoke-and-reauthorize outside this api leaves it stale;
	// auth_nonce moves in that case and is the signal to distrust it.
	SignerExpiry int64  `json:"signer_expiry"`
	AuthNonce    string `json:"auth_nonce"`
	AccountCore  string `json:"account_core"`
	ChainID      int64  `json:"chain_id"`
}

func view(ka store.KuruAccount) accountView {
	v := accountView{
		Registered:       ka.KuruUserID != 0,
		UserID:           ka.KuruUserID,
		SignerAuthorized: ka.SignerAuthorized,
		SignerExpiry:     ka.SignerExpiry,
		AuthNonce:        orZero(ka.AuthNonce),
		AccountCore:      ka.AccountCore,
		ChainID:          ka.ChainID,
	}
	if !ka.SignerAuthorized {
		// the permission is gone, so its deadline is not a fact about anything
		v.SignerExpiry = 0
	}
	return v
}

func (s *Server) getAccount(w http.ResponseWriter, r *http.Request) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	ka, err := s.account(r.Context(), u)
	if err != nil {
		httpx.Fail(w, http.StatusPreconditionFailed, "kuru_account_unknown", unknownAccount)
		s.log().Error("read kuru account", "user_id", u.ID, "err", err)
		return
	}
	httpx.OK(w, view(ka))
}

type depositRequest struct {
	MarketID string `json:"market_id"`
	Token    string `json:"token"`
	// raw token atoms of that side of the pair, never a display amount
	Amount string `json:"amount"`
	// optional: two deliberate deposits of the same amount are two deposits
	IdempotencyKey string `json:"idempotency_key"`
}

// deposit is the bootstrap: until AccountCore holds funds the address has no
// Kuru user id and cannot trade, so this route has no account precondition.
func (s *Server) deposit(w http.ResponseWriter, r *http.Request) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	var req depositRequest
	if !decode(w, r, &req) {
		return
	}
	req.MarketID, req.Token = strings.TrimSpace(req.MarketID), strings.ToLower(strings.TrimSpace(req.Token))
	if !s.knownMarket(r.Context(), req.MarketID) {
		httpx.Fail(w, http.StatusBadRequest, "bad_request",
			fmt.Sprintf("Market %q is not on the executor's allowlist, so nothing can be deposited against it.", req.MarketID))
		return
	}
	if req.Token != "base" && req.Token != "quote" {
		httpx.Fail(w, http.StatusBadRequest, "bad_request",
			fmt.Sprintf("token must be base or quote, not %q: a deposit names a side of the pair, never an address.", req.Token))
		return
	}
	amount, err := policy.ParseAmount(req.Amount)
	if err != nil || amount.Sign() == 0 {
		httpx.Fail(w, http.StatusBadRequest, "bad_request",
			"amount must be a positive integer of raw token atoms, because a float would not survive the round trip to the chain.")
		return
	}

	in := store.Intent{
		Key: intentKey("deposit", u.ID, req.MarketID, req.Token, amount.String(),
			strings.TrimSpace(req.IdempotencyKey)),
		UserID:    u.ID,
		MarketID:  req.MarketID,
		Venue:     "kuru",
		OrderType: "deposit",
		Token:     req.Token,
		Size:      amount.String(),
		ChainID:   s.ChainID,
		State:     "pending",
	}
	resp, execErr := s.perform(r.Context(), in, func(ctx context.Context) (executor.RouteResponse, error) {
		return s.Executor.Deposit(ctx, executor.DepositRequest{
			IntentKey:     in.Key,
			PrivyWalletID: u.PrivyWalletID,
			UserWallet:    u.WalletAddress,
			MarketID:      in.MarketID,
			Token:         in.Token,
			Amount:        in.Size,
			ChainID:       in.ChainID,
		})
	})
	// the first deposit is what creates the account id, so the cached row is stale
	if _, err := s.refresh(r.Context(), u); err != nil {
		s.log().Warn("refresh kuru account after deposit", "user_id", u.ID, "err", err)
	}
	if execErr != nil {
		httpx.JSON(w, http.StatusBadGateway, httpx.Envelope{Data: resp,
			Error: &httpx.Err{Code: "execution_failed", Message: resp.Error}})
		return
	}
	httpx.OK(w, resp)
}

type authorizeRequest struct {
	// unix seconds; omitted means now + defaultGrant. never forever.
	Expiry int64 `json:"expiry"`
}

// mirrors the executor's DEFAULT_GRANT_SECS. named here rather than sent as a
// zero so this service knows the deadline it asked for and can report it back.
const defaultGrant = 30 * 24 * time.Hour

type signerView struct {
	SignerAuthorized bool   `json:"signer_authorized"`
	SignerExpiry     int64  `json:"signer_expiry"`
	AuthNonce        string `json:"auth_nonce"`
	TxHash           string `json:"tx_hash"`
}

func (s *Server) authorizeSigner(w http.ResponseWriter, r *http.Request) {
	var req authorizeRequest
	if r.ContentLength > 0 && !decode(w, r, &req) {
		return
	}
	now := time.Now()
	if req.Expiry == 0 {
		req.Expiry = now.Add(defaultGrant).Unix()
	}
	if req.Expiry <= now.Unix() {
		httpx.Fail(w, http.StatusBadRequest, "bad_request",
			"expiry must be a unix second in the future, because a grant that has already lapsed authorizes nothing.")
		return
	}
	s.grant(w, r, "authorize", req.Expiry)
}

func (s *Server) revokeSigner(w http.ResponseWriter, r *http.Request) {
	s.grant(w, r, "revoke", 0)
}

// grant keys the intent on the authorization epoch: every grant and revoke
// advances it on chain, so a retry inside one epoch replays and the next
// deliberate change derives a fresh key.
func (s *Server) grant(w http.ResponseWriter, r *http.Request, op string, expiry int64) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	before, err := s.account(r.Context(), u)
	if err != nil {
		httpx.Fail(w, http.StatusPreconditionFailed, "kuru_account_unknown", unknownAccount)
		s.log().Error("read kuru account", "user_id", u.ID, "err", err)
		return
	}

	in := store.Intent{
		Key:       intentKey(op, u.ID, orZero(before.AuthNonce), strconv.FormatInt(expiry, 10)),
		UserID:    u.ID,
		Venue:     "kuru",
		OrderType: op,
		Size:      "0",
		ChainID:   s.ChainID,
		State:     "pending",
	}
	req := executor.SignerRequest{
		IntentKey:     in.Key,
		PrivyWalletID: u.PrivyWalletID,
		UserWallet:    u.WalletAddress,
		Expiry:        expiry,
		ChainID:       in.ChainID,
	}
	resp, execErr := s.perform(r.Context(), in, func(ctx context.Context) (executor.RouteResponse, error) {
		if op == "revoke" {
			return s.Executor.Revoke(ctx, req)
		}
		return s.Executor.Authorize(ctx, req)
	})

	// the epoch moved, so read the permission bit back rather than infer it
	after, rerr := s.refresh(r.Context(), u)
	if rerr != nil {
		s.log().Warn("refresh kuru account after "+op, "user_id", u.ID, "err", rerr)
		after = before
	}
	if execErr == nil {
		after.SignerExpiry = expiry
		if err := s.Store.SetSignerExpiry(r.Context(), u.ID, expiry); err != nil {
			s.log().Error("store signer expiry", "user_id", u.ID, "err", err)
		}
	}
	out := signerView{SignerAuthorized: after.SignerAuthorized, SignerExpiry: view(after).SignerExpiry,
		AuthNonce: orZero(after.AuthNonce), TxHash: resp.TxHash}
	if execErr != nil {
		httpx.JSON(w, http.StatusBadGateway, httpx.Envelope{Data: out,
			Error: &httpx.Err{Code: "execution_failed", Message: resp.Error}})
		return
	}
	httpx.OK(w, out)
}

const unknownAccount = "This service could not read your Kuru trading account from the executor, so it cannot prove the account may trade and nothing was signed."

// account returns the cached Kuru state, refreshing it only when it is missing
// or stale. An unreachable executor falls back to the stored row: a read must
// not go dark because the signing service blinked.
func (s *Server) account(ctx context.Context, u store.User) (store.KuruAccount, error) {
	ka, err := s.Store.KuruAccount(ctx, u.ID)
	switch {
	case err == nil && time.Since(ka.UpdatedAt) < accountFresh:
		return ka, nil
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return ka, err
	}
	fresh, ferr := s.refresh(ctx, u)
	if ferr == nil {
		return fresh, nil
	}
	if err == nil {
		s.log().Warn("kuru account refresh failed, serving the stored row", "user_id", u.ID, "err", ferr)
		return ka, nil
	}
	return store.KuruAccount{}, ferr
}

func (s *Server) refresh(ctx context.Context, u store.User) (store.KuruAccount, error) {
	acct, err := s.Executor.EnsureAccount(ctx, executor.AccountRequest{
		PrivyWalletID: u.PrivyWalletID, UserWallet: u.WalletAddress, ChainID: s.ChainID})
	if err != nil {
		return store.KuruAccount{}, err
	}
	ka := store.KuruAccount{
		UserID:           u.ID,
		ChainID:          acct.ChainID,
		KuruUserID:       acct.UserID,
		SignerAuthorized: acct.SignerAuthorized,
		AuthNonce:        orZero(acct.AuthNonce),
		AccountCore:      acct.AccountCore,
		UpdatedAt:        time.Now(),
	}
	if err := s.Store.PutKuruAccount(ctx, ka); err != nil {
		return store.KuruAccount{}, err
	}
	// signer_expiry survives the upsert; read it back rather than report a zero
	stored, err := s.Store.KuruAccount(ctx, u.ID)
	if err != nil {
		return ka, nil
	}
	return stored, nil
}

// kuruReady is the precondition chain every trading call clears before anything
// is signed. Each failure names what is missing rather than what went wrong.
func (s *Server) kuruReady(ctx context.Context, u store.User) *policy.Rejection {
	ka, err := s.account(ctx, u)
	if err != nil {
		s.log().Error("read kuru account", "user_id", u.ID, "err", err)
		return &policy.Rejection{Code: "kuru_account_unknown", Reason: unknownAccount}
	}
	if ka.KuruUserID == 0 {
		return &policy.Rejection{Code: "kuru_account_unfunded",
			Reason: "Your Kuru trading account has no deposit yet, so orders cannot be placed. Fund it first with POST /v1/account/deposit."}
	}
	if !ka.SignerAuthorized {
		return &policy.Rejection{Code: "kuru_signer_unauthorized",
			Reason: "This terminal's signer holds no TRADE delegation on your Kuru account, so orders cannot be placed. Grant it with POST /v1/account/authorize."}
	}
	return nil
}

func orZero(v string) string {
	if strings.TrimSpace(v) == "" {
		return "0"
	}
	return v
}
