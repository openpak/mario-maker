# mario-maker

The OpenPak course site for Super Mario Maker (Wii U) and Super Mario Maker 2 (Switch): browse,
search and share the courses players upload to OpenPak. Unofficial; not affiliated with Nintendo.

Licence: AGPL-3.0-only. Go, standard library plus `pgx/v5` (and a pure-Go SQLite reader for the archive import). Server-rendered, no JavaScript needed.

- It keeps its own Postgres and copies courses and makers from each title's read API every 60 s
  ([docs/api-contract.md](docs/api-contract.md)).
- Pages: home shelves with a tab per game, `/browse` (search, sort, pagination), `/course/{game}/{id}`,
  `/maker/{game}/{id}`, `/thumb/{game}/{id}.jpg`, `/healthz`.
- `/history`: archived Wii U courses (2015 to 2018) as credited, browse-only history, with takedown and claim paths ([docs/history.md](docs/history.md)).
- Optional sign-in display with the OpenPak account, off unless configured.

Docs: [architecture](docs/architecture.md), [deploy](docs/deploy.md), [API contract](docs/api-contract.md).

## Run

```sh
cp example.env .env   # set MARIO_MAKER_DATABASE_URL and the SMM1_/SMM2_ API URLs and keys
go run ./cmd/mario-maker
```

Default listen address `:20161` (`MARIO_MAKER_HTTP_ADDR`). With no upstream configured the site runs and
shows empty states. `examples/fake-upstream` serves generated courses for local work.

## Test

```sh
MARIO_MAKER_TEST_DATABASE_URL=postgres://user:pw@host/db go test ./...
```

Database tests skip when the variable is unset.

## Release

Tag `vX.Y.Z`; the workflow in `.github/workflows/release.yml` tests, builds and pushes
`ghcr.io/openpak/mario-maker`. Nothing builds without a tag.
