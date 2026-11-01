package render

import (
	"image"
	"image/color"
)

// Item is one course on a contact sheet.
type Item struct {
	Number int
	Band   string
	Image  image.Image
}

var digits = [10][5]string{
	{"111", "101", "101", "101", "111"}, {"010", "110", "010", "010", "111"},
	{"111", "001", "111", "100", "111"}, {"111", "001", "111", "001", "111"},
	{"101", "101", "111", "001", "001"}, {"111", "100", "111", "001", "111"},
	{"111", "100", "111", "101", "111"}, {"111", "001", "010", "010", "010"},
	{"111", "101", "111", "101", "111"}, {"111", "101", "111", "001", "111"},
}

func number(img *image.RGBA, x, y, n, scale int, c color.RGBA) {
	s := []byte{}
	if n == 0 {
		s = []byte{'0'}
	}
	for ; n > 0; n /= 10 {
		s = append([]byte{byte('0' + n%10)}, s...)
	}
	for i, d := range s {
		g := digits[d-'0']
		for r, row := range g {
			for col, ch := range row {
				if ch == '1' {
					fillRect(img, x+(i*4+col)*scale, y+r*scale, x+(i*4+col+1)*scale, y+(r+1)*scale, c)
				}
			}
		}
	}
}

func bandColor(b string) color.RGBA {
	switch b {
	case "easy":
		return rgb(70, 200, 100)
	case "normal":
		return rgb(240, 190, 50)
	}
	return rgb(230, 70, 70)
}

// Sheet lays items out in a grid with cols columns, cell-sized slots, the
// course number and a band-coloured chip above each image.
func Sheet(items []Item, cols, cellW, cellH int) *image.RGBA {
	const pad, head = 8, 22
	rows := (len(items) + cols - 1) / cols
	img := image.NewRGBA(image.Rect(0, 0, cols*(cellW+pad)+pad, rows*(cellH+head+pad)+pad))
	fillRect(img, 0, 0, img.Bounds().Dx(), img.Bounds().Dy(), rgb(24, 26, 32))
	for i, it := range items {
		x := pad + (i%cols)*(cellW+pad)
		y := pad + (i/cols)*(cellH+head+pad)
		fillRect(img, x, y, x+cellW, y+head-4, bandColor(it.Band))
		number(img, x+6, y+2, it.Number, 3, rgb(20, 20, 20))
		b := it.Image.Bounds()
		for yy := 0; yy < b.Dy() && yy < cellH; yy++ {
			for xx := 0; xx < b.Dx() && xx < cellW; xx++ {
				img.Set(x+xx, y+head+yy, it.Image.At(b.Min.X+xx, b.Min.Y+yy))
			}
		}
	}
	return img
}
