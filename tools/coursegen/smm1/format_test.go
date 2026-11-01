package smm1

import (
	"bytes"
	"hash/crc32"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sample() *Course {
	c := &Course{Title: "Test Meadow", Maker: "OpenPak", Style: NSMBU, Theme: Water, SubTheme: Underground,
		Time: 300, WidthPx: 160, Modified: time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)}
	for i := 0; i < 5; i++ {
		c.Objects = append(c.Objects, Object{X: uint32(BlockToRaw(i)), Y: 80, W: 1, H: 1, Flags: DefaultFlags,
			ChildFlags: DefaultFlags, Ext: uint32(i), Type: TGround, ChildType: -1, Link: -1, Effect: -1, U1: -1, U2: -1, Sort: BlockSort})
	}
	return c
}

func TestRoundTrip(t *testing.T) {
	b, err := sample().Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != CourseSize {
		t.Fatalf("size %d", len(b))
	}
	if crc32.ChecksumIEEE(b[0x10:]) != uint32(b[8])<<24|uint32(b[9])<<16|uint32(b[10])<<8|uint32(b[11]) {
		t.Fatal("crc not stored big endian over bytes 0x10..")
	}
	c, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != "Test Meadow" || c.Maker != "OpenPak" || c.Style != NSMBU || c.Theme != Water || c.Time != 300 || len(c.Objects) != 5 {
		t.Fatalf("parsed %+v", c)
	}
	back, _ := c.Marshal()
	if !bytes.Equal(b, back) {
		t.Fatal("not byte identical after parse and marshal")
	}
}

func TestCorruptionIsCaught(t *testing.T) {
	b, _ := sample().Marshal()
	b[0x100] ^= 1
	if _, err := Parse(b); err != ErrCRC {
		t.Fatalf("want ErrCRC, got %v", err)
	}
	if _, err := Parse(b[:100]); err != ErrSize {
		t.Fatalf("want ErrSize, got %v", err)
	}
}

func TestLimits(t *testing.T) {
	c := sample()
	c.Objects = make([]Object, MaxObjects+1)
	if _, err := c.Marshal(); err == nil {
		t.Fatal("expected an object-limit error")
	}
	c = sample()
	c.Time = 501
	if _, err := c.Marshal(); err == nil {
		t.Fatal("expected a timer error")
	}
}

func TestSubIsEmptyAndValid(t *testing.T) {
	b, _ := sample().MarshalSub()
	c, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Objects) != 0 || c.WidthPx != 0 || c.Theme != Underground {
		t.Fatalf("sub area %+v", c)
	}
}

func TestThumbnail(t *testing.T) {
	jpg := append([]byte{0xff, 0xd8}, bytes.Repeat([]byte{1}, 1000)...)
	b, err := PackThumbnail(jpg)
	if err != nil || len(b) != ThumbSize {
		t.Fatal(err, len(b))
	}
	got, err := UnpackThumbnail(b)
	if err != nil || !bytes.Equal(got, jpg) {
		t.Fatal("thumbnail did not round-trip", err)
	}
	if _, err := PackThumbnail(make([]byte, ThumbMaxJPEG+1)); err == nil {
		t.Fatal("expected a size error")
	}
}

func TestSoundIsEmptyWithCRC(t *testing.T) {
	s := Sound()
	if len(s) != SoundSize || s[0] != 0x76 || s[1] != 0x24 || s[2] != 0x6a || s[3] != 0xae {
		t.Fatalf("sound header % x", s[:4])
	}
}

// TestRealCourses parses real courses when COURSEGEN_REAL_DIR points at a
// folder of course directories (course000/course_data.cdt, thumbnail0.tnl...).
// The test is skipped otherwise; no real course is shipped in this repository.
func TestRealCourses(t *testing.T) {
	dir := os.Getenv("COURSEGEN_REAL_DIR")
	if dir == "" {
		t.Skip("COURSEGEN_REAL_DIR not set")
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*", "course_data.cdt"))
	if len(files) == 0 {
		t.Fatal("no courses found")
	}
	for _, f := range files {
		b, _ := os.ReadFile(f)
		c, err := Parse(b)
		if err != nil {
			t.Errorf("%s: %v", f, err)
			continue
		}
		if len(c.Objects) == 0 || c.Title == "" {
			t.Errorf("%s: empty parse %+v", f, c)
		}
		for _, n := range []string{"thumbnail0.tnl", "thumbnail1.tnl"} {
			tb, _ := os.ReadFile(filepath.Join(filepath.Dir(f), n))
			if _, err := UnpackThumbnail(tb); err != nil {
				t.Errorf("%s %s: %v", f, n, err)
			}
		}
	}
}
