package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/chains"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/policy"
)

// Privy allows one policy per wallet, so every rule lives in this one object.
type Policy struct {
	ID        string `json:"id,omitempty"`
	Version   string `json:"version"`
	Name      string `json:"name"`
	ChainType string `json:"chain_type"`
	Rules     []Rule `json:"rules"`
	OwnerID   string `json:"owner_id,omitempty"`
}

type Rule struct {
	Name       string      `json:"name"`
	Method     string      `json:"method"`
	Conditions []Condition `json:"conditions"`
	Action     string      `json:"action"`
}

type Condition struct {
	FieldSource string          `json:"field_source"`
	Field       string          `json:"field"`
	ABI         json.RawMessage `json:"abi,omitempty"`
	Operator    string          `json:"operator"`
	Value       any             `json:"value"`
}

var methods = []string{"eth_sendTransaction", "eth_signTransaction"}

var methodTag = map[string]string{"eth_sendTransaction": "send", "eth_signTransaction": "sign"}

const zeroAddress = "0x0000000000000000000000000000000000000000"

// approve is the only calldata shape this policy reads. Privy fails open when
// the ABI does not match the calldata, which is exactly why the real guard is
// internal/policy and this document is only a backstop.
var approveABI = json.RawMessage(`[{"name":"approve","type":"function","inputs":[{"name":"spender","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[],"stateMutability":"nonpayable"}]`)

// Build derives the policy from the same market allowlist the API validates
// delegations against, so the two guards cannot name different markets.
// spot v2 has no trading router. orders and swaps go to each market's own
// OrderBook, funds live in AccountCore, and AccountCore is the approve spender.
func Build(name, ownerID, accountCore string, markets []policy.Market) (Policy, error) {
	if len(markets) == 0 {
		return Policy{}, fmt.Errorf("no markets: refusing to build a policy that allows nothing")
	}
	accountCore = strings.ToLower(strings.TrimSpace(accountCore))
	if len(accountCore) != 42 || !strings.HasPrefix(accountCore, "0x") {
		return Policy{}, fmt.Errorf("account core %q is not a 20 byte address", accountCore)
	}

	tokens := &set{}
	books := &set{}
	for _, m := range markets {
		if !chains.IsSupported(int(m.ChainID)) {
			return Policy{}, fmt.Errorf("market %s is on chain %d, which this terminal does not trade on", m.ID, m.ChainID)
		}
		a := strings.ToLower(strings.TrimSpace(m.MarketAddress))
		if len(a) != 42 || !strings.HasPrefix(a, "0x") {
			return Policy{}, fmt.Errorf("market %s has no usable market_address, so its orderbook cannot be allowlisted", m.ID)
		}
		books.add(a)
		// native MON is spent as value, never approved
		if !strings.EqualFold(m.BaseToken, zeroAddress) {
			tokens.add(m.BaseToken)
		}
		tokens.add(m.QuoteToken)
	}
	if tokens.empty() {
		return Policy{}, fmt.Errorf("no approvable tokens across %d markets, so the executor could never fund a swap", len(markets))
	}

	// a rule with no conditions is rejected as invalid_policy_format, so every
	// rule below carries this one at least
	chainCond := Condition{FieldSource: "ethereum_transaction", Field: "chain_id", Operator: "in", Value: supportedChainIDs()}

	var rules []Rule
	add := func(name string, conds ...Condition) {
		for _, m := range methods {
			rules = append(rules, Rule{
				Name:       name + " (" + methodTag[m] + ")",
				Method:     m,
				Action:     "ALLOW",
				Conditions: conds,
			})
		}
	}

	add("Kuru orderbook batch and swap",
		chainCond,
		Condition{FieldSource: "ethereum_transaction", Field: "to", Operator: "in", Value: books.list()},
	)
	add("Kuru account core deposit and signer authorization",
		chainCond,
		Condition{FieldSource: "ethereum_transaction", Field: "to", Operator: "in", Value: []string{accountCore}},
	)
	add("ERC-20 approve to AccountCore",
		chainCond,
		Condition{FieldSource: "ethereum_transaction", Field: "to", Operator: "in", Value: tokens.list()},
		Condition{FieldSource: "ethereum_calldata", Field: "function_name", ABI: approveABI, Operator: "eq", Value: "approve"},
		Condition{FieldSource: "ethereum_calldata", Field: "approve.spender", ABI: approveABI, Operator: "in", Value: []string{accountCore}},
	)

	return Policy{Version: "1.0", Name: name, ChainType: "ethereum", Rules: rules, OwnerID: ownerID}, nil
}

func supportedChainIDs() []string {
	ids := chains.Supported()
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = strconv.Itoa(id)
	}
	sort.Strings(out)
	return out
}

// Diff names every way the live policy differs from the spec.
func Diff(live, want Policy) []string {
	var out []string
	if live.Name != want.Name {
		out = append(out, fmt.Sprintf("name is %q on Privy but %q in the spec", live.Name, want.Name))
	}
	if live.ChainType != want.ChainType {
		out = append(out, fmt.Sprintf("chain_type is %q on Privy but %q in the spec", live.ChainType, want.ChainType))
	}
	byName := map[string]Rule{}
	for _, r := range live.Rules {
		byName[r.Name+" "+r.Method] = r
	}
	for _, r := range want.Rules {
		key := r.Name + " " + r.Method
		got, ok := byName[key]
		if !ok {
			out = append(out, fmt.Sprintf("rule %q is missing from the live policy", key))
			continue
		}
		delete(byName, key)
		if !sameJSON(got, r) {
			out = append(out, fmt.Sprintf("rule %q on Privy does not match the spec", key))
		}
	}
	for key := range byName {
		out = append(out, fmt.Sprintf("rule %q exists on Privy but not in the spec", key))
	}
	sort.Strings(out)
	return out
}

func sameJSON(a, b any) bool {
	x, err := json.Marshal(a)
	if err != nil {
		return false
	}
	y, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(x) == string(y)
}

type set struct{ m map[string]struct{} }

func (s *set) add(v string) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return
	}
	if s.m == nil {
		s.m = map[string]struct{}{}
	}
	s.m[v] = struct{}{}
}

func (s *set) empty() bool { return len(s.m) == 0 }

func (s *set) list() []string {
	out := make([]string, 0, len(s.m))
	for v := range s.m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
