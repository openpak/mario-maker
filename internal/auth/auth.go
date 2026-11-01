// Package auth signs people in through openpak.org single sign-on (docs/sso.md in the website
// repo; client "openmaker"). The browser goes to openpak.org, comes back to /auth/callback with a
// one-minute code, and this server swaps the code for the account behind it. The site keeps its
// own signed session cookie and never sees a password. Without a configuration it is disabled and
// every visitor is anonymous.
package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ClientID is the SSO client id registered on openpak.org.
const ClientID = "openmaker"

const (
	// CookieName is this site's own session cookie.
	CookieName = "openmaker_session"
	flowCookie = "openmaker_sso"
	sessionTTL = 7 * 24 * time.Hour
	flowTTL    = 10 * time.Minute
)

// Config wires the client. All of PublicURL, InternalURL, Secret and SessionKey are needed.
type Config struct {
	PublicURL   string // https://maker.openpak.org, the registered redirect origin
	OpenPakURL  string // https://openpak.org, where the browser signs in
	InternalURL string // http://openpak-website:20010, server to server
	Secret      string // SSO_CLIENT_OPENMAKER_SECRET
	SessionKey  string // 32+ characters; signs the session and flow cookies
	HTTP        *http.Client
	Log         *slog.Logger
}

// User is the signed-in account, as much as the pages need.
type User struct {
	ID          string
	DisplayName string
	WiiUPID     int64 // 0 when the account has no Wii U linked
}

// Name is the label for the user.
func (u User) Name() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return "Player"
}

// Initial is the letter on the account chip, like openpak.org's.
func (u User) Initial() string {
	for _, r := range u.Name() {
		return strings.ToUpper(string(r))
	}
	return "?"
}

// Auth is the SSO client. A nil *Auth is anonymous.
type Auth struct {
	cfg Config
}

// New returns nil unless the configuration is complete (sign-in is then off).
func New(c Config) *Auth {
	c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	c.InternalURL = strings.TrimRight(c.InternalURL, "/")
	c.OpenPakURL = strings.TrimRight(c.OpenPakURL, "/")
	if c.OpenPakURL == "" {
		c.OpenPakURL = "https://openpak.org"
	}
	if c.PublicURL == "" || c.InternalURL == "" || c.Secret == "" || len(c.SessionKey) < 32 {
		return nil
	}
	if c.HTTP == nil {
		c.HTTP = &http.Client{Timeout: 10 * time.Second}
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
	return &Auth{cfg: c}
}

type session struct {
	Account string `json:"a"`
	Name    string `json:"n"`
	PID     int64  `json:"p,omitempty"`
	Expires int64  `json:"e"`
}

type flow struct {
	State    string `json:"s"`
	Verifier string `json:"v"`
	Next     string `json:"n"`
	Expires  int64  `json:"e"`
}

func (a *Auth) sign(data []byte) string {
	m := hmac.New(sha256.New, []byte(a.cfg.SessionKey))
	m.Write(data)
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (a *Auth) seal(v any) string {
	b, _ := json.Marshal(v)
	return base64.RawURLEncoding.EncodeToString(b) + "." + a.sign(b)
}

func (a *Auth) open(value string, v any) bool {
	body, sig, ok := strings.Cut(value, ".")
	if !ok {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || !hmac.Equal([]byte(sig), []byte(a.sign(raw))) {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

func (a *Auth) setCookie(w http.ResponseWriter, name, value, path string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: path, HttpOnly: true,
		Secure: strings.HasPrefix(a.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

// User returns the signed-in visitor, if the session cookie is ours and unexpired.
func (a *Auth) User(r *http.Request) *User {
	if a == nil {
		return nil
	}
	c, err := r.Cookie(CookieName)
	if err != nil {
		return nil
	}
	var s session
	if !a.open(c.Value, &s) || s.Account == "" || time.Now().Unix() > s.Expires {
		return nil
	}
	return &User{ID: s.Account, DisplayName: s.Name, WiiUPID: s.PID}
}

// SessionCookie is a valid session cookie for u, for tests.
func (a *Auth) SessionCookie(u User) *http.Cookie {
	return &http.Cookie{Name: CookieName, Value: a.seal(session{Account: u.ID, Name: u.DisplayName, PID: u.WiiUPID,
		Expires: time.Now().Add(sessionTTL).Unix()})}
}

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// localPath keeps "next" on this site: a path, never another host or the auth routes.
func localPath(p string) string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/\\") ||
		strings.HasPrefix(p, "/auth/") || strings.ContainsAny(p, "\r\n") {
		return "/"
	}
	return p
}

func (a *Auth) callbackURL() string { return a.cfg.PublicURL + "/auth/callback" }

// Routes registers /auth/login, /auth/callback and /auth/logout. fail renders the site's own
// page for a sign-in that did not finish. A nil Auth registers nothing (the paths are 404).
func (a *Auth) Routes(mux *http.ServeMux, fail func(w http.ResponseWriter, r *http.Request, status int)) {
	if a == nil {
		return
	}
	mux.HandleFunc("GET /auth/login", a.login)
	mux.HandleFunc("GET /auth/callback", func(w http.ResponseWriter, r *http.Request) { a.callback(w, r, fail) })
	mux.HandleFunc("GET /auth/logout", a.logout)
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	f := flow{State: random(24), Verifier: random(32), Next: localPath(r.URL.Query().Get("next")), Expires: time.Now().Add(flowTTL).Unix()}
	a.setCookie(w, flowCookie, a.seal(f), "/auth", int(flowTTL.Seconds()))
	sum := sha256.Sum256([]byte(f.Verifier))
	q := url.Values{
		"client":                {ClientID},
		"redirect_uri":          {a.callbackURL()},
		"state":                 {f.State},
		"code_challenge":        {base64.RawURLEncoding.EncodeToString(sum[:])},
		"code_challenge_method": {"S256"},
	}
	http.Redirect(w, r, a.cfg.OpenPakURL+"/account/sso/authorize?"+q.Encode(), http.StatusFound)
}

func (a *Auth) callback(w http.ResponseWriter, r *http.Request, fail func(http.ResponseWriter, *http.Request, int)) {
	w.Header().Set("Cache-Control", "no-store")
	var f flow
	c, err := r.Cookie(flowCookie)
	a.setCookie(w, flowCookie, "", "/auth", -1) // a flow is used once, whatever happens
	q := r.URL.Query()
	code, state := q.Get("code"), q.Get("state")
	if err != nil || !a.open(c.Value, &f) || time.Now().Unix() > f.Expires || f.State == "" || state == "" || state != f.State || code == "" {
		fail(w, r, http.StatusBadRequest)
		return
	}
	s, err := a.exchange(r.Context(), code, f.Verifier)
	if err != nil {
		a.cfg.Log.Error("sign-in exchange", "err", err)
		fail(w, r, http.StatusBadGateway)
		return
	}
	a.setCookie(w, CookieName, a.seal(s), "/", int(sessionTTL.Seconds()))
	http.Redirect(w, r, localPath(f.Next), http.StatusSeeOther)
}

var errRefused = errors.New("token exchange refused")

// exchange swaps the one-time code for the account, server to server.
func (a *Auth) exchange(ctx context.Context, code, verifier string) (session, error) {
	form := url.Values{"code": {code}, "redirect_uri": {a.callbackURL()}, "code_verifier": {verifier}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.cfg.InternalURL+"/internal/sso/token", strings.NewReader(form.Encode()))
	if err != nil {
		return session{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(ClientID, a.cfg.Secret)
	resp, err := a.cfg.HTTP.Do(req)
	if err != nil {
		return session{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return session{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return session{}, errors.Join(errRefused, errors.New(resp.Status))
	}
	var t struct {
		AccountID   string      `json:"account_id"`
		DisplayName string      `json:"display_name"`
		WiiUPID     json.Number `json:"wiiu_pid"`
	}
	if err := json.Unmarshal(body, &t); err != nil || t.AccountID == "" {
		return session{}, errors.Join(errRefused, errors.New("unreadable answer"))
	}
	s := session{Account: t.AccountID, Name: t.DisplayName, Expires: time.Now().Add(sessionTTL).Unix()}
	if n, err := strconv.ParseInt(t.WiiUPID.String(), 10, 64); err == nil && n > 0 {
		s.PID = n
	}
	return s, nil
}

// logout ends this site's session only and returns to the home page. openpak.org stays signed
// in, like leaving one Google app; the next Sign in may sign straight back in.
func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	a.setCookie(w, CookieName, "", "/", -1)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
