package gen

import "github.com/openpak/mario-maker/tools/coursegen/validate"

// Band is a difficulty band. The names match OpenMaker's 100 Mario Challenge
// bands; the generator never makes "super expert" courses.
type Band string

const (
	Easy   Band = "easy"
	Normal Band = "normal"
	Expert Band = "expert"
)

// Bands lists the bands in order.
var Bands = []Band{Easy, Normal, Expert}

// bandCfg holds the knobs of one band. Gap sizes are blocks of missing ground
// and are clamped to the style's physics, so a band can never ask for more
// than the validator accepts.
type bandCfg struct {
	minLen, maxLen int // goal block range
	gapMin, gapMax int
	maxStep        int // tallest single step or stack
	stepWidth      int
	pipeHeight     int
	pipeCount      int
	groupSize      int
	enemySpacing   int
	extraSections  int
	spiny          bool
	timerFactor    float64
}

func cfgFor(b Band, ph validate.Physics) bandCfg {
	var c bandCfg
	switch b {
	case Easy:
		c = bandCfg{minLen: 90, maxLen: 120, gapMin: 2, gapMax: 3, maxStep: 2, stepWidth: 3, pipeHeight: 2, pipeCount: 2,
			groupSize: 2, enemySpacing: 5, extraSections: 3, timerFactor: 2.6}
	case Normal:
		c = bandCfg{minLen: 125, maxLen: 160, gapMin: 3, gapMax: 4, maxStep: 3, stepWidth: 2, pipeHeight: 2, pipeCount: 3,
			groupSize: 3, enemySpacing: 4, extraSections: 5, spiny: true, timerFactor: 2.3}
	default:
		c = bandCfg{minLen: 160, maxLen: 185, gapMin: 4, gapMax: 5, maxStep: 3, stepWidth: 1, pipeHeight: 3, pipeCount: 3,
			groupSize: 4, enemySpacing: 3, extraSections: 7, spiny: true, timerFactor: 2.0}
	}
	if c.gapMax > ph.RunGap {
		c.gapMax = ph.RunGap
	}
	if c.gapMin > c.gapMax {
		c.gapMin = c.gapMax
	}
	if c.maxStep > ph.MaxRise {
		c.maxStep = ph.MaxRise
	}
	return c
}
