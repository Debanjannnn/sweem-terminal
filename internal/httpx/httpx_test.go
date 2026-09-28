package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func get(h http.Handler, path string, mod func(*http.Request)) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	if mod != nil {
		mod(r)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestHealth(t *testing.T) {
	var fail error
	mux := NewMux(map[string]Check{"redis": func(context.Context) error { return fail }})
	if w := get(mux, "/health", nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := get(mux, "/healthz", nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	fail = errors.New("down")
	w := get(mux, "/healthz", nil)
	if w.Code != 503 || !strings.Contains(w.Body.String(), `"redis":"down"`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := get(mux, "/health", nil); w.Code != 200 {
		t.Fatal("liveness must not depend on checks")
	}
}

func TestRequireInternal(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	h := RequireInternal("s3cret", "scheduler")(ok)
	cases := []struct {
		secret, caller string
		want           int
	}{
		{"s3cret", "scheduler", 204},
		{"s3cret", "seeder", 403},
		{"wrong", "scheduler", 401},
		{"", "scheduler", 401},
	}
	for _, c := range cases {
		if w := get(h, "/", func(r *http.Request) { SetInternal(r, c.secret, c.caller) }); w.Code != c.want {
			t.Errorf("%+v: got %d", c, w.Code)
		}
	}
	if w := get(RequireInternal("")(ok), "/", func(r *http.Request) { SetInternal(r, "", "") }); w.Code != 401 {
		t.Error("empty secret must fail closed")
	}
}
