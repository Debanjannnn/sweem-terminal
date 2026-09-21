package indexerclient

import (
	"context"
	"encoding/json"
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

func TestTypedQueries(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		call     func(c *Client) (int, error)
		wantVars map[string]any
		wantRows int
	}{
		{
			name: "positions lowercases the owner",
			body: `{"data":{"Position":[{"id":"1","owner":"0xabc","marketId":"eth","isLong":true,"size":"1000000000000000000","entryPrice":"2500000000000000000000","collateral":"500000000","blockNumber":42,"updatedAt":1700000000}]}}`,
			call: func(c *Client) (int, error) {
				p, err := c.Positions(context.Background(), "0xABC")
				if err == nil && (p[0].Size != "1000000000000000000" || !p[0].IsLong || p[0].Block != 42) {
					t.Fatalf("decoded badly: %+v", p[0])
				}
				return len(p), err
			},
			wantVars: map[string]any{"owner": "0xabc"},
			wantRows: 1,
		},
		{
			name: "position events filter from block",
			body: `{"data":{"PositionEvent":[{"id":"e1","marketId":"eth","kind":"increase","blockNumber":100,"timestamp":1700000000,"size":"1","price":"2","fee":"0"}]}}`,
			call: func(c *Client) (int, error) {
				e, err := c.PositionEvents(context.Background(), "eth", 100)
				return len(e), err
			},
			wantVars: map[string]any{"marketId": "eth", "fromBlock": float64(100)},
			wantRows: 1,
		},
		{
			name: "funding rate points",
			body: `{"data":{"FundingRatePoint":[{"marketId":"eth","fundingRate":"-125000000000000","timestamp":1700000000},{"marketId":"eth","fundingRate":"3","timestamp":1700003600}]}}`,
			call: func(c *Client) (int, error) {
				r, err := c.FundingRatePoints(context.Background(), "eth", 1, 2)
				if err == nil && r[0].Rate != "-125000000000000" {
					t.Fatalf("negative rate mangled: %q", r[0].Rate)
				}
				return len(r), err
			},
			wantVars: map[string]any{"marketId": "eth", "fromTs": float64(1), "toTs": float64(2)},
			wantRows: 2,
		},
		{
			name: "vault flows",
			body: `{"data":{"VaultFlow":[{"id":"v1","owner":"0xa","kind":"deposit","assets":"1000000","shares":"999999","timestamp":1}]}}`,
			call: func(c *Client) (int, error) {
				f, err := c.VaultFlows(context.Background(), "0xA")
				return len(f), err
			},
			wantVars: map[string]any{"owner": "0xa"},
			wantRows: 1,
		},
		{
			name: "kuru trades",
			body: `{"data":{"KuruTrade":[{"id":"t1","marketId":"eth","price":"2500","size":"1","isBuy":true,"timestamp":1}]}}`,
			call: func(c *Client) (int, error) {
				tr, err := c.KuruTrades(context.Background(), "eth", 1, 2)
				return len(tr), err
			},
			wantVars: map[string]any{"marketId": "eth", "fromTs": float64(1), "toTs": float64(2)},
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
