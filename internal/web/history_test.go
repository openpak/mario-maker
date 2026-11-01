package web_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/openpak/mario-maker/internal/archive"
	"github.com/openpak/mario-maker/internal/auth"
	"github.com/openpak/mario-maker/internal/link"
	"github.com/openpak/mario-maker/internal/store"
	"github.com/openpak/mario-maker/internal/testutil"
	"github.com/openpak/mario-maker/internal/web"
)

func seedHistory(t *testing.T) *store.Store {
	t.Helper()
	st := testutil.DB(t)
	ctx := context.Background()
	now := time.Now()
	ms := []store.ArchiveMaker{{ID: "mia_99", Name: "Mia <b>", Source: archive.SourceID, ImportedAt: now}, {ID: "rudeman", Name: "Rude", Source: archive.SourceID, ImportedAt: now, FilterLang: "en", FilterTerm: "rude"}}
	cs := []store.ArchiveCourse{
		{ID: "AAAA-0000-0000-0001", Name: "Sky Castle", MakerID: "mia_99", Theme: "mario_bros", Difficulty: "Easy", Tag: "Speedrun", Created: time.Date(2015, 9, 17, 0, 0, 0, 0, time.UTC),
			Stars: 1200, Plays: 5000, Attempts: 1000, Clears: 405, ClearRate: 40.5, Source: archive.SourceID, ImportedAt: now},
		{ID: "AAAA-0000-0000-0002", Name: "Hidden by filter", MakerID: "mia_99", Theme: "mario_world", FilterLang: "en", FilterTerm: "x", Source: archive.SourceID, ImportedAt: now},
		{ID: "AAAA-0000-0000-0003", Name: "Other", MakerID: "rudeman", Source: archive.SourceID, ImportedAt: now},
	}
	if err := st.UpsertArchive(ctx, ms, cs); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.FinishArchiveImport(ctx, store.ArchiveSource{Source: archive.SourceID, Title: archive.SourceTitle, URL: archive.SourceURL, Licence: archive.Licence, ImportedAt: now}); err != nil {
		t.Fatal(err)
	}
	return st
}

func historyServer(t *testing.T, st *store.Store, au *auth.Auth, lk *link.HTTP, adminKey string) http.Handler {
	t.Helper()
	cfg := web.Config{Store: st, Auth: au, SessionCookie: auth.CookieName, CSRFSecret: "csrf-secret", WebsiteURL: "https://openpak.example",
		PublicURL: "https://maker.example", Log: slog.New(slog.DiscardHandler), AdminKey: adminKey}
	if lk != nil {
		cfg.Link = lk
	}
	s, err := web.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s.Handler()
}

func post(h http.Handler, path string, form url.Values, hdr ...string) (int, string, http.Header) {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), rec.Header()
}

func TestHistoryPages(t *testing.T) {
	st := seedHistory(t)
	h := historyServer(t, st, nil, nil, "")

	// nav and footer carry History
	code, body, _ := get(h, "/history")
	if code != 200 {
		t.Fatalf("/history: %d", code)
	}
	for _, want := range []string{`href="/history"`, "Archived, not playable yet", "/history/course/AAAA-0000-0000-0001", "Sky Castle", "Remove this", "This is mine",
		"Takedowns are honoured within 48 hours", "SMMData by tachyo", "Open Database License", "<title>History · OpenMaker</title>", "Highest clear rate"} {
		if !strings.Contains(body, want) {
			t.Errorf("/history missing %q", want)
		}
	}
	for _, bad := range []string{"Hidden by filter", "country", "Country", "pretendo", "Pretendo", "{{", "%!"} {
		if strings.Contains(body, bad) {
			t.Errorf("/history must not contain %q", bad)
		}
	}
	if strings.Contains(body, "/course/smm") {
		t.Error("archived rows must not link to live course pages")
	}

	// search by name, maker, code, theme, year, sort
	for q, want := range map[string]string{
		"/history?q=castle":                        "1 archived course matching",
		"/history?q=mia":                           "/history/course/AAAA-0000-0000-0001",
		"/history?q=AAAA-0000-0000-0001":           "Sky Castle",
		"/history?theme=mario_bros&year=2015":      "Sky Castle",
		"/history?theme=mario_world":               "No archived courses match",
		"/history?sort=rate":                       "Sky Castle",
		"/history?sort=bogus&year=junk&theme=junk": "Sky Castle",
	} {
		if code, body, _ := get(h, q); code != 200 || !strings.Contains(body, want) {
			t.Errorf("%s: %d missing %q", q, code, want)
		}
	}

	// course detail
	code, body, hd := get(h, "/history/course/AAAA-0000-0000-0001")
	for _, want := range []string{"Archived · originally by", `href="/history/maker/mia_99"`, "Mia &lt;b&gt;", "Not playable yet", `<code class="copy"`, "AAAA-0000-0000-0001",
		"1,200", "5,000", "40.5%", "17 Sep 2015", "Super Mario Bros.", "Remove this", "/history/takedown?kind=course&amp;id=AAAA-0000-0000-0001", "This is mine", `content="noindex, follow"`} {
		if code != 200 || !strings.Contains(body, want) {
			t.Errorf("course page missing %q (%d)", want, code)
		}
	}
	if strings.Contains(body, "Mia <b>") || hd.Get("Content-Security-Policy") == "" {
		t.Error("escaping or CSP")
	}
	// hidden things 404
	for _, p := range []string{"/history/course/AAAA-0000-0000-0002", "/history/maker/rudeman", "/history/course/NOPE", "/history/maker/bad%20id"} {
		if code, _, _ := get(h, p); code != 404 {
			t.Errorf("%s: %d, want 404", p, code)
		}
	}
	// a filtered maker's courses are hidden with it
	if _, body, _ := get(h, "/history?q=other"); strings.Contains(body, "AAAA-0000-0000-0003") {
		t.Error("course of a filtered maker shown")
	}

	// maker page
	code, body, _ = get(h, "/history/maker/MIA_99")
	for _, want := range []string{"Mia &lt;b&gt;", "Network ID", "mia_99", "Archived courses", "Sky Castle", "Remove this", "This is mine", "/history/takedown?kind=maker&amp;id=mia_99"} {
		if code != 200 || !strings.Contains(body, want) {
			t.Errorf("maker page missing %q (%d)", want, code)
		}
	}
	if strings.Contains(body, "Hidden by filter") {
		t.Error("filtered course on maker page")
	}

	// about page: credits and licences
	_, body, _ = get(h, "/history/about")
	for _, want := range []string{"tachyo", "Open Database License", "CC BY 4.0", "LDNOOBW", "English", "Japanese", "Spanish", "French", "German", "within 48 hours", "takedown&#64;openpak.org"} {
		if !strings.Contains(body, want) {
			t.Errorf("about missing %q", want)
		}
	}
	if strings.Contains(body, "takedown@openpak.org") {
		t.Error("the address must be entity-encoded so Cloudflare does not obfuscate it")
	}
	// live pages are untouched, nav shows History
	if _, body, _ = get(h, "/browse"); !strings.Contains(body, `<a href="/history" >History</a>`) && !strings.Contains(body, ">History</a>") {
		t.Error("nav lacks History")
	}
	if _, body, _ = get(h, "/robots.txt"); !strings.Contains(body, "Disallow: /history/course/") {
		t.Error("robots")
	}
}

func TestTakedownFlow(t *testing.T) {
	st := seedHistory(t)
	h := historyServer(t, st, nil, nil, "")

	code, body, _ := get(h, "/history/takedown?kind=maker&id=mia_99")
	for _, want := range []string{"honoured within 48 hours", "takedown&#64;openpak.org", `value="mia_99"`, `<option value="maker" selected>`, "Your name"} {
		if code != 200 || !strings.Contains(body, want) {
			t.Errorf("form missing %q", want)
		}
	}
	// validation
	if code, body, _ := post(h, "/history/takedown", url.Values{"kind": {"course"}, "id": {"AAAA-0000-0000-0001"}}); code != 400 || !strings.Contains(body, "required") {
		t.Errorf("missing name: %d", code)
	}
	if code, _, _ := post(h, "/history/takedown", url.Values{"kind": {"course"}, "id": {"bad id!"}, "name": {"a"}, "contact": {"b"}}); code != 400 {
		t.Errorf("bad id: %d", code)
	}
	// a honeypot hit is not stored and hides nothing
	if code, _, _ := post(h, "/history/takedown", url.Values{"kind": {"course"}, "id": {"AAAA-0000-0000-0001"}, "name": {"bot"}, "contact": {"x"}, "website": {"http://spam"}}); code != 200 {
		t.Errorf("honeypot: %d", code)
	}
	if code, _, _ := get(h, "/history/course/AAAA-0000-0000-0001"); code != 200 {
		t.Error("honeypot must not hide")
	}
	if l, _ := st.Takedowns(context.Background(), "", 10); len(l) != 0 {
		t.Errorf("honeypot stored: %+v", l)
	}

	// a real request hides the course at once and is stored for review
	code, body, _ = post(h, "/history/takedown", url.Values{"kind": {"course"}, "id": {"aaaa-0000-0000-0001"}, "name": {"Mia"}, "contact": {"mia@example.org"}, "reason": {"it is mine"}})
	if code != 200 || !strings.Contains(body, "The entry is hidden now") || !strings.Contains(body, "within 48 hours") {
		t.Errorf("submit: %d %s", code, body)
	}
	if code, _, _ := get(h, "/history/course/AAAA-0000-0000-0001"); code != 404 {
		t.Errorf("course still visible after takedown: %d", code)
	}
	if _, body, _ := get(h, "/history"); strings.Contains(body, "Sky Castle") {
		t.Error("course still listed after takedown")
	}
	l, _ := st.Takedowns(context.Background(), "pending", 10)
	if len(l) != 1 || l[0].Target != "AAAA-0000-0000-0001" || l[0].Reason != "it is mine" || l[0].Contact != "mia@example.org" {
		t.Errorf("stored: %+v", l)
	}

	// a maker request hides the maker page and all their courses
	post(h, "/history/takedown", url.Values{"kind": {"maker"}, "id": {"Mia_99"}, "name": {"Mia"}, "contact": {"c"}})
	if code, _, _ := get(h, "/history/maker/mia_99"); code != 404 {
		t.Errorf("maker still visible: %d", code)
	}
	// unknown target: stored, says so
	if _, body, _ := post(h, "/history/takedown", url.Values{"kind": {"maker"}, "id": {"ghost"}, "name": {"n"}, "contact": {"c"}}); !strings.Contains(body, "did not find") {
		t.Error("unknown target message")
	}
}

func TestTakedownRateLimit(t *testing.T) {
	st := seedHistory(t)
	h := historyServer(t, st, nil, nil, "")
	last := 0
	for i := 0; i < 12; i++ {
		last, _, _ = post(h, "/history/takedown", url.Values{"kind": {"maker"}, "id": {"ghost"}, "name": {"n"}, "contact": {"c"}}, "CF-Connecting-IP", "203.0.113.9")
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("12th request: %d, want 429", last)
	}
	if code, _, _ := post(h, "/history/takedown", url.Values{"kind": {"maker"}, "id": {"ghost"}, "name": {"n"}, "contact": {"c"}}, "CF-Connecting-IP", "203.0.113.10"); code != 200 {
		t.Errorf("another address: %d", code)
	}
}

func TestAdminArchive(t *testing.T) {
	st := seedHistory(t)
	off := historyServer(t, st, nil, nil, "")
	if code, _, _ := get(off, "/admin/archive"); code != 404 {
		t.Errorf("admin without a key: %d, want 404", code)
	}
	h := historyServer(t, st, nil, nil, "adm-key")
	if code, _, hd := get(h, "/admin/archive"); code != 401 || hd.Get("WWW-Authenticate") == "" {
		t.Errorf("no credentials: %d", code)
	}
	basic := func(pw string) string {
		r := httptest.NewRequest("GET", "/", nil)
		r.SetBasicAuth("admin", pw)
		return r.Header.Get("Authorization")
	}
	if code, _, _ := get(h, "/admin/archive", "Authorization", basic("wrong")); code != 401 {
		t.Errorf("wrong key: %d", code)
	}
	post(h, "/history/takedown", url.Values{"kind": {"course"}, "id": {"AAAA-0000-0000-0001"}, "name": {"Mia"}, "contact": {"m@example.org"}})
	code, body, _ := get(h, "/admin/archive", "Authorization", basic("adm-key"))
	if code != 200 || !strings.Contains(body, "Takedown requests") || !strings.Contains(body, "m&#64;example.org") || !strings.Contains(body, "Sky Castle") || !strings.Contains(body, "Override list") {
		t.Fatalf("admin page: %d", code)
	}
	csrf := regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`).FindStringSubmatch(body)[1]
	// a post without the token is refused; with it, restore brings the course back
	if code, _, _ := post(h, "/admin/archive", url.Values{"action": {"takedown-restore"}, "id": {"1"}}, "Authorization", basic("adm-key")); code != 403 {
		t.Errorf("no token: %d", code)
	}
	if code, _, _ := post(h, "/admin/archive", url.Values{"action": {"takedown-restore"}, "id": {"1"}, "csrf": {csrf}}); code != 401 {
		t.Errorf("post without credentials: %d", code)
	}
	if code, _, _ := post(h, "/admin/archive", url.Values{"action": {"takedown-restore"}, "id": {"1"}, "csrf": {csrf}}, "Authorization", basic("adm-key")); code != 303 {
		t.Errorf("restore: %d", code)
	}
	if code, _, _ := get(h, "/history/course/AAAA-0000-0000-0001"); code != 200 {
		t.Errorf("restored course: %d", code)
	}
	// override: show a filtered course
	post(h, "/admin/archive", url.Values{"action": {"override"}, "kind": {"course"}, "target": {"AAAA-0000-0000-0002"}, "mode": {"show"}, "csrf": {csrf}}, "Authorization", basic("adm-key"))
	if code, _, _ := get(h, "/history/course/AAAA-0000-0000-0002"); code != 200 {
		t.Errorf("override show: %d", code)
	}
}

func TestClaimFlow(t *testing.T) {
	st := seedHistory(t)
	au := auth.New(auth.Config{PublicURL: "https://maker.test", InternalURL: "http://unused.test", Secret: "s", SessionKey: strings.Repeat("k", 32)})
	ck := func(id, name string, pid int64) string {
		return au.SessionCookie(auth.User{ID: id, DisplayName: name, WiiUPID: pid}).String()
	}
	links := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("account_id") {
		case "linked":
			io.WriteString(w, `{"nnid":"MIA_99"}`)
		case "other":
			io.WriteString(w, `{"nnid":"someone_else"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer links.Close()
	h := historyServer(t, st, au, link.New(links.URL, ""), "")
	const mk = "/history/maker/mia_99"

	// anonymous: sign in prompt; no form
	if _, body, _ := get(h, mk); !strings.Contains(body, "Sign in to claim") || strings.Contains(body, `action="/history/maker/mia_99/claim"`) {
		t.Error("anonymous claim section")
	}
	token := func(cookie string) string {
		_, body, _ := get(h, mk, "Cookie", cookie)
		m := regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`).FindStringSubmatch(body)
		if m == nil {
			t.Fatalf("no claim form for %s", cookie)
		}
		return m[1]
	}
	// a forged post is refused
	pend := ck("nolink", "Wanda", 77)
	if code, _, _ := post(h, mk+"/claim", url.Values{"csrf": {"bad"}}, "Cookie", pend); code != 403 {
		t.Errorf("bad token: %d", code)
	}
	if code, _, _ := post(h, mk+"/claim", url.Values{}); code != 403 {
		t.Errorf("anonymous post: %d", code)
	}

	// no linked NNID: stored as pending for a moderator, not shown as claimed
	if code, _, hd := post(h, mk+"/claim", url.Values{"csrf": {token(pend)}, "note": {"it is me"}}, "Cookie", pend); code != 303 || !strings.HasSuffix(hd.Get("Location"), "#claim") {
		t.Fatalf("pending claim: %d %s", code, hd.Get("Location"))
	}
	_, body, _ := get(h, mk, "Cookie", pend)
	if !strings.Contains(body, "waiting for a moderator") || strings.Contains(body, "Claimed by Wanda") {
		t.Error("pending state")
	}
	if _, body, _ = get(h, mk); strings.Contains(body, "Claimed by") {
		t.Error("pending must not show Claimed by")
	}
	cl, _ := st.Claims(context.Background(), "pending", 10)
	if len(cl) != 1 || cl[0].AccountID != "nolink" || cl[0].WiiUPID != 77 || cl[0].Note != "it is me" || cl[0].Via != "moderator" {
		t.Errorf("stored claim: %+v", cl)
	}

	// linked to a different NNID: refused
	other := ck("other", "Olga", 0)
	if code, body, _ := post(h, mk+"/claim", url.Values{"csrf": {token(other)}}, "Cookie", other); code != 409 || !strings.Contains(body, "different one") {
		t.Errorf("mismatch: %d", code)
	}

	// linked to this NNID (any case): approved at once, shown as Claimed by
	good := ck("linked", "Tobi", 1)
	if code, _, _ := post(h, mk+"/claim", url.Values{"csrf": {token(good)}}, "Cookie", good); code != 303 {
		t.Fatalf("linked claim: %d", code)
	}
	for _, p := range []string{mk, "/history/course/AAAA-0000-0000-0001"} {
		if _, body, _ := get(h, p); !strings.Contains(body, "Tobi") || !strings.Contains(body, "laimed by") {
			t.Errorf("%s: Claimed by missing", p)
		}
	}
	if _, body, _ := get(h, mk, "Cookie", good); !strings.Contains(body, "You have claimed this profile") {
		t.Error("claimant view")
	}
	// the maker is taken now; the pending claimant gets no form
	if _, body, _ := get(h, mk, "Cookie", pend); strings.Contains(body, `action="/history/maker/mia_99/claim"`) {
		t.Error("claimed maker still offers a form")
	}
	// a moderator approving the older pending claim must not make a second owner
	if err := st.DecideClaim(context.Background(), cl[0].ID, "approved"); err == nil {
		t.Error("second approval accepted")
	}
}
