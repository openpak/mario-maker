package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openpak/mario-maker/tools/coursegen/gen"
	"github.com/openpak/mario-maker/tools/coursegen/smm1"
)

func TestWriteSet(t *testing.T) {
	cs, ms := gen.Set(1)
	dir := t.TempDir()
	res, err := Write(dir, cs[:6], ms[:6])
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 6 {
		t.Fatal(len(res))
	}
	for _, r := range res {
		if !r.Report.OK() {
			t.Errorf("%s: %v", r.Meta.Name, r.Report.Errors)
		}
		for _, n := range []string{"course_data.cdt", "course_data_sub.cdt", "sound.bwv", "thumbnail0.tnl", "thumbnail1.tnl", "manifest.yml", "preview.png"} {
			if _, err := os.Stat(filepath.Join(r.Dir, n)); err != nil {
				t.Errorf("%s missing %s", r.Meta.Slug, n)
			}
		}
		b, _ := os.ReadFile(filepath.Join(r.Dir, "course_data.cdt"))
		if _, err := smm1.Parse(b); err != nil {
			t.Errorf("written file does not parse: %v", err)
		}
		tb, _ := os.ReadFile(filepath.Join(r.Dir, "thumbnail1.tnl"))
		if _, err := smm1.UnpackThumbnail(tb); err != nil {
			t.Errorf("thumbnail: %v", err)
		}
		man, _ := os.ReadFile(filepath.Join(r.Dir, "manifest.yml"))
		for _, k := range []string{"name:", "style:", "theme:", "difficulty:", "templates:", "description:", "verified_in_game: false"} {
			if !strings.Contains(string(man), k) {
				t.Errorf("manifest of %s lacks %q", r.Meta.Slug, k)
			}
		}
		if !strings.Contains(string(man), `maker: "OpenPak"`) {
			t.Error("maker name must be OpenPak")
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "contact-sheet.png")); err != nil {
		t.Error("no contact sheet")
	}
}
