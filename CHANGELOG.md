# Changelog: mario-maker

## v0.2.0 — 2026-11-01

- Sign-in through openpak.org single sign-on (`/auth/login`, `/auth/callback`, `/auth/logout`; state and PKCE, server-side code exchange, the site's own signed session cookie). Replaces the old session lookup that could never see openpak.org's cookie on this host. The header has the same Sign in button and account chip as openpak.org; the site has no sign-in form
- New settings: `MARIO_MAKER_SSO_URL`, `SSO_CLIENT_OPENMAKER_SECRET`, `MARIO_MAKER_SESSION_KEY`. Removed: `MARIO_MAKER_CORE_BASE_URL`, `MARIO_MAKER_SESSION_COOKIE`

## v0.1.2

- Address is maker.openpak.org.
- Footer links back with "Part of the OpenPak network".

## v0.1.1

- Named OpenMaker (openmaker.openpak.org), with the OpenMaker mark and the shared Open site header and footer.
- Release fix: v0.1.0 failed its tests in CI (the notice moved into the footer) and published no image.
- Bookmarks for Super Mario Maker (Wii U): a signed-in player with a linked Network ID bookmarks a course from its
  page (`POST /bookmark/smm1/{id}`, CSRF token tied to the session) and sees the list at `/bookmarks`; the list
  lives in the title. Off unless `MARIO_MAKER_LINK_URL` is set. See docs/api-contract.md.

## v0.1.0

First release.

- Server-rendered course site for Super Mario Maker (Wii U) and Super Mario Maker 2 (Switch): home shelves
  (newest, most starred, most played) with a tab per game, `/browse` with full-text search, game filter,
  sort and pagination, course and maker pages with Open Graph tags, and a cached thumbnail proxy.
- Own Postgres (`mario_maker`) filled by a sync every 60 seconds from the two titles' read APIs, with
  soft-delete of courses that disappear upstream.
- Optional display of the signed-in OpenPak account through the core's `/v1/whoami`.
- No JavaScript required anywhere; light and dark follow the system setting.
