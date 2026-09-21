// Package chain reads contract state over JSON-RPC at an explicit block tag.
// Every read states which Monad tier it wants; nothing defaults to `latest`.
package chain

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/sha3"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/finality"
)

type Reader struct {
	url  string
	http *http.Client
	id   atomic.Uint64
}

func New(rpcURL string) *Reader {
	return &Reader{url: rpcURL, http: &http.Client{Timeout: 10 * time.Second}}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (r *Reader) rpc(ctx context.Context, method string, params ...any) (json.RawMessage, error) {
	body, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": r.id.Add(1), "method": method, "params": params,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("monad rpc at %s is unreachable: %w", r.url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("monad rpc at %s returned HTTP %d for %s", r.url, resp.StatusCode, method)
	}
	var env struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("monad rpc response to %s was not valid JSON: %w", method, err)
	}
	if env.Error != nil {
		return nil, fmt.Errorf("monad rpc rejected %s: %s (code %d)", method, env.Error.Message, env.Error.Code)
	}
	return env.Result, nil
}

func isNull(raw json.RawMessage) bool {
	return len(raw) == 0 || string(raw) == "null"
}

// BlockNumber resolves a tier to a height. A node without the safe/finalized
// tags falls back to head minus FinalityDepth rather than pretending latest is settled.
func (r *Reader) BlockNumber(ctx context.Context, t finality.Tier) (uint64, error) {
	n, err := r.blockAt(ctx, t.BlockTag())
	if err == nil && n > 0 {
		return n, nil
	}
	if t == finality.Seen {
		return n, err
	}
	head, headErr := r.blockAt(ctx, "latest")
	if headErr != nil {
		return 0, headErr
	}
	if head < finality.FinalityDepth {
		return 0, nil
	}
	return head - finality.FinalityDepth, nil
}

func (r *Reader) blockAt(ctx context.Context, tag string) (uint64, error) {
	raw, err := r.rpc(ctx, "eth_getBlockByNumber", tag, false)
	if err != nil {
		return 0, err
	}
	if isNull(raw) {
		return 0, fmt.Errorf("monad rpc has no block for tag %q", tag)
	}
	var b struct {
		Number string `json:"number"`
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return 0, err
	}
	return hexUint(b.Number)
}

type Receipt struct {
	BlockNumber uint64
	Success     bool
}

// A nil receipt with no error means the tx is mempool-only: Monad returns null
// for it rather than a pending object. That is not a failure.
func (r *Reader) Receipt(ctx context.Context, txHash string) (*Receipt, error) {
	raw, err := r.rpc(ctx, "eth_getTransactionReceipt", txHash)
	if err != nil || isNull(raw) {
		return nil, err
	}
	var rc struct {
		BlockNumber string `json:"blockNumber"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal(raw, &rc); err != nil {
		return nil, err
	}
	bn, err := hexUint(rc.BlockNumber)
	if err != nil {
		return nil, err
	}
	st, _ := hexUint(rc.Status)
	return &Receipt{BlockNumber: bn, Success: st == 1}, nil
}

func (r *Reader) call(ctx context.Context, to string, data []byte, t finality.Tier) ([]byte, error) {
	raw, err := r.rpc(ctx, "eth_call", map[string]string{"to": to, "data": "0x" + hex.EncodeToString(data)}, t.BlockTag())
	if err != nil {
		return nil, err
	}
	if isNull(raw) {
		return nil, fmt.Errorf("eth_call to %s at tag %s returned no data", to, t.BlockTag())
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}

type Position struct {
	Size            *big.Int // signed
	Collateral      *big.Int
	EntryIndex      *big.Int
	EntryNotional   *big.Int
	EntryBorrowCum  *big.Int
	EntryFundingCum *big.Int
}

func (r *Reader) Position(ctx context.Context, engine string, marketID [32]byte, owner string, t finality.Tier) (Position, error) {
	out, err := r.call(ctx, engine, encode("getPosition(bytes32,address)", marketID[:], addrWord(owner)), t)
	if err != nil {
		return Position{}, err
	}
	w, err := words(out, 6)
	if err != nil {
		return Position{}, fmt.Errorf("getPosition: %w", err)
	}
	return Position{
		Size: toInt(w[0]), Collateral: toUint(w[1]), EntryIndex: toUint(w[2]),
		EntryNotional: toUint(w[3]), EntryBorrowCum: toUint(w[4]), EntryFundingCum: toInt(w[5]),
	}, nil
}

type MarketParams struct {
	IndexOracle          string
	MaxOiPerSide         *big.Int
	MaintenanceMarginBps *big.Int
	Enabled              bool
}

func (r *Reader) MarketParams(ctx context.Context, registry string, marketID [32]byte, t finality.Tier) (MarketParams, error) {
	out, err := r.call(ctx, registry, encode("params(bytes32)", marketID[:]), t)
	if err != nil {
		return MarketParams{}, err
	}
	w, err := words(out, 14)
	if err != nil {
		return MarketParams{}, fmt.Errorf("params: %w", err)
	}
	return MarketParams{
		IndexOracle: toAddr(w[0]), MaxOiPerSide: toUint(w[3]),
		MaintenanceMarginBps: toUint(w[6]), Enabled: toUint(w[13]).Sign() == 1,
	}, nil
}

type MarketState struct {
	LongOI  *big.Int
	ShortOI *big.Int
	Halted  bool
}

func (r *Reader) MarketState(ctx context.Context, engine string, marketID [32]byte, t finality.Tier) (MarketState, error) {
	out, err := r.call(ctx, engine, encode("marketState(bytes32)", marketID[:]), t)
	if err != nil {
		return MarketState{}, err
	}
	w, err := words(out, 6)
	if err != nil {
		return MarketState{}, fmt.Errorf("marketState: %w", err)
	}
	return MarketState{LongOI: toUint(w[2]), ShortOI: toUint(w[3]), Halted: toUint(w[5]).Sign() == 1}, nil
}

type Oracle struct {
	Value     *big.Int
	UpdatedAt int64 // unix seconds
}

func (r *Reader) Oracle(ctx context.Context, oracle string, t finality.Tier) (Oracle, error) {
	v, err := r.uint256(ctx, oracle, "value()", t)
	if err != nil {
		return Oracle{}, err
	}
	u, err := r.uint256(ctx, oracle, "updatedAt()", t)
	if err != nil {
		return Oracle{}, err
	}
	return Oracle{Value: v, UpdatedAt: u.Int64()}, nil
}

func (r *Reader) TotalAssets(ctx context.Context, vault string, t finality.Tier) (*big.Int, error) {
	return r.uint256(ctx, vault, "totalAssets()", t)
}

func (r *Reader) Utilization(ctx context.Context, vault string, t finality.Tier) (util, max *big.Int, err error) {
	if util, err = r.uint256(ctx, vault, "utilization()", t); err != nil {
		return nil, nil, err
	}
	max, err = r.uint256(ctx, vault, "maxUtilization()", t)
	return util, max, err
}

func (r *Reader) uint256(ctx context.Context, to, sig string, t finality.Tier) (*big.Int, error) {
	out, err := r.call(ctx, to, encode(sig), t)
	if err != nil {
		return nil, err
	}
	w, err := words(out, 1)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", sig, err)
	}
	return toUint(w[0]), nil
}

func encode(sig string, args ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(sig))
	out := h.Sum(nil)[:4]
	for _, a := range args {
		out = append(out, a...)
	}
	return out
}

func addrWord(addr string) []byte {
	b, _ := hex.DecodeString(strings.TrimPrefix(strings.ToLower(addr), "0x"))
	w := make([]byte, 32)
	if len(b) > 32 {
		b = b[len(b)-32:]
	}
	copy(w[32-len(b):], b)
	return w
}

// MarketKey parses a 0x-prefixed 32 byte market id.
func MarketKey(s string) ([32]byte, error) {
	var out [32]byte
	b, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "0x"))
	if err != nil || len(b) != 32 {
		return out, fmt.Errorf("market id %q is not a 0x-prefixed 32 byte key", s)
	}
	copy(out[:], b)
	return out, nil
}

func words(data []byte, want int) ([][]byte, error) {
	if len(data) < want*32 {
		return nil, fmt.Errorf("expected %d abi words but the call returned %d bytes", want, len(data))
	}
	out := make([][]byte, want)
	for i := range out {
		out[i] = data[i*32 : (i+1)*32]
	}
	return out, nil
}

func toUint(w []byte) *big.Int { return new(big.Int).SetBytes(w) }

// two's complement
func toInt(w []byte) *big.Int {
	v := new(big.Int).SetBytes(w)
	if w[0]&0x80 != 0 {
		v.Sub(v, new(big.Int).Lsh(big.NewInt(1), 256))
	}
	return v
}

func toAddr(w []byte) string { return "0x" + hex.EncodeToString(w[12:]) }

func hexUint(s string) (uint64, error) {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return 0, nil
	}
	return strconv.ParseUint(s, 16, 64)
}
