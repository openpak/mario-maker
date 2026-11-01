// Package web serves the course site: server-rendered html/template pages, embedded
// static assets, and the thumbnail proxy. No page needs JavaScript.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/openpak/mario-maker/internal/auth"
	"github.com/openpak/mario-maker/internal/link"
	"github.com/openpak/mario-maker/internal/model"
	"github.com/openpak/mario-maker/internal/store"
	"github.com/openpak/mario-maker/internal/upstream"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static
var staticFS embed.FS

const (
	pageSize  = 24
	shelfSize = 8
)

// idRe bounds what a URL may name; both course codes and maker ids fit.
var idRe = regexp.MustCompile(`^[0-9A-Za-z_-]{1,40}$`)

// Config wires the server.
type Config struct {
	Store         *store.Store
	Upstream      map[string]*upstream.Client // by game; missing games have no thumbnails
	Auth          *auth.Auth                  // nil when sign-in is off
	Link          link.Resolver               // OpenPak account -> Wii U NNID; nil turns web bookmarking off
	SessionCookie string                      // this site's session cookie name (auth.CookieName; also keys the bookmark CSRF token)
	CSRFSecret    string                      // optional fixed key for bookmark tokens; random per start when empty
	PublicURL     string                      // absolute base for OG tags; derived from the request when empty
	WebsiteURL    string                      // openpak.org, for the sign-in and account links
	TakedownEmail string                      // public contact for History takedowns; defaults to takedown@openpak.org
	AdminKey      string                      // protects /admin/archive (HTTP Basic, any user name); empty turns the admin page off
	CacheDir      string
	CacheMB       int
	Log           *slog.Logger
	SyncStatus    func() any // shown by /healthz
}

// Server is the HTTP handler set.
type Server struct {
	cfg     Config
	pages   map[string]*template.Template
	thumbs  *thumbCache
	csrfKey []byte
	asset   string
	limit   *rateLimit
}

// New parses the templates and builds the server.
func New(cfg Config) (*Server, error) {
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.CacheMB <= 0 {
		cfg.CacheMB = 64
	}
	if cfg.TakedownEmail == "" {
		cfg.TakedownEmail = "takedown@openpak.org"
	}
	s := &Server{cfg: cfg, limit: newRateLimit(8, time.Hour), csrfKey: newCSRFKey(cfg.CSRFSecret), pages: map[string]*template.Template{}, thumbs: newThumbCache(cfg.CacheDir, cfg.CacheMB<<20), asset: strconv.FormatInt(time.Now().Unix(), 36)}
	funcs := template.FuncMap{
		"num":       num,
		"date":      fmtDate,
		"gameName":  model.GameName,
		"gameShort": model.GameShort,
		"rate":      func(c model.Course) string { return fmtRate(c.ClearRate()) },
		"title":     titleCase,
		"thumb":     thumbPath,
		"add":       func(a, b int) int { return a + b },
		"sub":       func(a, b int) int { return a - b },
		"pct":       func(f float64) string { return fmtRate(f) },
		"act": func(kind, id, mine string) map[string]string {
			return map[string]string{"Kind": kind, "ID": id, "Mine": mine, "Remove": "/history/takedown?kind=" + url.QueryEscape(kind) + "&id=" + url.QueryEscape(id)}
		},
		"theme": themeName,
		"arate": func(c store.ArchiveCourse) string {
			if c.Attempts <= 0 {
				return "n/a"
			}
			return fmtRate(c.ClearRate)
		},
		// Plain text, "@" as an entity, no mailto link: Cloudflare's address obfuscation rewrites mailto links and needs
		// JavaScript to undo it, which the CSP blocks.
		"mailto": func(addr string) template.HTML {
			return template.HTML(`<span class="addr">` + strings.ReplaceAll(html.EscapeString(addr), "@", "&#64;") + `</span>`)
		},
		"noat": func(v string) template.HTML {
			return template.HTML(strings.ReplaceAll(html.EscapeString(v), "@", "&#64;"))
		}, // keeps contact addresses readable past Cloudflare's obfuscation
		"hasPrefix": strings.HasPrefix,
		"stamp":     func(t time.Time) string { return t.UTC().Format("2 Jan 2006 15:04 UTC") },
	}
	names, err := fs.Glob(templateFS, "templates/*.html")
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		base := strings.TrimSuffix(strings.TrimPrefix(n, "templates/"), ".html")
		if base == "layout" {
			continue
		}
		t, err := template.New(base).Funcs(funcs).ParseFS(templateFS, "templates/layout.html", n)
		if err != nil {
			return nil, err
		}
		s.pages[base] = t
	}
	return s, nil
}

// Handler returns the routed handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(staticFS, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheFor(http.FileServerFS(static), "public, max-age=86400")))
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /{$}", s.home)
	mux.HandleFunc("GET /browse", s.browse)
	mux.HandleFunc("GET /course/{game}/{id}", s.course)
	mux.HandleFunc("GET /maker/{game}/{id}", s.maker)
	mux.HandleFunc("GET /thumb/{game}/{file}", s.thumb)
	s.bookmarkRoutes(mux)
	s.historyRoutes(mux)
	s.cfg.Auth.Routes(mux, s.signInFailed)
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, "User-agent: *\nAllow: /\nDisallow: /history/course/\nDisallow: /history/maker/\nDisallow: /admin/\n")
	})
	return secure(mux)
}

func secure(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		hd.Set("Content-Security-Policy", "default-src 'none'; img-src 'self' data:; style-src 'self'; font-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		h.ServeHTTP(w, r)
	})
}

func cacheFor(h http.Handler, v string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", v)
		h.ServeHTTP(w, r)
	})
}

// page is the data every template gets.
type page struct {
	Title      string
	Desc       string
	Path       string
	Asset      string
	User       *auth.User
	AuthOn     bool
	WebsiteURL string
	Canonical  string
	OGImage    string
	OGType     string
	NoIndex    bool
	Takedown   string // public contact address for removal requests
	Data       any
}

func (s *Server) base(r *http.Request) string {
	if s.cfg.PublicURL != "" {
		return strings.TrimRight(s.cfg.PublicURL, "/")
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, name string, status int, p page) {
	p.Asset, p.Path = s.asset, r.URL.Path
	p.AuthOn = s.cfg.Auth != nil
	p.User = s.cfg.Auth.User(r)
	p.WebsiteURL = strings.TrimRight(s.cfg.WebsiteURL, "/")
	p.Takedown = s.cfg.TakedownEmail
	if p.OGType == "" {
		p.OGType = "website"
	}
	if p.Canonical == "" {
		p.Canonical = s.base(r) + r.URL.RequestURI()
	}
	t := s.pages[name]
	var sb strings.Builder
	if err := t.ExecuteTemplate(&sb, "page", p); err != nil {
		s.cfg.Log.Error("render", "page", name, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if p.User != nil {
		w.Header().Set("Cache-Control", "private, no-store")
	}
	w.WriteHeader(status)
	fmt.Fprint(w, sb.String())
}

// signInFailed is the page for a sign-in that did not finish (bad state, or the code was refused).
func (s *Server) signInFailed(w http.ResponseWriter, r *http.Request, status int) {
	s.render(w, r, "error", status, page{Title: "Sign-in", Data: "Signing in did not finish. Go back and press Sign in to try again."})
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request, what string) {
	s.render(w, r, "error", http.StatusNotFound, page{Title: "Not found", Data: what})
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	s.cfg.Log.Error("request", "path", r.URL.Path, "err", err)
	s.render(w, r, "error", http.StatusInternalServerError, page{Title: "Something went wrong", Data: "The course database did not answer. Try again in a moment."})
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	out := map[string]any{"ok": true}
	status := http.StatusOK
	if err := s.cfg.Store.Ping(ctx); err != nil {
		out["ok"], out["error"] = false, "database unreachable"
		status = http.StatusServiceUnavailable
	}
	if s.cfg.SyncStatus != nil {
		out["sync"] = s.cfg.SyncStatus()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(out)
}

// shelf is one titled row of course tiles.
type shelf struct {
	Title   string
	Sort    string
	Courses []model.Course
}

type homeData struct {
	Game    string // "" = both
	Counts  map[string]int
	Total   int
	Shelves []shelf
}

func gameParam(r *http.Request) string {
	if g := r.URL.Query().Get("game"); model.ValidGame(g) {
		return g
	}
	return ""
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	game := gameParam(r)
	counts, err := s.cfg.Store.Counts(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d := homeData{Game: game, Counts: counts}
	for _, g := range model.Games {
		if game == "" || game == g {
			d.Total += counts[g]
		}
	}
	for _, sh := range []struct{ title, sort string }{{"Newest", "newest"}, {"Most starred", "stars"}, {"Most played", "plays"}} {
		cs, _, err := s.cfg.Store.Search(r.Context(), store.Query{Game: game, Sort: sh.sort, Limit: shelfSize})
		if err != nil {
			s.fail(w, r, err)
			return
		}
		d.Shelves = append(d.Shelves, shelf{Title: sh.title, Sort: sh.sort, Courses: cs})
	}
	s.render(w, r, "home", http.StatusOK, page{Title: "Courses", Desc: "Browse courses made by OpenPak players in Super Mario Maker and Super Mario Maker 2.", Data: d})
}

type browseData struct {
	Q, Sort, Game    string
	Courses          []model.Course
	Total            int
	Page, Pages      int
	PrevURL, NextURL string
	Counts           map[string]int
}

func pageURL(base string, q url.Values, page int) string {
	v := url.Values{}
	for k, vs := range q {
		v[k] = vs
	}
	if page <= 1 {
		v.Del("page")
	} else {
		v.Set("page", strconv.Itoa(page))
	}
	if e := v.Encode(); e != "" {
		return base + "?" + e
	}
	return base
}

func (s *Server) browse(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	q := strings.TrimSpace(qs.Get("q"))
	if len(q) > 100 {
		q = q[:100]
	}
	sortKey, game := store.NormSort(qs.Get("sort")), gameParam(r)
	pg, _ := strconv.Atoi(qs.Get("page"))
	pg = max(pg, 1)
	cs, total, err := s.cfg.Store.Search(r.Context(), store.Query{Game: game, Text: q, Sort: sortKey, Limit: pageSize, Offset: (pg - 1) * pageSize})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	pages := max((total+pageSize-1)/pageSize, 1)
	d := browseData{Q: q, Sort: sortKey, Game: game, Courses: cs, Total: total, Page: pg, Pages: pages}
	keep := url.Values{}
	if q != "" {
		keep.Set("q", q)
	}
	if game != "" {
		keep.Set("game", game)
	}
	if sortKey != "newest" {
		keep.Set("sort", sortKey)
	}
	if pg > 1 {
		d.PrevURL = pageURL("/browse", keep, pg-1)
	}
	if pg < pages {
		d.NextURL = pageURL("/browse", keep, pg+1)
	}
	s.render(w, r, "browse", http.StatusOK, page{Title: "Browse courses", Desc: "Search and browse Super Mario Maker courses.", Data: d})
}

type courseData struct {
	C        model.Course
	HasMaker bool
	Rate     string
	BM       bookmarkView
}

func (s *Server) course(w http.ResponseWriter, r *http.Request) {
	game, id := r.PathValue("game"), r.PathValue("id")
	if !model.ValidGame(game) || !idRe.MatchString(id) {
		s.notFound(w, r, "That course")
		return
	}
	c, err := s.cfg.Store.Course(r.Context(), game, id)
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r, "That course")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	name := c.Name
	if name == "" {
		name = "Untitled course"
	}
	desc := fmt.Sprintf("%s by %s. %s", name, orDash(c.MakerName), model.GameName(game))
	if c.Description != "" {
		desc = c.Description
	}
	p := page{Title: name, Desc: truncate(desc, 200), OGType: "article", Data: courseData{C: c, HasMaker: c.MakerID != "" && idRe.MatchString(c.MakerID), BM: s.bookmarkView(r, c)}}
	if c.HasThumbnail {
		p.OGImage = s.base(r) + thumbPath(c)
	}
	s.render(w, r, "course", http.StatusOK, p)
}

type makerData struct {
	M       model.Maker
	Courses []model.Course
	Total   int
}

func (s *Server) maker(w http.ResponseWriter, r *http.Request) {
	game, id := r.PathValue("game"), r.PathValue("id")
	if !model.ValidGame(game) || !idRe.MatchString(id) {
		s.notFound(w, r, "That maker")
		return
	}
	m, err := s.cfg.Store.Maker(r.Context(), game, id)
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r, "That maker")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	cs, total, err := s.cfg.Store.Search(r.Context(), store.Query{Game: game, Maker: id, Sort: store.NormSort(r.URL.Query().Get("sort")), Limit: 60})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	name := m.Name
	if name == "" {
		name = "Maker " + id
	}
	s.render(w, r, "maker", http.StatusOK, page{Title: name, Desc: fmt.Sprintf("Courses by %s in %s.", name, model.GameName(game)), Data: makerData{M: m, Courses: cs, Total: total}})
}

// thumbPath is the public thumbnail URL of a course; the query changes when the course does.
func thumbPath(c model.Course) string {
	return fmt.Sprintf("/thumb/%s/%s.jpg?v=%d", c.Game, url.PathEscape(c.ID), c.Updated.Unix())
}

func (s *Server) thumb(w http.ResponseWriter, r *http.Request) {
	game := r.PathValue("game")
	id, ok := strings.CutSuffix(r.PathValue("file"), ".jpg")
	cl := s.cfg.Upstream[game]
	if !ok || !model.ValidGame(game) || !idRe.MatchString(id) || cl == nil {
		http.NotFound(w, r)
		return
	}
	c, err := s.cfg.Store.Course(r.Context(), game, id)
	if err != nil || !c.HasThumbnail {
		http.NotFound(w, r)
		return
	}
	key := fmt.Sprintf("%s_%s_%d", game, id, c.Updated.Unix())
	b, hit := s.thumbs.get(key)
	if !hit {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		b, err = cl.Thumbnail(ctx, id)
		if errors.Is(err, upstream.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			s.cfg.Log.Warn("thumbnail", "game", game, "id", id, "err", err)
			http.Error(w, "thumbnail unavailable", http.StatusBadGateway)
			return
		}
		s.thumbs.put(key, b, true)
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Header().Set("ETag", `"`+key+`"`)
	if r.Header.Get("If-None-Match") == `"`+key+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	_, _ = w.Write(b)
}

func num(n int) string {
	s := strconv.Itoa(max(n, -n))
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if n < 0 {
		s = "-" + s
	}
	return s
}

func fmtDate(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.UTC().Format("2 Jan 2006")
}

func fmtRate(r float64) string {
	if r < 0 {
		return "n/a"
	}
	return strconv.FormatFloat(r, 'f', 1, 64) + "%"
}

func titleCase(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func orDash(s string) string {
	if s == "" {
		return "unknown maker"
	}
	return s
}

func truncate(s string, n int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}
