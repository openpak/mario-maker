# Changelog: mario-maker

## Unreleased

- `tools/coursegen`: generator, validator and review folder for about 60 OpenPak starter courses for Super Mario Maker (Wii U). Not part of the site binary. The files are not verified in the game and the DataStore import is off by default; see `tools/coursegen/README.md`

## v0.3.5 — 2026-11-01

- Shared Open-site shell on every page: content centred in the 74rem wrap with OpenKart's header, current-page style (raised pill with a cyan underline), spacing and footer
- One search box per page: the header search is hidden on Browse and History, which have their own filter form. Header search on History pages searches the archive
- History cards match the live grid: a small original picture per game style, equal heights, a short style pill (SMB, SMB3, SMW, NSMBU, full name on hover), "Originally by" everywhere, and one stats line (stars, plays, clear rate, shortened as 2.5M)
- Course and maker names carry `translate="no"` and `dir="auto"` so browser translation leaves them alone; the filter label reads "Style"
- Claims by matching Network ID work once `MARIO_MAKER_LINK_URL` points at nn-account's `/internal/wiiu-nnid`

## v0.3.4 — 2026-11-01

- History word filter widened: Portuguese and Italian lists (LDNOOBW, CC BY 4.0) and an own list of hate figures, groups, slogans, slurs and sexual terms in seven languages, with look-alike digits and symbols folded and severe terms matched through spacing and repeated letters. Checked against the archive so clean titles ("LOL!", "Bowser's Inferno") stay visible. The About page lists all seven languages

## v0.3.3 — 2026-11-01

- History: the About page and the removal form use the full page width on wide screens

## v0.3.2 — 2026-11-01

- The takedown address is plain text (no mail link): Cloudflare rewrites mail links to a form that needs JavaScript

## v0.3.1 — 2026-11-01

- The takedown contact address was unreadable behind Cloudflare's address obfuscation (it needs JavaScript, which the site's content policy blocks). Addresses are now written so they stay plain text, on the pages and in the admin list

## v0.3.0 — 2026-11-01

- New History section (`/history`): archived Wii U Super Mario Maker course details from tachyo's 2018 scrape (archive.org `tachyo_SMMData`, ODbL), browse-only and credited to the makers. Browse and search by name, maker, course code, game style and year; sort by stars, plays or clear rate. Course and maker pages, an About page with the licences and credits, and "This is mine" and "Remove this" on every page. Country is never imported or shown
- `mario-maker import-archive <tar.gz|SMM.db>`: idempotent import into separate `archive_*` tables, with the source and import date stored
- Word filter for course names and maker names (English, Japanese, Spanish, French, German; LDNOOBW lists, CC BY 4.0) that hides rows without deleting them, plus an admin override list
- Takedown form that hides the item at once and stores the request (honoured within 48 hours), with `/admin/archive` behind `MARIO_MAKER_ADMIN_KEY`
- Maker claims by signed-in users: accepted at once when the account's linked Network ID matches, otherwise pending for a moderator
- New settings: `MARIO_MAKER_TAKEDOWN_EMAIL`, `MARIO_MAKER_ADMIN_KEY`

## v0.2.1 — 2026-11-01

- Sign out ends only this site's session and returns to the home page; it no longer goes through openpak.org's "Sign out of OpenPak?" page, and openpak.org stays signed in. The next Sign in may sign straight back in while openpak.org is signed in

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
