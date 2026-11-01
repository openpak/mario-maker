package gen

import (
	"math/rand/v2"
	"strings"

	"github.com/openpak/mario-maker/tools/coursegen/smm1"
)

var adjectives = []string{
	"Breezy", "Mellow", "Sunny", "Lantern", "Hollow", "Quiet", "Misty", "Brisk", "Gentle", "Windy",
	"Frosty", "Amber", "Silver", "Lucky", "Tiny", "Grand", "Hasty", "Dusty", "Merry", "Noble",
	"Spry", "Plucky", "Cobalt", "Crimson", "Velvet", "Copper", "Rusty", "Jolly", "Cosy", "Bright",
	"Twilight", "Golden", "Sleepy", "Rambling", "Humble", "Lively", "Clever", "Patient", "Wandering", "Steady",
}

var nouns = map[smm1.Theme][]string{
	smm1.Ground:      {"Meadow", "Hilltop", "Orchard", "Trail", "Ridge", "Valley", "Farm Lane", "Cliffside", "Footpath", "Hayfield"},
	smm1.Underground: {"Mineshaft", "Cavern", "Tunnel", "Quarry", "Crystal Cellar", "Burrow", "Boiler Room", "Lamp Passage", "Stone Hall", "Dig Site"},
	smm1.Castle:      {"Keep", "Rampart", "Fortress", "Bastion", "Gatehouse", "Tower Climb", "Moat Walk", "Great Hall", "Drawbridge", "Citadel"},
	smm1.Airship:     {"Skyway", "Cloud Convoy", "Balloon Bay", "Sky Harbor", "Windjammer", "Zeppelin Run", "Deck Dash", "Gale Route", "Kite Field", "Mast Climb"},
	smm1.Water:       {"Lagoon", "Reef", "Tide Pool", "Harbor", "Kelp Forest", "Sunken Road", "Coral Run", "Shoreline", "Wave Walk", "Bay"},
	smm1.GhostHouse:  {"Manor", "Attic", "Haunt", "Hallway", "Crypt", "Lantern Hall", "Foggy Stairs", "Cobweb Room", "Parlour", "Cellar"},
}

// banned lists words that must never appear in a course name: Nintendo
// trademarks and characters, and a few generic game terms the owner wants kept
// out of the starter set. Matching is on whole words, lower case.
var banned = []string{
	"mario", "luigi", "peach", "bowser", "koopa", "goomba", "yoshi", "toad", "toadette", "wario", "waluigi", "kamek",
	"nintendo", "wii", "switch", "zelda", "link", "kirby", "pikachu", "pokemon", "donkey", "kong", "metroid",
	"super", "thwomp", "boo", "bullet", "bill", "warp", "zone", "pipe", "world", "maker", "smb", "smw", "nsmbu",
}

// BannedWord reports a banned word in a course name, or "".
func BannedWord(name string) string {
	words := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') })
	for _, w := range words {
		for _, b := range banned {
			if w == b {
				return b
			}
		}
	}
	return ""
}

// namer hands out unique two-word names.
type namer struct {
	rng  *rand.Rand
	used map[string]bool
	adj  map[string]int
}

func newNamer(rng *rand.Rand) *namer {
	return &namer{rng: rng, used: map[string]bool{}, adj: map[string]int{}}
}

func (n *namer) name(theme smm1.Theme) string {
	for {
		a := adjectives[n.rng.IntN(len(adjectives))]
		set := nouns[theme]
		s := set[n.rng.IntN(len(set))]
		full := a + " " + s
		if n.used[full] || n.adj[a] >= 2 || len(full) > 32 || BannedWord(full) != "" {
			continue
		}
		n.used[full] = true
		n.adj[a]++
		return full
	}
}
