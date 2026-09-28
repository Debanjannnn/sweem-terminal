package client

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// verbatim shape of GET /v1/book on market-data: every level is a WAD string
const liveBook = `{"data":{"marketId":"0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f","venue":"kuru","bids":[{"price":"83969900000000000000000","size":"2743140000000000"}],"asks":[{"price":"84030100000000000000000","size":"1000000000000000"}],"ts":1790542089740}}`

// a grid level is typed as 84000, so the mid it is compared with must be 84000
// and not 8.4e22, or no level is ever crossed
func TestMidIsADisplayPrice(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(liveBook))
	}))
	defer srv.Close()

	mid, err := NewMarketData(srv.URL).Mid(t.Context(), "0x5bdea6f9f9aba34f4ecb9b865646a792b835ef7f")
	if err != nil {
		t.Fatal(err)
	}
	if mid < 83999.9 || mid > 84000.1 {
		t.Fatalf("mid = %v, want 84000", mid)
	}
}
