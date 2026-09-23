package kuru

import (
	"encoding/hex"
	"testing"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/finality"
)

// l2Book and the finalized lifecycle frame are bytes captured off the live
// gateway; the rest are built from the spec's field widths.
const (
	fxL2Book         = "00000000000000014b584d4401010301000000000000011dfdbe356828c8f5a5d5ed4f69dde0816f4058ef610000000000005f7e00000000004c67900000000003db3ecda8f2ebc6c2965931aa0f3cc6c56651720abf734e94dc49590356f9025709bd0900000000000000000000001400000000000000000000000000000000010000000000000000000000000000000000004e2000000000000000000000000ba43b7400"
	fxLifecycleFinal = "00000000000000014b584d4401040100000000000000011dfdbe356828c8f5a5d5ed4f69dde0816f4058ef610000000000005f800000000000005f7f00000000004c67a1030000000000000003db3ecda8f2ebc6c2965931aa0f3cc6c56651720abf734e94dc49590356f9025709bd090000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"
	fxL2Delta        = "00000000000000074b584d4401020100000000000000011da9c2936656a7d2143720bcd91ba8506200b7cbe70000000000033947000000000003394600000000004c67a10000000003db3ecd1111111111111111111111111111111111111111111111111111111111111111000000010200000000000433760000000000000000000000003178cb800000000000000000000000003178cb800000000000000000000000000000000000000001"
	fxTrades         = "00000000000000084b584d4401030300000000000000011da9c2936656a7d2143720bcd91ba8506200b7cbe700000000000335f200000000000335f100000000004c4d980000000003db3c1d1111111111111111111111111111111111111111111111111111111111111111000000010000000000003d8f00000200000000000431c400000000000000000000000013ab6680000000006ab2504b"
	fxLifecycleDrop  = "00000000000000014b584d4401040100000000000000011dfdbe356828c8f5a5d5ed4f69dde0816f4058ef610000000000005f800000000000005f7f00000000004c67a2050000000000000003db3ece11111111111111111111111111111111111111111111111111111111111111112222222222222222222222222222222222222222222222222222222222222222010100003333333333333333333333333333333333333333333333333333333333333333"
	fxBBO            = "00000000000000094b584d4401050300000000000000011da9c2936656a7d2143720bcd91ba8506200b7cbe700000000000335a800000000004c4d5d0000000003db3c1d111111111111111111111111111111111111111111111111111111111111111100000000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000433760000000000000000000000003178cb80"
	fxAllMids        = "000000000000000a4b584d4401060300000000000000011d00000002a9c2936656a7d2143720bcd91ba8506200b7cbe700000000000000000000000000043376fdbe356828c8f5a5d5ed4f69dde0816f4058ef6100000000000000000000000000004e20"
	fxUserOrdersSnap = "000000000000000b4b584d4401070301000000000000011d00000000000000020000000000044c170000000000402ace0000000000000000010000000003d89f66111111111111111111111111111111111111111111111111111111111111111100000001a9c2936656a7d2143720bcd91ba8506200b7cbe7000000000002e828030100000000000433c80000000000000000000000003178cb80010000000003db3f28015555555555555555555555555555555555555555555555555555555555555555000000006ab24cf000000000"
	fxUserOrdersDlt  = "000000000000000c4b584d4401070300000000000000011d00000000000000020000000000044c180000000000402acf0000000000044c170000000003d89f67111111111111111111111111111111111111111111111111111111111111111100000001014444444444444444444444444444444444444444444444444444444444444444000000020000003500000000000000000048000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000006ab2504b"
	fxUserBalances   = "000000000000000d4b584d4401080301000000000000011d00000000000000020000000000044c170000000000402ace0000000000000000010000000003d89f6611111111111111111111111111111111111111111111111111111111111111110000000200000000000000000000000000000000000000000000000000000000000000000000000000000000b64b004f977ab00000000000000000000000000000000000000000000000000000000000ee0722ead54f1b4fe97be399be43bc0226a6f97e0000000000000000000000016b5e51b800000000000000000000000000000000"
	fxUserTrades     = "000000000000000e4b584d4401090300000000000000011d0000000000000004000000000006880e0000000000402ace000000000006880d0000000003c17696111111111111111111111111111111111111111111111111111111111111111100000002a9c2936656a7d2143720bcd91ba8506200b7cbe7000000000000289600000000000000000004000000000000000202000000000003ac6300000000000000000000000005f5e1000105000000000002e827010000000000000000000000024820220000000fa04444444444444444444444444444444444444444444444444444444444444444000000020000003500001b580000000001000000006aaa750fa9c2936656a7d2143720bcd91ba8506200b7cbe7000000000000289600000000000000000004000000000000000202000000000003ac6300000000000000000000000005f5e10002000000000003a9800000000000000000000000001dcd65004444444444444444444444444444444444444444444444444444444444444444000000020000003500001b580000000001000000006aaa750f"
)

func decodeFixture(t *testing.T, fx string) (Envelope, any) {
	t.Helper()
	raw, err := hex.DecodeString(fx)
	if err != nil {
		t.Fatal(err)
	}
	env, err := DecodeEnvelope(raw)
	if err != nil {
		t.Fatalf("envelope: %v", err)
	}
	v, err := env.Decode()
	if err != nil {
		t.Fatalf("decode %s: %v", env.Kind, err)
	}
	return env, v
}

func TestDecodePerKind(t *testing.T) {
	tests := []struct {
		name  string
		fx    string
		kind  Kind
		view  finality.Tier
		check func(*testing.T, any)
	}{
		{"l2Book", fxL2Book, KindL2Book, finality.Final, func(t *testing.T, v any) {
			f := v.(L2Book)
			eq(t, "market", f.Market, "0xfdbe356828c8f5a5d5ed4f69dde0816f4058ef61")
			eq(t, "marketSeq", f.Seq, uint64(24446))
			eq(t, "globalSeq", f.GlobalSeq, uint64(5007248))
			eq(t, "block", f.Head.Number, uint64(64700109))
			eq(t, "maxDepth", f.MaxDepth, uint32(20))
			eq(t, "bids", len(f.Bids), 1)
			eq(t, "asks", len(f.Asks), 0)
			eq(t, "bid price", f.Bids[0].Price.String(), "20000")
			eq(t, "bid size", f.Bids[0].TotalBase.String(), "50000000000")
		}},
		{"l2Delta", fxL2Delta, KindL2Delta, finality.Seen, func(t *testing.T, v any) {
			f := v.(L2Delta)
			eq(t, "marketSeq", f.Seq, uint64(211271))
			eq(t, "prevSeq", f.PrevSeq, uint64(211270))
			eq(t, "updates", len(f.Updates), 1)
			u := f.Updates[0]
			eq(t, "side", u.Side, SideSell)
			eq(t, "price", u.Price.String(), "275318")
			eq(t, "totalBase", u.TotalBase.String(), "830000000")
			eq(t, "activeBase", u.ActiveBase.String(), "830000000")
			eq(t, "passiveBase", u.PassiveBase.String(), "0")
			eq(t, "orders", u.Orders, uint32(1))
		}},
		{"trades", fxTrades, KindTrades, finality.Final, func(t *testing.T, v any) {
			f := v.(Trades)
			eq(t, "rows", len(f.Rows), 1)
			r := f.Rows[0]
			eq(t, "tradeId", r.ID, uint64(15759))
			eq(t, "takerSide", r.TakerSide, SideSell)
			eq(t, "price", r.Price.String(), "274884")
			eq(t, "baseFilled", r.BaseFilled.String(), "330000000")
			eq(t, "ts", r.Ts, uint64(1790070859))
		}},
		{"lifecycle finalized", fxLifecycleFinal, KindLifecycle, finality.Seen, func(t *testing.T, v any) {
			f := v.(Lifecycle)
			eq(t, "action", f.Action, BlockFinalized)
			eq(t, "marketSeq", f.Market.Seq, uint64(24448))
			eq(t, "prevSeq", f.Market.PrevSeq, uint64(24447))
			eq(t, "block", f.Block.Number, uint64(64700109))
			eq(t, "parent is null", f.ParentID, "")
			eq(t, "replacement is null", f.ReplacementID, "")
			eq(t, "dropReason", f.DropReason, uint8(0))
		}},
		{"lifecycle branchDropped", fxLifecycleDrop, KindLifecycle, finality.Seen, func(t *testing.T, v any) {
			f := v.(Lifecycle)
			eq(t, "action", f.Action, BranchDropped)
			eq(t, "dropReason", f.DropReason, uint8(1))
			eq(t, "parent", f.ParentID, "0x"+rep("22", 32))
			eq(t, "replacement", f.ReplacementID, "0x"+rep("33", 32))
			if f.User != nil {
				t.Fatal("market-scope lifecycle decoded a user context")
			}
		}},
		{"bbo", fxBBO, KindBBO, finality.Final, func(t *testing.T, v any) {
			f := v.(BBO)
			if f.Bid != nil {
				t.Fatal("a zero-filled side must decode to null, not a zero level")
			}
			eq(t, "ask price", f.Ask.Price.String(), "275318")
			eq(t, "ask size", f.Ask.TotalBase.String(), "830000000")
		}},
		{"allMids", fxAllMids, KindAllMids, finality.Final, func(t *testing.T, v any) {
			f := v.(AllMids)
			eq(t, "rows", len(f.Mids), 2)
			eq(t, "market", f.Mids[1].Market, "0xfdbe356828c8f5a5d5ed4f69dde0816f4058ef61")
			eq(t, "mid", f.Mids[1].Price.String(), "20000")
		}},
		{"userOrders snapshot", fxUserOrdersSnap, KindUserOrders, finality.Final, func(t *testing.T, v any) {
			f := v.(UserOrders)
			eq(t, "userId", f.UserID, uint64(2))
			eq(t, "globalUserSeq", f.Seq, uint64(281623))
			eq(t, "prev is null", f.PrevSeq, uint64(0))
			eq(t, "orders", len(f.Orders), 1)
			o := f.Orders[0]
			eq(t, "orderId", o.OrderID, uint64(190504))
			eq(t, "slotIdx", o.SlotIdx, uint8(3))
			eq(t, "price", o.Price.String(), "275400")
			eq(t, "remaining", o.RemainingBase.String(), "830000000")
			eq(t, "minSizeAfterBlock", o.MinSizeAfterBlock, uint64(64700200))
			eq(t, "clientOrderId", o.ClientOrderID, "0x"+rep("55", 32))
			eq(t, "createdAt", o.CreatedAt, uint64(1790070000))
		}},
		{"userOrders delta", fxUserOrdersDlt, KindUserOrders, finality.Final, func(t *testing.T, v any) {
			f := v.(UserOrders)
			eq(t, "events", len(f.Events), 1)
			e := f.Events[0]
			eq(t, "code", e.Code, uint8(1))
			eq(t, "txIdx", e.TxIdx, uint32(2))
			eq(t, "logIdx", e.LogIdx, uint32(53))
			eq(t, "makerId", e.MakerID, uint64(72))
			eq(t, "payload", len(e.Payload), 96)
			eq(t, "ts", e.Ts, uint64(1790070859))
		}},
		{"userBalances", fxUserBalances, KindUserBalances, finality.Final, func(t *testing.T, v any) {
			f := v.(UserBalances)
			eq(t, "rows", len(f.Balances), 2)
			eq(t, "native token", f.Balances[0].Token, "0x0000000000000000000000000000000000000000")
			eq(t, "native free", f.Balances[0].Free.String(), "13135593080000000000")
			eq(t, "usdc", f.Balances[1].Token, "0xee0722ead54f1b4fe97be399be43bc0226a6f97e")
			eq(t, "usdc free", f.Balances[1].Free.String(), "6096310712")
			eq(t, "usdc reserved", f.Balances[1].Reserved.String(), "0")
		}},
		{"userTrades", fxUserTrades, KindUserTrades, finality.Final, func(t *testing.T, v any) {
			f := v.(UserTrades)
			eq(t, "rows", len(f.Rows), 2)
			act, pas := f.Rows[0], f.Rows[1]
			eq(t, "active liquidity", act.Liquidity, uint8(1))
			eq(t, "active slot", act.SlotIdx, uint8(5))
			eq(t, "active orderId", act.OrderID, uint64(190503))
			eq(t, "active remaining", act.RemainingBaseAfter.String(), "9800000000")
			eq(t, "active makerFeePps", act.MakerFeePps, uint32(4000))
			eq(t, "taker fee pps", act.TakerFeePps, uint32(7000))
			eq(t, "price", act.Price.String(), "240739")
			eq(t, "passive liquidity", pas.Liquidity, uint8(2))
			eq(t, "passive lowPrice", pas.LowPrice.String(), "240000")
			eq(t, "passive remaining", pas.PassiveRemainingAfter.String(), "500000000")
			eq(t, "matchEnd", pas.MatchEnd, uint8(1))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, v := decodeFixture(t, tt.fx)
			eq(t, "kind", env.Kind, tt.kind)
			eq(t, "view", env.View, tt.view)
			eq(t, "feedEpoch", env.FeedEpoch, uint64(285))
			tt.check(t, v)
		})
	}
}

// @p/@v/@f are Monad's proposed/voted/finalized, which is the shared
// seen/safe/final model and not a second enum.
func TestViewTier(t *testing.T) {
	tests := []struct {
		code    uint8
		want    finality.Tier
		suffix  string
		state   string
		wantErr bool
	}{
		{1, finality.Seen, "@p", "proposed", false},
		{2, finality.Safe, "@v", "voted", false},
		{3, finality.Final, "@f", "finalized", false},
		{0, 0, "", "", true},
		{4, 0, "", "", true},
	}
	for _, tt := range tests {
		got, err := viewTier(tt.code)
		if (err != nil) != tt.wantErr {
			t.Fatalf("viewTier(%d) err = %v", tt.code, err)
		}
		if tt.wantErr {
			continue
		}
		eq(t, "tier", got, tt.want)
		eq(t, "suffix", viewSuffix(got), tt.suffix)
		eq(t, "state", viewState(got), tt.state)
	}
}

func TestDecodeFailsClosed(t *testing.T) {
	tests := []struct{ name, fx string }{
		{"truncated envelope", fxBBO[:40]},
		{"bad magic", "0000000000000001" + "4b584d45" + fxBBO[24:]},
		{"unknown wire version", fxBBO[:24] + "02" + fxBBO[26:]},
		{"unknown view code", fxBBO[:28] + "07" + fxBBO[30:]},
		{"flags on a kind that forbids them", fxBBO[:30] + "01" + fxBBO[32:]},
		{"l2Book without a level flag", fxL2Book[:30] + "00" + fxL2Book[32:]},
		{"trailing bytes", fxAllMids + "ff"},
		{"truncated body", fxTrades[:len(fxTrades)-2]},
		{"row count beyond the frame", fxAllMids[:56] + "0000ffff" + fxAllMids[60:]},
		{"unknown kind", fxBBO[:26] + "0f" + fxBBO[28:]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := hex.DecodeString(tt.fx)
			if err != nil {
				t.Fatal(err)
			}
			env, err := DecodeEnvelope(raw)
			if err == nil {
				_, err = env.Decode()
			}
			if err == nil {
				t.Fatal("decoded a frame that should have failed closed")
			}
		})
	}
}

func eq[T comparable](t *testing.T, what string, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

func rep(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}
