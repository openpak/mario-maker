// Package erase serves account erasure (card #120): DELETE /internal/accounts/{account_id}/data
// with Authorization: Bearer ACCOUNT_ERASE_KEY, on its own listener (the site's public listener
// never carries it).
package erase

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/openpak/mario-maker/internal/link"
)

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Eraser is the store side.
type Eraser interface {
	EraseAccount(ctx context.Context, accountID string, makerKeys []string) (int, error)
}

// Service erases one account. Link may be nil (no Wii U link service configured): then only the
// claims, which are keyed by account id, can be found.
type Service struct {
	Store Eraser
	Link  link.Resolver
	Key   string // ACCOUNT_ERASE_KEY; empty = the endpoint answers 503
	Log   *slog.Logger
}

// Erase removes the account's data and returns the rows changed.
func (s *Service) Erase(ctx context.Context, accountID string) (int, error) {
	var keys []string
	if s.Link != nil {
		id, err := s.Link.Identity(ctx, accountID)
		if err != nil {
			return 0, err // the link is read before anything is deleted: a failure retries cleanly
		}
		keys = append(keys, id.NNID)
		if id.PID > 0 {
			keys = append(keys, strconv.FormatInt(id.PID, 10))
		}
	}
	n, err := s.Store.EraseAccount(ctx, accountID, keys)
	if err != nil {
		return 0, err
	}
	s.Log.Info("account erased", "account", accountID, "rows", n, "service", "mario-maker")
	return n, nil
}

// Handler is the erase listener's handler.
func (s *Service) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("DELETE /internal/accounts/{account_id}/data", func(w http.ResponseWriter, r *http.Request) {
		if s.Key == "" {
			reply(w, 503, map[string]any{"error": "erase disabled"})
			return
		}
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") || subtle.ConstantTimeCompare([]byte(strings.TrimPrefix(h, "Bearer ")), []byte(s.Key)) != 1 {
			reply(w, 401, map[string]any{"error": "unauthorized"})
			return
		}
		id := r.PathValue("account_id")
		if !uuidRE.MatchString(id) {
			reply(w, 400, map[string]any{"error": "bad account id"})
			return
		}
		n, err := s.Erase(r.Context(), strings.ToLower(id))
		if err != nil {
			s.Log.Warn("erase failed", "err", err)
			reply(w, 502, map[string]any{"error": "erase failed"})
			return
		}
		reply(w, 200, map[string]any{"service": "mario-maker", "rows": n})
	})
	return m
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
