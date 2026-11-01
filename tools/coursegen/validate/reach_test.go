package validate

import (
	"strings"
	"testing"

	"github.com/openpak/mario-maker/tools/coursegen/smm1"
)

// grid builds a solid grid from column heights.
func grid(h func(x int) int) *[GridW][GridH]bool {
	var g [GridW][GridH]bool
	for x := 0; x < GridW; x++ {
		for y := 0; y < h(x) && y < GridH; y++ {
			g[x][y] = true
		}
	}
	return &g
}

func run(t *testing.T, h func(x int) int, goalX int) *Report {
	t.Helper()
	r := &Report{}
	ph := PhysicsFor(smm1.SMB1)
	reach(nil, r, grid(h), ph, Options{MaxGap: ph.RunGap, MaxRise: ph.MaxRise}, h(0), goalX)
	return r
}

func hasErr(r *Report, sub string) bool {
	for _, e := range r.Errors {
		if strings.Contains(e, sub) {
			return true
		}
	}
	return false
}

func TestTrapPocketIsASoftlock(t *testing.T) {
	// Plateau 5 high; a pocket whose floor is 4 lower; a far side 5 high. Falling
	// into the pocket is allowed, climbing out (rise 4) is not, but the plateau
	// can still jump across to the far side.
	r := run(t, func(x int) int {
		switch {
		case x < 8:
			return 5
		case x < 12:
			return 1
		}
		return 5
	}, 14)
	if !hasErr(r, "softlock") {
		t.Fatalf("want a softlock error, got %v", r.Errors)
	}
}

func TestPlainPathHasNoSoftlock(t *testing.T) {
	r := run(t, func(x int) int { return 2 }, 30)
	if len(r.Errors) != 0 {
		t.Fatal(r.Errors)
	}
	if r.Stats.Reachable == 0 || r.Stats.CanFinish != r.Stats.Reachable {
		t.Fatalf("stats %+v", r.Stats)
	}
}

func TestPitTooWide(t *testing.T) {
	r := run(t, func(x int) int {
		if x >= 10 && x < 20 {
			return 0
		}
		return 2
	}, 30)
	if !hasErr(r, "not reachable") {
		t.Fatalf("want an unreachable-goal error, got %v", r.Errors)
	}
}
