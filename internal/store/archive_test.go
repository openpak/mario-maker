package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openpak/mario-maker/internal/store"
	"github.com/openpak/mario-maker/internal/testutil"
)

func seedArchive(t *testing.T, st *store.Store) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	ms := []store.ArchiveMaker{
		{ID: "Mia", Name: "Mia", Source: "t", ImportedAt: now},
		{ID: "bad", Name: "Rude", Source: "t", ImportedAt: now, FilterLang: "en", FilterTerm: "rude"},
		{ID: "Zed", Name: "Zed", Source: "t", ImportedAt: now},
	}
	cs := []store.ArchiveCourse{
		{ID: "1111-0000-0000-0001", Name: "Sky Castle", MakerID: "Mia", Theme: "mario_bros", Created: time.Date(2015, 9, 9, 0, 0, 0, 0, time.UTC), Stars: 50, Plays: 10, Attempts: 200, Clears: 20, ClearRate: 10, Source: "t", ImportedAt: now},
		{ID: "1111-0000-0000-0002", Name: "Water World", MakerID: "Mia", Theme: "mario_world", Created: time.Date(2016, 2, 2, 0, 0, 0, 0, time.UTC), Stars: 5, Plays: 500, Attempts: 200, Clears: 150, ClearRate: 75, Source: "t", ImportedAt: now},
		{ID: "1111-0000-0000-0003", Name: "Rude course", MakerID: "Zed", Theme: "mario_bros", Created: time.Date(2017, 1, 1, 0, 0, 0, 0, time.UTC), Stars: 1, Plays: 1, Attempts: 5, Clears: 5, ClearRate: 100, FilterLang: "en", FilterTerm: "rude", Source: "t", ImportedAt: now},
		{ID: "1111-0000-0000-0004", Name: "By a hidden maker", MakerID: "bad", Source: "t", ImportedAt: now},
	}
	if err := st.UpsertArchive(ctx, ms, cs); err != nil {
		t.Fatal(err)
	}
	if err := st.RefreshArchive(ctx, "", ""); err != nil {
		t.Fatal(err)
	}
}

func archIDs(t *testing.T, st *store.Store, q store.ArchiveQuery) []string {
	t.Helper()
	q.Limit = 50
	cs, total, err := st.SearchArchive(context.Background(), q)
	if err != nil || total != len(cs) {
		t.Fatalf("search: %v total %d len %d", err, total, len(cs))
	}
	var out []string
	for _, c := range cs {
		out = append(out, c.ID[len(c.ID)-1:])
	}
	return out
}

func sameIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestArchiveSearchAndSorts(t *testing.T) {
	st := testutil.DB(t)
	seedArchive(t, st)
	for name, c := range map[string]struct {
		q    store.ArchiveQuery
		want []string
	}{
		"stars (filtered rows are hidden)": {store.ArchiveQuery{Sort: "stars"}, []string{"1", "2"}},
		"plays":                            {store.ArchiveQuery{Sort: "plays"}, []string{"2", "1"}},
		"rate":                             {store.ArchiveQuery{Sort: "rate"}, []string{"2", "1"}},
		"hard":                             {store.ArchiveQuery{Sort: "hard"}, []string{"1", "2"}},
		"newest":                           {store.ArchiveQuery{Sort: "newest"}, []string{"2", "1"}},
		"text by name":                     {store.ArchiveQuery{Text: "castle"}, []string{"1"}},
		"text by maker":                    {store.ArchiveQuery{Text: "mia"}, []string{"1", "2"}},
		"text by code":                     {store.ArchiveQuery{Text: "0000-0000-0002"}, []string{"2"}},
		"text with like characters":        {store.ArchiveQuery{Text: "100%"}, nil},
		"theme":                            {store.ArchiveQuery{Theme: "mario_world"}, []string{"2"}},
		"year":                             {store.ArchiveQuery{Year: 2015}, []string{"1"}},
		"maker":                            {store.ArchiveQuery{Maker: "Mia", Sort: "plays"}, []string{"2", "1"}},
	} {
		if got := archIDs(t, st, c.q); !sameIDs(got, c.want) {
			t.Errorf("%s: got %v, want %v", name, got, c.want)
		}
	}
	if _, err := st.ArchiveMaker(context.Background(), "bad"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("filtered maker must be hidden: %v", err)
	}
	if m, err := st.ArchiveMaker(context.Background(), "mia"); err != nil || m.ID != "Mia" || m.Courses != 2 || m.Stars != 55 {
		t.Errorf("maker lookup is case-insensitive: %+v %v", m, err)
	}
}

func TestTakedownHidesAtOnceAndRestores(t *testing.T) {
	st := testutil.DB(t)
	seedArchive(t, st)
	ctx := context.Background()

	// A course takedown hides that course only, immediately (status pending).
	id, found, err := st.AddTakedown(ctx, store.Takedown{Kind: "course", Target: " 1111-0000-0000-0001 ", Name: "Mia", Contact: "mia@example.org", Reason: "mine"})
	if err != nil || !found {
		t.Fatalf("takedown: %v found=%v", err, found)
	}
	if got := archIDs(t, st, store.ArchiveQuery{Sort: "stars"}); !sameIDs(got, []string{"2"}) {
		t.Errorf("after course takedown: %v", got)
	}
	if _, err := st.ArchiveCourse(ctx, "1111-0000-0000-0001"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("detail must 404: %v", err)
	}
	list, _ := st.Takedowns(ctx, "", 10)
	if len(list) != 1 || list[0].Status != "pending" || list[0].TargetName != "Sky Castle" || list[0].Contact != "mia@example.org" {
		t.Errorf("stored request: %+v", list)
	}

	// Restoring after review brings it back; approving keeps it hidden.
	if err := st.DecideTakedown(ctx, id, "restored"); err != nil {
		t.Fatal(err)
	}
	if got := archIDs(t, st, store.ArchiveQuery{Sort: "stars"}); !sameIDs(got, []string{"1", "2"}) {
		t.Errorf("after restore: %v", got)
	}
	if err := st.DecideTakedown(ctx, id, "approved"); err != nil {
		t.Fatal(err)
	}
	if got := archIDs(t, st, store.ArchiveQuery{Sort: "stars"}); !sameIDs(got, []string{"2"}) {
		t.Errorf("after approve: %v", got)
	}
	// A full refresh (what an import does) keeps it hidden.
	_ = st.RefreshArchive(ctx, "", "")
	if got := archIDs(t, st, store.ArchiveQuery{Sort: "stars"}); !sameIDs(got, []string{"2"}) {
		t.Errorf("after refresh: %v", got)
	}

	// A maker takedown hides the maker and every course of theirs, by Network ID in any case.
	mid, _, err := st.AddTakedown(ctx, store.Takedown{Kind: "maker", Target: "MIA", Name: "Mia", Contact: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got := archIDs(t, st, store.ArchiveQuery{}); len(got) != 0 {
		t.Errorf("after maker takedown: %v", got)
	}
	if _, err := st.ArchiveMaker(ctx, "Mia"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("maker page must 404: %v", err)
	}
	// Restoring the maker brings back their courses except the one still under its own takedown.
	_ = st.DecideTakedown(ctx, mid, "restored")
	if got := archIDs(t, st, store.ArchiveQuery{Sort: "stars"}); !sameIDs(got, []string{"2"}) {
		t.Errorf("after maker restore: %v", got)
	}

	// A request for something not in the archive is stored but hides nothing.
	_, found, err = st.AddTakedown(ctx, store.Takedown{Kind: "maker", Target: "nobody", Name: "N", Contact: "c"})
	if err != nil || found {
		t.Errorf("unknown target: found=%v err=%v", found, err)
	}
}

func TestOverrideBeatsFilterButNotTakedown(t *testing.T) {
	st := testutil.DB(t)
	seedArchive(t, st)
	ctx := context.Background()
	if err := st.SetOverride(ctx, "course", "1111-0000-0000-0003", "show"); err != nil {
		t.Fatal(err)
	}
	if got := archIDs(t, st, store.ArchiveQuery{Sort: "stars"}); !sameIDs(got, []string{"1", "2", "3"}) {
		t.Errorf("show override: %v", got)
	}
	if err := st.SetOverride(ctx, "course", "1111-0000-0000-0001", "hide"); err != nil {
		t.Fatal(err)
	}
	if got := archIDs(t, st, store.ArchiveQuery{Sort: "stars"}); !sameIDs(got, []string{"2", "3"}) {
		t.Errorf("hide override: %v", got)
	}
	if _, _, err := st.AddTakedown(ctx, store.Takedown{Kind: "course", Target: "1111-0000-0000-0003", Name: "n", Contact: "c"}); err != nil {
		t.Fatal(err)
	}
	if got := archIDs(t, st, store.ArchiveQuery{Sort: "stars"}); !sameIDs(got, []string{"2"}) {
		t.Errorf("takedown beats show: %v", got)
	}
	// Showing a filtered maker brings back its courses.
	if err := st.SetOverride(ctx, "maker", "BAD", "show"); err != nil {
		t.Fatal(err)
	}
	if got := archIDs(t, st, store.ArchiveQuery{Text: "hidden maker"}); !sameIDs(got, []string{"4"}) {
		t.Errorf("maker show: %v", got)
	}
	if o, err := st.Overrides(ctx); err != nil || len(o) != 3 {
		t.Errorf("override list: %+v %v", o, err)
	}
	if err := st.SetOverride(ctx, "course", "nope", "show"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown override target: %v", err)
	}
}

func TestClaims(t *testing.T) {
	st := testutil.DB(t)
	seedArchive(t, st)
	ctx := context.Background()
	k, err := st.AddClaim(ctx, store.ArchiveClaim{MakerID: "Mia", AccountID: "a1", AccountName: "Tobi", Status: "pending"})
	if err != nil || k.Status != "pending" {
		t.Fatalf("claim: %+v %v", k, err)
	}
	if _, err := st.ApprovedClaim(ctx, "Mia"); !errors.Is(err, store.ErrNotFound) {
		t.Error("pending is not Claimed by")
	}
	// the same account claiming again changes nothing
	if again, _ := st.AddClaim(ctx, store.ArchiveClaim{MakerID: "Mia", AccountID: "a1", Status: "approved"}); again.ID != k.ID || again.Status != "pending" {
		t.Errorf("repeat claim: %+v", again)
	}
	if err := st.DecideClaim(ctx, k.ID, "approved"); err != nil {
		t.Fatal(err)
	}
	m, err := st.ArchiveMaker(ctx, "Mia")
	if err != nil || m.Claim == nil || m.Claim.AccountName != "Tobi" {
		t.Errorf("maker claim: %+v %v", m.Claim, err)
	}
	c, _ := st.ArchiveCourse(ctx, "1111-0000-0000-0001")
	if c.MakerClaim != "Tobi" {
		t.Errorf("course shows claimant: %q", c.MakerClaim)
	}
	if _, err := st.AddClaim(ctx, store.ArchiveClaim{MakerID: "Mia", AccountID: "a2", Status: "approved", Via: "link"}); !errors.Is(err, store.ErrClaimTaken) {
		t.Errorf("second approved claim: %v", err)
	}
	if _, err := st.AddClaim(ctx, store.ArchiveClaim{MakerID: "Mia", AccountID: "a2", Status: "pending"}); !errors.Is(err, store.ErrClaimTaken) {
		t.Errorf("claim on a claimed maker: %v", err)
	}
}
