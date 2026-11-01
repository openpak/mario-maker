// Command mario-maker is the OpenPak course site for Super Mario Maker and Super
// Mario Maker 2: it copies courses from the two titles' read APIs into its own
// Postgres and serves them as server-rendered pages.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/openpak/mario-maker/internal/auth"
	"github.com/openpak/mario-maker/internal/link"
	"github.com/openpak/mario-maker/internal/model"
	"github.com/openpak/mario-maker/internal/store"
	"github.com/openpak/mario-maker/internal/syncer"
	"github.com/openpak/mario-maker/internal/upstream"
	"github.com/openpak/mario-maker/internal/web"
)

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envDur(log *slog.Logger, k string, def time.Duration) time.Duration {
	v := os.Getenv(k)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		log.Warn("bad duration, using default", "var", k, "value", v)
		return def
	}
	return d
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	dsn := os.Getenv("MARIO_MAKER_DATABASE_URL")
	if dsn == "" {
		return errors.New("MARIO_MAKER_DATABASE_URL is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := openStore(ctx, dsn, log)
	if err != nil {
		return err
	}
	defer st.Close()

	clients := map[string]*upstream.Client{}
	var list []*upstream.Client
	for game, p := range map[string]string{model.SMM1: "SMM1", model.SMM2: "SMM2"} {
		if u := os.Getenv(p + "_API_URL"); u != "" {
			c := upstream.New(game, u, os.Getenv(p+"_API_KEY"))
			clients[game] = c
		}
	}
	for _, g := range model.Games { // stable order
		if c := clients[g]; c != nil {
			list = append(list, c)
		}
	}

	sy := &syncer.Syncer{Store: st, Clients: list, Interval: envDur(log, "MARIO_MAKER_SYNC_INTERVAL", 60*time.Second),
		FullEvery: envDur(log, "MARIO_MAKER_FULL_SYNC_INTERVAL", 10*time.Minute), Log: log}
	go sy.Run(ctx)

	mb, _ := strconv.Atoi(env("MARIO_MAKER_THUMB_CACHE_MB", "64"))
	srv, err := web.New(web.Config{
		Store: st, Upstream: clients,
		Auth: auth.New(os.Getenv("MARIO_MAKER_CORE_BASE_URL"), os.Getenv("MARIO_MAKER_SESSION_COOKIE")),
		Link: linkResolver(), SessionCookie: env("MARIO_MAKER_SESSION_COOKIE", "opak_session"), CSRFSecret: os.Getenv("MARIO_MAKER_CSRF_SECRET"),
		PublicURL:  os.Getenv("MARIO_MAKER_PUBLIC_URL"),
		WebsiteURL: os.Getenv("MARIO_MAKER_WEBSITE_URL"),
		CacheDir:   os.Getenv("MARIO_MAKER_CACHE_DIR"), CacheMB: mb, Log: log,
		SyncStatus: func() any { return sy.Statuses() },
	})
	if err != nil {
		return err
	}
	hs := &http.Server{Addr: env("MARIO_MAKER_HTTP_ADDR", ":20161"), Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 120 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	log.Info("listening", "addr", hs.Addr, "games", len(list))
	if err := hs.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http: %w", err)
	}
	return nil
}

// openStore retries for a while: after a reboot the site may start before Postgres.
func openStore(ctx context.Context, dsn string, log *slog.Logger) (*store.Store, error) {
	var err error
	for i := 0; i < 30; i++ {
		var st *store.Store
		if st, err = store.Open(ctx, dsn); err == nil {
			return st, nil
		}
		log.Warn("database not ready", "err", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return nil, err
}

// linkResolver is nil (web bookmarking off) unless MARIO_MAKER_LINK_URL is set.
func linkResolver() link.Resolver {
	if l := link.New(os.Getenv("MARIO_MAKER_LINK_URL"), os.Getenv("MARIO_MAKER_LINK_KEY")); l != nil {
		return l
	}
	return nil
}
