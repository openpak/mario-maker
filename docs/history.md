# History (archived Wii U courses)

`/history` shows archived Super Mario Maker (Wii U) course details as browsable, credited, browse-only
history. The courses cannot be played: the source holds metadata only. Nothing here touches the live
`courses` and `makers` tables; everything lives in `archive_*` tables.

## Source and licence

| | |
|---|---|
| Data | tachyo's `SMMData`, https://archive.org/details/tachyo_SMMData (file `SMMData-92ca620c8c6975b882cc3d7c4b069d1db22c6626.tar.gz`, 94,334,585 bytes, md5 1132c8a55f23c8f004c612e590da1a22) |
| What it is | A 2018 scrape of the Super Mario Maker Bookmark site: 333,830 courses, 109,654 makers, 2015-09-09 to 2018-09-14 |
| Licence text (checked 2026-11-01) | `license.txt`: "The database in this repository is made available under the Open Database License: http://opendatacommons.org/licenses/dbcl/1.0/". The name says ODbL, the link is the Database Contents License. OpenMaker applies the stricter ODbL: attribution, state changes, share-alike for a published derived database. We publish pages, not a bulk export. |
| Not covered | The makers' rights in their course names, Nintendo's rights. The licence gives no maker consent, hence the opt-out, takedown and claim paths. |
| Code in the item | GPL-3.0 scraper; not used, not shipped. |

Credit is shown on `/history` and `/history/about` (constants in `internal/archive/archive.go`).

Imported per course: code, name, maker Network ID, theme, difficulty, tag, date, stars, plays, attempts,
clears, clear rate. Per maker: Network ID and display name. **Country is never read** (the import
query does not select it and no table has a column for it; a test enforces that).

## Import

```sh
mario-maker import-archive SMMData-92ca620c8c6975b882cc3d7c4b069d1db22c6626.tar.gz   # or the SMM.db inside it
```

Needs `MARIO_MAKER_DATABASE_URL`. Idempotent: rows are upserted by course code and Network ID, source
fields are replaced, and `taken_down`/`override` are never touched, so re-running never undoes a takedown
or an admin decision. Visibility is recomputed at the end. The source name, URL, licence and import date go
into `archive_sources`; each row carries `source` and `imported_at`. The tar.gz is unpacked to a private
directory under `$TMPDIR` and removed.

## Word filter

`internal/wordfilter`, lists in `internal/wordfilter/lists/` (English, Japanese, Spanish, French, German, Portuguese, Italian).
Source: LDNOOBW "List of Dirty, Naughty, Obscene, and Otherwise Bad Words",
https://github.com/LDNOOBW/List-of-Dirty-Naughty-Obscene-and-Otherwise-Bad-Words at commit
`5faf2ba42d7b1c0977169ec3611df25a3c08eb13`, licence **CC BY 4.0** (the repository's licence file; an earlier
note said CC BY-SA). Attribution is on the About page and in `lists/NOTICE.md`. Plus `lists/x.txt`, our own list of hate figures, groups and slogans, slurs and sexual terms in all seven languages; severe terms also match through spacing and repeated letters. Look-alike digits fold only inside a word that has a letter, and `!` is never folded ("LOL!" and "1515" stay clean).

Latin-script entries match whole words (or word sequences) after folding case, accents and look-alike
characters (`$`→s, `0`→o, ...). Japanese and symbol entries match as substrings with spaces removed. Titles
and maker names are both checked; a maker hit hides all of that maker's courses. A filtered row is hidden
(`visible = false`), never deleted. About 50 entries that are ordinary words in titles or only mildly rude
are skipped (`ignore` in `wordfilter.go`, with the measured reason): "Hardcore Parkour", "Escort The
Payload" and "S.M.B." were being hidden. Filtering cannot catch everything; the admin override list
(`show`/`hide` per course or maker) is the fix.

## Takedowns

`/history/takedown` (name, contact, course or maker, reason). Submitting hides the item at once and stores
the request as `pending`; a moderator then chooses "Keep hidden" (`approved`) or "Restore". A maker
takedown hides all of that maker's courses. A takedown beats an override and the filter. Throttled to 8
requests per hour per address; a honeypot field catches bots. The public contact is
`MARIO_MAKER_TAKEDOWN_EMAIL` (default `takedown@openpak.org`). The UI promises removal within 48 hours.

Admin: `/admin/archive`, HTTP Basic with `MARIO_MAKER_ADMIN_KEY` as the password (off while unset). Lists
takedowns, claims and the override list; every form carries a token.

## Claims

A signed-in OpenPak user (SSO) can claim an archived maker, but only with proof from account identity. A claim is accepted when the claimant's linked Wii U PID, taken from the core links (`MARIO_MAKER_LINK_URL`, nn-account's `GET /internal/wiiu-nnid?account_id=` answering `{"nnid","pid"}`, header `X-Internal-Key`, with `MARIO_MAKER_LINK_KEY`), equals the uploader PID recorded for the maker (`archive_makers.pid`). A typed name, a handle, a Network ID or a note never counts: OpenPak handles are free to register, so matching names let anyone take another player's courses (the earlier NNID-equality rule, removed 2026-11-04, card prds#97). A maker with no recorded uploader PID (the 2018 archive has none; `Store.SetMakerPID` records one) cannot be claimed and its page says so. A mismatch, an account with no Wii U link, or a link service that does not answer refuses the claim and stores nothing. A moderator can reject a claim but cannot approve one the PIDs do not prove (`ErrClaimUnproven`). One approved claim per maker; moderators can reject or revoke a claim. Switch PIDs: the core link endpoint serves Wii U only, so the Switch side of the rule waits for an equivalent endpoint.

## Pages

`/history` (browse and search: name, maker, code; filter by game style and year; sort by stars, plays,
highest or lowest clear rate (100+ attempts) or newest), `/history/course/{code}`, `/history/maker/{nnid}`,
`/history/about`, `/history/takedown`. Course and maker pages are `noindex` and disallowed in `robots.txt`.
Every page has "This is mine" and "Remove this".
