// Package testutil gives tests a private schema in the Postgres named by
// MARIO_MAKER_TEST_DATABASE_URL; tests that need it skip when it is unset.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openpak/mario-maker/internal/store"
)

// DB returns a Store on a fresh schema, dropped when the test ends.
func DB(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("MARIO_MAKER_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("MARIO_MAKER_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	schema := "t_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	st, err := store.OpenConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		st.Close()
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})
	return st
}
