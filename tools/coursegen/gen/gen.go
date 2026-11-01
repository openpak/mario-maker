// Package gen builds the OpenPak starter courses: simple, valid, beatable
// Super Mario Maker (Wii U) courses composed from hand-written templates.
package gen

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/openpak/mario-maker/tools/coursegen/smm1"
	"github.com/openpak/mario-maker/tools/coursegen/validate"
)

// maxTemplate is the widest a template can get, in columns.
const maxTemplate = 34

// MakerName is the maker name written into every generated course.
const MakerName = "OpenPak"

// Epoch is the fixed creation time of generated courses, so output is
// reproducible.
var Epoch = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)

// Spec selects one course.
type Spec struct {
	Index int
	Style smm1.Style
	Theme smm1.Theme
	Band  Band
	Seed  uint64
}

// Meta describes a generated course for manifests.
type Meta struct {
	Spec
	Name        string
	Slug        string
	Templates   []string
	Timer       int
	Description string
}

// Plan returns the specs for the starter set: every style and theme in every
// band, minus four style/theme pairs (all three bands each) that are dropped to
// reach 60 courses. Courses alternate easy, normal, expert so any prefix is
// balanced. Index starts at 1.
func Plan(seed uint64) []Spec {
	byBand := map[Band][]Spec{}
	for _, bd := range Bands {
		for ti, th := range smm1.Themes {
			for si, st := range smm1.Styles {
				if (si+ti)%6 == 0 {
					continue
				}
				byBand[bd] = append(byBand[bd], Spec{Style: st, Theme: th, Band: bd})
			}
		}
	}
	var out []Spec
	for i := range byBand[Easy] {
		for _, bd := range Bands {
			out = append(out, byBand[bd][i])
		}
	}
	for i := range out {
		out[i].Index = i + 1
		out[i].Seed = seed*1000003 + uint64(i)*7919 + 17
	}
	return out
}

// Build makes one course from a spec. The name is passed in so that a whole
// set shares one namer.
func Build(sp Spec, name string) (*smm1.Course, *Meta) {
	rng := rand.New(rand.NewPCG(sp.Seed, sp.Seed^0x9e3779b97f4a7c15))
	b := newBuilder(sp.Style, sp.Theme, sp.Band, rng)
	target := b.in(b.cfg.minLen, b.cfg.maxLen)

	// Opening: a long level run so no enemy sits near the start.
	b.flat(6)
	b.note("opening")
	required := []func(){b.tGap, b.tPipes, b.tEnemies, b.tStairsUp, b.tCoins, b.tBlockRow}
	rng.Shuffle(len(required), func(i, j int) { required[i], required[j] = required[j], required[i] })
	optional := []func(){b.tFlat, b.tGap, b.tEnemies, b.tEnemies, b.tHill, b.tStairsDown, b.tBlockStairs, b.tCoins, b.tPipes, b.tBlockRow}
	if sp.Theme == smm1.Ground || sp.Theme == smm1.Water {
		optional = append(optional, b.tHill)
	}
	var seq []func()
	seq = append(seq, required...)
	for i := 0; i < b.cfg.extraSections; i++ {
		seq = append(seq, optional[rng.IntN(len(optional))])
	}
	mid := len(seq) / 2
	powered := false
	for i := 0; ; i++ {
		// The longest template is about 32 columns and the run-in 9, so a
		// template may only start early enough to keep the goal at block 230
		// or lower.
		if b.x() >= target || b.x() > validate.MaxGoalBlock-validate.GoalMargin-1-maxTemplate {
			break
		}
		if i == mid && !powered {
			b.tPowerUp()
			powered = true
			continue
		}
		var f func()
		if i < len(seq) {
			f = seq[i]
		} else {
			f = optional[rng.IntN(len(optional))]
		}
		f()
	}
	if !powered {
		b.tPowerUp()
	}
	// Run-in to the goal.
	b.flat(validate.GoalMargin + 1)
	goalX := b.x()
	goalH := b.h()
	c := assemble(b, goalX, goalH)
	c.Title = name
	c.Maker = MakerName
	c.Style = sp.Style
	c.Theme = sp.Theme
	c.SubTheme = smm1.Underground
	c.Modified = Epoch
	c.Time = timerFor(goalX, b.cfg.timerFactor)
	m := &Meta{Spec: sp, Name: name, Templates: b.used, Timer: c.Time}
	m.Slug = fmt.Sprintf("%02d-%s", sp.Index, slug(name))
	m.Description = describe(sp, b, goalX)
	return c, m
}

func timerFor(goal int, factor float64) int {
	t := int(float64(validate.ParTimer(goal)) * factor)
	t = (t + 49) / 50 * 50
	if t > validate.MaxTimer {
		t = validate.MaxTimer
	}
	if p := validate.ParTimer(goal); t < p {
		t = (p + 49) / 50 * 50
	}
	return t
}

func slug(s string) string {
	var sb strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
		case sb.Len() > 0 && !strings.HasSuffix(sb.String(), "-"):
			sb.WriteByte('-')
		}
	}
	return strings.TrimSuffix(sb.String(), "-")
}

// Set builds the whole plan with unique names.
func Set(seed uint64) ([]*smm1.Course, []*Meta) {
	plan := Plan(seed)
	nm := newNamer(rand.New(rand.NewPCG(seed, 0x6f70656e70616b)))
	var cs []*smm1.Course
	var ms []*Meta
	for _, sp := range plan {
		c, m := Build(sp, nm.name(sp.Theme))
		cs = append(cs, c)
		ms = append(ms, m)
	}
	return cs, ms
}

func describe(sp Spec, b *builder, goal int) string {
	counts := map[string]int{}
	for _, t := range b.used {
		counts[t]++
	}
	var parts []string
	if b.gaps > 0 {
		parts = append(parts, fmt.Sprintf("%d jump gap(s), the widest %d blocks", b.gaps, b.maxGap))
	}
	if n := counts["pipe-row"]; n > 0 {
		parts = append(parts, fmt.Sprintf("%d pipe row(s)", n))
	}
	if b.enemies > 0 {
		parts = append(parts, fmt.Sprintf("%d enemies", b.enemies))
	}
	parts = append(parts, "one power-up block about half way")
	return fmt.Sprintf("A %s %s course in the %s style, about %d blocks long. It has %s. Generated starter course: judge whether it is fun, fair and readable on a first try.",
		sp.Band, sp.Theme, styleName(sp.Style), goal+10, strings.Join(parts, ", "))
}

func styleName(s smm1.Style) string {
	switch s {
	case smm1.SMB1:
		return "SMB1"
	case smm1.SMB3:
		return "SMB3"
	case smm1.SMW:
		return "SMW"
	}
	return "NSMBU"
}
