package archive_test

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/openpak/mario-maker/internal/archive"
	"github.com/openpak/mario-maker/internal/store"
	"github.com/openpak/mario-maker/internal/testutil"
)

// fixture writes a small SMM.db shaped like tachyo's: authors carry a country that must never be imported.
func fixture(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "SMM.db")
	db, err := sql.Open("sqlite", p)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, q := range []string{
		"CREATE TABLE authors (id TEXT NOT NULL PRIMARY KEY, name TEXT NOT NULL, medals INTEGER NOT NULL, country TEXT NOT NULL)",
		`CREATE TABLE levels (id TEXT NOT NULL PRIMARY KEY, name TEXT NOT NULL, theme TEXT NOT NULL, created TEXT NOT NULL, difficulty TEXT NOT NULL, tag TEXT,
		  liked INTEGER NOT NULL, played INTEGER NOT NULL, shared INTEGER NOT NULL, clear_rate REAL NOT NULL, tries_taken INTEGER NOT NULL, tries_success INTEGER NOT NULL, author_id TEXT)`,
		"INSERT INTO authors VALUES ('mia', 'Mia', 3, 'JP'), ('rude', 'Hitler Fan', 0, 'US'), ('plain', 'ふつう', 1, 'DE')",
		`INSERT INTO levels VALUES
		 ('AAAA-0000-0000-0001', 'Sky Castle', 'mario_bros', '2015-09-17 00:00', 'Easy', 'Speedrun', 1200, 5000, 0, 40.5, 1000, 405, 'mia'),
		 ('AAAA-0000-0000-0002', 'Golden Shower', 'mario_world', '2016-01-02 10:30', 'Expert', NULL, 10, 20, 0, 10, 30, 3, 'mia'),
		 ('AAAA-0000-0000-0003', 'Fine course', 'mario_bros3', '2017-05-05 00:00', 'Normal', '', 7, 8, 0, 50, 10, 5, 'rude'),
		 ('AAAA-0000-0000-0004', 'Orphan', 'mario_bros_u', '2018-09-14 21:32', 'Normal', '', 1, 1, 0, 0, 1, 0, 'ghost'),
		 ('AAAA-0000-0000-0005', 'No maker at all', 'mario_bros', '2018-01-01 00:00', 'Easy', '', 2, 2, 0, 0, 0, 0, NULL)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func visibleIDs(t *testing.T, st *store.Store) map[string]bool {
	t.Helper()
	cs, _, err := st.SearchArchive(context.Background(), store.ArchiveQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, c := range cs {
		out[c.ID] = true
	}
	return out
}

func TestImportIsIdempotentAndFilters(t *testing.T) {
	st := testutil.DB(t)
	ctx := context.Background()
	db := fixture(t)
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

	res, err := archive.Run(ctx, st, db, archive.Options{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	// 3 authors + the orphan author 'ghost' that only a course names
	if res.Courses != 5 || res.Makers != 4 {
		t.Errorf("counts: %+v", res)
	}
	if res.FilteredCourses != 1 || res.FilteredMakers != 1 {
		t.Errorf("filtered: %+v", res)
	}
	// 'Golden Shower' hidden by name, 'Fine course' hidden because its maker's name matched (hitler)
	got := visibleIDs(t, st)
	if len(got) != 3 || !got["AAAA-0000-0000-0001"] || !got["AAAA-0000-0000-0004"] || !got["AAAA-0000-0000-0005"] {
		t.Errorf("visible after import: %v", got)
	}
	if res.Hidden.Courses != 2 || res.Hidden.Makers != 1 {
		t.Errorf("hidden: %+v", res.Hidden)
	}
	// hidden, not deleted
	if s, err := st.ArchiveStats(ctx); err != nil || s.Courses != 5 || s.Makers != 4 {
		t.Errorf("rows must stay: %+v %v", s, err)
	}
	c, err := st.ArchiveCourse(ctx, "AAAA-0000-0000-0001")
	if err != nil || c.Stars != 1200 || c.Plays != 5000 || c.Attempts != 1000 || c.Clears != 405 || c.ClearRate != 40.5 || c.MakerName != "Mia" ||
		c.Tag != "Speedrun" || c.Created.Format("2006-01-02") != "2015-09-17" || c.Source != archive.SourceID || !c.ImportedAt.Equal(now) {
		t.Errorf("course: %+v %v", c, err)
	}
	if _, err := st.ArchiveCourse(ctx, "AAAA-0000-0000-0002"); err != store.ErrNotFound {
		t.Errorf("filtered course must not be served: %v", err)
	}
	sum, err := st.ArchiveSummary(ctx)
	if err != nil || !sum.HasSource || sum.Source.Licence != archive.Licence || sum.Source.URL != archive.SourceURL || !sum.Source.ImportedAt.Equal(now) || sum.Courses != 3 {
		t.Errorf("summary: %+v %v", sum, err)
	}

	// A takedown and an admin override survive a second import; the counts do not change.
	if _, _, err := st.AddTakedown(ctx, store.Takedown{Kind: "course", Target: "aaaa-0000-0000-0001", Name: "Mia", Contact: "m@example.org"}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetOverride(ctx, "course", "AAAA-0000-0000-0002", "show"); err != nil {
		t.Fatal(err)
	}
	res2, err := archive.Run(ctx, st, db, archive.Options{Now: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if res2.Courses != res.Courses || res2.Makers != res.Makers || res2.FilteredCourses != res.FilteredCourses {
		t.Errorf("second import differs: %+v vs %+v", res2, res)
	}
	got = visibleIDs(t, st)
	if got["AAAA-0000-0000-0001"] || !got["AAAA-0000-0000-0002"] || len(got) != 3 {
		t.Errorf("after re-import (takedown kept, override kept): %v", got)
	}
	if s, _ := st.ArchiveStats(ctx); s.Courses != 5 || s.Makers != 4 {
		t.Errorf("re-import duplicated rows: %+v", s)
	}
}

func TestImportReadsTarGz(t *testing.T) {
	st := testutil.DB(t)
	db := fixture(t)
	b, err := os.ReadFile(db)
	if err != nil {
		t.Fatal(err)
	}
	tgz := filepath.Join(t.TempDir(), "SMMData.tar.gz")
	f, err := os.Create(tgz)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "SMMData-abc/SMM.db", Mode: 0o644, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	_, _ = tw.Write(b)
	tw.Close()
	gz.Close()
	f.Close()
	res, err := archive.Run(context.Background(), st, tgz, archive.Options{})
	if err != nil || res.Courses != 5 {
		t.Fatalf("tar.gz import: %+v %v", res, err)
	}
}

func TestNoCountryColumn(t *testing.T) {
	st := testutil.DB(t)
	if _, err := archive.Run(context.Background(), st, fixture(t), archive.Options{}); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"archive_makers", "archive_courses", "archive_sources", "archive_takedowns", "archive_claims"} {
		cols, err := st.ColumnNames(context.Background(), tbl)
		if err != nil || len(cols) == 0 {
			t.Fatalf("%s: %v %v", tbl, cols, err)
		}
		for _, c := range cols {
			if strings.Contains(c, "country") || strings.Contains(c, "region") {
				t.Errorf("%s has column %s", tbl, c)
			}
		}
	}
}
