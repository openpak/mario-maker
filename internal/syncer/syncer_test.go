package syncer_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openpak/mario-maker/internal/fakeupstream"
	"github.com/openpak/mario-maker/internal/store"
	"github.com/openpak/mario-maker/internal/syncer"
	"github.com/openpak/mario-maker/internal/testutil"
	"github.com/openpak/mario-maker/internal/upstream"
)

func course(id, name string, updated time.Time) fakeupstream.Course {
	return fakeupstream.Course{Game: "smm1", ID: id, Name: name, MakerID: "M1", MakerName: "Mia", Tags: []string{"t"},
		Created: updated.Format(time.RFC3339), Updated: updated.Format(time.RFC3339), Stars: 1}
}

func TestSync(t *testing.T) {
	st := testutil.DB(t)
	f := fakeupstream.New("key")
	ts := httptest.NewServer(f)
	defer ts.Close()
	now := time.Now().UTC()
	// More than one page so paging is exercised.
	for i := 0; i < 230; i++ {
		f.Set(course(fmt.Sprintf("C%03d", i), "Course", now.Add(-time.Duration(i)*time.Minute)))
	}
	f.Set(course("GONE", "Doomed", now))
	f.SetMaker(fakeupstream.Maker{Game: "smm1", ID: "M1", Name: "Mia", Courses: 231})

	sy := &syncer.Syncer{Store: st, Clients: []*upstream.Client{upstream.New("smm1", ts.URL, "key")},
		Interval: time.Minute, FullEvery: time.Hour, Log: slog.New(slog.DiscardHandler)}
	ctx := context.Background()

	sy.Once(ctx)
	_, total, err := st.Search(ctx, store.Query{})
	if err != nil || total != 231 {
		t.Fatalf("first sync: total=%d err=%v status=%+v", total, err, sy.Statuses())
	}
	if m, err := st.Maker(ctx, "smm1", "M1"); err != nil || m.Courses != 231 {
		t.Fatalf("maker: %+v %v", m, err)
	}
	if _, total, _ := st.Search(ctx, store.Query{Text: "doomed"}); total != 1 {
		t.Fatal("search before delete")
	}

	// Incremental cycle: a changed course is picked up; a vanished one stays until the full pass.
	f.Set(course("GONE2", "Fresh", time.Now().UTC()))
	f.Remove("GONE")
	sy.Once(ctx)
	if _, total, _ := st.Search(ctx, store.Query{Text: "fresh"}); total != 1 {
		t.Fatal("incremental missed the new course")
	}
	if _, err := st.Course(ctx, "smm1", "GONE"); err != nil {
		t.Fatalf("deleted before the full pass: %v", err)
	}

	// Full pass: the vanished course is soft-deleted.
	sy.FullEvery = 0
	sy.Once(ctx)
	if _, err := st.Course(ctx, "smm1", "GONE"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GONE should be soft-deleted, got %v", err)
	}
	if _, total, _ := st.Search(ctx, store.Query{Text: "doomed"}); total != 0 {
		t.Fatal("deleted course still searchable")
	}
	if _, total, _ := st.Search(ctx, store.Query{}); total != 231 { // 230 + GONE2
		t.Fatalf("total after full pass = %d", total)
	}
	if s := sy.Statuses(); len(s) != 1 || s[0].LastError != "" || s[0].LastOK.IsZero() {
		t.Fatalf("status: %+v", s)
	}
}

func TestSyncUpstreamDownKeepsData(t *testing.T) {
	st := testutil.DB(t)
	f := fakeupstream.New("")
	ts := httptest.NewServer(f)
	f.Set(course("ONE", "Keep me", time.Now().UTC()))
	sy := &syncer.Syncer{Store: st, Clients: []*upstream.Client{upstream.New("smm1", ts.URL, "")},
		Interval: time.Minute, FullEvery: 0, Log: slog.New(slog.DiscardHandler)}
	sy.Once(context.Background())
	ts.Close() // upstream gone: nothing may be deleted
	sy.Once(context.Background())
	if _, err := st.Course(context.Background(), "smm1", "ONE"); err != nil {
		t.Fatalf("course lost while upstream was down: %v", err)
	}
	if s := sy.Statuses(); s[0].LastError == "" {
		t.Fatal("error not recorded")
	}
}
