package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openpak/mario-maker/internal/model"
	"github.com/openpak/mario-maker/internal/store"
	"github.com/openpak/mario-maker/internal/testutil"
)

func seed(t *testing.T, st *store.Store) {
	t.Helper()
	now := time.Now().UTC()
	cs := []model.Course{
		{Game: "smm1", ID: "AAAA000000000001", Name: "Sky Castle", Description: "climb the tower", MakerID: "M1", MakerName: "Mia", Tags: []string{"speedrun"}, Created: now.Add(-3 * time.Hour), Stars: 5, Plays: 100},
		{Game: "smm1", ID: "AAAA000000000002", Name: "Pipe Maze", MakerID: "M1", MakerName: "Mia", Tags: []string{"puzzle", "maze"}, Created: now.Add(-2 * time.Hour), Stars: 50, Plays: 10},
		{Game: "smm2", ID: "ABC-DEF-GH1", Name: "Ghost House Rave", MakerID: "M2", MakerName: "Zed", Tags: []string{"music"}, Created: now.Add(-1 * time.Hour), Stars: 20, Plays: 500},
		{Game: "smm2", ID: "ABC-DEF-GH2", Name: "", MakerID: "", Created: time.Time{}},
	}
	if err := st.UpsertCourses(context.Background(), cs); err != nil {
		t.Fatal(err)
	}
}

func ids(cs []model.Course) []string {
	var o []string
	for _, c := range cs {
		o = append(o, c.ID)
	}
	return o
}

func TestSearchAndSort(t *testing.T) {
	st := testutil.DB(t)
	seed(t, st)
	ctx := context.Background()

	check := func(q store.Query, wantTotal int, first string) {
		t.Helper()
		cs, total, err := st.Search(ctx, q)
		if err != nil || total != wantTotal {
			t.Fatalf("%+v: total=%d err=%v ids=%v", q, total, err, ids(cs))
		}
		if first != "" && (len(cs) == 0 || cs[0].ID != first) {
			t.Fatalf("%+v: first=%v want %s", q, ids(cs), first)
		}
	}
	check(store.Query{Sort: "newest"}, 4, "ABC-DEF-GH1")
	check(store.Query{Sort: "stars"}, 4, "AAAA000000000002")
	check(store.Query{Sort: "plays"}, 4, "ABC-DEF-GH1")
	check(store.Query{Game: "smm1", Sort: "newest"}, 2, "AAAA000000000002")
	check(store.Query{Text: "castle"}, 1, "AAAA000000000001")
	check(store.Query{Text: "cas"}, 1, "AAAA000000000001")    // prefix
	check(store.Query{Text: "tower"}, 1, "AAAA000000000001")  // description
	check(store.Query{Text: "maze"}, 1, "AAAA000000000002")   // tag and name
	check(store.Query{Text: "ghost rave"}, 1, "ABC-DEF-GH1")  // all words
	check(store.Query{Text: "zed"}, 1, "ABC-DEF-GH1")         // maker name
	check(store.Query{Text: "abc-def-gh2"}, 1, "ABC-DEF-GH2") // pasted course code
	check(store.Query{Text: "!!! ???"}, 0, "")                // no usable words
	check(store.Query{Text: "castle'; DROP TABLE courses;--"}, 0, "")
	check(store.Query{Maker: "M1"}, 2, "")
	cs, total, _ := st.Search(ctx, store.Query{Sort: "newest", Limit: 2, Offset: 2})
	if total != 4 || len(cs) != 2 {
		t.Fatalf("paging: total=%d n=%d", total, len(cs))
	}
}

func TestSoftDeleteAndRestore(t *testing.T) {
	st := testutil.DB(t)
	seed(t, st)
	ctx := context.Background()
	if err := st.SoftDelete(ctx, "smm1", "AAAA000000000001"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Course(ctx, "smm1", "AAAA000000000001"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted course visible: %v", err)
	}
	if _, total, _ := st.Search(ctx, store.Query{Text: "castle"}); total != 0 {
		t.Fatal("deleted course searchable")
	}
	if err := st.UpsertCourses(ctx, []model.Course{{Game: "smm1", ID: "AAAA000000000001", Name: "Sky Castle"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Course(ctx, "smm1", "AAAA000000000001"); err != nil {
		t.Fatalf("not restored: %v", err)
	}
}

func TestMaker(t *testing.T) {
	st := testutil.DB(t)
	seed(t, st)
	ctx := context.Background()
	// No maker row: totals come from the courses.
	m, err := st.Maker(ctx, "smm1", "M1")
	if err != nil || m.Courses != 2 || m.Stars != 55 || m.Name != "Mia" {
		t.Fatalf("derived: %+v %v", m, err)
	}
	_ = st.UpsertMakers(ctx, []model.Maker{{Game: "smm1", ID: "M1", Name: "Mia!", Courses: 9, Stars: 90, Plays: 900}})
	m, _ = st.Maker(ctx, "smm1", "M1")
	if m.Courses != 9 || m.Name != "Mia!" {
		t.Fatalf("row: %+v", m)
	}
	if _, err := st.Maker(ctx, "smm1", "nobody"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}
