package render

import (
	"strings"

	"github.com/openpak/mario-maker/tools/coursegen/smm1"
)

// ASCII draws the course as text, one character per block, top row first:
// # ground, = brick, ? question block, H hard block, P pipe, o coin, e enemy,
// S start, G goal.
func ASCII(c *smm1.Course) string {
	const w, h = 240, 16
	var g [h][w]byte
	for y := range g {
		for x := range g[y] {
			g[y][x] = ' '
		}
	}
	set := func(x, y int, ch byte) {
		if x >= 0 && x < w && y >= 0 && y < h {
			g[h-1-y][x] = ch
		}
	}
	for _, o := range c.Objects {
		bx, by := smm1.RawToBlock(int(o.X)), smm1.RawToBlock(int(o.Y))
		switch o.Type {
		case smm1.TStartGround, smm1.TGoalGround:
			ch := byte('S')
			if o.Type == smm1.TGoalGround {
				ch = 'G'
			}
			for x := 0; x < int(o.W); x++ {
				for y := 0; y < int(o.H); y++ {
					set(bx+x, y, ch)
				}
			}
		case smm1.TGround:
			set(bx, by, '#')
		case smm1.TBrick:
			set(bx, by, '=')
		case smm1.TQuestion:
			set(bx, by, '?')
		case smm1.THard:
			set(bx, by, 'H')
		case smm1.TCoin:
			set(bx, by, 'o')
		case smm1.TPipe:
			for x := 0; x < 2; x++ {
				for y := 0; y < int(o.H); y++ {
					set(bx+x, by+y, 'P')
				}
			}
		case smm1.TPlayer, smm1.TStartSign, smm1.TGoalPole:
		default:
			set(bx, by, 'e')
		}
	}
	end := c.WidthPx/16 + 3
	var sb strings.Builder
	for y := 0; y < h; y++ {
		sb.WriteString(strings.TrimRight(string(g[y][:min(end, w)]), " "))
		sb.WriteByte('\n')
	}
	return sb.String()
}
