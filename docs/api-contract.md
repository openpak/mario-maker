# Upstream read API contract

The site copies its data from one read API per game. Both Mario Maker titles implement exactly
this contract; the site configures them with `SMM1_API_URL` / `SMM1_API_KEY` and
`SMM2_API_URL` / `SMM2_API_KEY`. A game whose URL is unset is skipped.

Every request carries the key in the header `X-Internal-Key` (omitted when the key is empty).
All responses are JSON unless noted. A missing record is `404`; any other non-200 is treated
as an outage and the site keeps what it has.

## Courses

`GET /v1/courses`

| Query | Meaning |
|---|---|
| `sort` | `newest` (default), `stars`, `plays` |
| `limit` | page size, at most 200 |
| `offset` | rows to skip |
| `q` | free text over name, description and tags |
| `maker` | only courses of this maker id |
| `updated_since` | RFC 3339; only courses whose `updated` is at or after it |

Answer: `{"total": int, "courses": [Course]}`. `total` is the number of matches before
`limit`/`offset`.

`GET /v1/courses/{id}` answers one `Course`, or `404`.

`GET /v1/courses/{id}/thumbnail` answers `image/jpeg` (the game's own uploaded thumbnail), or
`404` when the course has none.

`{id}` is the display course code, URL-escaped.

## Makers

`GET /v1/makers?limit=&offset=` answers `{"total": int, "makers": [Maker]}`.
`GET /v1/makers/{id}` answers one `Maker`, or `404`.

## Records

```json
{
  "game": "smm1",
  "id": "0123456789ABCDEF",
  "data_id": 1234,
  "name": "Sky Castle",
  "description": "",
  "maker_id": "ABCDE12345",
  "maker_name": "Mia",
  "style": "smb1",
  "theme": "castle",
  "difficulty": "hard",
  "tags": ["speedrun"],
  "created": "2026-11-01T12:00:00Z",
  "updated": "2026-11-01T12:30:00Z",
  "stars": 12,
  "plays": 340,
  "clears": 20,
  "attempts": 340,
  "has_thumbnail": true
}
```

- `id` is the display course code: SMM1 16 hex characters, SMM2 `XXX-XXX-XXX` (9 characters).
  It is the only key the site uses; `data_id` (uint64) is stored for reference.
- Any field may be an empty string, `0`, `null` or absent when the game does not provide it.
  Times that are empty or unparseable are shown as "unknown". `has_thumbnail` should be `true`
  only when the thumbnail endpoint answers.

```json
{ "game": "smm1", "id": "ABCDE12345", "name": "Mia", "courses": 3, "stars": 40, "plays": 900, "created": "2026-10-01T09:00:00Z" }
```

The site ignores the `game` field in responses and trusts the configured game instead.

## How the site uses it

- Each cycle (`MARIO_MAKER_SYNC_INTERVAL`, 60 s) it pages `GET /v1/courses?sort=newest&limit=200`
  with `updated_since` set to the previous successful cycle (minus two minutes), and pages
  all makers.
- Every `MARIO_MAKER_FULL_SYNC_INTERVAL` (10 min, and on start) it pages all courses without
  `updated_since`. Courses it stored earlier but did not see are re-fetched one by one with
  `GET /v1/courses/{id}`; a `404` soft-deletes the course (it disappears from the site and
  comes back if the upstream lists it again). Any other error aborts the pass without deleting.
- Thumbnails are fetched on demand through `/thumb/{game}/{id}.jpg` and cached.

For this to track stats, `updated` should change when `stars`, `plays` or `clears` change; if
it does not, counters catch up at the next full pass.

`examples/fake-upstream` implements the contract over generated data for local runs.

## Bookmarks (Super Mario Maker on Wii U only)

The in-game bookmark list belongs to a Wii U maker and lives in the title, not in the site. The
site forwards a signed-in player's bookmark to the title. A maker is named by `nnid` (the Network
ID the game uploads as its maker profile; case-insensitive), or by `pid`.

| Request | Answer |
|---|---|
| `PUT /v1/bookmarks` with `{"nnid": "...", "course": "<id>"}` | `200`; `404` when the maker has not played the game on OpenPak yet, or the course is gone; `409` at the 1000-bookmark cap |
| `DELETE /v1/bookmarks` with the same body | `200` |
| `GET /v1/bookmarks?nnid=...` | `{"courses": [Course]}`, newest bookmark first; `404` for an unknown maker |

`{id}` and `course` accept the display code (`4348-0000-000E-64A6`, any case, checksum checked) or the
decimal data ID. The Super Mario Maker (Wii U) title also reports `failures` and `miiverse_comments`
on a Course, and `medals` (0 to 10) on a Maker.

Site settings: `MARIO_MAKER_LINK_URL` (and `MARIO_MAKER_LINK_KEY`) point at a service that answers
`GET <url>?account_id=<OpenPak account id>` with `{"nnid": "..."}` (or 404 when the account has no
Wii U Network ID linked). Without it the site shows no bookmark control. Nothing implements that
service yet: it belongs to the core account / nn-account link. `MARIO_MAKER_CSRF_SECRET` fixes the
key of the bookmark form tokens (random per start otherwise); the tokens are tied to the session cookie.

Whether the game's Bookmarks list reads this list is not known (the title's `OPENPAK.md`, "Bookmarks").
