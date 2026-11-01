// Package auth shows who is signed in, using the same core session the OpenPak
// website uses. It is read-only and optional: the website owns the sign-in forms
// and the session cookie, which holds a core session id. This package presents
// that id to the core's GET /v1/whoami and nothing more. Without a core URL it is
// disabled and every visitor is anonymous.
package auth

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// coreCookie is the cookie name the core reads its session id from.
const coreCookie = "openpak_session"

// User is the signed-in account, as much as the header needs.
type User struct {
	ID          string
	DisplayName string
	Handle      string
}

// Name is the best label for the user.
func (u User) Name() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Handle
}

// Client resolves sessions through the core.
type Client struct {
	CoreBaseURL string // e.g. http://openpak-account:20000
	CookieName  string // the website's cookie name, default opak_session
	HTTP        *http.Client

	mu    sync.Mutex
	cache map[[32]byte]entry
}

type entry struct {
	user *User
	at   time.Time
}

const (
	okTTL   = 30 * time.Second
	failTTL = 10 * time.Second
)

// New returns nil when coreBaseURL is empty (sign-in display disabled).
func New(coreBaseURL, cookieName string) *Client {
	if strings.TrimSpace(coreBaseURL) == "" {
		return nil
	}
	if cookieName == "" {
		cookieName = "opak_session"
	}
	return &Client{
		CoreBaseURL: strings.TrimRight(coreBaseURL, "/"), CookieName: cookieName,
		HTTP: &http.Client{Timeout: 3 * time.Second}, cache: map[[32]byte]entry{},
	}
}

// User returns the signed-in user for the request, or nil. A nil Client is anonymous.
func (c *Client) User(r *http.Request) *User {
	if c == nil {
		return nil
	}
	ck, err := r.Cookie(c.CookieName)
	if err != nil || ck.Value == "" {
		return nil
	}
	key := sha256.Sum256([]byte(ck.Value))
	c.mu.Lock()
	if e, ok := c.cache[key]; ok {
		ttl := okTTL
		if e.user == nil {
			ttl = failTTL
		}
		if time.Since(e.at) < ttl {
			c.mu.Unlock()
			return e.user
		}
	}
	c.mu.Unlock()

	u := c.whoami(r.Context(), ck.Value)
	c.mu.Lock()
	if len(c.cache) > 4096 {
		clear(c.cache)
	}
	c.cache[key] = entry{user: u, at: time.Now()}
	c.mu.Unlock()
	return u
}

func (c *Client) whoami(ctx context.Context, session string) *User {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.CoreBaseURL+"/v1/whoami", nil)
	if err != nil {
		return nil
	}
	req.AddCookie(&http.Cookie{Name: coreCookie, Value: session})
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var body struct {
		AccountID   string `json:"account_id"`
		DisplayName string `json:"display_name"`
		Handle      string `json:"handle"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body) != nil || body.AccountID == "" {
		return nil
	}
	return &User{ID: body.AccountID, DisplayName: body.DisplayName, Handle: body.Handle}
}
