// Package link finds the Wii U account (NNID) that belongs to a signed-in OpenPak account. The
// in-game bookmark list belongs to the Wii U maker, so the site can only bookmark for someone
// whose OpenPak account is tied to an NNID. The tie lives outside this service (the core
// account and nn-account); this package asks for it over one small HTTP call and does nothing
// when that call is not configured.
package link

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Resolver turns an OpenPak account id into an NNID.
type Resolver interface {
	// NNID returns the linked NNID, or "" when the account has none.
	NNID(ctx context.Context, accountID string) (string, error)
	// Identity returns the Wii U name and PID the core links tie to the account (zero values when
	// it has none). The PID, not the name, is what proves a claim.
	Identity(ctx context.Context, accountID string) (Identity, error)
}

// Identity is the Wii U identity of an OpenPak account, from the core links.
type Identity struct {
	NNID string
	PID  int64
}

// HTTP asks GET {URL}?account_id=<id> with header X-Internal-Key, and expects {"nnid": "..."}
// (an empty or missing nnid, or 404, means not linked).
type HTTP struct {
	URL, Key string
	Client   *http.Client
}

// New returns nil when url is empty (bookmarking from the web is then off).
func New(rawURL, key string) *HTTP {
	if strings.TrimSpace(rawURL) == "" {
		return nil
	}
	return &HTTP{URL: rawURL, Key: key, Client: &http.Client{Timeout: 3 * time.Second}}
}

// NNID implements Resolver.
func (h *HTTP) NNID(ctx context.Context, accountID string) (string, error) {
	id, err := h.Identity(ctx, accountID)
	return id.NNID, err
}

// Identity implements Resolver: {"nnid": "...", "pid": 123}, 404 or empty for no link.
func (h *HTTP) Identity(ctx context.Context, accountID string) (Identity, error) {
	u, err := url.Parse(h.URL)
	if err != nil {
		return Identity{}, err
	}
	q := u.Query()
	q.Set("account_id", accountID)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Identity{}, err
	}
	if h.Key != "" {
		req.Header.Set("X-Internal-Key", h.Key)
	}
	resp, err := h.Client.Do(req)
	if err != nil {
		return Identity{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return Identity{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		return Identity{}, io.ErrUnexpectedEOF
	}
	var body struct {
		NNID string      `json:"nnid"`
		PID  json.Number `json:"pid"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body); err != nil {
		return Identity{}, err
	}
	pid, _ := body.PID.Int64()
	if pid < 0 {
		pid = 0
	}
	return Identity{NNID: strings.TrimSpace(body.NNID), PID: pid}, nil
}
