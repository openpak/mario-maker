package erase_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openpak/mario-maker/internal/erase"
	"github.com/openpak/mario-maker/internal/link"
	"github.com/openpak/mario-maker/internal/model"
	"github.com/openpak/mario-maker/internal/store"
	"github.com/openpak/mario-maker/internal/testutil"
)

const (
	acctA = "11111111-1111-4111-8111-111111111111"
	acctB = "22222222-2222-4222-8222-222222222222"
)

type fakeLink map[string]link.Identity

func (f fakeLink) NNID(_ context.Context, a string) (string, error) { return f[a].NNID, nil }
func (f fakeLink) Identity(_ context.Context, a string) (link.Identity, error) {
	return f[a], nil
}

func TestEraseAccount(t *testing.T) {
	st := testutil.DB(t)
	ctx := context.Background()
	now := time.Now().UTC()
	courses := []model.Course{
		{Game: "smm1", ID: "C1", Name: "Sky Castle", MakerID: "MiaNNID", MakerName: "Mia", Created: now},
		{Game: "smm1", ID: "C2", Name: "Pipe Maze", MakerID: "miannid", MakerName: "Mia", Created: now},
		{Game: "smm1", ID: "C3", Name: "Other", MakerID: "ZedNNID", MakerName: "Zed", Created: now},
	}
	makers := []model.Maker{{Game: "smm1", ID: "MiaNNID", Name: "Mia"}, {Game: "smm1", ID: "ZedNNID", Name: "Zed"}}
	if err := st.UpsertCourses(ctx, courses); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertMakers(ctx, makers); err != nil {
		t.Fatal(err)
	}
	for _, k := range []store.ArchiveClaim{
		{MakerID: "arch1", AccountID: acctA, AccountName: "Mia", Status: "pending", Via: "web"},
		{MakerID: "arch2", AccountID: acctB, AccountName: "Zed", Status: "pending", Via: "web"},
	} {
		if _, err := st.AddClaim(ctx, k); err != nil {
			t.Fatal(err)
		}
	}
	svc := &erase.Service{Store: st, Key: "erase-key-0123456789", Log: slog.Default(),
		Link: fakeLink{acctA: {NNID: "MiaNNID"}, acctB: {NNID: "ZedNNID"}}}
	srv := httptest.NewServer(svc.Handler())
	defer srv.Close()
	del := func(key, path string) int {
		req, _ := http.NewRequest("DELETE", srv.URL+path, nil)
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	p := "/internal/accounts/" + acctA + "/data"
	if c := del("", p); c != 401 {
		t.Fatalf("no key %d", c)
	}
	if c := del("wrong", p); c != 401 {
		t.Fatalf("wrong key %d", c)
	}
	if c := del("erase-key-0123456789", "/internal/accounts/bad/data"); c != 400 {
		t.Fatalf("bad id %d", c)
	}
	if c := del("erase-key-0123456789", p); c != 200 {
		t.Fatalf("erase %d", c)
	}
	check := func() {
		t.Helper()
		for _, id := range []string{"C1", "C2"} {
			c, err := st.Course(ctx, "smm1", id)
			if err != nil || c.MakerName != store.DeletedUser || c.MakerID != "deleted" {
				t.Fatalf("course %s: %+v %v", id, c, err)
			}
		}
		if c, err := st.Course(ctx, "smm1", "C3"); err != nil || c.MakerName != "Zed" || c.MakerID != "ZedNNID" {
			t.Fatalf("other maker touched: %+v %v", c, err)
		}
		if _, err := st.Maker(ctx, "smm1", "MiaNNID"); err == nil {
			t.Fatal("maker row kept")
		}
		if _, err := st.Maker(ctx, "smm1", "ZedNNID"); err != nil {
			t.Fatalf("other maker row lost: %v", err)
		}
		if _, err := st.AccountClaim(ctx, "arch1", acctA); err == nil {
			t.Fatal("claim kept")
		}
		if _, err := st.AccountClaim(ctx, "arch2", acctB); err != nil {
			t.Fatalf("other claim lost: %v", err)
		}
	}
	check()
	// the syncer reads the same courses from upstream again: the credit stays scrubbed
	if err := st.UpsertCourses(ctx, courses); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertMakers(ctx, makers); err != nil {
		t.Fatal(err)
	}
	check()
	// second run: nothing left, no error
	if n, err := svc.Erase(ctx, acctA); err != nil || n != 0 {
		t.Fatalf("second run: %d %v", n, err)
	}
	svc.Key = ""
	if c := del("x", p); c != 503 {
		t.Fatalf("no env key %d", c)
	}
}
