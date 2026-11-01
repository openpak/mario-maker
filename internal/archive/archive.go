// Package archive imports the History section's data: the 2018 scrape of Super Mario Maker
// (Wii U) course pages published by tachyo on archive.org (item tachyo_SMMData).
//
// The import reads a downloaded copy, either the item's .tar.gz or its SMM.db, and writes only
// to the archive_* tables. It is idempotent: running it again updates the rows in place and
// keeps takedowns and admin decisions. Country is deliberately never read.
package archive

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, no cgo

	"github.com/openpak/mario-maker/internal/store"
	"github.com/openpak/mario-maker/internal/wordfilter"
)

// Source facts, shown on the History pages and stored with the import.
const (
	SourceID    = "tachyo-2018"
	SourceTitle = "SMMData by tachyo (Super Mario Maker Bookmark scrape, 2015 to September 2018)"
	SourceURL   = "https://archive.org/details/tachyo_SMMData"
	Licence     = "Open Database License (ODbL) 1.0"
	LicenceNote = "The item's license.txt says the database is made available under the Open Database License but links the Database Contents License URL " +
		"(opendatacommons.org/licenses/dbcl/1.0/). OpenMaker treats it as the stricter ODbL: attribution, share-alike for a published derived database. " +
		"It covers the compilation only, not the makers' rights in their course names, and not Nintendo's rights."
)

// Options configures one import.
type Options struct {
	Filter *wordfilter.Filter // nil = the bundled lists
	Now    time.Time          // import date; zero = now
	Log    *slog.Logger
}

// Result counts one import.
type Result struct {
	Courses, Makers                 int
	FilteredCourses, FilteredMakers int
	Hidden                          struct{ Courses, Makers int } // not visible after the import (filter, takedown, override)
}

const batchSize = 1000

// Run imports from path into st. path is the item's tar.gz or an SMM.db file.
func Run(ctx context.Context, st *store.Store, srcPath string, opt Options) (Result, error) {
	var res Result
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	if opt.Filter == nil {
		opt.Filter = wordfilter.Default()
	}
	if opt.Now.IsZero() {
		opt.Now = time.Now()
	}
	dbPath, cleanup, err := openSource(srcPath)
	if err != nil {
		return res, err
	}
	defer cleanup()

	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		return res, err
	}
	defer db.Close()

	// Authors first. A course may name an author the authors table lacks; those get a maker row
	// carrying only the Network ID.
	known := map[string]bool{}
	var makers []store.ArchiveMaker
	flushMakers := func() error {
		if err := st.UpsertArchive(ctx, makers, nil); err != nil {
			return err
		}
		makers = makers[:0]
		return nil
	}
	addMaker := func(id, name string) error {
		h := hit(opt.Filter, name, id)
		makers = append(makers, store.ArchiveMaker{ID: id, Name: name, Source: SourceID, ImportedAt: opt.Now, FilterLang: h.Lang, FilterTerm: h.Term})
		res.Makers++
		if !h.OK() {
			res.FilteredMakers++
		}
		known[id] = true
		if len(makers) >= batchSize {
			return flushMakers()
		}
		return nil
	}
	// The country column is not selected: it is never read or stored.
	rows, err := db.QueryContext(ctx, `SELECT id, name FROM authors`)
	if err != nil {
		return res, fmt.Errorf("read authors: %w", err)
	}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return res, err
		}
		if id = strings.TrimSpace(id); id == "" {
			continue
		}
		if err := addMaker(id, strings.TrimSpace(name)); err != nil {
			rows.Close()
			return res, err
		}
	}
	if err := rows.Err(); err != nil {
		return res, err
	}
	rows.Close()

	var courses []store.ArchiveCourse
	var orphans = map[string]bool{}
	flushCourses := func() error {
		if err := st.UpsertArchive(ctx, nil, courses); err != nil {
			return err
		}
		courses = courses[:0]
		return nil
	}
	rows, err = db.QueryContext(ctx, `SELECT id, name, theme, created, difficulty, COALESCE(tag,''), liked, played, tries_taken, tries_success, clear_rate, COALESCE(author_id,'') FROM levels`)
	if err != nil {
		return res, fmt.Errorf("read levels: %w", err)
	}
	for rows.Next() {
		var c store.ArchiveCourse
		var created string
		if err := rows.Scan(&c.ID, &c.Name, &c.Theme, &created, &c.Difficulty, &c.Tag, &c.Stars, &c.Plays, &c.Attempts, &c.Clears, &c.ClearRate, &c.MakerID); err != nil {
			rows.Close()
			return res, err
		}
		c.ID, c.Name, c.MakerID = strings.TrimSpace(c.ID), strings.TrimSpace(c.Name), strings.TrimSpace(c.MakerID)
		if c.ID == "" {
			continue
		}
		c.Created = parseDate(created)
		c.Source, c.ImportedAt = SourceID, opt.Now
		h := hit(opt.Filter, c.Name, "")
		c.FilterLang, c.FilterTerm = h.Lang, h.Term
		if !h.OK() {
			res.FilteredCourses++
		}
		if c.MakerID != "" && !known[c.MakerID] {
			orphans[c.MakerID] = true
		}
		courses = append(courses, c)
		res.Courses++
		if len(courses) >= batchSize {
			if err := flushCourses(); err != nil {
				rows.Close()
				return res, err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return res, err
	}
	rows.Close()
	if err := flushCourses(); err != nil {
		return res, err
	}
	for id := range orphans {
		if err := addMaker(id, id); err != nil {
			return res, err
		}
	}
	if err := flushMakers(); err != nil {
		return res, err
	}

	if _, _, err := st.FinishArchiveImport(ctx, store.ArchiveSource{Source: SourceID, Title: SourceTitle, URL: SourceURL, Licence: Licence,
		LicenceNote: LicenceNote, ImportedAt: opt.Now}); err != nil {
		return res, err
	}
	stats, err := st.ArchiveStats(ctx)
	if err != nil {
		return res, err
	}
	res.Hidden.Courses, res.Hidden.Makers = stats.HiddenCourses, stats.HiddenMakers
	return res, nil
}

// hit checks a name (and optionally a Network ID) against the word lists.
func hit(f *wordfilter.Filter, name, id string) wordfilter.Hit {
	if h := f.Check(name); !h.OK() {
		return h
	}
	if id != "" {
		return f.Check(id)
	}
	return wordfilter.Hit{}
}

func parseDate(s string) time.Time {
	for _, layout := range []string{"2006-01-02 15:04", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t
		}
	}
	return time.Time{}
}

// openSource returns a path to the SQLite file: srcPath itself, or SMM.db extracted from a tar.gz
// into a private temp directory (TMPDIR) that cleanup removes.
func openSource(srcPath string) (string, func(), error) {
	noop := func() {}
	f, err := os.Open(srcPath)
	if err != nil {
		return "", noop, err
	}
	defer f.Close()
	magic := make([]byte, 2)
	if _, err := io.ReadFull(f, magic); err != nil {
		return "", noop, err
	}
	if magic[0] != 0x1f || magic[1] != 0x8b {
		return srcPath, noop, nil // a plain SQLite file
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", noop, err
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", noop, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return "", noop, errors.New("SMM.db not found in the archive")
		}
		if err != nil {
			return "", noop, err
		}
		if h.Typeflag != tar.TypeReg || path.Base(h.Name) != "SMM.db" {
			continue
		}
		dir, err := os.MkdirTemp("", "smm-archive-")
		if err != nil {
			return "", noop, err
		}
		cleanup := func() { _ = os.RemoveAll(dir) }
		out := dir + "/SMM.db"
		w, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			cleanup()
			return "", noop, err
		}
		if _, err := io.Copy(w, io.LimitReader(tr, 1<<30)); err != nil {
			w.Close()
			cleanup()
			return "", noop, err
		}
		if err := w.Close(); err != nil {
			cleanup()
			return "", noop, err
		}
		return out, cleanup, nil
	}
}
