# Changelog: mario-maker

## Unreleased

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
