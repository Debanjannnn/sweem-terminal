package api

import (
	"fmt"
	"math/big"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/httpx"
	"github.com/sweem-terminal/sweem-terminal/internal/shared/indexerclient"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/marketdata"
	"github.com/sweem-terminal/sweem-terminal/services/wallet/internal/store"
)

// This service aggregates; it never recomputes a number an indexer or
// market-data already owns.

type portfolioPosition struct {
	MarketID   string `json:"market_id"`
	Side       string `json:"side,omitempty"`
	Size       string `json:"size"`
	EntryPrice string `json:"entry_price,omitempty"`
	Collateral string `json:"collateral,omitempty"`
	Mark       string `json:"mark,omitempty"`
	// "indexer" when the chain reports it, "local" when only our rows do
	Source          string `json:"source"`
	Reconciled      bool   `json:"reconciled"`
	ReconcileReason string `json:"reconcile_reason,omitempty"`
	UpdatedAt       int64  `json:"updated_at,omitempty"`
}

type portfolioResponse struct {
	UserID        string              `json:"user_id"`
	WalletAddress string              `json:"wallet_address"`
	Positions     []portfolioPosition `json:"positions"`
	// sources that failed, named rather than stood in for with a zero
	Degraded  []string  `json:"degraded"`
	UpdatedAt time.Time `json:"updated_at"`
}

// local is what this service's own rows say about a market.
type local struct {
	MarketID string
	Side     string
	Size     string
	Orders   int
	Last     time.Time
}

func (s *Server) portfolio(w http.ResponseWriter, r *http.Request) {
	u, ok := s.caller(w, r)
	if !ok {
		return
	}
	degraded := []string{}

	var positions []indexerclient.Position
	if s.Indexer != nil {
		var err error
		if positions, err = s.Indexer.Positions(r.Context(), u.WalletAddress); err != nil {
			s.log().Warn("indexer positions unavailable", "err", err)
			degraded = append(degraded, "indexer")
		}
	}

	intents, err := s.Store.Intents(r.Context(), u.ID, 200)
	if err != nil {
		s.fail(w, "list intents", err)
		return
	}
	mine := localActivity(intents)

	ids := map[string]bool{}
	for _, p := range positions {
		ids[p.MarketID] = true
	}
	for id := range mine {
		ids[mine[id].MarketID] = true
	}
	marks := map[string]marketdata.Mark{}
	if s.Market != nil && len(ids) > 0 {
		if marks, err = s.Market.Marks(r.Context(), keys(ids)); err != nil {
			s.log().Warn("market-data marks unavailable", "err", err)
			degraded = append(degraded, "market-data")
			marks = map[string]marketdata.Mark{}
		}
	}

	httpx.OK(w, portfolioResponse{
		UserID:        u.ID,
		WalletAddress: u.WalletAddress,
		Positions:     reconcile(positions, marks, mine),
		Degraded:      degraded,
		UpdatedAt:     time.Now().UTC(),
	})
}

// reconcile joins the indexer's view with our own rows. A position the two
// disagree about is reported as unreconciled with the reason, never quietly
// trusted.
func reconcile(positions []indexerclient.Position, marks map[string]marketdata.Mark, mine map[string]local) []portfolioPosition {
	out := []portfolioPosition{}
	seen := map[string]bool{}

	for _, p := range positions {
		key := normalizeID(p.MarketID)
		seen[key] = true
		pos := portfolioPosition{
			MarketID:   p.MarketID,
			Side:       side(p.IsLong),
			Size:       p.Size,
			EntryPrice: p.EntryPrice,
			Collateral: p.Collateral,
			Mark:       markOf(marks, p.MarketID),
			Source:     "indexer",
			Reconciled: true,
			UpdatedAt:  p.UpdatedAt,
		}
		if _, ok := mine[key]; !ok {
			pos.Reconciled = false
			pos.ReconcileReason = fmt.Sprintf(
				"The indexer reports a position in %s that this terminal has no submitted order for, so it was opened elsewhere and its size is the chain's, not ours.",
				p.MarketID)
		}
		out = append(out, pos)
	}

	for key, l := range mine {
		if seen[key] {
			continue
		}
		out = append(out, portfolioPosition{
			MarketID:   l.MarketID,
			Side:       l.Side,
			Size:       l.Size,
			Mark:       markOf(marks, l.MarketID),
			Source:     "local",
			Reconciled: false,
			ReconcileReason: fmt.Sprintf(
				"This terminal submitted %d order(s) on %s but the indexer reports no position, so the fill is either not indexed yet or never landed.",
				l.Orders, l.MarketID),
			UpdatedAt: l.Last.Unix(),
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].MarketID < out[j].MarketID })
	return out
}

// localActivity keeps only intents we actually handed to the executor: a
// pending or failed intent moved nothing and must not look like a position.
func localActivity(intents []store.Intent) map[string]local {
	out := map[string]local{}
	for _, in := range intents {
		if in.State != "submitted" && in.State != "filled" {
			continue
		}
		key := normalizeID(in.MarketID)
		l, ok := out[key]
		if !ok {
			l = local{MarketID: in.MarketID, Side: in.Side, Size: "0"}
		}
		l.Orders++
		if in.CreatedAt.After(l.Last) {
			l.Last, l.Side = in.CreatedAt, in.Side
		}
		l.Size = addAmounts(l.Size, in.Size)
		out[key] = l
	}
	return out
}

func addAmounts(a, b string) string {
	x, okA := new(big.Int).SetString(a, 10)
	y, okB := new(big.Int).SetString(b, 10)
	if !okA || !okB {
		return a
	}
	return x.Add(x, y).String()
}

func markOf(marks map[string]marketdata.Mark, marketID string) string {
	if m, ok := marks[marketID]; ok {
		return m.Price
	}
	for id, m := range marks {
		if normalizeID(id) == normalizeID(marketID) {
			return m.Price
		}
	}
	return ""
}

func side(isLong bool) string {
	if isLong {
		return "long"
	}
	return "short"
}

func normalizeID(id string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(id)) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
