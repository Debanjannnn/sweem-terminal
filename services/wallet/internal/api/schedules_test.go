package api

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"testing"
	"time"

	"github.com/sweem-terminal/sweem-terminal/internal/shared/privyauth"
)

func TestPlanSlices(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		total   string
		count   int
		spacing time.Duration
		amounts []string
	}{
		{name: "even split", total: "100", count: 4, spacing: time.Minute, amounts: []string{"25", "25", "25", "25"}},
		{name: "remainder lands on the last slice", total: "10", count: 3, spacing: time.Minute, amounts: []string{"3", "3", "4"}},
		{name: "single slice takes everything", total: "7", count: 1, amounts: []string{"7"}},
		{name: "wad sized amounts stay exact", total: "1000000000000000001", count: 2,
			amounts: []string{"500000000000000000", "500000000000000001"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			total, _ := new(big.Int).SetString(tt.total, 10)
			got := planSlices(total, tt.count, tt.spacing, start)
			if len(got) != tt.count {
				t.Fatalf("%d slices, want %d", len(got), tt.count)
			}
			sum := new(big.Int)
			for i, sl := range got {
				if sl.Amount != tt.amounts[i] {
					t.Errorf("slice %d amount = %s, want %s", i, sl.Amount, tt.amounts[i])
				}
				if want := start.Add(time.Duration(i) * tt.spacing); !sl.DueAt.Equal(want) {
					t.Errorf("slice %d due at %s, want %s", i, sl.DueAt, want)
				}
				n, _ := new(big.Int).SetString(sl.Amount, 10)
				sum.Add(sum, n)
			}
			if sum.Cmp(total) != 0 {
				t.Fatalf("slices sum to %s, want %s: no amount may be rounded away", sum, total)
			}
		})
	}
}

func TestCreateScheduleValidation(t *testing.T) {
	tests := []struct{ name, body, says string }{
		{"no idempotency key", `{"kind":"twap","market_id":"MON-USDC","venue":"kuru","side":"buy","total":"100","slice_count":4,"window_seconds":60}`, "idempotency_key"},
		{"unknown kind", `{"kind":"iceberg","market_id":"MON-USDC","venue":"kuru","side":"buy","total":"100","slice_count":4,"window_seconds":60,"idempotency_key":"a"}`, "kind"},
		{"twap without a window", `{"kind":"twap","market_id":"MON-USDC","venue":"kuru","side":"buy","total":"100","slice_count":4,"idempotency_key":"a"}`, "window_seconds"},
		{"dca without a cadence", `{"kind":"dca","market_id":"MON-USDC","venue":"kuru","side":"buy","total":"100","slice_count":4,"idempotency_key":"a"}`, "cadence_seconds"},
		{"grid without levels", `{"kind":"grid","market_id":"MON-USDC","venue":"kuru","side":"buy","total":"100","idempotency_key":"a"}`, "levels"},
		{"total smaller than the slice count", `{"kind":"twap","market_id":"MON-USDC","venue":"kuru","side":"buy","total":"3","slice_count":4,"window_seconds":60,"idempotency_key":"a"}`, "slice_count"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if reason := buildReason(t, tt.body); !containsFold(reason, tt.says) {
				t.Fatalf("reason %q does not name %q", reason, tt.says)
			}
		})
	}
}

// buildReason runs the validator directly; the HTTP path is covered below.
func buildReason(t *testing.T, raw string) string {
	t.Helper()
	var req scheduleRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	s := &Server{ChainID: 10143}
	_, _, reason := s.buildSchedule(context.Background(), did, req, time.Now())
	return reason
}

func TestCreateScheduleIsIdempotent(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	h.delegated(t, "1000", "10000")
	const body1 = `{"kind":"twap","market_id":"MON-USDC","venue":"kuru","side":"buy","total":"100","slice_count":4,"window_seconds":3600,"idempotency_key":"sched-1"}`

	first := h.do(t, http.MethodPost, "/v1/schedules", body1)
	if first.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", first.Code, first.Body)
	}
	id, _ := body(t, first)["id"].(string)
	if id == "" {
		t.Fatal("create returned no schedule id")
	}
	second := h.do(t, http.MethodPost, "/v1/schedules", body1)
	if got, _ := body(t, second)["id"].(string); got != id {
		t.Fatalf("retry created schedule %s, want the first one %s", got, id)
	}
	if len(h.store.schedules) != 1 {
		t.Fatalf("%d schedules, want 1", len(h.store.schedules))
	}
}

// A schedule whose first slice already breaches the delegation is refused at
// creation rather than failing silently, slice by slice, later.
func TestCreateScheduleRefusedWithoutDelegation(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	rec := h.do(t, http.MethodPost, "/v1/schedules",
		`{"kind":"dca","market_id":"MON-USDC","venue":"kuru","side":"buy","total":"100","slice_count":2,"cadence_seconds":60,"idempotency_key":"s"}`)
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, want 412; body = %s", rec.Code, rec.Body)
	}
	if body(t, rec)["code"] != "no_delegation" {
		t.Fatalf("code = %v, want no_delegation", body(t, rec)["code"])
	}
}

func (h *harness) schedule(t *testing.T) string {
	t.Helper()
	rec := h.do(t, http.MethodPost, "/v1/schedules",
		`{"kind":"dca","market_id":"MON-USDC","venue":"kuru","side":"buy","total":"100","slice_count":2,"cadence_seconds":60,"idempotency_key":"s"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create schedule: status = %d, body = %s", rec.Code, rec.Body)
	}
	id, _ := body(t, rec)["id"].(string)
	return id
}

func TestDispatchSlice(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	h.delegated(t, "1000", "10000")
	id := h.schedule(t)

	headers := map[string]string{
		privyauth.HeaderInternalSecret: internalSecret,
		privyauth.HeaderActingUser:     did,
	}
	path := "/v1/internal/slices/" + id + "/0/dispatch"

	first := h.send(t, http.MethodPost, path, "", headers)
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", first.Code, first.Body)
	}
	got := body(t, first)
	if got["state"] != "dispatched" {
		t.Fatalf("state = %v, want dispatched", got["state"])
	}
	if h.store.slices[sliceKey(id, 0)].State != "dispatched" {
		t.Fatalf("slice state = %s, want dispatched", h.store.slices[sliceKey(id, 0)].State)
	}

	// the scheduler retrying a slice must not place a second order
	second := h.send(t, http.MethodPost, path, "", headers)
	if second.Code != http.StatusOK || body(t, second)["replayed"] != true {
		t.Fatalf("retry: status = %d, body = %s", second.Code, second.Body)
	}
	if h.exec.calls != 1 {
		t.Fatalf("executor was called %d times, want 1", h.exec.calls)
	}
}

// A revoked delegation stops the whole schedule, not just this slice.
func TestDispatchSliceOnRevokedDelegationPausesTheSchedule(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	h.delegated(t, "1000", "10000")
	id := h.schedule(t)
	if err := h.store.RevokeDelegation(context.Background(), did); err != nil {
		t.Fatal(err)
	}

	rec := h.send(t, http.MethodPost, "/v1/internal/slices/"+id+"/0/dispatch", "", map[string]string{
		privyauth.HeaderInternalSecret: internalSecret,
		privyauth.HeaderActingUser:     did,
	})
	if rec.Code != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, want 412; body = %s", rec.Code, rec.Body)
	}
	if body(t, rec)["code"] != "delegation_revoked" {
		t.Fatalf("code = %v, want delegation_revoked", body(t, rec)["code"])
	}
	if sl := h.store.slices[sliceKey(id, 0)]; sl.State != "skipped" || sl.SkipReason == "" {
		t.Fatalf("slice = %+v, want skipped with a reason", sl)
	}
	if h.store.schedules[id].State != "paused_revoked" {
		t.Fatalf("schedule state = %s, want paused_revoked", h.store.schedules[id].State)
	}
	if h.exec.calls != 0 {
		t.Fatalf("executor was called %d times for a revoked delegation", h.exec.calls)
	}
}

func TestDispatchSliceOfAnotherUserIsNotFound(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	h.delegated(t, "1000", "10000")
	id := h.schedule(t)

	rec := h.send(t, http.MethodPost, "/v1/internal/slices/"+id+"/0/dispatch", "", map[string]string{
		privyauth.HeaderInternalSecret: internalSecret,
		privyauth.HeaderActingUser:     "did:privy:mallory",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", rec.Code, rec.Body)
	}
}

func TestCancelSchedule(t *testing.T) {
	h := newHarness(t)
	h.registered(t)
	h.delegated(t, "1000", "10000")
	id := h.schedule(t)

	if rec := h.do(t, http.MethodDelete, "/v1/schedules/"+id, ""); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	if h.store.schedules[id].State != "cancelled" {
		t.Fatalf("state = %s, want cancelled", h.store.schedules[id].State)
	}
	if rec := h.do(t, http.MethodGet, "/v1/schedules/not-a-uuid", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a malformed id", rec.Code)
	}
}
