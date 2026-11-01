package gen

import (
	"math/rand/v2"

	"github.com/openpak/mario-maker/tools/coursegen/smm1"
	"github.com/openpak/mario-maker/tools/coursegen/validate"
)

// thing is one placed object in block coordinates (bottom-left block).
type thing struct {
	typ       int8
	x, y      int
	w, h      int
	flags     uint32
	child     int8 // object held inside a block; 0 = none
	childFlag uint32
}

// builder lays a course out column by column. heights[x] is the number of
// ground blocks in column x (0 is a pit).
type builder struct {
	style   smm1.Style
	theme   smm1.Theme
	band    Band
	ph      validate.Physics
	cfg     bandCfg
	rng     *rand.Rand
	heights []int
	things  []thing
	used    []string
	enemies int
	gaps    int
	maxGap  int
	// clean is the first column from which the ground is bare: columns before
	// it carry pipes or block stacks, so they are no run-up.
	clean int
}

func newBuilder(style smm1.Style, theme smm1.Theme, band Band, rng *rand.Rand) *builder {
	ph := validate.PhysicsFor(style)
	b := &builder{style: style, theme: theme, band: band, ph: ph, cfg: cfgFor(band, ph), rng: rng}
	h0 := 1 + rng.IntN(3)
	for i := 0; i < validate.StartWidth; i++ {
		b.heights = append(b.heights, h0)
	}
	return b
}

func (b *builder) x() int            { return len(b.heights) }
func (b *builder) h() int            { return b.heights[len(b.heights)-1] }
func (b *builder) in(lo, hi int) int { return lo + b.rng.IntN(hi-lo+1) }
func (b *builder) note(n string)     { b.used = append(b.used, n) }

func (b *builder) add(t thing) {
	if t.w == 0 {
		t.w, t.h = 1, 1
	}
	if t.flags == 0 {
		t.flags = smm1.DefaultFlags
	}
	if t.childFlag == 0 {
		t.childFlag = smm1.DefaultFlags
	}
	b.things = append(b.things, t)
}

func (b *builder) col(h int) { b.heights = append(b.heights, h) }

func (b *builder) flat(n int) {
	h := b.h()
	for i := 0; i < n; i++ {
		b.col(h)
	}
}

// flatRun is the number of trailing columns at the current height.
func (b *builder) flatRun() int {
	h, n := b.h(), 0
	for i := len(b.heights) - 1; i >= b.clean && b.heights[i] == h; i-- {
		n++
	}
	return n
}

// settle pads with flat ground so the next template starts on at least n
// level blocks (enemy margins, run-ups).
func (b *builder) settle(n int) {
	if r := b.flatRun(); r < n {
		b.flat(n - r)
	}
}

func (b *builder) coin(x, y int) { b.add(thing{typ: smm1.TCoin, x: x, y: y}) }

// ---- templates -----------------------------------------------------------

// tFlat is a flat run with a row of coins.
func (b *builder) tFlat() {
	b.note("flat-run")
	b.settle(1)
	n := b.in(6, 10)
	x0, h := b.x(), b.h()
	b.flat(n)
	for i := 1; i < n-1; i += 1 {
		b.coin(x0+i, h+1+b.rng.IntN(1))
	}
}

// tCoins is a coin arc over flat ground.
func (b *builder) tCoins() {
	b.note("coin-arc")
	b.settle(1)
	x0, h := b.x(), b.h()
	b.flat(9)
	for i, dy := range []int{1, 2, 3, 3, 3, 3, 3, 2, 1} {
		b.coin(x0+i, h+dy)
	}
}

// tGap is a pit with a safe jump: a flat run-up, 2..N missing blocks, a flat
// landing, and a coin arc that shows the jump.
func (b *builder) tGap() {
	b.note("gap-hop")
	b.settle(b.ph.RunUp + 1)
	g := b.in(b.cfg.gapMin, b.cfg.gapMax)
	h := b.h()
	x0 := b.x()
	for i := 0; i < g; i++ {
		b.col(0)
	}
	b.gaps++
	if g > b.maxGap {
		b.maxGap = g
	}
	// Landing: level, or one block higher for the narrow gaps.
	land := h
	if g >= 3 && b.band != Easy && h < 6 && b.rng.IntN(3) == 0 {
		land = h + 1
	}
	for i := 0; i < 4; i++ {
		b.col(land)
	}
	// Coin arc across the pit.
	arc := []int{2, 3, 3, 3, 3, 3, 2}
	for i := 0; i < g+2 && i < len(arc); i++ {
		b.coin(x0-1+i, h+arc[i])
	}
}

// tStairsUp climbs a staircase of ground, one block per step.
func (b *builder) tStairsUp() {
	b.settle(2)
	n := b.in(2, 2+b.cfg.maxStep)
	if b.h()+n > 7 {
		n = 7 - b.h()
	}
	if n < 1 {
		return
	}
	b.note("stairs-up")
	for i := 0; i < n; i++ {
		b.col(b.h() + 1)
		for j := 1; j < b.cfg.stepWidth+1; j++ {
			b.col(b.h())
		}
	}
	b.flat(3)
}

// tStairsDown descends a staircase of ground, one block per step.
func (b *builder) tStairsDown() {
	n := b.in(2, 2+b.cfg.maxStep)
	if b.h()-n < 1 {
		n = b.h() - 1
	}
	if n < 1 {
		return
	}
	b.note("stairs-down")
	b.settle(2)
	for i := 0; i < n; i++ {
		b.col(b.h() - 1)
		for j := 1; j < b.cfg.stepWidth+1; j++ {
			b.col(b.h())
		}
	}
	b.flat(3)
}

// tHill goes up, runs along the top and comes back down.
func (b *builder) tHill() {
	if b.h()+2 > 6 {
		return
	}
	b.note("hill")
	save := b.used
	b.tStairsUp()
	b.flat(b.in(3, 5))
	b.tStairsDown()
	b.used = save
}

// tBlockStairs is a small pyramid of hard blocks standing on flat ground.
func (b *builder) tBlockStairs() {
	b.note("block-stairs")
	b.settle(3)
	n := b.in(2, b.cfg.maxStep)
	h := b.h()
	x0 := b.x()
	heights := []int{}
	for i := 1; i <= n; i++ {
		heights = append(heights, i)
	}
	for i := n - 1; i >= 1; i-- {
		heights = append(heights, i)
	}
	for i, hh := range heights {
		b.col(h)
		for y := 0; y < hh; y++ {
			b.add(thing{typ: smm1.THard, x: x0 + i, y: h + y})
		}
	}
	b.clean = b.x()
	b.flat(3)
}

// tPipes is a row of upright pipes with level ground between them.
func (b *builder) tPipes() {
	b.note("pipe-row")
	b.settle(3)
	k := b.in(2, b.cfg.pipeCount)
	for i := 0; i < k; i++ {
		ph := b.cfg.pipeHeight
		if ph > 2 && b.rng.IntN(2) == 0 {
			ph--
		}
		x0, h := b.x(), b.h()
		b.add(thing{typ: smm1.TPipe, x: x0, y: h, w: 2, h: ph})
		b.flat(2)
		b.clean = b.x()
		b.flat(b.in(3, 5))
	}
}

var enemySets = map[smm1.Theme][]int8{
	smm1.Ground:      {smm1.TGoomba, smm1.TKoopa},
	smm1.Underground: {smm1.TGoomba, smm1.TBuzzy},
	smm1.Castle:      {smm1.TBuzzy, smm1.TDryBones, smm1.TSpiny},
	smm1.Airship:     {smm1.TGoomba, smm1.TKoopa, smm1.TSpiny},
	smm1.Water:       {smm1.TGoomba, smm1.TCheep},
	smm1.GhostHouse:  {smm1.TDryBones, smm1.TGoomba},
}

func (b *builder) pickEnemy() int8 {
	set := enemySets[b.theme]
	for {
		t := set[b.rng.IntN(len(set))]
		if t == smm1.TSpiny && !b.cfg.spiny {
			continue
		}
		return t
	}
}

// tEnemies places a group of walkers on a level stretch with a safe margin on
// both sides.
func (b *builder) tEnemies() {
	b.note("enemy-group")
	b.settle(4)
	n := b.in(b.cfg.groupSize-1, b.cfg.groupSize)
	if n < 1 {
		n = 1
	}
	sp := b.cfg.enemySpacing
	x0, h := b.x(), b.h()
	total := 4 + n*sp
	b.flat(total)
	for i := 0; i < n; i++ {
		t := thing{typ: b.pickEnemy(), x: x0 + 3 + i*sp, y: h}
		if t.typ == smm1.TKoopa && b.rng.IntN(2) == 0 {
			t.flags |= smm1.FlagVariant
		}
		if t.typ == smm1.TCheep {
			t.y = h + 2 + b.rng.IntN(2)
		}
		b.add(t)
		b.enemies++
	}
}

// tBlockRow floats a short row of bricks and question blocks over flat ground.
func (b *builder) tBlockRow() {
	b.note("block-row")
	b.settle(2)
	x0, h := b.x(), b.h()
	n := b.in(4, 6)
	b.flat(n + 2)
	for i := 0; i < n; i++ {
		t := thing{typ: smm1.TBrick, x: x0 + 1 + i, y: h + 4}
		if i%2 == 1 {
			t.typ = smm1.TQuestion
		}
		b.add(t)
	}
	for i := 1; i < n; i += 2 {
		b.coin(x0+1+i, h+6)
	}
}

// tPowerUp is the mid-course reward: a question block holding a mushroom
// between two bricks, with level ground around it.
func (b *builder) tPowerUp() {
	b.note("power-up")
	b.settle(3)
	x0, h := b.x(), b.h()
	b.flat(7)
	b.add(thing{typ: smm1.TBrick, x: x0 + 2, y: h + 4})
	b.add(thing{typ: smm1.TQuestion, x: x0 + 3, y: h + 4, child: smm1.TSuperMush})
	b.add(thing{typ: smm1.TBrick, x: x0 + 4, y: h + 4})
}
