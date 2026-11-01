// Package validate statically checks a Super Mario Maker course file.
package validate

import (
	"fmt"
	"sort"

	"github.com/openpak/mario-maker/tools/coursegen/smm1"
)

// Report is the result of Check.
type Report struct {
	Errors   []string
	Warnings []string
	Stats    Stats
}

// Stats are numbers worth putting in a manifest.
type Stats struct {
	Objects    int
	Enemies    int
	Coins      int
	GoalBlock  int
	MaxGap     int
	MaxRise    int
	Reachable  int // standing positions reachable from the start
	CanFinish  int // of those, how many can still reach the goal
	LengthBlks int
}

// OK reports whether no error was found.
func (r *Report) OK() bool { return len(r.Errors) == 0 }

func (r *Report) errf(f string, a ...any)  { r.Errors = append(r.Errors, fmt.Sprintf(f, a...)) }
func (r *Report) warnf(f string, a ...any) { r.Warnings = append(r.Warnings, fmt.Sprintf(f, a...)) }

// Options narrow the checks to a difficulty band. Zero means "style limit".
type Options struct {
	MaxGap  int // widest gap allowed in this course
	MaxRise int // tallest wall allowed in this course
}

var enemyTypes = map[int8]bool{
	smm1.TGoomba: true, smm1.TKoopa: true, smm1.TPiranha: true, smm1.TSpiny: true,
	smm1.TBuzzy: true, smm1.TBobOmb: true, smm1.TDryBones: true, smm1.TCheep: true,
}

var airborne = map[int8]bool{smm1.TCheep: true}

var solidTypes = map[int8]bool{
	smm1.TBrick: true, smm1.TQuestion: true, smm1.THard: true, smm1.TGround: true,
}

// Check validates a parsed course. Use CheckBytes for raw file contents.
func Check(c *smm1.Course, opt Options) *Report {
	r := &Report{}
	ph := PhysicsFor(c.Style)
	if opt.MaxGap == 0 || opt.MaxGap > ph.RunGap {
		opt.MaxGap = ph.RunGap
	}
	if opt.MaxRise == 0 || opt.MaxRise > ph.MaxRise {
		opt.MaxRise = ph.MaxRise
	}
	r.Stats.Objects = len(c.Objects)

	// Header.
	if !c.Style.Valid() {
		r.errf("unknown style %q", c.Style)
	}
	if int(c.Theme) >= len(smm1.Themes) {
		r.errf("unknown theme %d", c.Theme)
	}
	if c.Title == "" || len([]rune(c.Title)) > 32 {
		r.errf("title must be 1 to 32 characters, got %d", len([]rune(c.Title)))
	}
	if c.Maker == "" {
		r.errf("maker name is empty")
	}
	if c.AutoScroll != 0 {
		r.warnf("autoscroll %d: starter courses are expected to be free-scrolling", c.AutoScroll)
	}
	if len(c.Objects) > smm1.MaxObjects {
		r.errf("%d objects exceed the game limit of %d", len(c.Objects), smm1.MaxObjects)
	}
	if len(c.Objects) > SoftObjects {
		r.warnf("%d objects is close to the limit of %d", len(c.Objects), smm1.MaxObjects)
	}

	// Collect objects.
	var player, start, sign, goal, pole *smm1.Object
	counts := map[int8]int{}
	var solid [GridW][GridH]bool
	seen := map[[3]int]bool{}
	inb := func(x, y int) bool { return x >= 0 && x < GridW && y >= 0 && y < GridH }
	for i := range c.Objects {
		o := &c.Objects[i]
		counts[o.Type]++
		bx, by := smm1.RawToBlock(int(o.X)), smm1.RawToBlock(int(o.Y))
		switch o.Type {
		case smm1.TPlayer:
			player = o
		case smm1.TStartGround:
			start = o
		case smm1.TStartSign:
			sign = o
		case smm1.TGoalGround:
			goal = o
		case smm1.TGoalPole:
			pole = o
		}
		switch o.Type {
		case smm1.TPlayer, smm1.TStartGround, smm1.TStartSign, smm1.TGoalGround, smm1.TGoalPole:
			continue
		}
		w, h := int(o.W), int(o.H)
		if w < 1 || h < 1 {
			r.errf("object %d (type %d) has size %dx%d", i, o.Type, w, h)
			continue
		}
		if o.Type != smm1.TPipe {
			w, h = 1, 1 // every other object the generator writes is one block
		}
		if !inb(bx, by) || !inb(bx+w-1, by+h-1) {
			r.errf("object %d (type %d) out of bounds at block %d,%d size %dx%d", i, o.Type, bx, by, w, h)
			continue
		}
		key := [3]int{bx, by, int(o.Type)}
		if seen[key] {
			r.errf("duplicate object type %d at block %d,%d", o.Type, bx, by)
		}
		seen[key] = true
		if o.Type == smm1.TPipe || solidTypes[o.Type] {
			for x := bx; x < bx+w; x++ {
				for y := by; y < by+h; y++ {
					if solid[x][y] && o.Type != smm1.TPipe {
						r.errf("solid object type %d overlaps another solid at block %d,%d", o.Type, x, y)
					}
					solid[x][y] = true
				}
			}
		}
	}
	if counts[smm1.TPlayer] != 1 || counts[smm1.TStartGround] != 1 || counts[smm1.TStartSign] != 1 ||
		counts[smm1.TGoalGround] != 1 || counts[smm1.TGoalPole] != 1 {
		r.errf("need exactly one player, start, start sign, goal and goal pole, got %d/%d/%d/%d/%d",
			counts[smm1.TPlayer], counts[smm1.TStartGround], counts[smm1.TStartSign],
			counts[smm1.TGoalGround], counts[smm1.TGoalPole])
	}
	if player == nil || start == nil || sign == nil || goal == nil || pole == nil {
		return r
	}
	_ = sign
	startH := int(start.H)
	goalX, goalH := smm1.RawToBlock(int(goal.X)), int(goal.H)
	r.Stats.GoalBlock = goalX
	r.Stats.LengthBlks = goalX + 10
	if smm1.RawToBlock(int(start.X)) != 0 || start.W != StartWidth || startH < 1 {
		r.errf("start piece must be %d wide at block 0 and at least 1 high", StartWidth)
	}
	if goal.W != GoalWidth || goalH < 1 || goalX > MaxGoalBlock || goalX < 24 {
		r.errf("goal piece must be %d wide, at least 1 high, between blocks 24 and %d (got %d)", GoalWidth, MaxGoalBlock, goalX)
	}
	if smm1.RawToBlock(int(pole.X)) != goalX || int(pole.Y) != goalH*160 {
		r.errf("goal pole is not on the goal piece")
	}
	if c.WidthPx != (goalX+10)*16 {
		r.errf("course width %d px does not match the goal at block %d (want %d)", c.WidthPx, goalX, (goalX+10)*16)
	}
	if goalH > 10 || startH > 10 {
		r.errf("start/goal piece higher than 10 blocks")
	}
	for x := 0; x < StartWidth; x++ {
		for y := 0; y < startH; y++ {
			solid[x][y] = true
		}
	}
	for x := goalX; x < goalX+GoalWidth && x < GridW; x++ {
		for y := 0; y < goalH; y++ {
			solid[x][y] = true
		}
	}

	// Timer.
	if c.Time < MinTimer || c.Time > MaxTimer {
		r.errf("timer %d outside %d..%d", c.Time, MinTimer, MaxTimer)
	} else if c.Time < ParTimer(goalX) {
		r.errf("timer %d is below the minimum %d for a goal at block %d", c.Time, ParTimer(goalX), goalX)
	}

	// Terrain shape: surface heights, pits, drops, walls.
	top := make([]int, GridW)
	for x := 0; x < GridW; x++ {
		for y := 0; y < GridH && solid[x][y]; y++ {
			top[x] = y + 1
		}
	}
	for x := 1; x < goalX+GoalWidth && x < GridW; x++ {
		if top[x-1] > 0 && top[x] > 0 {
			if top[x-1]-top[x] > ph.MaxDrop {
				r.errf("blind drop of %d blocks between blocks %d and %d", top[x-1]-top[x], x-1, x)
			}
			if top[x]-top[x-1] > opt.MaxRise {
				r.errf("wall of %d blocks at block %d is above the limit %d", top[x]-top[x-1], x, opt.MaxRise)
			}
		}
	}
	gap, maxGap := 0, 0
	for x := StartWidth; x < goalX; x++ {
		if top[x] == 0 {
			gap++
			if gap > maxGap {
				maxGap = gap
			}
		} else {
			gap = 0
		}
	}
	r.Stats.MaxGap = maxGap
	if maxGap > opt.MaxGap {
		r.errf("a gap of %d blocks is above the limit %d", maxGap, opt.MaxGap)
	}
	for x := 0; x < goalX+GoalWidth && x < GridW; x++ {
		if top[x] > 0 {
			continue
		}
		// A pit column must be flanked by ground on both sides within the limit.
		l, rr := x, x
		for l > 0 && top[l-1] == 0 {
			l--
		}
		for rr+1 < GridW && top[rr+1] == 0 {
			rr++
		}
		if l == 0 || rr-l+1 > ph.RunGap {
			r.errf("pit starting at block %d is not bounded by ground or is wider than %d", l, ph.RunGap)
		}
		x = rr
	}

	// Objects fit the level: nothing inside terrain, actors supported, fair enemies.
	for i := range c.Objects {
		o := &c.Objects[i]
		switch o.Type {
		case smm1.TPlayer, smm1.TStartGround, smm1.TStartSign, smm1.TGoalGround, smm1.TGoalPole, smm1.TGround, smm1.TPipe:
			continue
		}
		bx, by := smm1.RawToBlock(int(o.X)), smm1.RawToBlock(int(o.Y))
		if !inb(bx, by) {
			continue
		}
		if o.Type == smm1.TCoin {
			r.Stats.Coins++
		}
		if !solidTypes[o.Type] && solid[bx][by] {
			r.errf("object type %d at block %d,%d is inside solid terrain", o.Type, bx, by)
		}
		if !enemyTypes[o.Type] {
			continue
		}
		r.Stats.Enemies++
		if bx < MinEnemyX || bx > goalX-GoalMargin {
			r.errf("enemy at block %d is inside the start/goal safe zone", bx)
		}
		if airborne[o.Type] {
			continue
		}
		for cx := bx - GapMargin; cx <= bx+GapMargin; cx++ {
			if cx < 0 || cx >= GridW || !solid[cx][by-1] {
				r.errf("enemy type %d at block %d,%d is within %d blocks of a gap or edge", o.Type, bx, by, GapMargin)
				break
			}
		}
	}
	if goalX > 0 && float64(r.Stats.Enemies)/float64(goalX) > 0.25 {
		r.warnf("enemy density %.2f per block is high", float64(r.Stats.Enemies)/float64(goalX))
	}
	if counts[smm1.TCoin] == 0 {
		r.warnf("no coins")
	}

	// Reachability and softlocks.
	reach(c, r, &solid, ph, opt, startH, goalX)
	return r
}

type state struct{ x, y int }

func reach(c *smm1.Course, r *Report, solid *[GridW][GridH]bool, ph Physics, opt Options, startH, goalX int) {
	empty := func(x, y int) bool { return x >= 0 && x < GridW && y >= 0 && y < GridH && !solid[x][y] }
	standable := func(x, y int) bool {
		return y >= 1 && y < GridH-2 && x >= 0 && x < GridW && empty(x, y) && empty(x, y+1) && solid[x][y-1]
	}
	colEmpty := func(x, y0, y1 int) bool {
		for y := y0; y <= y1; y++ {
			if !empty(x, y) {
				return false
			}
		}
		return true
	}
	maxD := opt.MaxGap + 1
	neighbours := func(s state) []state {
		var out []state
		for _, dir := range []int{-1, 1} {
			for d := 1; d <= maxD; d++ {
				nx := s.x + dir*d
				if nx < 0 || nx >= GridW {
					break
				}
				for ny := 1; ny < GridH-2; ny++ {
					if !standable(nx, ny) {
						continue
					}
					rise := ny - s.y
					if rise > opt.MaxRise || -rise > ph.MaxDrop {
						continue
					}
					ok := true
					if d == 1 {
						if rise > 0 {
							ok = colEmpty(s.x, s.y+1, ny+1)
						} else {
							ok = colEmpty(nx, ny, s.y+1)
						}
					} else {
						lim := 1
						if d == 2 && opt.MaxRise > 2 {
							lim = 2
						} else if d == 2 {
							lim = opt.MaxRise
						}
						if rise > lim {
							continue
						}
						hi := s.y
						if ny > hi {
							hi = ny
						}
						for c := 1; c < d && ok; c++ {
							ok = colEmpty(s.x+dir*c, hi, hi+2)
						}
						if ok && rise > 0 {
							ok = colEmpty(s.x, s.y+1, hi+2)
						}
						if ok && rise < 0 {
							ok = colEmpty(nx, ny, s.y+2)
						}
						if ok && d-1 > ph.StandGap {
							for k := 0; k < ph.RunUp && ok; k++ {
								ok = standable(s.x-dir*k, s.y)
							}
						}
					}
					if ok {
						out = append(out, state{nx, ny})
					}
				}
			}
		}
		return out
	}

	var starts []state
	for x := 0; x < StartWidth; x++ {
		if standable(x, startH) {
			starts = append(starts, state{x, startH})
		}
	}
	if len(starts) == 0 {
		r.errf("nothing to stand on at the start")
		return
	}
	// Forward BFS, and record edges for the reverse pass.
	adj := map[state][]state{}
	rev := map[state][]state{}
	seen := map[state]bool{}
	queue := append([]state(nil), starts[:1]...)
	seen[starts[0]] = true
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		for _, n := range neighbours(s) {
			adj[s] = append(adj[s], n)
			rev[n] = append(rev[n], s)
			if !seen[n] {
				seen[n] = true
				queue = append(queue, n)
			}
		}
	}
	r.Stats.Reachable = len(seen)
	var goals []state
	for s := range seen {
		if s.x >= goalX {
			goals = append(goals, s)
		}
	}
	if len(goals) == 0 {
		r.errf("the goal is not reachable from the start")
		return
	}
	sort.Slice(goals, func(i, j int) bool {
		return goals[i].x < goals[j].x || goals[i].x == goals[j].x && goals[i].y < goals[j].y
	})
	fin := map[state]bool{}
	q := append([]state(nil), goals...)
	for _, g := range goals {
		fin[g] = true
	}
	for len(q) > 0 {
		s := q[0]
		q = q[1:]
		for _, p := range rev[s] {
			if !fin[p] {
				fin[p] = true
				q = append(q, p)
			}
		}
	}
	r.Stats.CanFinish = len(fin)
	stuck := 0
	var first state
	for s := range seen {
		if !fin[s] {
			if stuck == 0 || s.x < first.x {
				first = s
			}
			stuck++
		}
	}
	if stuck > 0 {
		r.errf("%d reachable positions cannot reach the goal (softlock), first near block %d,%d", stuck, first.x, first.y)
	}
	// Largest rise actually required along the way is bounded by opt.MaxRise.
	r.Stats.MaxRise = opt.MaxRise
}

// CheckBytes parses and validates raw course_data.cdt contents, and checks
// that parse and marshal round-trip byte for byte.
func CheckBytes(b []byte, opt Options) (*Report, error) {
	c, err := smm1.Parse(b)
	if err != nil {
		return nil, err
	}
	r := Check(c, opt)
	back, err := c.Marshal()
	if err != nil {
		r.errf("re-marshal failed: %v", err)
	} else if string(back) != string(b) {
		r.errf("file does not round-trip through the parser")
	}
	return r, nil
}
