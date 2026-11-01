// Package importer stages play-tested starter courses for the SMM1 server's
// DataStore. It is OFF by default and only ever writes a local bundle (object
// files, JPEG thumbnails and an SQL file); it never connects to a database or
// to MinIO.
//
// The stored format is unconfirmed: nobody has captured a course a real Wii U
// uploaded to our server, so how the game wraps course_data.cdt inside the
// DataStore object (and which data_type, flag and meta_binary it expects) is
// unknown. Every value that would be a guess is therefore a required option, and
// the SQL says UNVERIFIED. Run it only after the console step in PLAYTEST.md.
package importer

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// EnvGate must hold EnvValue for Stage to run.
const (
	EnvGate  = "COURSEGEN_IMPORT"
	EnvValue = "yes-the-stored-format-is-unverified"
)

// Options configure a bundle.
type Options struct {
	Review    string // review folder
	Out       string // bundle folder to create
	FirstID   int64  // data id of the first course; required
	Owner     int    // owner PID (the OpenPak maker account); required
	DataType  int    // datastore data_type of a course object; required, 3..49
	Container string // "raw" or "zlib"; how course_data.cdt is wrapped in the object
}

// Enabled reports whether the environment gate is open.
func Enabled() bool { return os.Getenv(EnvGate) == EnvValue }

type entry struct {
	dir  string
	id   int
	name string
}

// keepers returns the courses whose manifest says keep: true, in id order.
func keepers(review string) ([]entry, error) {
	dirs, err := filepath.Glob(filepath.Join(review, "*", "manifest.yml"))
	if err != nil {
		return nil, err
	}
	var out []entry
	for _, m := range dirs {
		b, err := os.ReadFile(m)
		if err != nil {
			return nil, err
		}
		e := entry{dir: filepath.Dir(m)}
		keep := false
		for _, line := range strings.Split(string(b), "\n") {
			switch {
			case strings.HasPrefix(line, "id: "):
				e.id, _ = strconv.Atoi(strings.TrimSpace(line[4:]))
			case strings.HasPrefix(line, "name: "):
				e.name, _ = strconv.Unquote(strings.TrimSpace(line[6:]))
			case strings.HasPrefix(line, "  keep: true"):
				keep = true
			}
		}
		if keep {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out, nil
}

// Stage writes the bundle and returns the number of courses staged.
func Stage(o Options) (int, error) {
	if !Enabled() {
		return 0, fmt.Errorf("import is off: set %s=%s to stage a bundle (read PLAYTEST.md first)", EnvGate, EnvValue)
	}
	switch {
	case o.FirstID <= 0:
		return 0, errors.New("-first-id is required (use a value above the server's data id sequence)")
	case o.Owner <= 0:
		return 0, errors.New("-owner is required (the PID of the OpenPak maker account)")
	case o.DataType < 3 || o.DataType > 49:
		return 0, errors.New("-data-type is required and must be 3..49 (confirm it from one real upload)")
	case o.Container != "raw" && o.Container != "zlib":
		return 0, errors.New("-container must be raw or zlib")
	}
	ks, err := keepers(o.Review)
	if err != nil {
		return 0, err
	}
	if len(ks) == 0 {
		return 0, errors.New("no course has keep: true in its manifest; nothing is published before play-testing")
	}
	for _, d := range []string{"objects", "thumbnails"} {
		if err := os.MkdirAll(filepath.Join(o.Out, d), 0o755); err != nil {
			return 0, err
		}
	}
	var sql bytes.Buffer
	sql.WriteString("-- UNVERIFIED import bundle for datastore.objects (SMM1). Read PLAYTEST.md before running.\n")
	sql.WriteString("-- Object files go to the nex-smmwiiu bucket as <data_id>.bin; thumbnails are separate attached\n")
	sql.WriteString("-- objects (refer_data_id = the course) and are NOT inserted here: their data_type is unconfirmed.\n")
	sql.WriteString("BEGIN;\n")
	for i, e := range ks {
		id := o.FirstID + int64(i)
		cdt, err := os.ReadFile(filepath.Join(e.dir, "course_data.cdt"))
		if err != nil {
			return 0, err
		}
		obj := cdt
		if o.Container == "zlib" {
			var zb bytes.Buffer
			zw := zlib.NewWriter(&zb)
			zw.Write(cdt)
			zw.Close()
			obj = zb.Bytes()
		}
		if err := os.WriteFile(filepath.Join(o.Out, "objects", fmt.Sprintf("%d.bin", id)), obj, 0o644); err != nil {
			return 0, err
		}
		tnl, err := os.ReadFile(filepath.Join(e.dir, "thumbnail1.tnl"))
		if err != nil {
			return 0, err
		}
		if err := os.WriteFile(filepath.Join(o.Out, "thumbnails", fmt.Sprintf("%d.tnl", id)), tnl, 0o644); err != nil {
			return 0, err
		}
		fmt.Fprintf(&sql, "-- %s (review id %d)\n", strings.ReplaceAll(e.name, "\n", " "), e.id)
		fmt.Fprintf(&sql, "INSERT INTO datastore.objects (data_id, upload_completed, owner, size, name, data_type, creation_date, update_date)\n"+
			"VALUES (%d, TRUE, %d, %d, '%s', %d, now(), now());\n", id, o.Owner, len(obj), strings.ReplaceAll(e.name, "'", "''"), o.DataType)
	}
	sql.WriteString("COMMIT;\n")
	return len(ks), os.WriteFile(filepath.Join(o.Out, "import.sql"), sql.Bytes(), 0o644)
}
