package gen

import (
	"testing"

	"github.com/openpak/mario-maker/tools/coursegen/smm1"
	"github.com/openpak/mario-maker/tools/coursegen/validate"
)

func TestSetIsValid(t *testing.T) {
	cs, ms := Set(1)
	if len(cs) != 60 {
		t.Fatalf("got %d courses, want 60", len(cs))
	}
	names := map[string]bool{}
	styles, themes, bands := map[smm1.Style]int{}, map[smm1.Theme]int{}, map[Band]int{}
	for i, c := range cs {
		m := ms[i]
		b, err := c.Marshal()
		if err != nil {
			t.Fatalf("%s: %v", m.Name, err)
		}
		rep, err := validate.CheckBytes(b, validate.Options{MaxGap: cfgFor(m.Band, validate.PhysicsFor(m.Style)).gapMax})
		if err != nil {
			t.Fatalf("%s: %v", m.Name, err)
		}
		if !rep.OK() {
			t.Errorf("%s (%s/%s/%s): %v", m.Name, m.Style, m.Theme, m.Band, rep.Errors)
		}
		if names[m.Name] {
			t.Errorf("duplicate name %q", m.Name)
		}
		names[m.Name] = true
		if w := BannedWord(m.Name); w != "" {
			t.Errorf("name %q contains %q", m.Name, w)
		}
		if len(m.Templates) < 6 {
			t.Errorf("%s: only %d templates", m.Name, len(m.Templates))
		}
		styles[m.Style]++
		themes[m.Theme]++
		bands[m.Band]++
	}
	for _, s := range smm1.Styles {
		if styles[s] != 15 {
			t.Errorf("style %s has %d courses", s, styles[s])
		}
	}
	for _, th := range smm1.Themes {
		if themes[th] < 9 {
			t.Errorf("theme %s has %d courses", th, themes[th])
		}
	}
	for _, b := range Bands {
		if bands[b] != 20 {
			t.Errorf("band %s has %d courses", b, bands[b])
		}
	}
}

func TestDeterministic(t *testing.T) {
	a, _ := Set(7)
	b, _ := Set(7)
	for i := range a {
		x, _ := a[i].Marshal()
		y, _ := b[i].Marshal()
		if string(x) != string(y) {
			t.Fatalf("course %d differs between runs", i)
		}
	}
}

func TestBannedWord(t *testing.T) {
	if BannedWord("Mario Meadow") == "" || BannedWord("Boo Hall") == "" {
		t.Fatal("expected a hit")
	}
	if BannedWord("Breezy Meadow") != "" {
		t.Fatal("false positive")
	}
}

// TestManySeeds guards the generator against rare layouts the validator rejects.
func TestManySeeds(t *testing.T) {
	n := 60
	if testing.Short() {
		n = 10
	}
	for seed := uint64(1); seed <= uint64(n); seed++ {
		cs, ms := Set(seed)
		for i, c := range cs {
			b, err := c.Marshal()
			if err != nil {
				t.Fatalf("seed %d %s: %v", seed, ms[i].Name, err)
			}
			rep, err := validate.CheckBytes(b, validate.Options{MaxGap: cfgFor(ms[i].Band, validate.PhysicsFor(ms[i].Style)).gapMax})
			if err != nil || !rep.OK() {
				t.Errorf("seed %d %s (%s/%s/%s): %v %v", seed, ms[i].Name, ms[i].Style, ms[i].Theme, ms[i].Band, err, rep.Errors)
			}
		}
	}
}
