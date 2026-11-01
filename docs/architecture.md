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
- `internal/auth`: optional sign-in display.

Pages: `/` (shelves; `?game=smm1|smm2` for the tabs), `/browse` (`q`, `game`, `sort`, `page`),
`/course/{game}/{id}`, `/maker/{game}/{id}`, `/thumb/{game}/{id}.jpg`, `/healthz`.

## Thumbnails

`/thumb` serves only courses the database lists with `has_thumbnail`. The first request fetches
the JPEG from the title, then it lives in a bounded in-memory LRU (`MARIO_MAKER_THUMB_CACHE_MB`)
and, when `MARIO_MAKER_CACHE_DIR` is set, on disk. The cache key includes the course's `updated`
time, so a changed course refetches. Responses carry an ETag and a one-hour public cache header.

## Sign-in

The site has no accounts of its own. When `MARIO_MAKER_CORE_BASE_URL` is set, a request that
carries the website's session cookie (`MARIO_MAKER_SESSION_COOKIE`, default `opak_session`, whose
value is a core session id) is checked with the core's `GET /v1/whoami` (the session id is
presented as the core's `openpak_session` cookie, as the website does), cached for 30 seconds, and the
header shows the display name linking to `MARIO_MAKER_WEBSITE_URL/account`. Signed-out visitors get a
"Sign in" link to `MARIO_MAKER_WEBSITE_URL/login`. Without a core URL nothing is shown and no
request is made. Pages for signed-in visitors are sent `Cache-Control: private, no-store`.

The website sets its cookie without a `Domain`, so a browser only sends it to the website's own
host. On a separate host (`mario-maker.openpak.org`) visitors therefore look signed out until the
website sets the cookie for the parent domain, or the site is routed under the website's host. Until
then the feature is harmless: it just shows "Sign in".

## Content and branding

No Nintendo logos or game art ship with the site; the only images are the game's own uploaded
course thumbnails, shown in a well. The footer says the site is unofficial and not affiliated with
Nintendo, and that Super Mario Maker 2 maker codes are OpenPak's own, not Nintendo's.
