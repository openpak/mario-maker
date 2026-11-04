package web_test

import (
	"context"
	"errors"
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
	for _, want := range []string{"Archived · Originally by", `href="/history/maker/mia_99"`, "Mia &lt;b&gt;", "Not playable yet", `<code class="copy"`, "AAAA-0000-0000-0001",
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
	ctx := context.Background()
	const uploaderPID = 1800000001
	if err := st.SetMakerPID(ctx, "mia_99", uploaderPID); err != nil {
		t.Fatal(err)
	}
	// a maker the archive records no uploader PID for
	if err := st.UpsertArchive(ctx, []store.ArchiveMaker{{ID: "nopid_1", Name: "No Pid", Source: archive.SourceID, ImportedAt: time.Now()}}, nil); err != nil {
		t.Fatal(err)
	}
	au := auth.New(auth.Config{PublicURL: "https://maker.test", InternalURL: "http://unused.test", Secret: "s", SessionKey: strings.Repeat("k", 32)})
	ck := func(id, name string, pid int64) string {
		return au.SessionCookie(auth.User{ID: id, DisplayName: name, WiiUPID: pid}).String()
	}
	links := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("account_id") {
		case "linked": // the real uploader's account: the PID matches, the name does not have to
			io.WriteString(w, `{"nnid":"Whatever_Name","pid":1800000001}`)
		case "squatter": // registered the handle of the archived maker, another PID
			io.WriteString(w, `{"nnid":"MIA_99","pid":1800000999}`)
		case "namelink": // an old link answer with a name and no PID
			io.WriteString(w, `{"nnid":"mia_99"}`)
		case "down":
			http.Error(w, "boom", http.StatusInternalServerError)
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
	claim := func(cookie string) (int, string) {
		code, body, _ := post(h, mk+"/claim", url.Values{"csrf": {token(cookie)}}, "Cookie", cookie)
		return code, body
	}
	noClaims := func(why string) {
		t.Helper()
		if cl, _ := st.Claims(ctx, "", 10); len(cl) != 0 {
			t.Fatalf("%s: a claim was stored: %+v", why, cl)
		}
	}
	// a forged post is refused
	pend := ck("nolink", "Wanda", 0)
	if code, _, _ := post(h, mk+"/claim", url.Values{"csrf": {"bad"}}, "Cookie", pend); code != 403 {
		t.Errorf("bad token: %d", code)
	}
	if code, _, _ := post(h, mk+"/claim", url.Values{}); code != 403 {
		t.Errorf("anonymous post: %d", code)
	}

	// no Wii U linked: refused, nothing stored (a typed name or a note is not proof)
	if code, body, _ := post(h, mk+"/claim", url.Values{"csrf": {token(pend)}, "note": {"it is me, mia_99"}}, "Cookie", pend); code != 409 || !strings.Contains(body, "no Wii U linked") {
		t.Errorf("no link: %d", code)
	}
	noClaims("no link")

	// the loophole: an account whose handle equals the maker's Network ID but whose PID differs
	squatter := ck("squatter", "MIA_99", 1800000999)
	if code, body := claim(squatter); code != 409 || !strings.Contains(body, "not the one that uploaded") {
		t.Errorf("handle squatter: %d", code)
	}
	// a link answer with a matching name and no PID proves nothing either
	if code, _ := claim(ck("namelink", "mia_99", 0)); code != 409 {
		t.Errorf("name without PID: %d", code)
	}
	// the session's own PID is never trusted over the link service
	if code, _ := claim(ck("squatter", "MIA_99", uploaderPID)); code != 409 {
		t.Errorf("forged session PID: %d", code)
	}
	noClaims("squatter")

	// link service down: no guess, no claim
	if code, _, _ := post(h, mk+"/claim", url.Values{"csrf": {token(ck("down", "D", 0))}}, "Cookie", ck("down", "D", 0)); code != 503 {
		t.Errorf("link down: %d", code)
	}
	noClaims("link down")

	// a maker with no recorded uploader PID cannot be claimed by anyone
	if _, body, _ := get(h, "/history/maker/nopid_1", "Cookie", ck("linked", "Tobi", uploaderPID)); !strings.Contains(body, "cannot be claimed yet") || strings.Contains(body, `action="/history/maker/nopid_1/claim"`) {
		t.Error("unverifiable maker offers a form")
	}
	if code, _, _ := post(h, "/history/maker/nopid_1/claim", url.Values{"csrf": {"x"}}, "Cookie", ck("linked", "Tobi", uploaderPID)); code == 303 {
		t.Error("claim on a maker without PID accepted")
	}
	noClaims("no uploader PID")

	// the uploader's own linked PID: approved at once, shown as Claimed by
	good := ck("linked", "Tobi", 1)
	if code, _ := claim(good); code != 303 {
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
	cl, _ := st.Claims(ctx, "approved", 10)
	if len(cl) != 1 || cl[0].AccountID != "linked" || cl[0].WiiUPID != uploaderPID || cl[0].Via != "link" {
		t.Errorf("stored claim: %+v", cl)
	}
	// the maker is taken now; others get no form
	if _, body, _ := get(h, mk, "Cookie", pend); strings.Contains(body, `action="/history/maker/mia_99/claim"`) {
		t.Error("claimed maker still offers a form")
	}
}

func TestClaimsNeedThePID(t *testing.T) {
	st := seedHistory(t)
	ctx := context.Background()
	if err := st.SetMakerPID(ctx, "mia_99", 1800000001); err != nil {
		t.Fatal(err)
	}
	// the store itself refuses an approved claim the PIDs do not prove
	bad := store.ArchiveClaim{MakerID: "mia_99", AccountID: "a1", AccountName: "A", WiiUPID: 1800000999, Status: "approved", Via: "link"}
	if _, err := st.AddClaim(ctx, bad); !errors.Is(err, store.ErrClaimUnproven) {
		t.Errorf("approved claim with a foreign PID: %v", err)
	}
	bad.WiiUPID = 0
	if _, err := st.AddClaim(ctx, bad); !errors.Is(err, store.ErrClaimUnproven) {
		t.Errorf("approved claim with no PID: %v", err)
	}
	// a pending claim (the old moderator path) cannot be approved by a moderator without proof...
	bad.WiiUPID, bad.Status, bad.Via = 1800000999, "pending", "moderator"
	k, err := st.AddClaim(ctx, bad)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DecideClaim(ctx, k.ID, "approved"); !errors.Is(err, store.ErrClaimUnproven) {
		t.Errorf("moderator approval without proof: %v", err)
	}
	// ...may be rejected, and an unknown claim is not found
	if err := st.DecideClaim(ctx, k.ID, "rejected"); err != nil {
		t.Errorf("reject: %v", err)
	}
	if err := st.DecideClaim(ctx, 99999, "approved"); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown claim: %v", err)
	}
	// with the matching PID a pending claim can be approved, once
	good := store.ArchiveClaim{MakerID: "mia_99", AccountID: "a2", AccountName: "B", WiiUPID: 1800000001, Status: "pending", Via: "moderator"}
	g, err := st.AddClaim(ctx, good)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.DecideClaim(ctx, g.ID, "approved"); err != nil {
		t.Errorf("proven approval: %v", err)
	}
	if ok, _ := st.ClaimProven(ctx, "MIA_99", 1800000001); !ok {
		t.Error("ClaimProven is case-insensitive on the Network ID")
	}
	if ok, _ := st.ClaimProven(ctx, "rudeman", 1800000001); ok {
		t.Error("a maker without PID is never proven")
	}
}

func TestOneSearchBoxAndTranslationMarks(t *testing.T) {
	st := seedHistory(t)
	h := historyServer(t, st, nil, nil, "")
	for _, p := range []string{"/", "/browse", "/history", "/history?q=castle", "/history/about", "/history/course/AAAA-0000-0000-0001", "/history/maker/mia_99"} {
		_, body, _ := get(h, p)
		if n := strings.Count(body, `type="search"`); n > 1 {
			t.Errorf("%s: %d search boxes", p, n)
		}
	}
	_, body, _ := get(h, "/history")
	for _, want := range []string{`translate="no" dir="auto">Sky Castle`, `title="Super Mario Bros.">SMB</span>`, "Originally by", `<label for="theme">Style</label>`, "/static/art/mario_bros.svg"} {
		if !strings.Contains(body, want) {
			t.Errorf("history cards missing %q", want)
		}
	}
	if strings.Contains(body, "Archived ·") {
		t.Error("cards must not repeat the Archived prefix")
	}
	if strings.Contains(body, "originally by") {
		t.Error("inconsistent casing")
	}
	if code, _, _ := get(h, "/static/art/mario_world.svg"); code != 200 {
		t.Errorf("art: %d", code)
	}
}
