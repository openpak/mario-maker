// Package model holds the two records the site stores and shows. They mirror the
// upstream read contract (docs/api-contract.md) after lenient parsing.
package model

import "time"

// Game slugs, as used in URLs and in the database.
const (
	SMM1 = "smm1"
	SMM2 = "smm2"
)

// Games lists the supported games in display order.
var Games = []string{SMM1, SMM2}

// GameName is the long display name of a game slug.
func GameName(g string) string {
	switch g {
	case SMM1:
		return "Super Mario Maker (Wii U)"
	case SMM2:
		return "Super Mario Maker 2 (Switch)"
	}
	return g
}

// GameShort is the short tab label of a game slug.
func GameShort(g string) string {
	switch g {
	case SMM1:
		return "SMM1 · Wii U"
	case SMM2:
		return "SMM2 · Switch"
	}
	return g
}

// ValidGame reports whether g is a known game slug.
func ValidGame(g string) bool { return g == SMM1 || g == SMM2 }

// Course is one uploaded course. Any field may be empty or zero when the game
// does not provide it.
type Course struct {
	Game         string
	ID           string // display course code
	DataID       string // decimal uint64
	Name         string
	Description  string
	MakerID      string
	MakerName    string
	Style        string
	Theme        string
	Difficulty   string
	Tags         []string
	Created      time.Time
	Updated      time.Time
	Stars        int
	Plays        int
	Clears       int
	Attempts     int
	HasThumbnail bool
}

// ClearRate is clears per attempt as a percentage, or -1 when unknown.
func (c Course) ClearRate() float64 {
	if c.Attempts <= 0 {
		return -1
	}
	return float64(c.Clears) * 100 / float64(c.Attempts)
}

// Maker is a course maker's profile.
type Maker struct {
	Game    string
	ID      string
	Name    string
	Courses int
	Stars   int
	Plays   int
	Created time.Time
}
