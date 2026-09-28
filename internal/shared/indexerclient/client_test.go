package indexerclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type capture struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

// serve returns a client pointed at a server replying with body, plus the last request seen.
func serve(t *testing.T, status int, body string) (*Client, *capture) {
	t.Helper()
	got := &capture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, got); err != nil {
			t.Errorf("request body was not a graphql envelope: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("INDEXER_URL", srv.URL)
	return New(), got
}

// EIP-55 checksum of 0x0e5c…c08f, see TestChecksum
const ownerSum = "0x0E5cC5c5c14e89a4daFf87ea314C25EF37C8C08f"

func TestTypedQueries(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		call     func(c *Client) (int, error)
		wantVars map[string]any
		wantRows int
	}{
		{
			// shapes below are what hasura actually returns: BigInt as a string,
			// relations as <name>_id, addresses checksummed as envio stored them
			name: "positions checksum the owner and split the signed size",
			body: `{"data":{"Position":[{"id":"0xkey-` + ownerSum + `","owner":"` + ownerSum + `","marketId":"0xkey","size":"-1000000000000000000","entryPrice":"2500000000000000000000","collateral":"500000000","blockNumber":"42","updatedAt":"1700000000"}]}}`,
			call: func(c *Client) (int, error) {
				p, err := c.Positions(context.Background(), strings.ToLower(ownerSum))
				if err == nil && (p[0].Size != "1000000000000000000" || p[0].IsLong || p[0].Block != 42 || p[0].UpdatedAt != 1700000000) {
					t.Fatalf("decoded badly: %+v", p[0])
				}
				return len(p), err
			},
			wantVars: map[string]any{"owner": ownerSum},
			wantRows: 1,
		},
		{
			name: "position events filter by the market key from a block",
			body: `{"data":{"PositionEvent":[{"id":"100-3","marketId":"0xkey","owner":"` + ownerSum + `","kind":"OPEN","size":"5","price":"2","fee":"0","blockNumber":"100","txHash":"0xt","timestamp":"1700000000"}]}}`,
			call: func(c *Client) (int, error) {
				e, err := c.PositionEvents(context.Background(), "0xKEY", 100)
				if err == nil && (!e[0].IsLong || e[0].Block != 100) {
					t.Fatalf("decoded badly: %+v", e[0])
				}
				return len(e), err
			},
			wantVars: map[string]any{"marketId": "0xkey", "fromBlock": float64(100)},
			wantRows: 1,
		},
		{
			name: "funding rate points",
			body: `{"data":{"FundingRatePoint":[{"marketId":"0xkey","fundingRate":"-125000000000000","timestamp":"1700000000"},{"marketId":"0xkey","fundingRate":"3","timestamp":"1700003600"}]}}`,
			call: func(c *Client) (int, error) {
				r, err := c.FundingRatePoints(context.Background(), "0xkey", 1, 2)
				if err == nil && (r[0].Rate != "-125000000000000" || r[1].Ts != 1700003600) {
					t.Fatalf("decoded badly: %+v", r)
				}
				return len(r), err
			},
			wantVars: map[string]any{"marketId": "0xkey", "fromTs": float64(1), "toTs": float64(2)},
			wantRows: 2,
		},
		{
			name: "vault flows",
			body: `{"data":{"VaultFlow":[{"id":"1-0","owner":"` + ownerSum + `","kind":"DEPOSIT","assets":"1000000","shares":"999999","blockNumber":"1","txHash":"0xt","timestamp":"1"}]}}`,
			call: func(c *Client) (int, error) {
				f, err := c.VaultFlows(context.Background(), strings.ToLower(ownerSum))
				return len(f), err
			},
			wantVars: map[string]any{"owner": ownerSum},
			wantRows: 1,
		},
		{
			// verbatim row from the live indexer
			name: "kuru trades",
			body: `{"data":{"KuruFill":[{"id":"0xa9c2936656a7d2143720bcd91ba8506200b7cbe7-31651-0","marketId":"0xa9C2936656a7D2143720BcD91Ba8506200B7CbE7","price":"255179","size":"80000000","makerIsBuy":true,"txHash":"0xt","timestamp":"1788275816"}]}}`,
			call: func(c *Client) (int, error) {
				tr, err := c.KuruTrades(context.Background(), "0xa9c2936656a7d2143720bcd91ba8506200b7cbe7", 1, 2)
				if err == nil && len(tr) == 1 && (tr[0].IsBuy || tr[0].Ts != 1788275816) {
					return 0, fmt.Errorf("decoded badly (a maker bid means the taker SOLD): %+v", tr[0])
				}
				return len(tr), err
			},
			wantVars: map[string]any{"market": "0xa9C2936656a7D2143720BcD91Ba8506200B7CbE7", "fromTs": float64(1), "toTs": float64(2), "offset": float64(0)},
			wantRows: 1,
		},
		{
			name: "empty result is not an error",
			body: `{"data":{"Position":[]}}`,
			call: func(c *Client) (int, error) {
				p, err := c.Positions(context.Background(), "0xnone")
				return len(p), err
			},
			wantVars: map[string]any{"owner": "0xnone"},
			wantRows: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, got := serve(t, 200, tt.body)
			n, err := tt.call(c)
			if err != nil {
				t.Fatal(err)
			}
			if n != tt.wantRows {
				t.Fatalf("got %d rows want %d", n, tt.wantRows)
			}
			if !strings.HasPrefix(got.Query, "query ") {
				t.Fatalf("no graphql document sent: %q", got.Query)
			}
			for k, want := range tt.wantVars {
				if got.Variables[k] != want {
					t.Fatalf("var %s = %#v want %#v", k, got.Variables[k], want)
				}
			}
			if got.Variables["limit"] != float64(pageLimit) {
				t.Fatalf("limit not sent: %#v", got.Variables["limit"])
			}
		})
	}
}

func TestQueryFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"graphql errors surface", 200, `{"errors":[{"message":"field marketId not found"}]}`, "field marketId not found"},
		{"http error", 500, `boom`, "HTTP 500"},
		{"not json", 200, `<html>`, "not valid JSON"},
		{"neither data nor errors", 200, `{}`, "neither data nor errors"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := serve(t, tt.status, tt.body)
			_, err := c.Positions(context.Background(), "0xa")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tt.want)
			}
		})
	}
}

func TestUnreachableIndexer(t *testing.T) {
	t.Setenv("INDEXER_URL", "http://127.0.0.1:1")
	if _, err := New().Positions(context.Background(), "0xa"); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("err = %v", err)
	}
}

func TestChecksum(t *testing.T) {
	// vectors from EIP-55 itself
	for _, want := range []string{
		"0x5aAeb6053F3E94C9b9A09f33669435E7Ef1BeAed",
		"0xfB6916095ca1df60bB79Ce92cE3Ea74c37c5d359",
		"0xdbF03B407c01E7cD3CBea99509d93f8DDDC8C6FB",
		"0xD1220A0cf47c7B9Be7A2E6BA89F429762e7b9aDb",
	} {
		if got := Checksum(strings.ToLower(want)); got != want {
			t.Errorf("Checksum = %s, want %s", got, want)
		}
	}
	for _, passthrough := range []string{"", "0xnone", "0xkey", "sweem:vol:ETH/USD"} {
		if got := Checksum(passthrough); got != passthrough {
			t.Errorf("Checksum(%q) = %q, want it unchanged", passthrough, got)
		}
	}
}

// A busy window spans pages. Stopping at the first would chart only the oldest
// fills of the window and none of the recent ones.
func TestKuruTradesReadsEveryPage(t *testing.T) {
	var offsets []float64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got capture
		_ = json.NewDecoder(r.Body).Decode(&got)
		off := got.Variables["offset"].(float64)
		offsets = append(offsets, off)
		n := pageLimit
		if off >= pageLimit {
			n = 1 // the last, short page
		}
		rows := make([]string, n)
		for i := range rows {
			rows[i] = `{"id":"x","marketId":"m","price":"1","size":"1","makerIsBuy":false,"txHash":"0x","timestamp":"1"}`
		}
		io.WriteString(w, `{"data":{"KuruFill":[`+strings.Join(rows, ",")+`]}}`)
	}))
	defer srv.Close()
	t.Setenv("INDEXER_URL", srv.URL)

	tr, err := New().KuruTrades(context.Background(), "0xa9c2936656a7d2143720bcd91ba8506200b7cbe7", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr) != pageLimit+1 || len(offsets) != 2 || offsets[1] != pageLimit {
		t.Fatalf("read %d fills over offsets %v, want %d over [0 %d]", len(tr), offsets, pageLimit+1, pageLimit)
	}
}
