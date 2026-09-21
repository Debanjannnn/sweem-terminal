package main

import (
	"encoding/json"
	"strings"
	"testing"

	walletpolicy "github.com/sweem-terminal/sweem-terminal/services/wallet/internal/policy"
)

const router = "0x7EFbE105Ca7415dE98F96622173458ac1c054630"

// fixture mirrors executor/kuru-markets.json: one native-base market and one
// ERC-20 base market, deliberately mixed case so normalization shows.
func fixture() []walletpolicy.Market {
	return []walletpolicy.Market{
		{ID: "MON-USDC", ChainID: 10143,
			MarketAddress: "0xfdbe356828c8f5a5d5ed4f69dde0816f4058ef61",
			BaseToken:     "0x0000000000000000000000000000000000000000",
			QuoteToken:    "0xEE0722EAd54f1b4FE97bE399bE43bC0226a6F97E"},
		{ID: "CBBTC-USDC", ChainID: 10143,
			MarketAddress: "0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f",
			BaseToken:     "0xEF2a20a161AC9ed1117d721336226b6399F15B4d",
			QuoteToken:    "0xee0722ead54f1b4fe97be399be43bc0226a6f97e"},
	}
}

func build(t *testing.T) Policy {
	t.Helper()
	p, err := Build("test policy", "quorum-1", router, fixture())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	return p
}

// The golden document is the contract with Privy. Changing it means every
// delegated wallet's guard changes, so it is reviewed, not regenerated.
func TestBuildGolden(t *testing.T) {
	doc, err := json.MarshalIndent(build(t), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if string(doc) != golden {
		t.Fatalf("policy document changed.\n--- got ---\n%s\n--- want ---\n%s", doc, golden)
	}
}

// A rule with no conditions is rejected by Privy as invalid_policy_format.
func TestEveryRuleHasAtLeastOneCondition(t *testing.T) {
	p := build(t)
	if len(p.Rules) == 0 {
		t.Fatal("the policy has no rules")
	}
	for _, r := range p.Rules {
		if len(r.Conditions) == 0 {
			t.Errorf("rule %q has no conditions, so Privy would reject the whole policy", r.Name)
		}
		if r.Method == "" || r.Action == "" {
			t.Errorf("rule %q is missing a method or an action", r.Name)
		}
	}
}

func TestChainConditionCoversEverySupportedChain(t *testing.T) {
	for _, r := range build(t).Rules {
		for _, c := range r.Conditions {
			if c.Field != "chain_id" {
				continue
			}
			got, ok := c.Value.([]string)
			if !ok {
				t.Fatalf("chain_id value is %#v, want a list of strings", c.Value)
			}
			if strings.Join(got, ",") != "10143,143" {
				t.Fatalf("chain_id values = %v, want the supported chains as strings", got)
			}
		}
	}
}

func TestBuildRefusals(t *testing.T) {
	tests := []struct {
		name    string
		markets []walletpolicy.Market
		router  string
		says    string
	}{
		{name: "no markets", router: router, says: "allows nothing"},
		{name: "bad router", markets: fixture(), router: "0xdead", says: "20 byte address"},
		{
			name:    "market on an unsupported chain",
			markets: []walletpolicy.Market{{ID: "X", ChainID: 1, BaseToken: router, QuoteToken: router}},
			router:  router,
			says:    "does not trade on",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Build("n", "", tt.router, tt.markets)
			if err == nil {
				t.Fatal("Build accepted an unusable input")
			}
			if !strings.Contains(err.Error(), tt.says) {
				t.Fatalf("error %q does not say %q", err, tt.says)
			}
		})
	}
}

func TestDiff(t *testing.T) {
	spec := build(t)

	tests := []struct {
		name string
		live func(Policy) Policy
		says string
	}{
		{name: "identical", live: func(p Policy) Policy { return p }},
		{
			name: "renamed",
			live: func(p Policy) Policy { p.Name = "other"; return p },
			says: "name is",
		},
		{
			name: "a rule was dropped on Privy",
			live: func(p Policy) Policy { p.Rules = p.Rules[1:]; return p },
			says: "is missing from the live policy",
		},
		{
			name: "a rule was widened on Privy",
			live: func(p Policy) Policy {
				rules := append([]Rule(nil), p.Rules...)
				widened := rules[0]
				widened.Conditions = append([]Condition(nil), widened.Conditions...)
				widened.Conditions[1] = Condition{FieldSource: "ethereum_transaction", Field: "to", Operator: "in",
					Value: []string{"0x000000000000000000000000000000000000dead"}}
				rules[0] = widened
				p.Rules = rules
				return p
			},
			says: "does not match the spec",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Diff(tt.live(spec), spec)
			if tt.says == "" {
				if len(got) != 0 {
					t.Fatalf("identical policies reported drift: %v", got)
				}
				return
			}
			if len(got) == 0 {
				t.Fatal("drift went unreported")
			}
			if !strings.Contains(strings.Join(got, "\n"), tt.says) {
				t.Fatalf("drift %v does not say %q", got, tt.says)
			}
		})
	}
}

// Privy canonicalizes an empty body to "", not {}. Signing the wrong one makes
// every GET fail authorization.
func TestSignedPayloadEmptyBody(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "nil body", body: "", want: `{"body":"","headers":{"privy-app-id":"app1"},"method":"GET","url":"https://api.privy.io/v1/policies/p1","version":1}`},
		{name: "empty object", body: "{}", want: `{"body":"","headers":{"privy-app-id":"app1"},"method":"GET","url":"https://api.privy.io/v1/policies/p1","version":1}`},
		{name: "real body", body: `{"name":"x"}`, want: `{"body":{"name":"x"},"headers":{"privy-app-id":"app1"},"method":"GET","url":"https://api.privy.io/v1/policies/p1","version":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := canonical(signedPayload("app1", "GET", "https://api.privy.io/v1/policies/p1/", []byte(tt.body)))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Fatalf("canonical =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

const golden = `{
  "version": "1.0",
  "name": "test policy",
  "chain_type": "ethereum",
  "rules": [
    {
      "name": "Kuru router swap (send)",
      "method": "eth_sendTransaction",
      "conditions": [
        {
          "field_source": "ethereum_transaction",
          "field": "chain_id",
          "operator": "in",
          "value": [
            "10143",
            "143"
          ]
        },
        {
          "field_source": "ethereum_transaction",
          "field": "to",
          "operator": "in",
          "value": [
            "0x7efbe105ca7415de98f96622173458ac1c054630"
          ]
        }
      ],
      "action": "ALLOW"
    },
    {
      "name": "Kuru router swap (sign)",
      "method": "eth_signTransaction",
      "conditions": [
        {
          "field_source": "ethereum_transaction",
          "field": "chain_id",
          "operator": "in",
          "value": [
            "10143",
            "143"
          ]
        },
        {
          "field_source": "ethereum_transaction",
          "field": "to",
          "operator": "in",
          "value": [
            "0x7efbe105ca7415de98f96622173458ac1c054630"
          ]
        }
      ],
      "action": "ALLOW"
    },
    {
      "name": "ERC-20 approve to the Kuru router (send)",
      "method": "eth_sendTransaction",
      "conditions": [
        {
          "field_source": "ethereum_transaction",
          "field": "chain_id",
          "operator": "in",
          "value": [
            "10143",
            "143"
          ]
        },
        {
          "field_source": "ethereum_transaction",
          "field": "to",
          "operator": "in",
          "value": [
            "0xee0722ead54f1b4fe97be399be43bc0226a6f97e",
            "0xef2a20a161ac9ed1117d721336226b6399f15b4d"
          ]
        },
        {
          "field_source": "ethereum_calldata",
          "field": "function_name",
          "abi": [
            {
              "name": "approve",
              "type": "function",
              "inputs": [
                {
                  "name": "spender",
                  "type": "address"
                },
                {
                  "name": "amount",
                  "type": "uint256"
                }
              ],
              "outputs": [],
              "stateMutability": "nonpayable"
            }
          ],
          "operator": "eq",
          "value": "approve"
        },
        {
          "field_source": "ethereum_calldata",
          "field": "approve.spender",
          "abi": [
            {
              "name": "approve",
              "type": "function",
              "inputs": [
                {
                  "name": "spender",
                  "type": "address"
                },
                {
                  "name": "amount",
                  "type": "uint256"
                }
              ],
              "outputs": [],
              "stateMutability": "nonpayable"
            }
          ],
          "operator": "in",
          "value": [
            "0x7efbe105ca7415de98f96622173458ac1c054630"
          ]
        }
      ],
      "action": "ALLOW"
    },
    {
      "name": "ERC-20 approve to the Kuru router (sign)",
      "method": "eth_signTransaction",
      "conditions": [
        {
          "field_source": "ethereum_transaction",
          "field": "chain_id",
          "operator": "in",
          "value": [
            "10143",
            "143"
          ]
        },
        {
          "field_source": "ethereum_transaction",
          "field": "to",
          "operator": "in",
          "value": [
            "0xee0722ead54f1b4fe97be399be43bc0226a6f97e",
            "0xef2a20a161ac9ed1117d721336226b6399f15b4d"
          ]
        },
        {
          "field_source": "ethereum_calldata",
          "field": "function_name",
          "abi": [
            {
              "name": "approve",
              "type": "function",
              "inputs": [
                {
                  "name": "spender",
                  "type": "address"
                },
                {
                  "name": "amount",
                  "type": "uint256"
                }
              ],
              "outputs": [],
              "stateMutability": "nonpayable"
            }
          ],
          "operator": "eq",
          "value": "approve"
        },
        {
          "field_source": "ethereum_calldata",
          "field": "approve.spender",
          "abi": [
            {
              "name": "approve",
              "type": "function",
              "inputs": [
                {
                  "name": "spender",
                  "type": "address"
                },
                {
                  "name": "amount",
                  "type": "uint256"
                }
              ],
              "outputs": [],
              "stateMutability": "nonpayable"
            }
          ],
          "operator": "in",
          "value": [
            "0x7efbe105ca7415de98f96622173458ac1c054630"
          ]
        }
      ],
      "action": "ALLOW"
    }
  ],
  "owner_id": "quorum-1"
}`
