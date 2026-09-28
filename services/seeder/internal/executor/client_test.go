package executor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
)

const fixtures = "../../../../testdata/contracts/executor/"

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(fixtures + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func sameJSON(t *testing.T, got, want []byte) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(want, &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Fatalf("request\n got %s\nwant %s", got, want)
	}
}

// fake serves resp for path and asserts the request body equals wantReq (if set) and headers are internal.
func fake(t *testing.T, method, path string, wantReq []byte, status int, resp []byte) *Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method || r.URL.Path != path {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("X-Internal-Secret") != "s" || r.Header.Get("X-Caller") != "seeder" {
			t.Errorf("missing internal headers")
		}
		if wantReq != nil {
			b, _ := io.ReadAll(r.Body)
			sameJSON(t, b, wantReq)
		}
		w.WriteHeader(status)
		w.Write(resp)
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "s")
}

func TestOrdersMatchesFixture(t *testing.T) {
	c := fake(t, "POST", "/api/v1/bot/orders", fixture(t, "bot_orders.request.json"), 200, fixture(t, "bot_orders.response.json"))
	got, err := c.Orders(context.Background(), OrdersReq{
		Market:  "0xa9c2936656a7d2143720bcd91ba8506200b7cbe7",
		Cancels: []string{"12", "13"},
		Orders: []Order{
			{Side: "buy", Price: "299000", Size: "5000000000", Tif: "gtc"},
			{Side: "sell", Price: "301000", Size: "5000000000", Tif: "gtc"},
		},
	})
	if err != nil || got.TxHash == "" || got.Status != "confirmed" || len(got.OrderIDs) != 2 {
		t.Fatal(got, err)
	}
}

func TestOrdersNoop(t *testing.T) {
	c := fake(t, "POST", "/api/v1/bot/orders", []byte(`{"market":"0xm","cancels":["1"],"orders":[]}`), 200, fixture(t, "bot_orders_noop.response.json"))
	got, err := c.Orders(context.Background(), OrdersReq{Market: "0xm", Cancels: []string{"1"}})
	if err != nil || got.Status != "noop" || got.TxHash != "" {
		t.Fatal(got, err)
	}
}

func TestCancelOnlySendsEmptyArrays(t *testing.T) {
	c := fake(t, "POST", "/api/v1/bot/orders", []byte(`{"market":"0xm","cancels":["1"],"orders":[]}`), 200, fixture(t, "bot_orders.response.json"))
	if _, err := c.Orders(context.Background(), OrdersReq{Market: "0xm", Cancels: []string{"1"}}); err != nil {
		t.Fatal(err)
	}
}

func TestDepositMatchesFixture(t *testing.T) {
	c := fake(t, "POST", "/api/v1/bot/deposit", fixture(t, "bot_deposit.request.json"), 200, fixture(t, "bot_deposit.response.json"))
	got, err := c.Deposit(context.Background(), DepositReq{Token: "0xee0722ead54f1b4fe97be399be43bc0226a6f97e", Amount: "5000000000"})
	if err != nil || got.TxHash == "" {
		t.Fatal(got, err)
	}
}

func TestBalancesDecode(t *testing.T) {
	c := fake(t, "GET", "/api/v1/bot/balances", nil, 200, fixture(t, "bot_balances.response.json"))
	got, err := c.Balances(context.Background())
	if err != nil || got.AccountID != "42" || len(got.Balances) != 2 {
		t.Fatal(got, err)
	}
	if b := got.Balances[0]; b.Token != "0xee0722ead54f1b4fe97be399be43bc0226a6f97e" || b.Free.String() != "5000000000" || b.Locked.String() != "1000000000" {
		t.Fatal(b)
	}
}

func TestErrorDecode(t *testing.T) {
	c := fake(t, "GET", "/api/v1/bot/balances", nil, 409, fixture(t, "error.response.json"))
	_, err := c.Balances(context.Background())
	var e *Error
	if !errors.As(err, &e) || e.Code != "delegation_inactive" || e.Status != 409 {
		t.Fatal(err)
	}
}
