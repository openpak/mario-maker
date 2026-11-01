package web_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openpak/mario-maker/internal/auth"
	"github.com/openpak/mario-maker/internal/fakeupstream"
	"github.com/openpak/mario-maker/internal/model"
	"github.com/openpak/mario-maker/internal/store"
	"github.com/openpak/mario-maker/internal/testutil"
	"github.com/openpak/mario-maker/internal/upstream"
	"github.com/openpak/mario-maker/internal/web"
)

func newServer(t *testing.T, st *store.Store, ups map[string]*upstream.Client, au *auth.Client, cacheDir string) http.Handler {
	t.Helper()
	s, err := web.New(web.Config{Store: st, Upstream: ups, Auth: au, WebsiteURL: "https://openpak.example", CacheDir: cacheDir,
		Log: slog.New(slog.DiscardHandler), PublicURL: "https://courses.example"})
	if err != nil {
		t.Fatal(err)
	}
	return s.Handler()
}

func get(h http.Handler, path string, hdr ...string) (int, string, http.Header) {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String(), rec.Header()
}

func TestPagesEmpty(t *testing.T) {
	h := newServer(t, testutil.DB(t), nil, nil, "")
	for path, want := range map[string]string{
		"/":                          "No courses yet",
		"/?game=smm2":                "No courses yet",
		"/browse":                    "No courses to show yet",
		"/browse?q=zzz&sort=stars":   "No course matches",
		"/browse?page=99&sort=bogus": "0 courses",
	} {
		code, body, _ := get(h, path)
		if code != 200 || !strings.Contains(body, want) {
			t.Errorf("%s: %d, missing %q", path, code, want)
		}
		if !strings.Contains(body, "not affiliated with") || !strings.Contains(body, "maker codes shown here are OpenPak's own") {
			t.Errorf("%s: footer notice missing", path)
		}
	}
	for _, path := range []string{"/course/smm1/NOPE", "/course/zzz/x", "/maker/smm2/nobody", "/course/smm1/bad%20id", "/thumb/smm1/NOPE.jpg"} {
		if code, _, _ := get(h, path); code != 404 {
			t.Errorf("%s: %d, want 404", path, code)
		}
	}
	if code, body, _ := get(h, "/healthz"); code != 200 || !strings.Contains(body, `"ok":true`) {
		t.Errorf("healthz: %d %s", code, body)
	}
	if code, body, _ := get(h, "/static/site.css"); code != 200 || !strings.Contains(body, "--cursor") {
		t.Errorf("css: %d", code)
	}
	if code, _, _ := get(h, "/static/fonts/mplus-rounded-700.woff2"); code != 200 {
		t.Errorf("font: %d", code)
	}
}

func populate(t *testing.T, st *store.Store) {
	t.Helper()
	now := time.Now().UTC()
	err := st.UpsertCourses(context.Background(), []model.Course{
		{Game: "smm1", ID: "0123456789ABCDEF", DataID: "7", Name: "Sky <Castle>", Description: "climb & jump", MakerID: "M1", MakerName: "Mia", Style: "smb1", Theme: "castle", Difficulty: "hard",
			Tags: []string{"speedrun", "castle"}, Created: now.Add(-time.Hour), Updated: now, Stars: 1234, Plays: 99, Clears: 5, Attempts: 20, HasThumbnail: true},
		{Game: "smm2", ID: "ABC-DEF-GH1", Created: now},
	})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		_ = st.UpsertCourses(context.Background(), []model.Course{{Game: "smm1", ID: "PAGE" + strings.Repeat("0", 3) + string(rune('A'+i/26)) + string(rune('A'+i%26)), Name: "Filler course", MakerID: "M1", Created: now.Add(-time.Duration(i+2) * time.Hour)}})
	}
	_ = st.UpsertMakers(context.Background(), []model.Maker{{Game: "smm1", ID: "M1", Name: "Mia", Courses: 31, Stars: 1234, Plays: 99, Created: now}})
}

func TestPagesPopulated(t *testing.T) {
	st := testutil.DB(t)
	populate(t, st)
	h := newServer(t, st, nil, nil, "")

	code, body, _ := get(h, "/")
	if code != 200 || !strings.Contains(body, "Newest") || !strings.Contains(body, "Most starred") || !strings.Contains(body, "Most played") ||
		!strings.Contains(body, "/course/smm1/0123456789ABCDEF") || !strings.Contains(body, "SMM2 · Switch") {
		t.Errorf("home: %d", code)
	}
	if strings.Contains(body, "<Castle>") || !strings.Contains(body, "Sky &lt;Castle&gt;") {
		t.Error("course name not escaped")
	}

	code, body, _ = get(h, "/course/smm1/0123456789ABCDEF")
	for _, want := range []string{"Sky &lt;Castle&gt;", `<code class="copy"`, "0123456789ABCDEF", "1,234", "/maker/smm1/M1", "speedrun", "Hard", "25.0%",
		`og:image" content="https://courses.example/thumb/smm1/0123456789ABCDEF.jpg?v=`, `og:title`, `rel="canonical"`} {
		if !strings.Contains(body, want) {
			t.Errorf("course page missing %q", want)
		}
	}
	if code != 200 {
		t.Errorf("course: %d", code)
	}
	// A course with nothing but an id still renders.
	code, body, _ = get(h, "/course/smm2/ABC-DEF-GH1")
	for _, want := range []string{"Untitled course", "unknown maker", "No thumbnail"} {
		if code != 200 || !strings.Contains(body, want) {
			t.Errorf("bare course: %d, missing %q", code, want)
		}
	}

	code, body, _ = get(h, "/maker/smm1/M1")
	if code != 200 || !strings.Contains(body, "Mia") || !strings.Contains(body, "Stars received") || !strings.Contains(body, "/course/smm1/0123456789ABCDEF") {
		t.Errorf("maker: %d", code)
	}

	// Search, filters, pagination.
	if _, body, _ = get(h, "/browse?q=castle"); !strings.Contains(body, "1 course matching") {
		t.Error("search for castle")
	}
	if _, body, _ = get(h, "/browse?q=abc-def-gh1&game=smm2"); !strings.Contains(body, "/course/smm2/ABC-DEF-GH1") {
		t.Error("search by course id")
	}
	_, body, _ = get(h, "/browse?game=smm1&sort=stars")
	if !strings.Contains(body, "Page 1 of 2") || !strings.Contains(body, `rel="next" href="/browse?game=smm1&amp;page=2&amp;sort=stars"`) {
		t.Errorf("pager: %s", body[strings.Index(body, `class="pager"`):][:400])
	}
	_, body, _ = get(h, "/browse?game=smm1&sort=stars&page=2")
	if !strings.Contains(body, "Page 2 of 2") || !strings.Contains(body, `rel="prev"`) {
		t.Error("page 2")
	}
}

func TestThumbProxy(t *testing.T) {
	st := testutil.DB(t)
	populate(t, st)
	f := fakeupstream.New("k")
	f.Set(fakeupstream.Course{ID: "0123456789ABCDEF", HasThumbnail: true})
	ts := httptest.NewServer(f)
	defer ts.Close()
	ups := map[string]*upstream.Client{"smm1": upstream.New("smm1", ts.URL, "k")}
	dir := t.TempDir()
	h := newServer(t, st, ups, nil, dir)

	code, body, hd := get(h, "/thumb/smm1/0123456789ABCDEF.jpg")
	if code != 200 || hd.Get("Content-Type") != "image/jpeg" || len(body) < 100 || !strings.HasPrefix(body, "\xff\xd8") {
		t.Fatalf("thumb: %d %q", code, hd.Get("Content-Type"))
	}
	etag := hd.Get("ETag")
	if code, _, _ := get(h, "/thumb/smm1/0123456789ABCDEF.jpg", "If-None-Match", etag); code != http.StatusNotModified {
		t.Errorf("etag: %d", code)
	}
	get(h, "/thumb/smm1/0123456789ABCDEF.jpg")
	if _, n := f.Counters(); n != 1 {
		t.Errorf("memory cache: %d upstream fetches, want 1", n)
	}
	// A new server process finds the file on disk.
	h2 := newServer(t, st, ups, nil, dir)
	if code, _, _ := get(h2, "/thumb/smm1/0123456789ABCDEF.jpg"); code != 200 {
		t.Error("disk cache")
	}
	if _, n := f.Counters(); n != 1 {
		t.Errorf("disk cache: %d upstream fetches, want 1", n)
	}
	// Not proxied: unknown course, course flagged without thumbnail, game with no upstream, bad names, no suffix.
	for _, p := range []string{"/thumb/smm1/NOPE.jpg", "/thumb/smm2/ABC-DEF-GH1.jpg", "/thumb/smm1/0123456789ABCDEF", "/thumb/smm1/..%2f..%2fx.jpg", "/thumb/xx/a.jpg"} {
		if code, _, _ := get(h, p); code != 404 {
			t.Errorf("%s: %d, want 404", p, code)
		}
	}
	// Upstream error maps to 502 and is not cached.
	ts.Close()
	st2 := testutil.DB(t)
	populate(t, st2)
	if code, _, _ := get(newServer(t, st2, ups, nil, ""), "/thumb/smm1/0123456789ABCDEF.jpg"); code != http.StatusBadGateway {
		t.Errorf("upstream down: %d, want 502", code)
	}
}

func TestSignInDisplay(t *testing.T) {
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("openpak_session"); r.URL.Path != "/v1/whoami" || err != nil || c.Value != "good" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		io.WriteString(w, `{"account_id":"a1","display_name":"Tobi","handle":"tobi"}`)
	}))
	defer core.Close()
	st := testutil.DB(t)
	h := newServer(t, st, nil, auth.New(core.URL, ""), "")

	if _, body, _ := get(h, "/"); !strings.Contains(body, "https://openpak.example/login") || strings.Contains(body, "Tobi") {
		t.Error("anonymous header")
	}
	_, body, hd := get(h, "/", "Cookie", "opak_session=good")
	if !strings.Contains(body, "Tobi") || strings.Contains(body, ">Sign in<") || !strings.Contains(hd.Get("Cache-Control"), "no-store") {
		t.Error("signed-in header")
	}
	if _, body, _ := get(h, "/", "Cookie", "opak_session=stale"); strings.Contains(body, "Tobi") || !strings.Contains(body, "Sign in") {
		t.Error("stale session shown as signed in")
	}
	// Disabled: no sign-in UI at all.
	if _, body, _ := get(newServer(t, st, nil, nil, ""), "/"); strings.Contains(body, "Sign in") {
		t.Error("sign-in shown while disabled")
	}
}
