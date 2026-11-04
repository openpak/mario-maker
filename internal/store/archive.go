package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

// The archive_* tables hold the History section: archived Wii U course details imported from a
// third-party scrape. They never mix with the live courses and makers above. Country is not
// imported and has no column.
const archiveSchema = `
CREATE TABLE IF NOT EXISTS archive_sources (
  source        text PRIMARY KEY,
  title         text        NOT NULL,
  url           text        NOT NULL,
  licence       text        NOT NULL,
  licence_note  text        NOT NULL DEFAULT '',
  imported_at   timestamptz NOT NULL,
  courses       integer     NOT NULL DEFAULT 0,
  makers        integer     NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS archive_makers (
  maker_id    text PRIMARY KEY,
  name        text        NOT NULL DEFAULT '',
  source      text        NOT NULL,
  imported_at timestamptz NOT NULL,
  filter_lang text        NOT NULL DEFAULT '',
  filter_term text        NOT NULL DEFAULT '',
  taken_down  boolean     NOT NULL DEFAULT false,
  override    text        NOT NULL DEFAULT '',
  visible     boolean     NOT NULL DEFAULT true
);
-- The uploader's Wii U PID, 0 when the archive does not record it. Only a PID can prove a claim.
ALTER TABLE archive_makers ADD COLUMN IF NOT EXISTS pid bigint NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS archive_makers_lower_idx ON archive_makers (lower(maker_id));
CREATE TABLE IF NOT EXISTS archive_courses (
  course_id   text PRIMARY KEY,
  name        text        NOT NULL DEFAULT '',
  maker_id    text        NOT NULL DEFAULT '',
  theme       text        NOT NULL DEFAULT '',
  difficulty  text        NOT NULL DEFAULT '',
  tag         text        NOT NULL DEFAULT '',
  created     date,
  stars       integer     NOT NULL DEFAULT 0,
  plays       integer     NOT NULL DEFAULT 0,
  attempts    integer     NOT NULL DEFAULT 0,
  clears      integer     NOT NULL DEFAULT 0,
  clear_rate  real        NOT NULL DEFAULT 0,
  source      text        NOT NULL,
  imported_at timestamptz NOT NULL,
  filter_lang text        NOT NULL DEFAULT '',
  filter_term text        NOT NULL DEFAULT '',
  taken_down  boolean     NOT NULL DEFAULT false,
  override    text        NOT NULL DEFAULT '',
  visible     boolean     NOT NULL DEFAULT true
);
CREATE INDEX IF NOT EXISTS archive_courses_stars_idx ON archive_courses (stars DESC) WHERE visible;
CREATE INDEX IF NOT EXISTS archive_courses_plays_idx ON archive_courses (plays DESC) WHERE visible;
CREATE INDEX IF NOT EXISTS archive_courses_rate_idx ON archive_courses (clear_rate DESC) WHERE visible AND attempts >= 100;
CREATE INDEX IF NOT EXISTS archive_courses_created_idx ON archive_courses (created DESC) WHERE visible;
CREATE INDEX IF NOT EXISTS archive_courses_maker_idx ON archive_courses (maker_id) WHERE visible;
CREATE INDEX IF NOT EXISTS archive_courses_maker_lower_idx ON archive_courses (lower(maker_id));
CREATE TABLE IF NOT EXISTS archive_takedowns (
  id         bigserial PRIMARY KEY,
  kind       text        NOT NULL,
  target     text        NOT NULL,
  name       text        NOT NULL,
  contact    text        NOT NULL,
  reason     text        NOT NULL DEFAULT '',
  status     text        NOT NULL DEFAULT 'pending',
  created_at timestamptz NOT NULL DEFAULT now(),
  decided_at timestamptz
);
CREATE INDEX IF NOT EXISTS archive_takedowns_target_idx ON archive_takedowns (kind, target);
CREATE TABLE IF NOT EXISTS archive_claims (
  id          bigserial PRIMARY KEY,
  maker_id    text        NOT NULL,
  account_id  text        NOT NULL,
  account_name text       NOT NULL DEFAULT '',
  wiiu_pid    bigint      NOT NULL DEFAULT 0,
  linked_nnid text        NOT NULL DEFAULT '',
  status      text        NOT NULL DEFAULT 'pending',
  via         text        NOT NULL DEFAULT 'moderator',
  note        text        NOT NULL DEFAULT '',
  created_at  timestamptz NOT NULL DEFAULT now(),
  decided_at  timestamptz,
  UNIQUE (maker_id, account_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS archive_claims_one_approved ON archive_claims (maker_id) WHERE status = 'approved';
`

// ArchiveSource describes where the archived rows came from.
type ArchiveSource struct {
	Source      string
	Title       string
	URL         string
	Licence     string
	LicenceNote string
	ImportedAt  time.Time
	Courses     int
	Makers      int
}

// ArchiveMaker is one archived maker (a Wii U Network ID and its display name).
type ArchiveMaker struct {
	ID, Name, Source       string
	ImportedAt             time.Time
	FilterLang, FilterTerm string
	Override               string
	Visible                bool
	PID                    int64 // the uploader's Wii U PID; 0 when the archive does not record it
	Courses, Stars, Plays  int   // filled on read
	Claim                  *ArchiveClaim
}

// ArchiveCourse is one archived course.
type ArchiveCourse struct {
	ID, Name, MakerID, MakerName, Theme, Difficulty, Tag, Source string
	Created                                                      time.Time
	Stars, Plays, Attempts, Clears                               int
	ClearRate                                                    float64
	ImportedAt                                                   time.Time
	FilterLang, FilterTerm                                       string
	Override                                                     string
	Visible                                                      bool
	MakerClaim                                                   string // display name of the approved claimant
}

// Takedown is one removal request.
type Takedown struct {
	ID                                          int64
	Kind, Target, Name, Contact, Reason, Status string
	CreatedAt                                   time.Time
	DecidedAt                                   *time.Time
	TargetName                                  string // the course title or maker name, for moderators
}

// ArchiveClaim is a maker claim by an OpenPak account.
type ArchiveClaim struct {
	ID                              int64
	MakerID, AccountID, AccountName string
	WiiUPID                         int64
	LinkedNNID, Status, Via, Note   string
	CreatedAt                       time.Time
	DecidedAt                       *time.Time
}

// ArchiveQuery selects archived courses.
type ArchiveQuery struct {
	Text   string
	Theme  string
	Year   int
	Maker  string // exact Network ID
	Sort   string // newest | stars | plays | rate | hard
	Limit  int
	Offset int
}

// ArchiveSorts are the accepted sort keys.
var ArchiveSorts = []string{"stars", "plays", "rate", "hard", "newest"}

// NormArchiveSort maps unknown sorts to stars.
func NormArchiveSort(s string) string {
	for _, k := range ArchiveSorts {
		if s == k {
			return k
		}
	}
	return "stars"
}

// ArchiveThemes are the game styles in the data, in display order.
var ArchiveThemes = []string{"mario_bros", "mario_bros3", "mario_world", "mario_bros_u"}

// NormTheme returns t when it is a known theme, else "".
func NormTheme(t string) string {
	for _, k := range ArchiveThemes {
		if t == k {
			return k
		}
	}
	return ""
}

// NormKind returns "course" or "maker", else "".
func NormKind(k string) string {
	if k == "course" || k == "maker" {
		return k
	}
	return ""
}

// TakedownTarget normalises the identifier a takedown names: course codes are upper case, Network IDs lower case.
func TakedownTarget(kind, id string) string {
	id = strings.TrimSpace(id)
	if kind == "course" {
		return strings.ToUpper(id)
	}
	return strings.ToLower(id)
}

// flagsOK is the moderation rule for one row: a takedown wins; an admin override beats the word filter.
const flagsOK = `(NOT %[1]staken_down AND %[1]soverride <> 'hide' AND (%[1]soverride = 'show' OR %[1]sfilter_term = ''))`

// UpsertArchive stores archived rows. Source fields are replaced; moderation fields (taken_down,
// override) are kept, so importing again never undoes a takedown or an admin decision.
func (s *Store) UpsertArchive(ctx context.Context, makers []ArchiveMaker, courses []ArchiveCourse) error {
	b := &pgx.Batch{}
	for _, m := range makers {
		b.Queue(`INSERT INTO archive_makers (maker_id, name, source, imported_at, filter_lang, filter_term)
VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (maker_id) DO UPDATE SET name=EXCLUDED.name, source=EXCLUDED.source, imported_at=EXCLUDED.imported_at,
  filter_lang=EXCLUDED.filter_lang, filter_term=EXCLUDED.filter_term`,
			m.ID, m.Name, m.Source, m.ImportedAt, m.FilterLang, m.FilterTerm)
	}
	for _, c := range courses {
		b.Queue(`INSERT INTO archive_courses (course_id, name, maker_id, theme, difficulty, tag, created, stars, plays, attempts, clears,
  clear_rate, source, imported_at, filter_lang, filter_term)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
ON CONFLICT (course_id) DO UPDATE SET name=EXCLUDED.name, maker_id=EXCLUDED.maker_id, theme=EXCLUDED.theme,
  difficulty=EXCLUDED.difficulty, tag=EXCLUDED.tag, created=EXCLUDED.created, stars=EXCLUDED.stars, plays=EXCLUDED.plays,
  attempts=EXCLUDED.attempts, clears=EXCLUDED.clears, clear_rate=EXCLUDED.clear_rate, source=EXCLUDED.source,
  imported_at=EXCLUDED.imported_at, filter_lang=EXCLUDED.filter_lang, filter_term=EXCLUDED.filter_term`,
			c.ID, c.Name, c.MakerID, c.Theme, c.Difficulty, c.Tag, nullTime(c.Created), c.Stars, c.Plays, c.Attempts, c.Clears,
			c.ClearRate, c.Source, c.ImportedAt, c.FilterLang, c.FilterTerm)
	}
	res := s.pool.SendBatch(ctx, b)
	defer res.Close()
	for i := 0; i < len(makers)+len(courses); i++ {
		if _, err := res.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// FinishArchiveImport records the source and recomputes every row's visibility.
func (s *Store) FinishArchiveImport(ctx context.Context, src ArchiveSource) (courses, makers int, err error) {
	if err = s.RefreshArchive(ctx, "", ""); err != nil {
		return
	}
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM archive_courses WHERE source=$1`, src.Source).Scan(&courses); err != nil {
		return
	}
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM archive_makers WHERE source=$1`, src.Source).Scan(&makers); err != nil {
		return
	}
	_, err = s.pool.Exec(ctx, `INSERT INTO archive_sources (source, title, url, licence, licence_note, imported_at, courses, makers)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (source) DO UPDATE SET title=EXCLUDED.title, url=EXCLUDED.url, licence=EXCLUDED.licence, licence_note=EXCLUDED.licence_note,
  imported_at=EXCLUDED.imported_at, courses=EXCLUDED.courses, makers=EXCLUDED.makers`,
		src.Source, src.Title, src.URL, src.Licence, src.LicenceNote, src.ImportedAt, courses, makers)
	s.sum.reset()
	return
}

// RefreshArchive recomputes taken_down and visible for everything (kind ""), or only for the
// course or maker a takedown names.
func (s *Store) RefreshArchive(ctx context.Context, kind, target string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	mw, cw := "true", "true"
	var args []any
	switch kind {
	case "maker":
		mw, cw, args = "lower(m.maker_id) = $1", "lower(c.maker_id) = $1", []any{target}
	case "course":
		mw, cw, args = "false", "upper(c.course_id) = $1", []any{target}
	}
	down := `EXISTS (SELECT 1 FROM archive_takedowns t WHERE t.status IN ('pending','approved') AND t.kind='%s' AND t.target=%s)`
	// set updates only the rows whose value changes, so a full pass after an import rewrites nothing needlessly.
	set := func(table, alias, col, expr, where string) error {
		_, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s %s SET %s = %s WHERE %s AND %s.%s IS DISTINCT FROM (%s)`, table, alias, col, expr, where, alias, col, expr), args...)
		return err
	}
	if kind != "course" {
		if err = set("archive_makers", "m", "taken_down", fmt.Sprintf(down, "maker", "lower(m.maker_id)"), mw); err != nil {
			return err
		}
		if err = set("archive_makers", "m", "visible", fmt.Sprintf(flagsOK, "m."), mw); err != nil {
			return err
		}
	}
	if err = set("archive_courses", "c", "taken_down", fmt.Sprintf(down, "course", "upper(c.course_id)")+" OR "+fmt.Sprintf(down, "maker", "lower(c.maker_id)"), cw); err != nil {
		return err
	}
	if err = set("archive_courses", "c", "visible", fmt.Sprintf(flagsOK, "c.")+` AND COALESCE((SELECT m.visible FROM archive_makers m WHERE m.maker_id = c.maker_id), true)`, cw); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	s.sum.reset()
	return nil
}

const archiveCourseCols = `c.course_id, c.name, c.maker_id, COALESCE(m.name,''), c.theme, c.difficulty, c.tag, c.created, c.stars, c.plays,
  c.attempts, c.clears, c.clear_rate, c.source, c.imported_at, c.filter_lang, c.filter_term, c.override, c.visible,
  COALESCE((SELECT k.account_name FROM archive_claims k WHERE k.maker_id=c.maker_id AND k.status='approved'),'')`

func scanArchiveCourse(row pgx.Row) (ArchiveCourse, error) {
	var c ArchiveCourse
	var created *time.Time
	err := row.Scan(&c.ID, &c.Name, &c.MakerID, &c.MakerName, &c.Theme, &c.Difficulty, &c.Tag, &created, &c.Stars, &c.Plays,
		&c.Attempts, &c.Clears, &c.ClearRate, &c.Source, &c.ImportedAt, &c.FilterLang, &c.FilterTerm, &c.Override, &c.Visible, &c.MakerClaim)
	if created != nil {
		c.Created = *created
	}
	return c, err
}

// ArchiveCourse returns one visible archived course.
func (s *Store) ArchiveCourse(ctx context.Context, id string) (ArchiveCourse, error) {
	c, err := scanArchiveCourse(s.pool.QueryRow(ctx, `SELECT `+archiveCourseCols+` FROM archive_courses c
LEFT JOIN archive_makers m ON m.maker_id=c.maker_id WHERE upper(c.course_id)=upper($1) AND c.visible`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// ArchiveMaker returns one visible archived maker with totals over the visible courses.
func (s *Store) ArchiveMaker(ctx context.Context, id string) (ArchiveMaker, error) {
	var m ArchiveMaker
	err := s.pool.QueryRow(ctx, `SELECT m.maker_id, m.name, m.source, m.imported_at, m.filter_lang, m.filter_term, m.override, m.visible, m.pid,
  (SELECT count(*) FROM archive_courses c WHERE c.maker_id=m.maker_id AND c.visible),
  (SELECT COALESCE(sum(c.stars),0) FROM archive_courses c WHERE c.maker_id=m.maker_id AND c.visible),
  (SELECT COALESCE(sum(c.plays),0) FROM archive_courses c WHERE c.maker_id=m.maker_id AND c.visible)
FROM archive_makers m WHERE lower(m.maker_id)=lower($1) AND m.visible`, id).
		Scan(&m.ID, &m.Name, &m.Source, &m.ImportedAt, &m.FilterLang, &m.FilterTerm, &m.Override, &m.Visible, &m.PID, &m.Courses, &m.Stars, &m.Plays)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	if k, err := s.ApprovedClaim(ctx, m.ID); err == nil {
		m.Claim = &k
	}
	return m, nil
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// SearchArchive returns one page of visible archived courses and the total match count.
func (s *Store) SearchArchive(ctx context.Context, q ArchiveQuery) ([]ArchiveCourse, int, error) {
	where := []string{"c.visible"}
	var args []any
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	if t := strings.TrimSpace(q.Text); t != "" {
		p := arg("%" + escapeLike(t) + "%")
		where = append(where, fmt.Sprintf("(c.name ILIKE %[1]s OR m.name ILIKE %[1]s OR c.maker_id ILIKE %[1]s OR c.course_id ILIKE %[1]s)", p))
	}
	if q.Theme != "" {
		where = append(where, "c.theme="+arg(q.Theme))
	}
	if q.Year > 0 {
		where = append(where, "c.created >= "+arg(time.Date(q.Year, 1, 1, 0, 0, 0, 0, time.UTC))+" AND c.created < "+arg(time.Date(q.Year+1, 1, 1, 0, 0, 0, 0, time.UTC)))
	}
	if q.Maker != "" {
		where = append(where, "c.maker_id="+arg(q.Maker))
	}
	order := "c.stars DESC"
	switch q.Sort {
	case "plays":
		order = "c.plays DESC"
	case "newest":
		order = "c.created DESC NULLS LAST"
	case "rate":
		where = append(where, "c.attempts >= 100")
		order = "c.clear_rate DESC, c.attempts DESC"
	case "hard":
		where = append(where, "c.attempts >= 100")
		order = "c.clear_rate ASC, c.attempts DESC"
	}
	w := strings.Join(where, " AND ")
	from := ` FROM archive_courses c LEFT JOIN archive_makers m ON m.maker_id=c.maker_id WHERE ` + w
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*)`+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 24
	}
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`SELECT %s%s ORDER BY %s, c.course_id LIMIT %s OFFSET %s`,
		archiveCourseCols, from, order, arg(limit), arg(max(q.Offset, 0))), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []ArchiveCourse
	for rows.Next() {
		c, err := scanArchiveCourse(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// ArchiveSummary is what the History landing page shows about the archive as a whole.
type ArchiveSummary struct {
	Courses, Makers int
	Years           []int
	Source          ArchiveSource
	HasSource       bool
}

type summaryCache struct {
	mu   sync.Mutex
	at   time.Time
	data ArchiveSummary
}

func (c *summaryCache) reset() { c.mu.Lock(); c.at = time.Time{}; c.mu.Unlock() }

// Summary counts the visible archive (cached for a few minutes; takedowns and imports reset it).
func (s *Store) ArchiveSummary(ctx context.Context) (ArchiveSummary, error) {
	s.sum.mu.Lock()
	defer s.sum.mu.Unlock()
	if time.Since(s.sum.at) < 5*time.Minute && !s.sum.at.IsZero() {
		return s.sum.data, nil
	}
	var d ArchiveSummary
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM archive_courses WHERE visible`).Scan(&d.Courses); err != nil {
		return d, err
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM archive_makers m WHERE visible AND EXISTS (SELECT 1 FROM archive_courses c WHERE c.maker_id=m.maker_id AND c.visible)`).Scan(&d.Makers); err != nil {
		return d, err
	}
	rows, err := s.pool.Query(ctx, `SELECT DISTINCT extract(year FROM created)::int FROM archive_courses WHERE visible AND created IS NOT NULL ORDER BY 1`)
	if err != nil {
		return d, err
	}
	for rows.Next() {
		var y int
		if err := rows.Scan(&y); err != nil {
			rows.Close()
			return d, err
		}
		d.Years = append(d.Years, y)
	}
	rows.Close()
	err = s.pool.QueryRow(ctx, `SELECT source, title, url, licence, licence_note, imported_at, courses, makers FROM archive_sources ORDER BY imported_at DESC LIMIT 1`).
		Scan(&d.Source.Source, &d.Source.Title, &d.Source.URL, &d.Source.Licence, &d.Source.LicenceNote, &d.Source.ImportedAt, &d.Source.Courses, &d.Source.Makers)
	d.HasSource = err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return d, err
	}
	s.sum.at, s.sum.data = time.Now(), d
	return d, nil
}

// AddTakedown stores a request and hides its target at once. found says whether the target exists.
func (s *Store) AddTakedown(ctx context.Context, t Takedown) (id int64, found bool, err error) {
	t.Target = TakedownTarget(t.Kind, t.Target)
	if err = s.pool.QueryRow(ctx, `INSERT INTO archive_takedowns (kind, target, name, contact, reason) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		t.Kind, t.Target, t.Name, t.Contact, t.Reason).Scan(&id); err != nil {
		return
	}
	if err = s.RefreshArchive(ctx, t.Kind, t.Target); err != nil {
		return
	}
	found, err = s.archiveTargetExists(ctx, t.Kind, t.Target)
	return
}

func (s *Store) archiveTargetExists(ctx context.Context, kind, target string) (bool, error) {
	q := `SELECT EXISTS (SELECT 1 FROM archive_courses WHERE upper(course_id)=$1)`
	if kind == "maker" {
		q = `SELECT EXISTS (SELECT 1 FROM archive_makers WHERE lower(maker_id)=$1)`
	}
	var ok bool
	err := s.pool.QueryRow(ctx, q, target).Scan(&ok)
	return ok, err
}

// Takedowns lists requests, pending first and newest first. status "" lists all.
func (s *Store) Takedowns(ctx context.Context, status string, limit int) ([]Takedown, error) {
	rows, err := s.pool.Query(ctx, `SELECT t.id, t.kind, t.target, t.name, t.contact, t.reason, t.status, t.created_at, t.decided_at,
  COALESCE(CASE t.kind WHEN 'course' THEN (SELECT c.name FROM archive_courses c WHERE upper(c.course_id)=t.target LIMIT 1)
                       ELSE (SELECT m.name FROM archive_makers m WHERE lower(m.maker_id)=t.target LIMIT 1) END, '')
FROM archive_takedowns t WHERE ($1='' OR t.status=$1) ORDER BY (t.status='pending') DESC, t.created_at DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Takedown
	for rows.Next() {
		var t Takedown
		if err := rows.Scan(&t.ID, &t.Kind, &t.Target, &t.Name, &t.Contact, &t.Reason, &t.Status, &t.CreatedAt, &t.DecidedAt, &t.TargetName); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DecideTakedown sets a request to "approved" (stays hidden for good), "restored" (visible again) or back to "pending".
func (s *Store) DecideTakedown(ctx context.Context, id int64, status string) error {
	if status != "approved" && status != "restored" && status != "pending" {
		return fmt.Errorf("store: bad takedown status %q", status)
	}
	var kind, target string
	err := s.pool.QueryRow(ctx, `UPDATE archive_takedowns SET status=$2, decided_at=now() WHERE id=$1 RETURNING kind, target`, id, status).Scan(&kind, &target)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return s.RefreshArchive(ctx, kind, target)
}

// SetOverride sets the admin decision for a course or maker: "show" (beats the word filter),
// "hide", or "" (back to the filter). The takedown rule still wins over "show".
func (s *Store) SetOverride(ctx context.Context, kind, id, mode string) error {
	if mode != "" && mode != "show" && mode != "hide" {
		return fmt.Errorf("store: bad override %q", mode)
	}
	table, col, norm := "archive_courses", "course_id", "upper"
	if kind == "maker" {
		table, col, norm = "archive_makers", "maker_id", "lower"
	} else if kind != "course" {
		return fmt.Errorf("store: bad kind %q", kind)
	}
	tag, err := s.pool.Exec(ctx, fmt.Sprintf(`UPDATE %s SET override=$2 WHERE %s(%s)=%s($1)`, table, norm, col, norm), id, mode)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return s.RefreshArchive(ctx, kind, TakedownTarget(kind, id))
}

// Override is one row on the admin override list.
type Override struct{ Kind, ID, Name, Mode string }

// Overrides lists courses and makers with an admin decision.
func (s *Store) Overrides(ctx context.Context) ([]Override, error) {
	rows, err := s.pool.Query(ctx, `SELECT 'maker', maker_id, name, override FROM archive_makers WHERE override <> ''
UNION ALL SELECT 'course', course_id, name, override FROM archive_courses WHERE override <> '' ORDER BY 1, 2 LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Override
	for rows.Next() {
		var o Override
		if err := rows.Scan(&o.Kind, &o.ID, &o.Name, &o.Mode); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// ArchiveStats counts what the word filter and takedowns hide (admin page, import log).
type ArchiveStats struct {
	Courses, Makers                 int
	FilteredCourses, FilteredMakers int
	HiddenCourses, HiddenMakers     int
}

// ArchiveStats counts rows, filter hits and hidden rows.
func (s *Store) ArchiveStats(ctx context.Context) (ArchiveStats, error) {
	var a ArchiveStats
	err := s.pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE filter_term<>''), count(*) FILTER (WHERE NOT visible) FROM archive_courses`).
		Scan(&a.Courses, &a.FilteredCourses, &a.HiddenCourses)
	if err != nil {
		return a, err
	}
	err = s.pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE filter_term<>''), count(*) FILTER (WHERE NOT visible) FROM archive_makers`).
		Scan(&a.Makers, &a.FilteredMakers, &a.HiddenMakers)
	return a, err
}

// ApprovedClaim returns the approved claim on a maker.
func (s *Store) ApprovedClaim(ctx context.Context, makerID string) (ArchiveClaim, error) {
	return s.oneClaim(ctx, `WHERE maker_id=$1 AND status='approved'`, makerID)
}

// AccountClaim returns the claim an account made on a maker (any status).
func (s *Store) AccountClaim(ctx context.Context, makerID, accountID string) (ArchiveClaim, error) {
	return s.oneClaim(ctx, `WHERE maker_id=$1 AND account_id=$2`, makerID, accountID)
}

const claimCols = `id, maker_id, account_id, account_name, wiiu_pid, linked_nnid, status, via, note, created_at, decided_at`

func scanClaim(row pgx.Row) (ArchiveClaim, error) {
	var k ArchiveClaim
	err := row.Scan(&k.ID, &k.MakerID, &k.AccountID, &k.AccountName, &k.WiiUPID, &k.LinkedNNID, &k.Status, &k.Via, &k.Note, &k.CreatedAt, &k.DecidedAt)
	return k, err
}

func (s *Store) oneClaim(ctx context.Context, where string, args ...any) (ArchiveClaim, error) {
	k, err := scanClaim(s.pool.QueryRow(ctx, `SELECT `+claimCols+` FROM archive_claims `+where, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return k, ErrNotFound
	}
	return k, err
}

// ErrClaimUnproven means the claim's account PID is not the maker's uploader PID (or the archive
// records no uploader PID for the maker): nothing proves the claimant is that player.
var ErrClaimUnproven = errors.New("store: claim not proven by the uploader PID")

// SetMakerPID records the uploader's Wii U PID of an archived maker.
func (s *Store) SetMakerPID(ctx context.Context, makerID string, pid int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE archive_makers SET pid=$2 WHERE lower(maker_id)=lower($1)`, makerID, pid)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ClaimProven reports whether pid (the claimant's linked Wii U PID, from the core links) equals the
// recorded uploader PID of the maker. A typed name or Network ID never counts; an unknown uploader
// PID (0) proves nothing.
func (s *Store) ClaimProven(ctx context.Context, makerID string, pid int64) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM archive_makers WHERE lower(maker_id)=lower($1) AND pid>0 AND pid=$2)`, makerID, pid).Scan(&ok)
	return ok, err
}

// ErrClaimTaken means the maker already has an approved claim by someone else.
var ErrClaimTaken = errors.New("store: maker already claimed")

// AddClaim stores a claim. A claim with status "approved" fails with ErrClaimTaken when another
// account already holds the maker. Claiming twice with the same account returns the existing row.
func (s *Store) AddClaim(ctx context.Context, k ArchiveClaim) (ArchiveClaim, error) {
	if old, err := s.AccountClaim(ctx, k.MakerID, k.AccountID); err == nil {
		return old, nil
	}
	if _, err := s.ApprovedClaim(ctx, k.MakerID); err == nil {
		return k, ErrClaimTaken
	}
	if k.Status == "approved" {
		if ok, err := s.ClaimProven(ctx, k.MakerID, k.WiiUPID); err != nil {
			return k, err
		} else if !ok {
			return k, ErrClaimUnproven
		}
	}
	got, err := scanClaim(s.pool.QueryRow(ctx, `INSERT INTO archive_claims (maker_id, account_id, account_name, wiiu_pid, linked_nnid, status, via, note, decided_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8, CASE WHEN $6='approved' THEN now() END) RETURNING `+claimCols,
		k.MakerID, k.AccountID, k.AccountName, k.WiiUPID, k.LinkedNNID, k.Status, k.Via, k.Note))
	if err != nil && strings.Contains(err.Error(), "archive_claims_one_approved") {
		return k, ErrClaimTaken
	}
	return got, err
}

// Claims lists claims, pending first. status "" lists all.
func (s *Store) Claims(ctx context.Context, status string, limit int) ([]ArchiveClaim, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+claimCols+` FROM archive_claims WHERE ($1='' OR status=$1)
ORDER BY (status='pending') DESC, created_at DESC LIMIT $2`, status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArchiveClaim
	for rows.Next() {
		k, err := scanClaim(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// DecideClaim approves ("approved", via moderator) or rejects ("rejected") a claim.
func (s *Store) DecideClaim(ctx context.Context, id int64, status string) error {
	if status != "approved" && status != "rejected" {
		return fmt.Errorf("store: bad claim status %q", status)
	}
	if status == "approved" { // a moderator cannot approve what the PIDs do not prove
		var ok bool
		err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM archive_claims k JOIN archive_makers m ON lower(m.maker_id)=lower(k.maker_id)
WHERE k.id=$1 AND m.pid>0 AND m.pid=k.wiiu_pid)`, id).Scan(&ok)
		if err != nil {
			return err
		}
		if !ok {
			var n int
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM archive_claims WHERE id=$1`, id).Scan(&n); err == nil && n == 0 {
				return ErrNotFound
			}
			return ErrClaimUnproven
		}
	}
	tag, err := s.pool.Exec(ctx, `UPDATE archive_claims SET status=$2, decided_at=now() WHERE id=$1`, id, status)
	if err != nil {
		if strings.Contains(err.Error(), "archive_claims_one_approved") {
			return ErrClaimTaken
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ColumnNames lists a table's columns in the current schema (tests check that no country is stored).
func (s *Store) ColumnNames(ctx context.Context, table string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = $1`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
