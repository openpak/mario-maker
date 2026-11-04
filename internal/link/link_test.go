package link

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Key") != "k" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		switch r.URL.Query().Get("account_id") {
		case "a":
			w.Write([]byte(`{"nnid":" Name ","pid":1800000123}`))
		case "old":
			w.Write([]byte(`{"nnid":"x"}`))
		case "neg":
			w.Write([]byte(`{"nnid":"x","pid":-5}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	h := New(srv.URL, "k")
	ctx := context.Background()
	if id, err := h.Identity(ctx, "a"); err != nil || id.NNID != "Name" || id.PID != 1800000123 {
		t.Errorf("a: %+v %v", id, err)
	}
	if id, err := h.Identity(ctx, "old"); err != nil || id.PID != 0 || id.NNID != "x" {
		t.Errorf("old: %+v %v", id, err)
	}
	if id, _ := h.Identity(ctx, "neg"); id.PID != 0 {
		t.Errorf("negative pid kept: %+v", id)
	}
	if id, err := h.Identity(ctx, "none"); err != nil || id != (Identity{}) {
		t.Errorf("not linked: %+v %v", id, err)
	}
	if n, err := h.NNID(ctx, "a"); err != nil || n != "Name" {
		t.Errorf("NNID: %q %v", n, err)
	}
	if _, err := New(srv.URL, "bad").Identity(ctx, "a"); err == nil {
		t.Error("wrong key accepted")
	}
}
