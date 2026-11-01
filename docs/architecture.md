# Architecture

One Go binary (`cmd/mario-maker`), standard library plus `pgx/v5`.

```
 Wii U title ──/v1──┐                           ┌── browser (no JavaScript needed)
                    ├─▶ syncer ─▶ Postgres ◀── web (html/template, embedded assets)
 Switch title ─/v1──┘   (60 s)   mario_maker     │
                                                 └── /thumb/{game}/{id}.jpg ─▶ upstream (cached)
```

- `internal/upstream`: client for the read contract ([api-contract.md](api-contract.md)).
- `internal/syncer`: the copy. Incremental every 60 s, full pass every 10 minutes, soft-delete of
  vanished courses (confirmed with a per-course 404 before hiding).
- `internal/store`: the site's own Postgres. Tables `courses` (primary key `game, course_id`,
  a `search` tsvector over name, tags, style, theme, description and maker name, `deleted_at`) and
  `makers`. The schema is created on start under an advisory lock. Search uses the `simple`
  text-search configuration with prefix matching on every word, and a pasted course code matches
  exactly. The site never reads another service's storage.
- `internal/web`: pages, thumbnail proxy and the embedded `static/` (a stylesheet copied by hand
  from the website's design tokens, and the self-hosted M PLUS Rounded 1c fonts, OFL).
- `internal/auth`: openpak.org single sign-on and the site's session.

Pages: `/` (shelves; `?game=smm1|smm2` for the tabs), `/browse` (`q`, `game`, `sort`, `page`),
`/course/{game}/{id}`, `/maker/{game}/{id}`, `/thumb/{game}/{id}.jpg`, `/healthz`.

## Thumbnails

`/thumb` serves only courses the database lists with `has_thumbnail`. The first request fetches
the JPEG from the title, then it lives in a bounded in-memory LRU (`MARIO_MAKER_THUMB_CACHE_MB`)
and, when `MARIO_MAKER_CACHE_DIR` is set, on disk. The cache key includes the course's `updated`
time, so a changed course refetches. Responses carry an ETag and a one-hour public cache header.

## Sign-in

The site has no accounts of its own and no sign-in form. Sign-in is openpak.org single sign-on (client `openmaker`; the
contract is `docs/sso.md` in the website repo). `/auth/login` stores a random state and a PKCE verifier in a short-lived
signed cookie and redirects to `https://openpak.org/account/sso/authorize`; `/auth/callback` checks the state, swaps the
one-minute code at `POST $MARIO_MAKER_SSO_URL/internal/sso/token` (HTTP Basic `openmaker:<secret>`), and sets the site's
own signed session cookie `openmaker_session` (account id, display name, Wii U PID; HttpOnly, Secure, SameSite=Lax, seven
days); `/auth/logout` clears it and goes through `https://openpak.org/account/sso/logout`. The header shows openpak.org's
"Sign in" button, or "Sign out" and the account chip. Needs `MARIO_MAKER_PUBLIC_URL` (`https://maker.openpak.org`, the
only registered redirect origin), `MARIO_MAKER_SSO_URL`, `SSO_CLIENT_OPENMAKER_SECRET` and `MARIO_MAKER_SESSION_KEY`
(32+ random characters); with any missing, sign-in is off and no sign-in UI or route exists. Pages for signed-in
visitors are sent `Cache-Control: private, no-store`.

## Content and branding

No Nintendo logos or game art ship with the site; the only images are the game's own uploaded
course thumbnails, shown in a well. The footer says the site is unofficial and not affiliated with
Nintendo, and that Super Mario Maker 2 maker codes are OpenPak's own, not Nintendo's.
