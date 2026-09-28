package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/httpx"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/privyauth"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/store"
)

const maxBody = 1 << 20

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		httpx.Fail(w, http.StatusBadRequest, "bad_request", "The request body could not be read as JSON: "+err.Error()+".")
		return false
	}
	return true
}

func limitParam(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	return n
}

func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	httpx.OK(w, u)
}

type meRequest struct {
	WalletAddress string `json:"wallet_address"`
	PrivyWalletID string `json:"privy_wallet_id"`
}

func (s *Server) upsertMe(w http.ResponseWriter, r *http.Request) {
	did, ok := privyauth.DIDFromContext(r.Context())
	if !ok {
		httpx.Fail(w, http.StatusUnauthorized, "unauthorized", "This request carried no verified Privy identity, so it was refused.")
		return
	}
	var req meRequest
	if !decode(w, r, &req) {
		return
	}
	req.WalletAddress = strings.TrimSpace(req.WalletAddress)
	if !isAddress(req.WalletAddress) {
		httpx.Fail(w, http.StatusBadRequest, "bad_request",
			"wallet_address must be a 0x-prefixed 20 byte address, because it is the account every order is signed for.")
		return
	}
	u, err := s.Store.UpsertUser(r.Context(), did, req.WalletAddress, strings.TrimSpace(req.PrivyWalletID))
	if errors.Is(err, store.ErrWalletTaken) {
		httpx.Fail(w, http.StatusConflict, "wallet_taken",
			"This wallet is already registered to another sign-in, so it cannot be claimed by this one.")
		return
	}
	if err != nil {
		s.fail(w, "upsert user", err)
		return
	}
	httpx.OK(w, u)
}

func (s *Server) listExecutions(w http.ResponseWriter, r *http.Request) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	rows, err := s.Store.Executions(r.Context(), u.ID, limitParam(r))
	if err != nil {
		s.fail(w, "list executions", err)
		return
	}
	httpx.OK(w, map[string]any{"executions": rows})
}

func isAddress(s string) bool {
	if len(s) != 42 || !strings.HasPrefix(s, "0x") {
		return false
	}
	for _, c := range s[2:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}
