package store

import (
	"context"
	"strings"
)

// DeletedUser is the credit a removed account's published courses keep.
const DeletedUser = "Deleted user"

const eraseSchema = `
-- Makers whose account was erased: lower-cased upstream maker ids per game. The syncer re-reads
-- courses from the title servers, so the credit is scrubbed again after every upsert.
CREATE TABLE IF NOT EXISTS erased_makers (
  game      text        NOT NULL,
  maker_key text        NOT NULL,
  erased_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (game, maker_key)
);
CREATE INDEX IF NOT EXISTS courses_maker_lower_idx ON courses (game, lower(maker_id));
`

// scrubErased credits courses of erased makers to "Deleted user" (the course stays, the identity
// goes), drops their maker rows and returns how many rows it changed. Idempotent.
func (s *Store) scrubErased(ctx context.Context) (int, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE courses c SET maker_id='deleted', maker_name=$1,
  search=to_tsvector('simple', c.name||' '||array_to_string(c.tags,' ')||' '||c.style||' '||c.theme||' '||c.description||' '||$1)
FROM erased_makers e WHERE c.game=e.game AND lower(c.maker_id)=e.maker_key`, DeletedUser)
	if err != nil {
		return 0, err
	}
	n := int(tag.RowsAffected())
	tag, err = s.pool.Exec(ctx, `DELETE FROM makers m USING erased_makers e WHERE m.game=e.game AND lower(m.maker_id)=e.maker_key`)
	if err != nil {
		return 0, err
	}
	return n + int(tag.RowsAffected()), nil
}

// EraseAccount removes what the site holds for an OpenPak account: its History claims (account id,
// name, PID, NNID), and the identity on its published courses. makerKeys are the ids the account's
// Wii U link gives (NNID, PID); any live maker whose id equals one (case-insensitive) is marked
// erased and its courses are credited to "Deleted user". Returns the rows changed. Idempotent.
func (s *Store) EraseAccount(ctx context.Context, accountID string, makerKeys []string) (int, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `DELETE FROM archive_claims WHERE account_id=$1`, accountID)
	if err != nil {
		return 0, err
	}
	rows := int(tag.RowsAffected())
	for _, k := range makerKeys {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" || k == "0" {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO erased_makers (game, maker_key)
SELECT game, $1 FROM makers WHERE lower(maker_id)=$1 UNION SELECT game, $1 FROM courses WHERE lower(maker_id)=$1
ON CONFLICT DO NOTHING`, k); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	n, err := s.scrubErased(ctx)
	if err != nil {
		return 0, err
	}
	return rows + n, nil
}
