# Sources and licences

coursegen writes Super Mario Maker (Wii U) course files. No code was copied from any source below; only
format facts (offsets, sizes, ids, data values) were used. Each source, what it gave, and its licence:

| Source | What was used | Licence | Notes |
|---|---|---|---|
| [Tarnadas/smmdb-lib](https://github.com/Tarnadas/smmdb-lib) (Rust) | Header offsets (title, maker, style, theme, timer, autoscroll, width, object count/size, sound table), file size 0x15000, CRC over bytes from 0x10, `.tnl` layout (length at 4..7, JPEG from 8). Its six real test courses were read locally to confirm every layout fact and to derive the ground auto-tile table in `smm1/groundtab.go` (data values only). | MIT | Courses were not copied into this repo. |
| [wiiubrew.org Cdt page](https://wiiubrew.org/wiki/Cdt) | Header field list: version 11, CRC32 at 0x08, date, name, mode, theme, timer, autoscroll, object count, 0xF0 object array of 2600 x 32 bytes. | none stated on the page | Facts only. |
| [Treeki/MarioUnmaker `FormatNotes.md`](https://github.com/Treeki/MarioUnmaker/blob/master/FormatNotes.md) | Object structure (32 bytes), ".cdt ID" table (object type ids), flag meanings (variant bit 4), link ids for pipes, child type for blocks. | no licence file (GitHub reports none) | Unclear licence: treated as documentation of facts. No text or code copied. |
| [leomaurodesenv/smm-course-viewer](https://github.com/leomaurodesenv/smm-course-viewer) `FORMAT.md` and `course-example` | Confirmed the type id table; pipe direction is `(flags & 0x60) / 0x20` with 2 = opening up. Its sample course was read locally to learn coordinates (block centre = block * 160 + 80), start/goal piece shape. | GPL-3.0 | Facts only; no code or file copied; sample not shipped. |
| [kinnay/NintendoClients wiki](https://github.com/kinnay/NintendoClients/wiki) | DataStore method list (prepare/complete post, attach file) for the import notes. | MIT | It does not describe the stored course object. |

Consulted and not used: MariOver and toost (SMM2) were not needed, SMM2 is out of scope here; the "smm" npm
package is the WebAssembly build of smmdb-lib above. The Wii U save folder layout (`course_data.cdt`,
`course_data_sub.cdt`, `sound.bwv`, `thumbnail0.tnl`, `thumbnail1.tnl`) is the layout smmdb-lib reads.

## Where this tool's own facts come from

- Ground auto-tile table: derived from the MIT test courses above (about 7,000 tiles). Plain variant per
  eight-neighbour pattern; neighbourhoods never seen take the nearest seen one.
- Movement limits (`validate/physics.go`): conservative design margins chosen by us, not measurements and not
  copied from a source. See the file's comment. Nobody has measured them on a console for this project.
- Sort key (object field at offset 4), per-type anchor conventions and the empty `sound.bwv` (CRC32 of 55,300 zero
  bytes followed by zeros) were read off real courses.
