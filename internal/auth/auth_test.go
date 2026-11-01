package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// fakeWebsite stands in for openpak.org's /internal/sso/token: it checks the client credentials
// and the PKCE verifier like the real one.
type fakeWebsite struct {
	srv       *httptest.Server
	calls     int
	form      url.Values
	challenge string
	pid       string
}

func newFake(t *testing.T) *fakeWebsite {
	t.Helper()
	f := &fakeWebsite{pid: "1800000123"}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls++
		_ = r.ParseForm()
		f.form = r.PostForm
		id, secret, _ := r.BasicAuth()
		sum := sha256.Sum256([]byte(r.PostFormValue("code_verifier")))
		if r.URL.Path != "/internal/sso/token" || r.Method != "POST" || id != "openmaker" || secret != "s3cret" ||
			r.PostFormValue("code") != "good" || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":"invalid_grant"}`)
			return
		}
		fmt.Fprintf(w, `{"account_id":"acc-1","display_name":"Plumber","wiiu_pid":%s}`, f.pid)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func newAuth(t *testing.T, f *fakeWebsite) (*Auth, *http.ServeMux, *int) {
	t.Helper()
	a := New(Config{PublicURL: "https://maker.test", InternalURL: f.srv.URL, Secret: "s3cret", SessionKey: strings.Repeat("k", 32)})
	if a == nil {
		t.Fatal("not configured")
	}
	failed := new(int)
	mux := http.NewServeMux()
	a.Routes(mux, func(w http.ResponseWriter, r *http.Request, status int) { *failed = status; w.WriteHeader(status) })
	return a, mux, failed
}

func do(h http.Handler, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func cookieNamed(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// login runs /auth/login and returns the authorize URL and the flow cookie.
func login(t *testing.T, mux *http.ServeMux, f *fakeWebsite, path string) (*url.URL, *http.Cookie) {
	t.Helper()
	w := do(mux, path)
	if w.Code != 302 {
		t.Fatalf("login: %d", w.Code)
	}
	u, _ := url.Parse(w.Header().Get("Location"))
	f.challenge = u.Query().Get("code_challenge")
	c := cookieNamed(w, flowCookie)
	if c == nil {
		t.Fatal("no flow cookie")
	}
	return u, c
}

func TestLoginRedirect(t *testing.T) {
	f := newFake(t)
	_, mux, _ := newAuth(t, f)
	u, c := login(t, mux, f, "/auth/login?next=/browse")
	q := u.Query()
	if u.Host != "openpak.org" || u.Path != "/account/sso/authorize" || q.Get("client") != "openmaker" ||
		q.Get("redirect_uri") != "https://maker.test/auth/callback" || q.Get("code_challenge_method") != "S256" || len(q.Get("state")) < 8 {
		t.Errorf("authorize URL: %s", u)
	}
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/auth" {
		t.Errorf("flow cookie: %+v", c)
	}
}

func TestCallbackHappyPath(t *testing.T) {
	f := newFake(t)
	a, mux, _ := newAuth(t, f)
	u, flowC := login(t, mux, f, "/auth/login?next=/browse")
	w := do(mux, "/auth/callback?code=good&state="+url.QueryEscape(u.Query().Get("state")), flowC)
	if w.Code != 303 || w.Header().Get("Location") != "/browse" {
		t.Fatalf("callback: %d %s", w.Code, w.Header().Get("Location"))
	}
	if f.calls != 1 || f.form.Get("redirect_uri") != "https://maker.test/auth/callback" {
		t.Errorf("exchange: %d %v", f.calls, f.form)
	}
	sess := cookieNamed(w, CookieName)
	if sess == nil || !sess.HttpOnly || !sess.Secure || sess.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie: %+v", sess)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(sess)
	if u := a.User(r); u == nil || u.ID != "acc-1" || u.Name() != "Plumber" || u.WiiUPID != 1800000123 || u.Initial() != "P" {
		t.Errorf("user: %+v", u)
	}
	// A forged session is a stranger.
	r = httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: strings.Replace(sess.Value, ".", "x.", 1)})
	if a.User(r) != nil {
		t.Error("forged session accepted")
	}
	// Sign out clears only the site session and returns home; openpak.org is never visited.
	out := do(mux, "/auth/logout", sess)
	if out.Code != 303 || out.Header().Get("Location") != "/" {
		t.Errorf("logout: %d %s", out.Code, out.Header().Get("Location"))
	}
	if c := cookieNamed(out, CookieName); c == nil || c.MaxAge >= 0 {
		t.Error("session not cleared")
	}
}

func TestNoWiiULinked(t *testing.T) {
	f := newFake(t)
	f.pid = "null"
	a, mux, _ := newAuth(t, f)
	u, flowC := login(t, mux, f, "/auth/login")
	w := do(mux, "/auth/callback?code=good&state="+url.QueryEscape(u.Query().Get("state")), flowC)
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(cookieNamed(w, CookieName))
	if u := a.User(r); u == nil || u.WiiUPID != 0 {
		t.Errorf("user: %+v", u)
	}
}

func TestCallbackStateMismatch(t *testing.T) {
	f := newFake(t)
	_, mux, failed := newAuth(t, f)
	_, flowC := login(t, mux, f, "/auth/login")
	for name, path := range map[string]string{
		"wrong state":   "/auth/callback?code=good&state=nope",
		"missing state": "/auth/callback?code=good",
		"missing code":  "/auth/callback?state=x",
	} {
		*failed = 0
		w := do(mux, path, flowC)
		if *failed != 400 || f.calls != 0 || cookieNamed(w, CookieName) != nil {
			t.Errorf("%s: failed=%d exchanges=%d", name, *failed, f.calls)
		}
	}
	*failed = 0
	do(mux, "/auth/callback?code=good&state=abc")
	if *failed != 400 {
		t.Errorf("no flow cookie: %d", *failed)
	}
	*failed = 0
	do(mux, "/auth/callback?code=good&state=abc", &http.Cookie{Name: flowCookie, Value: "x" + flowC.Value})
	if *failed != 400 || f.calls != 0 {
		t.Errorf("tampered flow cookie: %d", *failed)
	}
}

func TestCallbackExchangeFails(t *testing.T) {
	f := newFake(t)
	a, mux, failed := newAuth(t, f)
	u, flowC := login(t, mux, f, "/auth/login")
	state := url.QueryEscape(u.Query().Get("state"))
	w := do(mux, "/auth/callback?code=bad&state="+state, flowC)
	if *failed != 502 || cookieNamed(w, CookieName) != nil {
		t.Errorf("refused code: %d", *failed)
	}
	*failed = 0
	a.cfg.InternalURL = "http://127.0.0.1:1"
	if do(mux, "/auth/callback?code=good&state="+state, flowC); *failed != 502 {
		t.Errorf("unreachable website: %d", *failed)
	}
}

func TestDisabledAndLocalPath(t *testing.T) {
	if New(Config{PublicURL: "https://maker.test", InternalURL: "http://x", Secret: "s", SessionKey: "short"}) != nil {
		t.Error("a short session key must disable sign-in")
	}
	var a *Auth
	mux := http.NewServeMux()
	a.Routes(mux, nil)
	if do(mux, "/auth/login").Code != 404 || a.User(httptest.NewRequest("GET", "/", nil)) != nil {
		t.Error("nil Auth must be anonymous with no routes")
	}
	for in, want := range map[string]string{"/browse": "/browse", "//evil.test": "/", "https://evil.test": "/", "": "/", "/auth/logout": "/", "/\\evil": "/"} {
		if got := localPath(in); got != want {
			t.Errorf("localPath(%q) = %q", in, got)
		}
	}
}
