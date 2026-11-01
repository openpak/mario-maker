package validate

import "github.com/openpak/mario-maker/tools/coursegen/smm1"

// Physics holds the conservative movement limits the validator and the
// generator share, in blocks.
//
// These are DESIGN MARGINS, not measurements. They were chosen well below the
// figures players commonly report for the four styles (a full jump clears
// roughly four to five blocks up, a running jump crosses roughly eight or more
// blocks of gap) because nobody has measured them on a console for this
// project and because a starter course must be beatable by a beginner. The
// first play-test round is expected to confirm or tighten them; change the
// numbers here and regenerate. Styles differ only slightly in the margins
// (SMW and NSMBU have the spin jump and a longer run); the table is kept per
// style so a measured value can be dropped in without touching the code.
type Physics struct {
	// MaxRise is the tallest wall, in blocks, that a single jump must clear
	// from level ground.
	MaxRise int
	// StandGap is the widest gap (blocks of missing ground) that can be
	// crossed from a standstill or a short walk.
	StandGap int
	// RunGap is the widest gap that can be crossed after a run-up of RunUp
	// flat blocks.
	RunGap int
	// RunUp is the number of flat blocks before the take-off edge (the edge
	// block included) that the validator requires for gaps above StandGap.
	RunUp int
	// MaxDrop is the longest fall, in blocks, that is allowed anywhere. Longer
	// falls are "blind drops": the landing is off screen.
	MaxDrop int
}

// PhysicsFor returns the limits for a game style.
func PhysicsFor(s smm1.Style) Physics {
	switch s {
	case smm1.SMB1:
		return Physics{MaxRise: 3, StandGap: 3, RunGap: 5, RunUp: 4, MaxDrop: 4}
	case smm1.SMB3:
		return Physics{MaxRise: 3, StandGap: 3, RunGap: 5, RunUp: 4, MaxDrop: 4}
	case smm1.SMW:
		return Physics{MaxRise: 3, StandGap: 4, RunGap: 6, RunUp: 4, MaxDrop: 4}
	default: // NSMBU
		return Physics{MaxRise: 3, StandGap: 4, RunGap: 6, RunUp: 4, MaxDrop: 4}
	}
}

// Game limits and layout constants.
const (
	GridW        = 240 // blocks in the widest course
	GridH        = 27  // blocks in a course
	MinTimer     = 100
	MaxTimer     = 500
	MaxGoalBlock = 230 // the goal piece is 13 wide and the course ends 10 blocks after its left edge
	StartWidth   = 8
	GoalWidth    = 13
	MinEnemyX    = 12
	GoalMargin   = 8 // no enemies this close in front of the goal
	GapMargin    = 2 // no enemies this close to a gap
	SoftObjects  = 2000
)

// ParTimer is the smallest timer, in seconds, the validator accepts for a
// course whose goal is at block goal: a slow walk of four blocks a second,
// doubled, plus a minute.
func ParTimer(goal int) int {
	t := goal/2 + 60
	if t < 150 {
		t = 150
	}
	return t
}
