package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/httpx"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/privyauth"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/policy"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/store"
)

type delegationRequest struct {
	SignerID            string    `json:"signer_id"`
	PolicyID            string    `json:"policy_id"`
	Venues              []string  `json:"venues"`
	Markets             []string  `json:"markets"`
	MaxNotionalPerSlice string    `json:"max_notional_per_slice"`
	MaxNotionalTotal    string    `json:"max_notional_total"`
	ExpiresAt           time.Time `json:"expires_at"`
}

type delegationView struct {
	policy.Delegation
	Live bool `json:"live"`
}

// getDelegation also answers the scheduler, which passes ?user= alongside its
// internal secret because it holds no token for that user.
func (s *Server) getDelegation(w http.ResponseWriter, r *http.Request) {
	did, ok := privyauth.DIDFromContext(r.Context())
	if !ok {
		httpx.Fail(w, http.StatusUnauthorized, "unauthorized", "This request carried no verified Privy identity, so it was refused.")
		return
	}
	// the middleware already validated the secret, so a header here is trusted
	if want := strings.TrimSpace(r.URL.Query().Get("user")); want != "" {
		if r.Header.Get(privyauth.HeaderInternalSecret) == "" {
			httpx.Fail(w, http.StatusForbidden, "forbidden",
				"Only an internal caller may read another user's delegation, so the user parameter was refused.")
			return
		}
		did = want
	}

	d, err := s.Store.Delegation(r.Context(), did)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Fail(w, http.StatusNotFound, "no_delegation",
			"This account has not delegated a signer, so there is no delegation to read.")
		return
	}
	if err != nil {
		s.fail(w, "load delegation", err)
		return
	}
	httpx.OK(w, delegationView{Delegation: d, Live: policy.Live(d, time.Now())})
}

func (s *Server) putDelegation(w http.ResponseWriter, r *http.Request) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	var req delegationRequest
	if !decode(w, r, &req) {
		return
	}
	d, reason := s.validateDelegation(r.Context(), u.ID, req)
	if reason != "" {
		httpx.Fail(w, http.StatusBadRequest, "bad_request", reason)
		return
	}
	saved, err := s.Store.PutDelegation(r.Context(), d)
	if err != nil {
		s.fail(w, "put delegation", err)
		return
	}
	httpx.OK(w, delegationView{Delegation: saved, Live: policy.Live(saved, time.Now())})
}

func (s *Server) deleteDelegation(w http.ResponseWriter, r *http.Request) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	err := s.Store.RevokeDelegation(r.Context(), u.ID)
	if errors.Is(err, store.ErrNotFound) {
		httpx.Fail(w, http.StatusNotFound, "no_delegation",
			"This account has no live delegation, so there was nothing to revoke.")
		return
	}
	if err != nil {
		s.fail(w, "revoke delegation", err)
		return
	}
	httpx.OK(w, map[string]any{"revoked": true})
}

// validateDelegation returns the row to store, or the sentence to refuse with.
func (s *Server) validateDelegation(ctx context.Context, userID string, req delegationRequest) (policy.Delegation, string) {
	if strings.TrimSpace(req.SignerID) == "" {
		return policy.Delegation{}, "signer_id is required: without the Privy signer this delegation names, nothing can be executed under it."
	}
	if len(req.Venues) == 0 || len(req.Markets) == 0 {
		return policy.Delegation{}, "venues and markets must each list at least one entry, because an empty allowlist would authorize nothing."
	}
	for _, v := range req.Venues {
		if !policy.KnownVenue(v) {
			return policy.Delegation{}, fmt.Sprintf("Venue %q is not one this terminal can route to (%s), so it cannot be delegated.",
				v, strings.Join(policy.Venues, ", "))
		}
	}
	for _, m := range req.Markets {
		if !s.knownMarket(ctx, m) {
			return policy.Delegation{}, fmt.Sprintf("Market %q is not on the executor's allowlist, so it cannot be delegated.", m)
		}
	}
	perSlice, err := policy.ParseAmount(req.MaxNotionalPerSlice)
	if err != nil {
		return policy.Delegation{}, "max_notional_per_slice must be an integer amount in base units: " + err.Error() + "."
	}
	total, err := policy.ParseAmount(req.MaxNotionalTotal)
	if err != nil {
		return policy.Delegation{}, "max_notional_total must be an integer amount in base units: " + err.Error() + "."
	}
	if perSlice.Cmp(total) > 0 {
		return policy.Delegation{}, "max_notional_per_slice is larger than max_notional_total, which would let one slice spend more than the whole delegation."
	}
	if !req.ExpiresAt.After(time.Now()) {
		return policy.Delegation{}, "expires_at must be in the future, because a delegation that is already expired authorizes nothing."
	}
	return policy.Delegation{
		UserID:              userID,
		SignerID:            strings.TrimSpace(req.SignerID),
		PolicyID:            strings.TrimSpace(req.PolicyID),
		Venues:              req.Venues,
		Markets:             req.Markets,
		MaxNotionalPerSlice: perSlice.String(),
		MaxNotionalTotal:    total.String(),
		ExpiresAt:           req.ExpiresAt,
	}, ""
}
