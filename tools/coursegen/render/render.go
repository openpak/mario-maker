// Package render draws simple, original previews of a course: flat coloured
// blocks, no Nintendo artwork. The images are made from the parsed course file,
// so what you see is what the file contains.
package render

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"

	"github.com/openpak/mario-maker/tools/coursegen/smm1"
)

// palette is the colour set of one theme.
type palette struct {
	sky, skyLow     color.RGBA
	groundTop, fill color.RGBA
}

func rgb(r, g, b uint8) color.RGBA { return color.RGBA{r, g, b, 255} }

func paletteFor(t smm1.Theme) palette {
	switch t {
	case smm1.Underground:
		return palette{rgb(16, 18, 40), rgb(30, 34, 70), rgb(96, 160, 150), rgb(62, 70, 100)}
	case smm1.Castle:
		return palette{rgb(40, 28, 34), rgb(80, 44, 44), rgb(170, 170, 176), rgb(96, 96, 108)}
	case smm1.Airship:
		return palette{rgb(60, 80, 140), rgb(150, 170, 220), rgb(214, 170, 96), rgb(150, 104, 60)}
	case smm1.Water:
		return palette{rgb(30, 100, 190), rgb(20, 60, 140), rgb(236, 214, 150), rgb(196, 170, 110)}
	case smm1.GhostHouse:
		return palette{rgb(30, 24, 52), rgb(60, 46, 90), rgb(150, 140, 190), rgb(84, 74, 120)}
	}
	return palette{rgb(110, 180, 255), rgb(200, 230, 255), rgb(90, 190, 80), rgb(150, 104, 60)}
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, c color.RGBA) {
	r := image.Rect(x0, y0, x1, y1).Intersect(img.Bounds())
	if r.Empty() {
		return
	}
	draw.Draw(img, r, &image.Uniform{c}, image.Point{}, draw.Src)
}

func disc(img *image.RGBA, cx, cy, rad int, c color.RGBA) {
	for y := -rad; y <= rad; y++ {
		for x := -rad; x <= rad; x++ {
			if x*x+y*y <= rad*rad && image.Pt(cx+x, cy+y).In(img.Bounds()) {
				img.SetRGBA(cx+x, cy+y, c)
			}
		}
	}
}

// view maps block coordinates to pixels: block (bx,by) with by counted up from
// the bottom of the course is drawn at scale px per block.
type view struct {
	img      *image.RGBA
	scale    int
	x0, rows int
}

func (v view) px(bx int) int { return (bx - v.x0) * v.scale }
func (v view) py(by int) int { return (v.rows - 1 - by) * v.scale }

func (v view) block(bx, by, w, h int, c color.RGBA) {
	fillRect(v.img, v.px(bx), v.py(by+h-1), v.px(bx+w), v.py(by-1), c)
}

// draw paints the course into v.
func draw1(v view, c *smm1.Course, pal palette) {
	b := v.img.Bounds()
	for y := 0; y < b.Dy(); y++ {
		f := float64(y) / float64(b.Dy())
		col := color.RGBA{
			uint8(float64(pal.sky.R)*(1-f) + float64(pal.skyLow.R)*f),
			uint8(float64(pal.sky.G)*(1-f) + float64(pal.skyLow.G)*f),
			uint8(float64(pal.sky.B)*(1-f) + float64(pal.skyLow.B)*f), 255}
		fillRect(v.img, 0, y, b.Dx(), y+1, col)
	}
	s := v.scale
	tile := func(bx, by, w, h int, top bool) {
		v.block(bx, by, w, h, pal.fill)
		if top {
			fillRect(v.img, v.px(bx), v.py(by+h-1), v.px(bx+w), v.py(by+h-1)+max(1, s/4), pal.groundTop)
		}
	}
	for _, o := range c.Objects {
		bx := smm1.RawToBlock(int(o.X))
		switch o.Type {
		case smm1.TStartGround:
			tile(0, 0, int(o.W), int(o.H), true)
		case smm1.TGoalGround:
			tile(bx, 0, int(o.W), int(o.H), true)
			fillRect(v.img, v.px(bx), v.py(int(o.H)-1), v.px(bx+int(o.W)), v.py(int(o.H)-1)+max(1, s/4), rgb(240, 240, 240))
		}
	}
	for _, o := range c.Objects {
		bx, by := smm1.RawToBlock(int(o.X)), smm1.RawToBlock(int(o.Y))
		cx, cy := v.px(bx)+s/2, v.py(by)+s/2
		switch o.Type {
		case smm1.TGround:
			up := false
			for _, p := range c.Objects {
				if p.Type == smm1.TGround && p.X == o.X && int(p.Y) == int(o.Y)+160 {
					up = true
					break
				}
			}
			tile(bx, by, 1, 1, !up)
		case smm1.TPipe:
			v.block(bx, by, 2, int(o.H), rgb(40, 170, 70))
			v.block(bx, by+int(o.H)-1, 2, 1, rgb(90, 220, 110))
			fillRect(v.img, v.px(bx), v.py(by), v.px(bx)+max(1, s/5), v.py(by-1), rgb(20, 110, 50))
		case smm1.TBrick:
			v.block(bx, by, 1, 1, rgb(200, 100, 50))
			fillRect(v.img, v.px(bx), cy, v.px(bx+1), cy+1, rgb(120, 56, 24))
		case smm1.TQuestion:
			v.block(bx, by, 1, 1, rgb(250, 190, 40))
			disc(v.img, cx, cy, max(1, s/6), rgb(150, 90, 10))
		case smm1.THard:
			v.block(bx, by, 1, 1, rgb(160, 160, 170))
			fillRect(v.img, v.px(bx), v.py(by), v.px(bx+1), v.py(by)+max(1, s/6), rgb(210, 210, 220))
		case smm1.TCoin:
			disc(v.img, cx, cy, max(1, s/3), rgb(255, 215, 0))
		case smm1.TGoomba:
			disc(v.img, cx, cy, max(1, s/2-1), rgb(150, 90, 50))
		case smm1.TKoopa:
			col := rgb(50, 170, 60)
			if o.Flags&smm1.FlagVariant != 0 {
				col = rgb(210, 50, 50)
			}
			disc(v.img, cx, cy, max(1, s/2-1), col)
		case smm1.TBuzzy:
			disc(v.img, cx, cy, max(1, s/2-1), rgb(60, 70, 130))
		case smm1.TSpiny:
			disc(v.img, cx, cy, max(1, s/2-1), rgb(220, 60, 40))
			fillRect(v.img, cx-1, cy-s/2, cx+1, cy-s/2+max(2, s/4), rgb(250, 240, 200))
		case smm1.TDryBones:
			disc(v.img, cx, cy, max(1, s/2-1), rgb(235, 235, 225))
		case smm1.TCheep:
			disc(v.img, cx, cy, max(1, s/2-1), rgb(240, 110, 130))
		case smm1.TGoalPole:
			fillRect(v.img, v.px(bx)+s/2-max(1, s/8), v.py(by+8), v.px(bx)+s/2+max(1, s/8)+1, v.py(by-1), rgb(240, 240, 240))
			fillRect(v.img, v.px(bx)+s/2, v.py(by+8), v.px(bx)+s/2+s, v.py(by+8)+s, rgb(60, 200, 90))
		case smm1.TStartSign:
			fillRect(v.img, v.px(bx), v.py(by), v.px(bx)+max(2, s/2), v.py(by)+max(2, s), rgb(250, 240, 120))
		case smm1.TPlayer:
			fillRect(v.img, v.px(1), v.py(startTop(c)), v.px(1)+max(2, s*3/4), v.py(startTop(c)-1), rgb(220, 40, 40))
		}
	}
}

func startTop(c *smm1.Course) int {
	for _, o := range c.Objects {
		if o.Type == smm1.TStartGround {
			return int(o.H)
		}
	}
	return 1
}

// Overview draws the whole course at three pixels per block, 27 rows high
// (81 pixels), like the long thumbnail the game keeps for each course.
func Overview(c *smm1.Course) *image.RGBA {
	w := c.WidthPx / 16
	if w < 1 {
		w = 1
	}
	if w > 240 {
		w = 240
	}
	img := image.NewRGBA(image.Rect(0, 0, w*3, 81))
	draw1(view{img: img, scale: 3, x0: 0, rows: 27}, c, paletteFor(c.Theme))
	return img
}

// Preview draws the first 20 by 15 blocks at 16 pixels per block (320 by 240),
// like the square thumbnail.
func Preview(c *smm1.Course) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 320, 240))
	draw1(view{img: img, scale: 16, x0: 0, rows: 15}, c, paletteFor(c.Theme))
	return img
}

// JPEG encodes img, lowering the quality until it fits in limit bytes.
func JPEG(img image.Image, limit int) ([]byte, error) {
	var last []byte
	for q := 90; q >= 30; q -= 10 {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: q}); err != nil {
			return nil, err
		}
		last = buf.Bytes()
		if len(last) <= limit {
			return last, nil
		}
	}
	return last, nil
}

// PNG encodes img.
func PNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	return buf.Bytes(), err
}
