package render

import (
	"image"
	"testing"

	"github.com/openpak/mario-maker/tools/coursegen/smm1"
)

func course() *smm1.Course {
	o := func(t int8, x, y int) smm1.Object {
		return smm1.Object{X: uint32(smm1.BlockToRaw(x)), Y: int16(smm1.BlockToRaw(y)), W: 1, H: 1, Type: t, ChildType: -1}
	}
	return &smm1.Course{Style: smm1.SMB1, Theme: smm1.Castle, WidthPx: 50 * 16, Objects: []smm1.Object{
		{X: 80, Y: 80, W: 8, H: 2, Type: smm1.TStartGround}, o(smm1.TCoin, 3, 4), o(smm1.TGoomba, 10, 2),
		{X: uint32(smm1.BlockToRaw(12)), Y: 80, W: 2, H: 3, Type: smm1.TPipe}, o(smm1.TPlayer, 0, 0)}}
}

func TestSizesAndJPEGFit(t *testing.T) {
	c := course()
	ov, pv := Overview(c), Preview(c)
	if ov.Bounds().Dx() != 150 || ov.Bounds().Dy() != 81 || pv.Bounds().Dx() != 320 || pv.Bounds().Dy() != 240 {
		t.Fatalf("sizes %v %v", ov.Bounds(), pv.Bounds())
	}
	for _, im := range []image.Image{ov, pv} {
		b, err := JPEG(im, smm1.ThumbMaxJPEG)
		if err != nil || len(b) > smm1.ThumbMaxJPEG {
			t.Fatal(err, len(b))
		}
		if _, err := smm1.PackThumbnail(b); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSheet(t *testing.T) {
	s := Sheet([]Item{{1, "easy", Preview(course())}, {2, "expert", Preview(course())}}, 2, 320, 240)
	if s.Bounds().Dx() != 2*328+8 {
		t.Fatalf("width %d", s.Bounds().Dx())
	}
}
