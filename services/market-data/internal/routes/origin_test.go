package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

func TestOrigins(t *testing.T) {
	srv := httptest.NewServer(New(t.Context(), nil, nil, []string{"http://localhost:3000"}))
	defer srv.Close()
	ws := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/v1/ws"
	dial := func(origin string) error {
		conn, _, err := websocket.Dial(t.Context(), ws, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {origin}}})
		if err == nil {
			conn.CloseNow()
		}
		return err
	}
	if err := dial("http://localhost:3000"); err != nil {
		t.Fatal("allowed origin rejected:", err)
	}
	if err := dial("http://evil.test"); err == nil {
		t.Fatal("foreign origin accepted")
	}
	req, _ := http.NewRequest("OPTIONS", srv.URL+"/api/v1/markets", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 204 || res.Header.Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatal("preflight", res.StatusCode, res.Header)
	}
}
