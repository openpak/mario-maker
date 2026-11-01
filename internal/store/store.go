// Package store is the site's own Postgres: a copy of courses and makers made by
// the syncer, with full-text search. The site never reads another service's storage.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openpak/mario-maker/internal/model"
)

const schema = `
CREATE TABLE IF NOT EXISTS courses (
  game          text        NOT NULL,
  course_id     text        NOT NULL,
  data_id       text        NOT NULL DEFAULT '0',
  name          text        NOT NULL DEFAULT '',
  description   text        NOT NULL DEFAULT '',
  maker_id      text        NOT NULL DEFAULT '',
  maker_name    text        NOT NULL DEFAULT '',
  style         text        NOT NULL DEFAULT '',
  theme         text        NOT NULL DEFAULT '',
  difficulty    text        NOT NULL DEFAULT '',
  tags          text[]      NOT NULL DEFAULT '{}',
  created       timestamptz,
  updated       timestamptz,
  stars         integer     NOT NULL DEFAULT 0,
  plays         integer     NOT NULL DEFAULT 0,
  clears        integer     NOT NULL DEFAULT 0,
  attempts      integer     NOT NULL DEFAULT 0,
  has_thumbnail boolean     NOT NULL DEFAULT false,
  search        tsvector    NOT NULL DEFAULT ''::tsvector,
  seen_at       timestamptz NOT NULL DEFAULT now(),
  deleted_at    timestamptz,
  PRIMARY KEY (game, course_id)
);
CREATE INDEX IF NOT EXISTS courses_search_idx ON courses USING gin (search);
CREATE INDEX IF NOT EXISTS courses_created_idx ON courses (created DESC NULLS LAST) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS courses_stars_idx ON courses (stars DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS courses_plays_idx ON courses (plays DESC) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS courses_maker_idx ON courses (game, maker_id) WHERE deleted_at IS NULL;
CREATE TABLE IF NOT EXISTS makers (
  game      text        NOT NULL,
  maker_id  text        NOT NULL,
  name      text        NOT NULL DEFAULT '',
  courses   integer     NOT NULL DEFAULT 0,
  stars     integer     NOT NULL DEFAULT 0,
  plays     integer     NOT NULL DEFAULT 0,
  created   timestamptz,
  seen_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (game, maker_id)
);
`

// Store wraps the pool.
type Store struct {
	pool *pgxpool.Pool
	sum  summaryCache // History landing numbers
}

// Open connects and creates the schema (idempotent, serialized by an advisory lock).
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	return OpenConfig(ctx, cfg)
}

// OpenConfig is Open with a prepared pool config (tests set search_path).
func OpenConfig(ctx context.Context, cfg *pgxpool.Config) (*Store, error) {
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	s := &Store{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate(ctx context.Context) error {
	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(727301)`); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(727301)`)
	if _, err = conn.Exec(ctx, schema); err != nil {
		return err
	}
	_, err = conn.Exec(ctx, archiveSchema)
	return err
}

// Close releases the pool.
func (s *Store) Close() { s.pool.Close() }

// Ping checks the database.
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func searchText(c model.Course) string {
	return c.Name + " " + strings.Join(c.Tags, " ") + " " + c.Style + " " + c.Theme + " " + c.Description + " " + c.MakerName
}

// UpsertCourses stores courses seen upstream and clears any soft delete.
func (s *Store) UpsertCourses(ctx context.Context, cs []model.Course) error {
	if len(cs) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, c := range cs {
		tags := c.Tags
		if tags == nil {
			tags = []string{}
		}
		b.Queue(`INSERT INTO courses (game, course_id, data_id, name, description, maker_id, maker_name, style, theme, difficulty,
  tags, created, updated, stars, plays, clears, attempts, has_thumbnail, search, seen_at, deleted_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,to_tsvector('simple', $19),now(),NULL)
ON CONFLICT (game, course_id) DO UPDATE SET data_id=EXCLUDED.data_id, name=EXCLUDED.name, description=EXCLUDED.description,
  maker_id=EXCLUDED.maker_id, maker_name=EXCLUDED.maker_name, style=EXCLUDED.style, theme=EXCLUDED.theme,
  difficulty=EXCLUDED.difficulty, tags=EXCLUDED.tags, created=EXCLUDED.created, updated=EXCLUDED.updated,
  stars=EXCLUDED.stars, plays=EXCLUDED.plays, clears=EXCLUDED.clears, attempts=EXCLUDED.attempts,
  has_thumbnail=EXCLUDED.has_thumbnail, search=EXCLUDED.search, seen_at=now(), deleted_at=NULL`,
			c.Game, c.ID, c.DataID, c.Name, c.Description, c.MakerID, c.MakerName, c.Style, c.Theme, c.Difficulty,
			tags, nullTime(c.Created), nullTime(c.Updated), c.Stars, c.Plays, c.Clears, c.Attempts, c.HasThumbnail, searchText(c))
	}
	res := s.pool.SendBatch(ctx, b)
	defer res.Close()
	for range cs {
		if _, err := res.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// UpsertMakers stores makers seen upstream.
func (s *Store) UpsertMakers(ctx context.Context, ms []model.Maker) error {
	if len(ms) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, m := range ms {
		b.Queue(`INSERT INTO makers (game, maker_id, name, courses, stars, plays, created, seen_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,now())
ON CONFLICT (game, maker_id) DO UPDATE SET name=EXCLUDED.name, courses=EXCLUDED.courses, stars=EXCLUDED.stars,
  plays=EXCLUDED.plays, created=EXCLUDED.created, seen_at=now()`,
			m.Game, m.ID, m.Name, m.Courses, m.Stars, m.Plays, nullTime(m.Created))
	}
	res := s.pool.SendBatch(ctx, b)
	defer res.Close()
	for range ms {
		if _, err := res.Exec(); err != nil {
			return err
		}
	}
	return nil
}

// UnseenIDs returns live course ids of a game not refreshed since t: the
// candidates the syncer re-checks after a full pass.
func (s *Store) UnseenIDs(ctx context.Context, game string, t time.Time) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT course_id FROM courses WHERE game=$1 AND deleted_at IS NULL AND seen_at < $2`, game, t)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SoftDelete hides a course that no longer exists upstream.
func (s *Store) SoftDelete(ctx context.Context, game, id string) error {
	_, err := s.pool.Exec(ctx, `UPDATE courses SET deleted_at=now() WHERE game=$1 AND course_id=$2 AND deleted_at IS NULL`, game, id)
	return err
}

const courseCols = `game, course_id, data_id, name, description, maker_id, maker_name, style, theme, difficulty, tags,
  created, updated, stars, plays, clears, attempts, has_thumbnail`

func scanCourse(row pgx.Row) (model.Course, error) {
	var c model.Course
	var created, updated *time.Time
	err := row.Scan(&c.Game, &c.ID, &c.DataID, &c.Name, &c.Description, &c.MakerID, &c.MakerName, &c.Style, &c.Theme,
		&c.Difficulty, &c.Tags, &created, &updated, &c.Stars, &c.Plays, &c.Clears, &c.Attempts, &c.HasThumbnail)
	if created != nil {
		c.Created = *created
	}
	if updated != nil {
		c.Updated = *updated
	}
	return c, err
}

// ErrNotFound is returned when a course or maker is absent (or soft-deleted).
var ErrNotFound = errors.New("store: not found")

// Course returns one live course.
func (s *Store) Course(ctx context.Context, game, id string) (model.Course, error) {
	c, err := scanCourse(s.pool.QueryRow(ctx, `SELECT `+courseCols+` FROM courses WHERE game=$1 AND course_id=$2 AND deleted_at IS NULL`, game, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// Maker returns a maker. When only courses exist (no maker row), totals are
// summed from the live courses; ErrNotFound when neither exists.
func (s *Store) Maker(ctx context.Context, game, id string) (model.Maker, error) {
	m := model.Maker{Game: game, ID: id}
	var created *time.Time
	err := s.pool.QueryRow(ctx, `SELECT name, courses, stars, plays, created FROM makers WHERE game=$1 AND maker_id=$2`, game, id).
		Scan(&m.Name, &m.Courses, &m.Stars, &m.Plays, &created)
	if err == nil {
		if created != nil {
			m.Created = *created
		}
		return m, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return m, err
	}
	err = s.pool.QueryRow(ctx, `SELECT count(*), coalesce(sum(stars),0), coalesce(sum(plays),0), coalesce(max(maker_name),'')
FROM courses WHERE game=$1 AND maker_id=$2 AND deleted_at IS NULL`, game, id).Scan(&m.Courses, &m.Stars, &m.Plays, &m.Name)
	if err != nil {
		return m, err
	}
	if m.Courses == 0 {
		return m, ErrNotFound
	}
	return m, nil
}

// Query selects courses for the shelves and /browse.
type Query struct {
	Game   string // "" for both games
	Text   string
	Maker  string
	Sort   string // newest | stars | plays
	Limit  int
	Offset int
}

// Sorts lists the accepted sort keys.
var Sorts = []string{"newest", "stars", "plays"}

// NormSort maps unknown sorts to newest.
func NormSort(s string) string {
	for _, k := range Sorts {
		if s == k {
			return k
		}
	}
	return "newest"
}

// tsQuery turns free text into a prefix-AND tsquery of its word characters, or "".
func tsQuery(text string) string {
	words := strings.FieldsFunc(text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) > 10 {
		words = words[:10]
	}
	for i, w := range words {
		words[i] = w + ":*"
	}
	return strings.Join(words, " & ")
}

// Search returns one page of live courses and the total match count.
func (s *Store) Search(ctx context.Context, q Query) ([]model.Course, int, error) {
	where := []string{"deleted_at IS NULL"}
	var args []any
	arg := func(v any) string { args = append(args, v); return fmt.Sprintf("$%d", len(args)) }
	if q.Game != "" {
		where = append(where, "game="+arg(q.Game))
	}
	if q.Maker != "" {
		where = append(where, "maker_id="+arg(q.Maker))
	}
	if t := strings.TrimSpace(q.Text); t != "" {
		// A pasted course code matches exactly; anything else is a full-text match.
		code := arg(strings.ToUpper(t))
		if tq := tsQuery(t); tq != "" {
			where = append(where, fmt.Sprintf("(search @@ to_tsquery('simple', %s) OR upper(course_id)=%s)", arg(tq), code))
		} else {
			where = append(where, "upper(course_id)="+code)
		}
	}
	w := strings.Join(where, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM courses WHERE `+w, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "created DESC NULLS LAST"
	switch q.Sort {
	case "stars":
		order = "stars DESC, created DESC NULLS LAST"
	case "plays":
		order = "plays DESC, created DESC NULLS LAST"
	}
	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 24
	}
	sql := fmt.Sprintf(`SELECT %s FROM courses WHERE %s ORDER BY %s, game, course_id LIMIT %s OFFSET %s`,
		courseCols, w, order, arg(limit), arg(max(q.Offset, 0)))
	rows, err := s.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []model.Course
	for rows.Next() {
		c, err := scanCourse(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, c)
	}
	return out, total, rows.Err()
}

// Counts returns live course counts per game.
func (s *Store) Counts(ctx context.Context) (map[string]int, error) {
	rows, err := s.pool.Query(ctx, `SELECT game, count(*) FROM courses WHERE deleted_at IS NULL GROUP BY game`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var g string
		var n int
		if err := rows.Scan(&g, &n); err != nil {
			return nil, err
		}
		out[g] = n
	}
	return out, rows.Err()
}
