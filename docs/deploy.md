# Deploy

The image is `ghcr.io/openpak/mario-maker`. Pushing a `v*.*.*` tag builds it for amd64 and arm64
(tests run first) and moves `:latest`; hosts that run `podman auto-update` against `:latest` pick it up.
Nothing else builds the image.

## Prerequisites

1. A Postgres database `mario_maker` and a role that owns it. The service creates its own tables.
2. The read APIs of the two titles reachable from the container (api-contract.md), and their keys.
3. A router for the public host (for example `maker.openpak.org`) to the container port.

## Run

```sh
podman run -d --name mario-maker --env-file .env -p 20161:20161 ghcr.io/openpak/mario-maker:latest
```

Configuration is environment only; `example.env` lists everything. Required: `MARIO_MAKER_DATABASE_URL`.
The container runs as uid 65532 and writes only to `MARIO_MAKER_CACHE_DIR` (`/app/cache`; mount a
volume there to keep thumbnails across restarts).

For the box: a quadlet pinned with `IP=` on `openpak.network`, `Wants=` (not `Requires=`) on the two
titles and Postgres so a title restart does not take the site down. The site starts without any
upstream and shows empty states; it retries the database for about a minute at start.

## Health

`GET /healthz` answers 200 with `{"ok":true,"sync":[{"game":"smm1","last_ok":"...","last_full":"..."}]}`,
or 503 when Postgres is unreachable. A failing sync does not fail health; it shows as `last_error`.

## Local run

```sh
export MARIO_MAKER_TEST_DATABASE_URL=postgres://postgres:pw@127.0.0.1:55432/mario_maker?sslmode=disable
go run ./examples/fake-upstream -game smm1 -addr :20170 &
go run ./examples/fake-upstream -game smm2 -addr :20171 &
MARIO_MAKER_DATABASE_URL=$MARIO_MAKER_TEST_DATABASE_URL SMM1_API_URL=http://127.0.0.1:20170 \
  SMM2_API_URL=http://127.0.0.1:20171 go run ./cmd/mario-maker
```

`go test ./...` skips the database tests unless `MARIO_MAKER_TEST_DATABASE_URL` is set; each test
works in its own throwaway schema.

## Production

Runs on the primary as the user service `openpak-mario-maker.service` (binary extracted from the
image into `~/openpak/deploy/mario-maker`, env in `mario-maker.env`), listening on
`127.0.0.1:20161`; the Cloudflare hostname `maker.openpak.org -> http://localhost:20161` is added by
the owner. It is a host service rather than a quadlet because the container keyring quota
(`kernel.keys.maxkeys=200`) is exhausted fleet-wide; the prepared quadlet is
`openpak-mario-maker.container.pending` next to the binary. The site reaches Postgres (database
`mario_maker`) and the Super Mario Maker (Wii U) API through `openpak-switch-bridge` on loopback
(19432, 19584) and the SMM2 API on `127.0.0.1:22333`.

## History import

The History section needs a one-off data load. Download the item (about 94 MB), then run the image once against the
same database and env:

```sh
podman run --rm --network openpak.network --env-file ~/openpak/deploy/.env --env-file ~/openpak/deploy/mario-maker.env \
  -e TMPDIR=/app/cache -v ~/openpak/deploy/smm-import:/import:ro,Z ghcr.io/openpak/mario-maker:latest \
  /app/mario-maker import-archive /import/SMMData.tar.gz
```

Safe to repeat. See `history.md`. Settings: `MARIO_MAKER_ADMIN_KEY` (admin page), `MARIO_MAKER_TAKEDOWN_EMAIL`.
