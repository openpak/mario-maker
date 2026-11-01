package wordfilter

import "testing"

func TestCheck(t *testing.T) {
	f := Default()
	hits := []string{"fuck this level", "FUCK", "Heil Hitler Run", "a$$ kicker", "sh1t", "ホモォ...", "アナル攻め", "Golden  Shower", "Culo Grande", "Scheiße Arschloch", "va te faire foutre", "Un coño", "wet-dream"}
	clean := []string{"Let's only try S.M.M.O.B'S sb-lv", "Hard trust me S.M 3", "Kid Icarus: Of Myths & Monsters", "S.M.B.海のステージ", "Hardcore Parkour", "Escort The Payload",
		"Peter's World", "Super Mario Bros. 3-1", "Bob-omb Battlefield", "Mario Kicks Butt", "Assassin's Creed", "Scunthorpe Classic", "カントリー・ロード", "ファイナルファンタジー7", "class act", "Cocktail", "Mississippi"}
	for _, s := range hits {
		if f.Check(s).OK() {
			t.Errorf("%q not caught", s)
		}
	}
	for _, s := range clean {
		if h := f.Check(s); !h.OK() {
			t.Errorf("%q caught by %s:%q", s, h.Lang, h.Term)
		}
	}
}

func TestCustomLists(t *testing.T) {
	f := New(map[string][]string{"en": {"bad word"}, "ja": {"悪い"}})
	for s, want := range map[string]string{"A BAD  Word here": "en", "これは悪いコース": "ja", "": "", "badword": "", "bad": ""} {
		if got := f.Check(s).Lang; got != want {
			t.Errorf("%q: lang %q, want %q", s, got, want)
		}
	}
}

func TestListsLoaded(t *testing.T) {
	f := Default()
	for _, l := range Langs {
		if f.Count(l) < 50 {
			t.Errorf("%s: only %d entries", l, f.Count(l))
		}
	}
}
