package validate_test

import (
	"strings"
	"testing"

	"github.com/openpak/mario-maker/tools/coursegen/gen"
	"github.com/openpak/mario-maker/tools/coursegen/smm1"
	"github.com/openpak/mario-maker/tools/coursegen/validate"
)

func base(t *testing.T) *smm1.Course {
	t.Helper()
	c, _ := gen.Build(gen.Spec{Index: 1, Style: smm1.SMB1, Theme: smm1.Ground, Band: gen.Normal, Seed: 5}, "Test Meadow")
	if r := validate.Check(c, validate.Options{}); !r.OK() {
		t.Fatalf("baseline invalid: %v", r.Errors)
	}
	return c
}

func expectErr(t *testing.T, c *smm1.Course, want string) {
	t.Helper()
	r := validate.Check(c, validate.Options{})
	for _, e := range r.Errors {
		if strings.Contains(e, want) {
			return
		}
	}
	t.Errorf("want an error containing %q, got %v", want, r.Errors)
}

func ground(c *smm1.Course, pred func(x, y int) bool) {
	var keep []smm1.Object
	for _, o := range c.Objects {
		if o.Type == smm1.TGround && pred(smm1.RawToBlock(int(o.X)), smm1.RawToBlock(int(o.Y))) {
			continue
		}
		keep = append(keep, o)
	}
	c.Objects = keep
}

func TestWideGapIsRejected(t *testing.T) {
	c := base(t)
	ground(c, func(x, y int) bool { return x >= 40 && x < 40+12 })
	expectErr(t, c, "gap")
}

func TestUnreachableGoal(t *testing.T) {
	c := base(t)
	// A wall of hard blocks five high across the course.
	for y := 1; y < 9; y++ {
		c.Objects = append(c.Objects, smm1.Object{X: uint32(smm1.BlockToRaw(30)), Y: int16(smm1.BlockToRaw(y)), W: 1, H: 1,
			Flags: smm1.DefaultFlags, ChildFlags: smm1.DefaultFlags, Type: smm1.THard, ChildType: -1, Link: -1, Effect: -1, U1: -1, U2: -1})
	}
	r := validate.Check(c, validate.Options{})
	if r.OK() {
		t.Fatal("a tall wall should be rejected")
	}
}

func TestOutOfBounds(t *testing.T) {
	c := base(t)
	c.Objects = append(c.Objects, smm1.Object{X: uint32(smm1.BlockToRaw(300)), Y: 80, W: 1, H: 1, Type: smm1.TCoin, ChildType: -1, Link: -1, Effect: -1})
	expectErr(t, c, "out of bounds")
}

func TestTimer(t *testing.T) {
	c := base(t)
	c.Time = 120
	expectErr(t, c, "timer")
	c.Time = 501
	expectErr(t, c, "timer")
}

func TestObjectLimit(t *testing.T) {
	c := base(t)
	for len(c.Objects) <= smm1.MaxObjects {
		c.Objects = append(c.Objects, smm1.Object{X: 80, Y: 80 + 160*20, W: 1, H: 1, Type: smm1.TCoin, ChildType: -1, Link: -1, Effect: -1})
	}
	expectErr(t, c, "exceed the game limit")
}

func TestEnemyNextToGap(t *testing.T) {
	c := base(t)
	// Put a goomba on the last block before the first pit.
	pit := 0
	for x := 8; x < 200; x++ {
		found := false
		for _, o := range c.Objects {
			if o.Type == smm1.TGround && smm1.RawToBlock(int(o.X)) == x {
				found = true
			}
		}
		if !found {
			pit = x
			break
		}
	}
	if pit == 0 {
		t.Skip("baseline has no pit")
	}
	h := 0
	for _, o := range c.Objects {
		if o.Type == smm1.TGround && smm1.RawToBlock(int(o.X)) == pit-1 && smm1.RawToBlock(int(o.Y)) >= h {
			h = smm1.RawToBlock(int(o.Y)) + 1
		}
	}
	c.Objects = append(c.Objects, smm1.Object{X: uint32(smm1.BlockToRaw(pit - 1)), Y: int16(smm1.BlockToRaw(h)), W: 1, H: 1,
		Type: smm1.TGoomba, ChildType: -1, Link: -1, Effect: -1})
	expectErr(t, c, "gap or edge")
}

func TestMissingGoal(t *testing.T) {
	c := base(t)
	var keep []smm1.Object
	for _, o := range c.Objects {
		if o.Type != smm1.TGoalPole {
			keep = append(keep, o)
		}
	}
	c.Objects = keep
	expectErr(t, c, "exactly one")
}

func TestBadWidthAndRoundTrip(t *testing.T) {
	c := base(t)
	c.WidthPx += 16
	expectErr(t, c, "width")
	c = base(t)
	b, _ := c.Marshal()
	r, err := validate.CheckBytes(b, validate.Options{})
	if err != nil || !r.OK() {
		t.Fatal(err, r)
	}
	b[0x40] ^= 0xff // corrupt without fixing the checksum
	if _, err := validate.CheckBytes(b, validate.Options{}); err == nil {
		t.Fatal("corrupt file accepted")
	}
}

func TestSoftlockDetected(t *testing.T) {
	c := base(t)
	// Make a pit of 3 columns in the middle deeper than a drop allows: remove
	// ground so a high plateau drops into a 1-block hole with a tall wall.
	// Simplest softlock: a one-wide hole walled by hard blocks higher than the
	// jump (4 high) placed right after a normal-height section.
	top := 0
	for _, o := range c.Objects {
		if o.Type == smm1.TGround && smm1.RawToBlock(int(o.X)) == 20 && smm1.RawToBlock(int(o.Y))+1 > top {
			top = smm1.RawToBlock(int(o.Y)) + 1
		}
	}
	for x := 20; x <= 24; x++ {
		for y := top; y < top+5; y++ {
			if x == 22 {
				continue // the hole keeps the floor
			}
			c.Objects = append(c.Objects, smm1.Object{X: uint32(smm1.BlockToRaw(x)), Y: int16(smm1.BlockToRaw(y)), W: 1, H: 1,
				Flags: smm1.DefaultFlags, ChildFlags: smm1.DefaultFlags, Type: smm1.THard, ChildType: -1, Link: -1, Effect: -1})
		}
	}
	if validate.Check(c, validate.Options{}).OK() {
		t.Fatal("a walled-in section should be rejected")
	}
}

// hand builds a course from column heights: start piece = column 0 height,
// terrain for columns 8..goalX-1, goal piece at goalX with height heights[goalX].
func hand(heights []int, goalX int) *smm1.Course {
	o := func(t int8, x, y, w, h int) smm1.Object {
		return smm1.Object{X: uint32(smm1.BlockToRaw(x)), Y: int16(smm1.BlockToRaw(y)), W: int8(w), H: int8(h),
			Flags: smm1.DefaultFlags, ChildFlags: smm1.DefaultFlags, Type: t, ChildType: -1, Link: -1, Effect: -1}
	}
	sh, gh := heights[0], heights[goalX]
	raw := func(t int8, rx, ry, w, h int) smm1.Object {
		x := o(t, 0, 0, w, h)
		x.X, x.Y = uint32(rx), int16(ry)
		return x
	}
	c := &smm1.Course{Title: "Hand", Maker: "OpenPak", Style: smm1.SMB1, Time: 400, WidthPx: (goalX + 10) * 16}
	c.Objects = append(c.Objects, raw(smm1.TPlayer, 0, 0, 1, 1), raw(smm1.TStartGround, 80, 80, 8, sh),
		raw(smm1.TStartSign, 400, sh*160, 3, 3), raw(smm1.TGoalGround, smm1.BlockToRaw(goalX), 80, 13, gh),
		raw(smm1.TGoalPole, smm1.BlockToRaw(goalX), gh*160, 10, 11))
	for x := 8; x < goalX; x++ {
		for y := 0; y < heights[x]; y++ {
			c.Objects = append(c.Objects, o(smm1.TGround, x, y, 1, 1))
		}
	}
	return c
}

func heights(goalX int, fn func(x int) int) []int {
	h := make([]int, goalX+14)
	for x := range h {
		h[x] = fn(x)
	}
	return h
}

func TestSimpleCourseIsFine(t *testing.T) {
	h := heights(40, func(x int) int { return 2 })
	if r := validate.Check(hand(h, 40), validate.Options{}); !r.OK() {
		t.Fatal(r.Errors)
	}
}

func TestRunUpIsRequiredForWideGaps(t *testing.T) {
	// Gap of 5 right after a 2-block ledge: no run-up, so it must fail on SMB1
	// (stand gap 3), even though 5 is within the running limit.
	h := heights(50, func(x int) int {
		if x >= 20 && x < 25 {
			return 0
		}
		if x >= 18 && x < 20 {
			return 2
		}
		if x < 18 && x >= 15 {
			return 3 // a step down onto a short ledge breaks the run-up
		}
		return 2
	})
	if validate.Check(hand(h, 50), validate.Options{}).OK() {
		t.Fatal("a wide gap without a run-up should fail")
	}
}
