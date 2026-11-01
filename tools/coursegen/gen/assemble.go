package gen

import (
	"sort"

	"github.com/openpak/mario-maker/tools/coursegen/smm1"
	"github.com/openpak/mario-maker/tools/coursegen/validate"
)

// assemble turns the column heights and placed things into a course object
// list: player, start, goal, then terrain tiles and things ordered by position.
func assemble(b *builder, goalX, goalH int) *smm1.Course {
	startH := b.heights[0]
	// Ground occupancy for the auto-tile neighbourhoods. Start and goal pieces
	// count as ground, the world floor (below row 0) counts as ground.
	heightAt := func(x int) int {
		switch {
		case x < 0:
			return 0
		case x < validate.StartWidth:
			return startH
		case x < goalX:
			return b.heights[x]
		case x < goalX+validate.GoalWidth:
			return goalH
		}
		return 0
	}
	occ := func(x, y int) bool { return y < 0 || y < heightAt(x) }
	dirs := [8][2]int{{0, 1}, {0, -1}, {1, 0}, {-1, 0}, {1, 1}, {-1, 1}, {1, -1}, {-1, -1}}

	base := func(t int8, x, y, w, h int) smm1.Object {
		return smm1.Object{
			X: uint32(smm1.BlockToRaw(x)), Y: int16(smm1.BlockToRaw(y)), W: int8(w), H: int8(h),
			Flags: smm1.DefaultFlags, ChildFlags: smm1.DefaultFlags, Type: t, ChildType: smm1.NoChild,
			Link: -1, Effect: -1, U1: -1, U2: -1, Sort: smm1.BlockSort,
		}
	}
	fixed := func(t int8, rawX, rawY, w, h int) smm1.Object {
		o := base(t, 0, 0, w, h)
		o.X, o.Y = uint32(rawX), int16(rawY)
		o.ChildFlags = 0x06000040
		o.Sort = 0
		return o
	}
	var objs []smm1.Object
	player := base(smm1.TPlayer, 0, 0, 1, 1)
	player.X, player.Y, player.Sort = 0, 0, 0
	objs = append(objs, player,
		fixed(smm1.TStartGround, 80, 80, validate.StartWidth, startH),
		fixed(smm1.TStartSign, 400, startH*160, 3, 3),
		fixed(smm1.TGoalGround, smm1.BlockToRaw(goalX), 80, validate.GoalWidth, goalH),
		fixed(smm1.TGoalPole, smm1.BlockToRaw(goalX), goalH*160, 10, 11),
	)

	type placed struct {
		x, y int
		o    smm1.Object
	}
	var rest []placed
	for x := validate.StartWidth; x < goalX; x++ {
		for y := 0; y < b.heights[x]; y++ {
			var k uint8
			for j, d := range dirs {
				if occ(x+d[0], y+d[1]) {
					k |= 1 << (7 - j)
				}
			}
			o := base(smm1.TGround, x, y, 1, 1)
			o.Ext = smm1.GroundExt(k)
			rest = append(rest, placed{x, y, o})
		}
	}
	for _, t := range b.things {
		o := base(t.typ, t.x, t.y, t.w, t.h)
		o.Flags, o.ChildFlags = t.flags, t.childFlag
		if t.child != 0 {
			o.ChildType = t.child
		}
		switch t.typ {
		case smm1.TCoin:
			o.Sort = smm1.CoinSort
		case smm1.TPipe:
			o.Sort = smm1.PipeSort
		case smm1.TBrick, smm1.TQuestion, smm1.THard:
		default:
			o.Sort = 1 // replaced below
		}
		rest = append(rest, placed{t.x, t.y, o})
	}
	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].x != rest[j].x {
			return rest[i].x < rest[j].x
		}
		return rest[i].y < rest[j].y
	})
	key, link := uint32(smm1.FirstActorKey), int16(0)
	for _, p := range rest {
		o := p.o
		if o.Sort == 1 {
			o.Sort = key
			key += 10
		}
		if o.Type == smm1.TPipe {
			o.Link = link
			link++
		}
		objs = append(objs, o)
	}
	return &smm1.Course{
		AutoScroll: 0,
		WidthPx:    (goalX + 10) * 16,
		Objects:    objs,
	}
}
