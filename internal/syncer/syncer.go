// Package syncer copies courses and makers from the upstream read APIs into the
// site's own database.
//
// Every cycle fetches courses changed since the last successful cycle
// (updated_since) and all makers. Every FullEvery (and on start) it lists every
// course, then re-checks courses the listing did not mention one by one and
// soft-deletes those upstream answers 404 for.
package syncer

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/openpak/mario-maker/internal/model"
	"github.com/openpak/mario-maker/internal/store"
	"github.com/openpak/mario-maker/internal/upstream"
)

// overlap re-reads a little before the watermark so clock skew loses nothing.
const overlap = 2 * time.Minute

// Status is one game's last sync outcome, shown by /healthz.
type Status struct {
	Game      string    `json:"game"`
	LastOK    time.Time `json:"last_ok,omitzero"`
	LastFull  time.Time `json:"last_full,omitzero"`
	LastError string    `json:"last_error,omitempty"`
}

// Syncer runs the periodic copy.
type Syncer struct {
	Store     *store.Store
	Clients   []*upstream.Client
	Interval  time.Duration
	FullEvery time.Duration
	Log       *slog.Logger

	mu     sync.Mutex
	status map[string]*Status
	water  map[string]time.Time
}

// Statuses returns the per-game status.
func (s *Syncer) Statuses() []Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Status
	for _, c := range s.Clients {
		if st := s.status[c.Game]; st != nil {
			out = append(out, *st)
		}
	}
	return out
}

// Run syncs on start and then every Interval until ctx ends.
func (s *Syncer) Run(ctx context.Context) {
	if len(s.Clients) == 0 {
		s.Log.Info("no upstream configured; the site shows only what is already stored")
		return
	}
	t := time.NewTicker(s.Interval)
	defer t.Stop()
	for {
		s.Once(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Once runs one cycle for every configured game.
func (s *Syncer) Once(ctx context.Context) {
	s.mu.Lock()
	if s.status == nil {
		s.status, s.water = map[string]*Status{}, map[string]time.Time{}
	}
	s.mu.Unlock()
	for _, c := range s.Clients {
		s.mu.Lock()
		st := s.status[c.Game]
		if st == nil {
			st = &Status{Game: c.Game}
			s.status[c.Game] = st
		}
		water, lastFull := s.water[c.Game], st.LastFull
		s.mu.Unlock()

		started := time.Now()
		full := water.IsZero() || time.Since(lastFull) >= s.FullEvery
		err := s.game(ctx, c, water, full)

		s.mu.Lock()
		if err != nil {
			st.LastError = err.Error()
			s.Log.Warn("sync failed", "game", c.Game, "err", err)
		} else {
			st.LastError, st.LastOK = "", time.Now()
			s.water[c.Game] = started.Add(-overlap)
			if full {
				st.LastFull = started
			}
		}
		s.mu.Unlock()
	}
}

func (s *Syncer) game(ctx context.Context, c *upstream.Client, water time.Time, full bool) error {
	started := time.Now()
	since := water
	if full {
		since = time.Time{}
	}
	n := 0
	for off := 0; ; off += upstream.PageSize {
		cs, total, err := c.Courses(ctx, upstream.PageSize, off, since)
		if err != nil {
			return err
		}
		if err := s.Store.UpsertCourses(ctx, cs); err != nil {
			return err
		}
		n += len(cs)
		if len(cs) == 0 || off+upstream.PageSize >= total {
			break
		}
	}
	for off := 0; ; off += upstream.PageSize {
		ms, total, err := c.Makers(ctx, upstream.PageSize, off)
		if err != nil {
			return err
		}
		if err := s.Store.UpsertMakers(ctx, ms); err != nil {
			return err
		}
		if len(ms) == 0 || off+upstream.PageSize >= total {
			break
		}
	}
	deleted := 0
	if full {
		ids, err := s.Store.UnseenIDs(ctx, c.Game, started)
		if err != nil {
			return err
		}
		for _, id := range ids {
			cur, err := c.Course(ctx, id)
			switch {
			case errors.Is(err, upstream.ErrNotFound):
				if err := s.Store.SoftDelete(ctx, c.Game, id); err != nil {
					return err
				}
				deleted++
			case err != nil:
				return err
			default:
				// Listed paging shifted under us; the course is still there.
				if err := s.Store.UpsertCourses(ctx, []model.Course{cur}); err != nil {
					return err
				}
			}
		}
	}
	s.Log.Debug("synced", "game", c.Game, "courses", n, "full", full, "deleted", deleted)
	return nil
}
