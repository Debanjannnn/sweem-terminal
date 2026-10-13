package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Sleeve validation rejects before touching the DB (nil here).
func TestMultiValidation(t *testing.T) {
	s := &Server{Now: time.Now}
	leg := `"legs":[{"kind":"lending","venue":"aave","weight":"1"}]`
	sl := func(asset, w string) string { return `{"asset":"` + asset + `","weight":"` + w + `",` + leg + `}` }
	for name, body := range map[string]string{
		"one sleeve":   `{"sleeves":[` + sl("USDC", "1") + `]}`,
		"six sleeves":  `{"sleeves":[` + strings.Repeat(sl("USDC", "0.2")+",", 5) + sl("USDC", "0") + `]}`,
		"sum != 1":     `{"sleeves":[` + sl("USDC", "0.4") + `,` + sl("WETH", "0.5") + `]}`,
		"zero weight":  `{"sleeves":[` + sl("USDC", "0") + `,` + sl("WETH", "1") + `]}`,
		"bad weight":   `{"sleeves":[` + sl("USDC", "x") + `,` + sl("WETH", "1") + `]}`,
		"parent asset": `{"asset":"USDC","sleeves":[` + sl("USDC", "0.5") + `,` + sl("WETH", "0.5") + `]}`,
		"bad sleeve":   `{"sleeves":[{"asset":"USDC","weight":"0.5","legs":[{"kind":"lending","weight":"y"}]},` + sl("WETH", "0.5") + `]}`,
		"bad amount":   `{"amount":"-1","sleeves":[` + sl("USDC", "0.5") + `,` + sl("WETH", "0.5") + `]}`,
	} {
		rec := httptest.NewRecorder()
		s.backtest(rec, httptest.NewRequest(http.MethodPost, "/api/v1/backtest", strings.NewReader(body)))
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), "bad_request") {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body)
		}
	}
}
