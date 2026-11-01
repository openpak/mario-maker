# Play-test guide

The 60 courses in `courses/` are generated and statically validated only. **None has been loaded in the game.**
This guide says how to test them once that is possible and what to judge.

## 0. Prerequisite: one real course from a Wii U

Before any import, a real Wii U (or a Wii U emulator that reaches SMM1 online) must upload one course to our SMM1
server, and someone must capture what the server stored: the object bytes in `nex-smmwiiu`, the
`datastore.objects` row (`data_type`, `flag`, `meta_binary`, size), and the thumbnail object. Compare the object's
payload with a plain `course_data.cdt`: is it raw, compressed, wrapped? Only then are `import -container` and
`-data-type` meaningful. Until then, test the files on their own (below).

## 1. Loading a course

- **Wii U with a save folder:** copy a course folder's five files into a free `course000`..`course059` slot of the
  Super Mario Maker save (with a Wii U save-editing tool), keeping the file names. The game
  checks the CRC, which these files carry. Expect to open it from the Course Maker.
- **Emulator (Quay) once it runs SMM1:** the same save folder approach.
- **Through the server (later):** after step 0, `COURSEGEN_IMPORT=yes-the-stored-format-is-unverified coursegen import -review DIR -out BUNDLE -first-id N -owner PID -data-type T [-container raw|zlib]`
  writes `import.sql` and the object files for courses marked `keep: true`. It never uploads; read the SQL,
  run it by hand, copy the `.bin` files to the bucket.

If the game refuses a file or shows garbage, stop and report which field (see `smm1/format.go`) is suspect:
the ground auto-tile values, the object sort key and the header fields documented as unknown are the likeliest.

## 2. What to judge (per course, 2 to 3 attempts)

Fill `status:` in the course's `manifest.yml`.

1. **Loads and looks right:** ground edges drawn correctly (no wrong tiles), pipes, blocks and coins as the contact
   sheet shows, start and goal where expected.
2. **Beatable:** every jump possible on a first try without tricks; note any gap that needed a perfect run-up.
   This checks the movement limits in `validate/physics.go`; report a number if one is too hard or too easy.
3. **Fair:** no enemy that kills without a chance to react, no unavoidable damage, power-up block reachable.
4. **Difficulty band:** easy should be clearable by a beginner, normal and expert progressively harder. Re-label
   if wrong.
5. **Length and timer:** neither rushed nor idle; timer sensible.
6. **Theme and style:** enemies and look suit the theme; the water and ghost-house courses behave sensibly.
7. **Fun:** be honest. Generated courses are plain; keep the 48 best and discard the rest.
8. **Name and description:** family-friendly, no trademarks.

Set `keep: true` for the 48 that pass and `keep: false` for the rest; set `play_tested: true`,
`verified_in_game: true`. If the tool's physics numbers need to change, edit `validate/physics.go` and regenerate.

Aim for about 16 courses per difficulty band among the kept ones, since 100 Mario Challenge needs spread across
bands, and note that difficulty bands in the game come from real clears and attempts, which a fresh course does
not have yet.
