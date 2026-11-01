// Command coursegen generates, validates and stages the OpenPak starter
// courses for Super Mario Maker (Wii U).
//
//	coursegen generate -out DIR [-seed N]    write the review folder (60 courses)
//	coursegen validate PATH...               check course_data.cdt files or review folders
//	coursegen ascii FILE...                  print a text map of a course
//	coursegen import -review DIR -out DIR ...  stage play-tested courses (off by default)
//
// The generated files have not been verified in the game; see README.md.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/openpak/mario-maker/tools/coursegen/gen"
	"github.com/openpak/mario-maker/tools/coursegen/importer"
	"github.com/openpak/mario-maker/tools/coursegen/render"
	"github.com/openpak/mario-maker/tools/coursegen/review"
	"github.com/openpak/mario-maker/tools/coursegen/smm1"
	"github.com/openpak/mario-maker/tools/coursegen/validate"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	var err error
	switch os.Args[1] {
	case "generate":
		err = generate(os.Args[2:])
	case "validate":
		err = check(os.Args[2:])
	case "import":
		err = stage(os.Args[2:])
	case "ascii":
		err = ascii(os.Args[2:])
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "coursegen:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: coursegen generate -out DIR [-seed N] | validate PATH... | import -review DIR -out DIR -first-id N -owner PID -data-type N [-container raw|zlib]")
	os.Exit(2)
}

func generate(args []string) error {
	fs := flag.NewFlagSet("generate", flag.ExitOnError)
	out := fs.String("out", "courses", "review folder to write")
	seed := fs.Uint64("seed", 1, "seed; the same seed always gives the same courses")
	fs.Parse(args)
	cs, ms := gen.Set(*seed)
	res, err := review.Write(*out, cs, ms)
	if err != nil {
		return err
	}
	bad := 0
	for _, r := range res {
		status := "ok"
		if !r.Report.OK() {
			status, bad = "INVALID", bad+1
		}
		fmt.Printf("%02d %-26s %-5s %-12s %-6s %s\n", r.Meta.Index, r.Meta.Name, r.Meta.Style, r.Meta.Theme, r.Meta.Band, status)
	}
	fmt.Printf("%d courses written to %s (%d invalid); contact sheet: %s\n", len(res), *out, bad, filepath.Join(*out, "contact-sheet.png"))
	if bad > 0 {
		return fmt.Errorf("%d invalid courses", bad)
	}
	return nil
}

func check(paths []string) error {
	if len(paths) == 0 {
		usage()
	}
	var files []string
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return err
		}
		if !st.IsDir() {
			files = append(files, p)
			continue
		}
		m, _ := filepath.Glob(filepath.Join(p, "*", "course_data.cdt"))
		files = append(files, m...)
		if one := filepath.Join(p, "course_data.cdt"); fileExists(one) {
			files = append(files, one)
		}
	}
	bad := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		r, err := validate.CheckBytes(b, validate.Options{})
		if err != nil {
			fmt.Printf("%s: unreadable: %v\n", f, err)
			bad++
			continue
		}
		if r.OK() {
			fmt.Printf("%s: ok (%d objects, goal at block %d, widest gap %d)\n", f, r.Stats.Objects, r.Stats.GoalBlock, r.Stats.MaxGap)
		} else {
			bad++
			fmt.Printf("%s: INVALID\n", f)
			for _, e := range r.Errors {
				fmt.Println("  error:", e)
			}
		}
		for _, w := range r.Warnings {
			fmt.Println("  warning:", w)
		}
	}
	if bad > 0 {
		return fmt.Errorf("%d of %d files failed", bad, len(files))
	}
	return nil
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func stage(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	var o importer.Options
	fs.StringVar(&o.Review, "review", "", "review folder (courses marked keep: true are staged)")
	fs.StringVar(&o.Out, "out", "", "bundle folder to write")
	fs.Int64Var(&o.FirstID, "first-id", 0, "data id of the first staged course")
	fs.IntVar(&o.Owner, "owner", 0, "owner PID of the OpenPak maker account")
	fs.IntVar(&o.DataType, "data-type", 0, "datastore data_type of a course (3..49), from a real upload")
	fs.StringVar(&o.Container, "container", "raw", "how the cdt is wrapped in the object: raw or zlib")
	fs.Parse(args)
	if o.Review == "" || o.Out == "" {
		usage()
	}
	n, err := importer.Stage(o)
	if err != nil {
		return err
	}
	fmt.Printf("staged %d courses in %s (nothing was uploaded)\n", n, o.Out)
	return nil
}

// ascii prints a text map of each course_data.cdt given.
func ascii(files []string) error {
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		c, err := smm1.Parse(b)
		if err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
		fmt.Printf("%s: %q %s %s timer %d\n%s\n", f, c.Title, c.Style, c.Theme, c.Time, render.ASCII(c))
	}
	return nil
}
