//go:build integration

package mm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/snehendu098/sweem-terminal/internal/streams"
	"github.com/snehendu098/sweem-terminal/services/seeder/internal/executor"
	"github.com/snehendu098/sweem-terminal/services/seeder/internal/kuru"
)

const (
	usdc   = "0xee0722ead54f1b4fe97be399be43bc0226a6f97e"
	wethT  = "0x8b6c5fafef85b030bb1e71ae7ac085cc2380aaf8"
	other  = "0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f"
	testDB = 9 // isolated from the publisher's db 0
)

type rec struct {
	mu       sync.Mutex
	orders   []executor.OrdersReq
	deposits []executor.DepositReq
}

func setup(t *testing.T, heartbeat bool, open string) (*Seeder, *rec) {
	t.Helper()
	ctx := context.Background()
	url := os.Getenv("REDIS_URL")
	if url == "" {
		url = "redis://localhost:6380"
	}
	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	opt.DB = testDB
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { rdb.FlushDB(context.Background()); rdb.Close() })
	if err := rdb.FlushDB(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	m := weth
	m.Base, m.Quote = wethT, usdc
	o := weth
	o.Market = other
	set := func(key string, v any) {
		b, _ := json.Marshal(v)
		if err := rdb.Set(ctx, key, b, 0).Err(); err != nil {
			t.Fatal(err)
		}
	}
	set(streams.RegistryMarkets, []streams.MarketInfo{m, o})
	set(streams.RegistryBaskets, []streams.BasketInfo{{Address: "0xb", Components: []streams.Component{{Market: m.Market, Token: wethT, WeightBps: 10000}}}})
	set(streams.Ref(string(m.Market)), streams.RefPrice{Market: m.Market, Ref: d("3000"), Mid: d("3000")})
	set(streams.Ref(other), streams.RefPrice{Market: other, Ref: d("85000")})
	if heartbeat {
		if err := streams.Beat(ctx, rdb); err != nil {
			t.Fatal(err)
		}
	}

	r := &rec{}
	ex := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		defer r.mu.Unlock()
		switch req.URL.Path {
		case "/api/v1/bot/balances":
			w.Write([]byte(`{"accountId":"42","balances":[{"token":"` + usdc + `","free":"5000000000","locked":"1000000000"},{"token":"` + wethT + `","free":"1000000000000000000","locked":"0"}]}`))
		case "/api/v1/bot/orders":
			var o executor.OrdersReq
			json.NewDecoder(req.Body).Decode(&o)
			r.orders = append(r.orders, o)
			w.Write([]byte(`{"txHash":"0x01","status":"submitted"}`))
		case "/api/v1/bot/deposit":
			var d executor.DepositReq
			json.NewDecoder(req.Body).Decode(&d)
			r.deposits = append(r.deposits, d)
			w.Write([]byte(`{"txHash":"0x02","status":"submitted"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(ex.Close)
	ku := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/api/v1/users/42/orders" || req.URL.Query().Get("state") != "proposed" {
			t.Errorf("kuru path %s", req.URL)
		}
		w.Write([]byte(`{"feedEpoch":285,"data":[` + open + `],"state":"proposed","truncated":false}`))
	}))
	t.Cleanup(ku.Close)

	return &Seeder{
		Cfg: Config{
			Params:         params,
			Enabled:        true,
			MaxNotionalUSD: d("2000"),
			RequoteBps:     20,
			RequoteEvery:   time.Minute,
			MinGap:         time.Minute,
			DepositTargets: map[string]decimal.Decimal{usdc: d("7000000000")},
		},
		RDB:  rdb,
		Exec: executor.New(ex.URL, "s"),
		Kuru: kuru.New(ku.URL),
	}, r
}

const liveOrders = `{"orderId":7,"marketAddress":"0xA9C2936656A7D2143720BCD91BA8506200B7CBE7","slotIdx":0,"isBuy":true},{"orderId":"8","marketAddress":"0xa9c2936656a7d2143720bcd91ba8506200b7cbe7","slotIdx":1,"isBuy":false}`

func TestCycleQuotesComponentMarket(t *testing.T) {
	s, r := setup(t, true, liveOrders)
	if err := s.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.deposits) != 1 || r.deposits[0] != (executor.DepositReq{Token: usdc, Amount: "1000000000"}) {
		t.Fatal("deposits", r.deposits)
	}
	if len(r.orders) != 1 {
		t.Fatalf("want one request (basket component only), got %+v", r.orders)
	}
	o := r.orders[0]
	if o.Market != string(weth.Market) || len(o.Cancels) != 2 || o.Cancels[0] != "7" || o.Cancels[1] != "8" {
		t.Fatal(o)
	}
	want := Ladder(weth, d("3000"), params, Inventory{Base: d("1e18"), Quote: d("6e9"), SideUSD: d("1000")})
	if len(o.Orders) != 4 || len(want) != 4 {
		t.Fatal(o.Orders)
	}
	for i := range want {
		if o.Orders[i] != want[i] {
			t.Fatalf("%d got %+v want %+v", i, o.Orders[i], want[i])
		}
	}
	st := s.Status()
	if ms := st.Markets[string(weth.Market)]; !st.PublisherAlive || ms == nil || len(ms.Bids) != 2 || len(ms.Asks) != 2 || ms.LastTx != "0x01" {
		t.Fatalf("status %+v", st)
	}
	// MinGap holds the next cycle
	if err := s.Cycle(context.Background()); err != nil || len(r.orders) != 1 {
		t.Fatal("requoted inside min gap", err, r.orders)
	}
}

func TestCycleNoHeartbeatPlacesNothing(t *testing.T) {
	s, r := setup(t, false, "")
	if err := s.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.orders) != 0 {
		t.Fatal(r.orders)
	}
	if ms := s.Status().Markets[string(weth.Market)]; ms == nil || ms.Stopped != "publisher_heartbeat_missing" {
		t.Fatalf("%+v", ms)
	}

	// with live orders it only cancels
	s, r = setup(t, false, liveOrders)
	if err := s.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(r.orders) != 1 || len(r.orders[0].Orders) != 0 || len(r.orders[0].Cancels) != 2 {
		t.Fatal(r.orders)
	}
}
