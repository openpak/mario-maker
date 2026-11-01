package web_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/openpak/mario-maker/internal/auth"
	"github.com/openpak/mario-maker/internal/link"
	"github.com/openpak/mario-maker/internal/testutil"
	"github.com/openpak/mario-maker/internal/upstream"
	"github.com/openpak/mario-maker/internal/web"
)

// fakeTitle is the bookmark half of the Super Mario Maker (Wii U) API.
type fakeTitle struct {
	mu    sync.Mutex
	marks map[string][]string // nnid -> course ids
	calls []string
}

func (f *fakeTitle) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("X-Internal-Key") != "k" || r.URL.Path != "/v1/bookmarks" {
		http.Error(w, "no", http.StatusUnauthorized)
		return
	}
	var body struct{ NNID, Course string }
	nnid := r.URL.Query().Get("nnid")
	if r.Method != http.MethodGet {
		_ = json.NewDecoder(r.Body).Decode(&body)
		nnid = body.NNID
	}
	if nnid != "wiiuplayer" {
		http.NotFound(w, r)
		return
	}
	f.calls = append(f.calls, r.Method+" "+body.Course)
	switch r.Method {
	case http.MethodPut:
		f.marks[nnid] = append(f.marks[nnid], body.Course)
	case http.MethodDelete:
		var keep []string
		for _, c := range f.marks[nnid] {
			if c != body.Course {
				keep = append(keep, c)
			}
		}
		f.marks[nnid] = keep
	case http.MethodGet:
		type c struct {
			ID     string `json:"id"`
			DataID uint64 `json:"data_id"`
			Name   string `json:"name"`
		}
		out := []c{}
		for _, id := range f.marks[nnid] {
			out = append(out, c{ID: id, Name: "Marked"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"courses": out})
		return
	}
	_, _ = io.WriteString(w, `{}`)
}

func TestWebBookmarks(t *testing.T) {
	title := &fakeTitle{marks: map[string][]string{}}
	ts := httptest.NewServer(title)
	defer ts.Close()

	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("openpak_session")
		switch {
		case err != nil:
			http.Error(w, "no", http.StatusUnauthorized)
		case c.Value == "linked":
			io.WriteString(w, `{"account_id":"a1","display_name":"Tobi"}`)
		case c.Value == "unlinked":
			io.WriteString(w, `{"account_id":"a2","display_name":"Nobody"}`)
		default:
			http.Error(w, "no", http.StatusUnauthorized)
		}
	}))
	defer core.Close()
	links := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Internal-Key") != "lk" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("account_id") == "a1" {
			io.WriteString(w, `{"nnid":"wiiuplayer"}`)
			return
		}
		http.NotFound(w, r)
	}))
	defer links.Close()

	st := testutil.DB(t)
	populate(t, st)
	srv, err := web.New(web.Config{
		Store: st, Upstream: map[string]*upstream.Client{"smm1": upstream.New("smm1", ts.URL, "k")},
		Auth: auth.New(core.URL, ""), Link: link.New(links.URL, "lk"), SessionCookie: "opak_session", CSRFSecret: "test-secret",
		Log: slog.New(slog.DiscardHandler),
	})
	if err != nil {
		t.Fatal(err)
	}
	h := srv.Handler()
	const course = "/course/smm1/0123456789ABCDEF"

	// * No control for anonymous visitors beyond the hint, and none for the Switch game
	if _, body, _ := get(h, course); !strings.Contains(body, "Sign in with your OpenPak account to bookmark") {
		t.Error("anonymous hint missing")
	}
	if _, body, _ := get(h, "/course/smm2/ABC-DEF-GH1", "Cookie", "opak_session=linked"); strings.Contains(body, "ookmark") {
		t.Error("the Switch game has no bookmark control")
	}
	if _, body, _ := get(h, course, "Cookie", "opak_session=unlinked"); !strings.Contains(body, "Link your Wii U Network ID") || strings.Contains(body, `action="/bookmark/`) {
		t.Error("unlinked account must be told to link, with no form")
	}

	// * Linked: a form with a token
	_, body, _ := get(h, course, "Cookie", "opak_session=linked")
	m := regexp.MustCompile(`name="csrf" value="([0-9a-f]+)"`).FindStringSubmatch(body)
	if m == nil || !strings.Contains(body, `action="/bookmark/smm1/0123456789ABCDEF"`) {
		t.Fatalf("no bookmark form:\n%s", body)
	}
	post := func(path, token, cookie string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(url.Values{"csrf": {token}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Cookie", "opak_session="+cookie)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// * Wrong token, wrong session, anonymous: refused, nothing written to the title
	title.calls = nil
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"bad token":     post("/bookmark/smm1/0123456789ABCDEF", "00", "linked"),
		"other session": post("/bookmark/smm1/0123456789ABCDEF", m[1], "unlinked"),
		"anonymous":     post("/bookmark/smm1/0123456789ABCDEF", m[1], "stale"),
	} {
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if len(title.calls) != 0 {
		t.Fatalf("title was called by a refused request: %v", title.calls)
	}

	if rec := post("/bookmark/smm1/0123456789ABCDEF", m[1], "linked"); rec.Code != http.StatusSeeOther {
		t.Fatalf("bookmark: %d %s", rec.Code, rec.Body)
	}
	if got := title.marks["wiiuplayer"]; len(got) != 1 || got[0] != "0123456789ABCDEF" {
		t.Fatalf("title marks = %v", title.marks)
	}

	// * Now it shows as saved, and in the list
	if _, body, _ := get(h, course, "Cookie", "opak_session=linked"); !strings.Contains(body, `action="/unbookmark/smm1/0123456789ABCDEF"`) {
		t.Error("saved state missing")
	}
	if _, body, _ := get(h, "/bookmarks", "Cookie", "opak_session=linked"); !strings.Contains(body, "Marked") {
		t.Errorf("list: %s", body)
	}
	if rec := post("/unbookmark/smm1/0123456789ABCDEF", m[1], "linked"); rec.Code != http.StatusSeeOther || len(title.marks["wiiuplayer"]) != 0 {
		t.Errorf("unbookmark: %d %v", rec.Code, title.marks)
	}
	if _, body, _ := get(h, "/bookmarks", "Cookie", "opak_session=unlinked"); !strings.Contains(body, "Link your Wii U") {
		t.Error("list for unlinked account")
	}
}

func TestWebBookmarksOffWithoutLink(t *testing.T) {
	st := testutil.DB(t)
	populate(t, st)
	srv, _ := web.New(web.Config{Store: st, Log: slog.New(slog.DiscardHandler)})
	h := srv.Handler()
	if code, _, _ := get(h, "/bookmarks"); code != http.StatusNotFound {
		t.Errorf("/bookmarks with no link configured: %d", code)
	}
	if _, body, _ := get(h, "/course/smm1/0123456789ABCDEF"); strings.Contains(body, "ookmark") {
		t.Error("bookmark control shown while off")
	}
}
