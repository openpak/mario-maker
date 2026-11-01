# coursegen: OpenPak starter courses for Super Mario Maker (Wii U)

Generates about 60 simple, valid, beatable starter courses (SMB1, SMB3, SMW, NSMBU styles; ground, underground,
castle, airship, water and ghost-house themes; easy, normal and expert bands) so OpenMaker and the 100 Mario
Challenge (it needs at least 32 courses) have content from day one. Humans play-test them and keep the best 48.
Nothing is published until it has been play-tested.

## Status: NOT verified in the game

- The files follow the documented course format and parse with our parser, but **no Wii U or emulator has loaded
  one yet**. Header fields documented as "unknown" are written as zero.
- The DataStore object format (how the server stores a course, its `data_type`, `meta_binary`, thumbnails) is
  **unconfirmed**: nobody has captured a real upload on our server. The `import` command is therefore off by default
  and only stages a local bundle. It needs one real course from a Wii U first (see PLAYTEST.md).
- Movement limits are conservative design margins, not measurements.

## Use

```sh
cd common/mario-maker
GOWORK=off go run ./tools/coursegen generate -out tools/coursegen/courses   # 60 courses, deterministic (-seed N)
GOWORK=off go run ./tools/coursegen validate tools/coursegen/courses        # re-check every file
GOWORK=off go run ./tools/coursegen ascii tools/coursegen/courses/01-*/course_data.cdt
GOWORK=off go test ./tools/coursegen/...
```

Each course folder holds `course_data.cdt`, `course_data_sub.cdt` (empty sub area), `sound.bwv` (empty),
`thumbnail0.tnl`, `thumbnail1.tnl`, `preview.png`, `overview.png` and `manifest.yml`. The `courses/` folder also has
`contact-sheet.png`, `overview-sheet.png` and `index.html`. Binary files are git-ignored (regenerate them); manifests and
the sheets are committed. Maker name is `OpenPak`.

## Layout

| Package | Role |
|---|---|
| `smm1` | File format: marshal, parse, CRC, thumbnails, ground auto-tile table |
| `gen` | Templates (flat run, gap hop with coin arc, stairs, hill, hard-block pyramid, pipe row, enemy group, coins, block row, mid-course power-up), band settings, original family-friendly names |
| `validate` | Static checks and the shared movement limits (`physics.go`) |
| `render` | Original flat-colour thumbnails, contact sheets, ASCII view |
| `review` | Review folder, YAML manifests, HTML index |
| `importer` | Staging bundle for play-tested courses (off by default) |

## What the validator guarantees, and what it does not

It checks the bytes of the written file (parse, CRC, byte-exact round trip), then:

- exactly one player, start piece, start sign, goal piece and goal pole; goal between blocks 24 and 230; course
  width matches the goal; every object in bounds; object count at most 2600 (warning above 2000); title 1..32
  characters; timer 100..500 and above a par time; no overlapping solids; nothing inside terrain.
- No blind drops (more than 4 blocks), no wall taller than the style's rise limit, no pit wider than the band and
  style limit; pits bounded by ground.
- A conservative reachability model (walk, step up, drop, jump with run-up rules) says the goal is reachable from the
  start, and that **every reachable standing position can still reach the goal** (no softlock).
- Fair enemies: none in the first 12 or last 8 blocks, none within 2 blocks of a gap or edge.

It does **not** prove: that the game accepts the file, that the real physics match the limits, that enemies,
projectiles or coins cannot hurt or block the player, that a course is fun, or anything about audio. Reachability
treats enemies as passable and ignores power-ups.
