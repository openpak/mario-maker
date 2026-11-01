package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/openpak/mario-maker/internal/store"
	"github.com/openpak/mario-maker/internal/wordfilter"
)

// History: archived Wii U Super Mario Maker course details, imported from a third-party scrape
// (internal/archive). Browse-only: the courses cannot be played. Every page credits the makers
// and offers "This is mine" and "Remove this".

// nnidRe bounds a Network ID in a URL (letters, digits, dot, dash, underscore).
var nnidRe = regexp.MustCompile(`^[0-9A-Za-z_.-]{1,40}$`)

// codeRe bounds a course code such as 635C-0000-0045-AF89.
var codeRe = regexp.MustCompile(`^[0-9A-Za-z-]{1,40}$`)

func themeName(t string) string {
	switch t {
	case "mario_bros":
		return "Super Mario Bros."
	case "mario_bros3":
		return "Super Mario Bros. 3"
	case "mario_world":
		return "Super Mario World"
	case "mario_bros_u":
		return "New Super Mario Bros. U"
	}
	return t
}

func (s *Server) historyRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /history", s.historyBrowse)
	mux.HandleFunc("GET /history/about", s.historyAbout)
	mux.HandleFunc("GET /history/course/{id}", s.historyCourse)
	mux.HandleFunc("GET /history/maker/{id}", s.historyMaker)
	mux.HandleFunc("POST /history/maker/{id}/claim", s.historyClaim)
	mux.HandleFunc("GET /history/takedown", s.takedownForm)
	mux.HandleFunc("POST /history/takedown", s.takedownSubmit)
	s.adminRoutes(mux)
}

type historyData struct {
	Q, Theme, Sort   string
	Year             int
	Years            []int
	Themes           []string
	Courses          []store.ArchiveCourse
	Total            int
	Page, Pages      int
	PrevURL, NextURL string
	Sum              store.ArchiveSummary
	Empty            bool // nothing imported yet
}

func (s *Server) historyBrowse(w http.ResponseWriter, r *http.Request) {
	qs := r.URL.Query()
	q := strings.TrimSpace(qs.Get("q"))
	if utf8.RuneCountInString(q) > 100 {
		q = string([]rune(q)[:100])
	}
	sum, err := s.cfg.Store.ArchiveSummary(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	year, _ := strconv.Atoi(qs.Get("year"))
	if year < 2000 || year > 2100 {
		year = 0
	}
	theme, sortKey := store.NormTheme(qs.Get("theme")), store.NormArchiveSort(qs.Get("sort"))
	pg, _ := strconv.Atoi(qs.Get("page"))
	pg = max(pg, 1)
	cs, total, err := s.cfg.Store.SearchArchive(r.Context(), store.ArchiveQuery{Text: q, Theme: theme, Year: year, Sort: sortKey, Limit: pageSize, Offset: (pg - 1) * pageSize})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	pages := max((total+pageSize-1)/pageSize, 1)
	d := historyData{Q: q, Theme: theme, Sort: sortKey, Year: year, Years: sum.Years, Themes: store.ArchiveThemes, Courses: cs, Total: total,
		Page: pg, Pages: pages, Sum: sum, Empty: sum.Courses == 0}
	keep := url.Values{}
	if q != "" {
		keep.Set("q", q)
	}
	if theme != "" {
		keep.Set("theme", theme)
	}
	if year != 0 {
		keep.Set("year", strconv.Itoa(year))
	}
	if sortKey != "stars" {
		keep.Set("sort", sortKey)
	}
	if pg > 1 {
		d.PrevURL = pageURL("/history", keep, pg-1)
	}
	if pg < pages {
		d.NextURL = pageURL("/history", keep, pg+1)
	}
	s.render(w, r, "history", http.StatusOK, page{Title: "History", Desc: "Browse Super Mario Maker courses from 2015 to 2018 as a record: names, makers, stars, plays and clear rates.", Data: d})
}

type historyAboutData struct {
	Sum   store.ArchiveSummary
	Lists []wordList
}

type wordList struct {
	Name  string
	Count int
}

func (s *Server) historyAbout(w http.ResponseWriter, r *http.Request) {
	sum, err := s.cfg.Store.ArchiveSummary(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	f := wordfilter.Default()
	d := historyAboutData{Sum: sum}
	for _, l := range wordfilter.Langs {
		d.Lists = append(d.Lists, wordList{wordfilter.LangName(l), f.Count(l)})
	}
	s.render(w, r, "history_about", http.StatusOK, page{Title: "About the History archive", Desc: "Where the History archive comes from, how it is licensed, how names are filtered, and how to remove or claim an entry.", Data: d})
}

type historyCourseData struct {
	C        store.ArchiveCourse
	Rate     string
	HasMaker bool
}

func (s *Server) historyCourse(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !codeRe.MatchString(id) {
		s.notFound(w, r, "That archived course")
		return
	}
	c, err := s.cfg.Store.ArchiveCourse(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r, "That archived course")
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
	rate := "n/a"
	if c.Attempts > 0 {
		rate = fmtRate(c.ClearRate)
	}
	maker := c.MakerName
	if maker == "" {
		maker = c.MakerID
	}
	desc := fmt.Sprintf("%s, a Super Mario Maker course archived from %d. Originally by %s. Not playable yet.", name, c.Created.Year(), orDashMaker(maker))
	s.render(w, r, "history_course", http.StatusOK, page{Title: name, Desc: truncate(desc, 200), NoIndex: true, OGType: "article",
		Data: historyCourseData{C: c, Rate: rate, HasMaker: c.MakerID != "" && nnidRe.MatchString(c.MakerID)}})
}

func orDashMaker(s string) string {
	if s == "" {
		return "an unknown maker"
	}
	return s
}

type claimView struct {
	State string // "", "signin", "ready", "pending", "mine", "claimed", "off"
	CSRF  string
}

type historyMakerData struct {
	M       store.ArchiveMaker
	Courses []store.ArchiveCourse
	Total   int
	Sort    string
	Claim   claimView
}

func (s *Server) historyMaker(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !nnidRe.MatchString(id) {
		s.notFound(w, r, "That archived maker")
		return
	}
	m, err := s.cfg.Store.ArchiveMaker(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r, "That archived maker")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sortKey := store.NormArchiveSort(r.URL.Query().Get("sort"))
	cs, total, err := s.cfg.Store.SearchArchive(r.Context(), store.ArchiveQuery{Maker: m.ID, Sort: sortKey, Limit: 60})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	name := m.Name
	if name == "" {
		name = m.ID
	}
	s.render(w, r, "history_maker", http.StatusOK, page{Title: name, Desc: fmt.Sprintf("Super Mario Maker courses by %s, archived from 2015 to 2018. Not playable yet.", name), NoIndex: true,
		Data: historyMakerData{M: m, Courses: cs, Total: total, Sort: sortKey, Claim: s.claimView(r, m)}})
}

// claimView is what the maker page needs to draw the claim section.
func (s *Server) claimView(r *http.Request, m store.ArchiveMaker) claimView {
	if m.Claim != nil {
		if u := s.cfg.Auth.User(r); u != nil && m.Claim.AccountID == u.ID {
			return claimView{State: "mine"}
		}
		return claimView{State: "claimed"}
	}
	if s.cfg.Auth == nil {
		return claimView{State: "off"}
	}
	u := s.cfg.Auth.User(r)
	if u == nil {
		return claimView{State: "signin"}
	}
	if k, err := s.cfg.Store.AccountClaim(r.Context(), m.ID, u.ID); err == nil && k.Status == "pending" {
		return claimView{State: "pending"}
	}
	return claimView{State: "ready", CSRF: s.csrfFor(r, "claim")}
}

func (s *Server) historyClaim(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !nnidRe.MatchString(id) || s.cfg.Auth == nil {
		s.notFound(w, r, "That archived maker")
		return
	}
	u := s.cfg.Auth.User(r)
	if u == nil || !s.csrfOKFor(r, "claim") {
		http.Error(w, "sign in again and retry", http.StatusForbidden)
		return
	}
	m, err := s.cfg.Store.ArchiveMaker(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r, "That archived maker")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	k := store.ArchiveClaim{MakerID: m.ID, AccountID: u.ID, AccountName: u.Name(), WiiUPID: u.WiiUPID, Status: "pending", Via: "moderator", Note: clip(r.PostFormValue("note"), 500)}
	if s.cfg.Link != nil {
		nnid, err := s.cfg.Link.NNID(r.Context(), u.ID)
		switch {
		case err != nil:
			s.cfg.Log.Warn("claim: link lookup", "err", err) // falls back to a moderator
		case nnid != "" && strings.EqualFold(nnid, m.ID):
			k.Status, k.Via, k.LinkedNNID = "approved", "link", nnid
		case nnid != "":
			s.render(w, r, "error", http.StatusConflict, page{Title: "Not your Network ID",
				Data: "The Wii U Network ID linked to your OpenPak account is a different one, so this profile cannot be claimed with it."})
			return
		}
	}
	if _, err := s.cfg.Store.AddClaim(r.Context(), k); errors.Is(err, store.ErrClaimTaken) {
		s.render(w, r, "error", http.StatusConflict, page{Title: "Already claimed",
			Data: "Someone has already claimed this maker. If that is a mistake, use Remove this on the maker page or ask for its removal on the maker page."})
		return
	} else if err != nil {
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/history/maker/"+url.PathEscape(m.ID)+"#claim", http.StatusSeeOther)
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > n {
		s = string([]rune(s)[:n])
	}
	return s
}

// Takedown ---------------------------------------------------------------------------------

type takedownData struct {
	Kind, ID, Name, Contact, Reason string
	Error                           string
	Sent                            bool
	Found                           bool
	RequestID                       int64
	Email                           string
}

func (s *Server) takedownForm(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	d := takedownData{Kind: store.NormKind(q.Get("kind")), ID: clip(q.Get("id"), 40), Email: s.cfg.TakedownEmail}
	if d.Kind == "" {
		d.Kind = "course"
	}
	s.render(w, r, "history_takedown", http.StatusOK, page{Title: "Remove an entry", Desc: "Ask for a Super Mario Maker course or maker to be removed from the History archive. It is hidden at once.", NoIndex: true, Data: d})
}

func (s *Server) takedownSubmit(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<10)
	d := takedownData{Kind: store.NormKind(r.PostFormValue("kind")), ID: clip(r.PostFormValue("id"), 40), Name: clip(r.PostFormValue("name"), 100),
		Contact: clip(r.PostFormValue("contact"), 200), Reason: clip(r.PostFormValue("reason"), 2000), Email: s.cfg.TakedownEmail}
	show := func(status int, msg string) {
		d.Error = msg
		s.render(w, r, "history_takedown", status, page{Title: "Remove an entry", NoIndex: true, Data: d})
	}
	if r.PostFormValue("website") != "" { // honeypot: pretend it worked
		d.Sent = true
		s.render(w, r, "history_takedown", http.StatusOK, page{Title: "Request received", NoIndex: true, Data: d})
		return
	}
	switch {
	case d.Kind == "":
		show(http.StatusBadRequest, "Choose whether this is a course or a maker.")
		return
	case d.ID == "" || (d.Kind == "course" && !codeRe.MatchString(d.ID)) || (d.Kind == "maker" && !nnidRe.MatchString(d.ID)):
		show(http.StatusBadRequest, "Enter the course code (like 635C-0000-0045-AF89) or the maker's Network ID, with no spaces.")
		return
	case d.Name == "" || d.Contact == "":
		show(http.StatusBadRequest, "Your name and a way to reach you are required.")
		return
	}
	if !s.limit.allow(clientIP(r)) {
		show(http.StatusTooManyRequests, "Too many requests from your network just now. Try again later, or use the address on the About page.")
		return
	}
	id, found, err := s.cfg.Store.AddTakedown(r.Context(), store.Takedown{Kind: d.Kind, Target: d.ID, Name: d.Name, Contact: d.Contact, Reason: d.Reason})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d.Sent, d.Found, d.RequestID = true, found, id
	s.render(w, r, "history_takedown", http.StatusOK, page{Title: "Request received", NoIndex: true, Data: d})
}

// clientIP is the caller as the proxy reports it.
func clientIP(r *http.Request) string {
	if v := r.Header.Get("CF-Connecting-IP"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

// rateLimit allows n events per key per window.
type rateLimit struct {
	mu     sync.Mutex
	n      int
	window time.Duration
	seen   map[string][]time.Time
}

func newRateLimit(n int, window time.Duration) *rateLimit {
	return &rateLimit{n: n, window: window, seen: map[string][]time.Time{}}
}

func (l *rateLimit) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	keep := l.seen[key][:0]
	for _, t := range l.seen[key] {
		if now.Sub(t) < l.window {
			keep = append(keep, t)
		}
	}
	if len(keep) >= l.n {
		l.seen[key] = keep
		return false
	}
	l.seen[key] = append(keep, now)
	if len(l.seen) > 5000 { // bound memory
		for k, ts := range l.seen {
			if len(ts) == 0 || now.Sub(ts[len(ts)-1]) >= l.window {
				delete(l.seen, k)
			}
		}
	}
	return true
}

// Admin ------------------------------------------------------------------------------------

type adminData struct {
	Stats     store.ArchiveStats
	Takedowns []store.Takedown
	Claims    []store.ArchiveClaim
	Overrides []store.Override
	Sum       store.ArchiveSummary
	CSRF      string
	Notice    string
}

func (s *Server) adminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/archive", s.adminPage)
	mux.HandleFunc("POST /admin/archive", s.adminAct)
}

// adminOK checks the key sent as the HTTP Basic password (any user name). Off when no key is set.
func (s *Server) adminOK(w http.ResponseWriter, r *http.Request) bool {
	key := s.cfg.AdminKey
	if key == "" {
		http.NotFound(w, r)
		return false
	}
	_, pw, ok := r.BasicAuth()
	if !ok || subtle.ConstantTimeCompare([]byte(pw), []byte(key)) != 1 {
		w.Header().Set("WWW-Authenticate", `Basic realm="OpenMaker archive admin", charset="UTF-8"`)
		http.Error(w, "admin key required", http.StatusUnauthorized)
		return false
	}
	return true
}

func (s *Server) adminCSRF() string {
	m := hmac.New(sha256.New, []byte(s.cfg.AdminKey))
	m.Write([]byte("archive-admin"))
	return hex.EncodeToString(m.Sum(nil))
}

func (s *Server) adminPage(w http.ResponseWriter, r *http.Request) {
	if !s.adminOK(w, r) {
		return
	}
	ctx := r.Context()
	var d adminData
	var err error
	if d.Stats, err = s.cfg.Store.ArchiveStats(ctx); err == nil {
		if d.Takedowns, err = s.cfg.Store.Takedowns(ctx, "", 100); err == nil {
			if d.Claims, err = s.cfg.Store.Claims(ctx, "", 100); err == nil {
				if d.Overrides, err = s.cfg.Store.Overrides(ctx); err == nil {
					d.Sum, err = s.cfg.Store.ArchiveSummary(ctx)
				}
			}
		}
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d.CSRF, d.Notice = s.adminCSRF(), r.URL.Query().Get("done")
	w.Header().Set("Cache-Control", "no-store")
	s.render(w, r, "admin_archive", http.StatusOK, page{Title: "Archive admin", NoIndex: true, Data: d})
}

func (s *Server) adminAct(w http.ResponseWriter, r *http.Request) {
	if !s.adminOK(w, r) {
		return
	}
	if !hmac.Equal([]byte(r.PostFormValue("csrf")), []byte(s.adminCSRF())) {
		http.Error(w, "bad form token", http.StatusForbidden)
		return
	}
	ctx := r.Context()
	id, _ := strconv.ParseInt(r.PostFormValue("id"), 10, 64)
	var err error
	switch r.PostFormValue("action") {
	case "takedown-approve":
		err = s.cfg.Store.DecideTakedown(ctx, id, "approved")
	case "takedown-restore":
		err = s.cfg.Store.DecideTakedown(ctx, id, "restored")
	case "takedown-reopen":
		err = s.cfg.Store.DecideTakedown(ctx, id, "pending")
	case "claim-approve":
		err = s.cfg.Store.DecideClaim(ctx, id, "approved")
	case "claim-reject":
		err = s.cfg.Store.DecideClaim(ctx, id, "rejected")
	case "override":
		kind := store.NormKind(r.PostFormValue("kind"))
		target := strings.TrimSpace(r.PostFormValue("target"))
		if kind == "" || target == "" {
			err = errors.New("kind and target are required")
		} else {
			err = s.cfg.Store.SetOverride(ctx, kind, target, r.PostFormValue("mode"))
		}
	default:
		err = errors.New("unknown action")
	}
	done := "done"
	if err != nil {
		s.cfg.Log.Warn("archive admin", "action", r.PostFormValue("action"), "err", err)
		done = "failed: " + err.Error()
	}
	http.Redirect(w, r, "/admin/archive?done="+url.QueryEscape(done), http.StatusSeeOther)
}
