// Package wordfilter checks course titles and maker names against per-language lists of
// offensive words. A hit hides the row from the History pages (it is never deleted).
//
// The lists are the LDNOOBW lists (CC BY 4.0), see lists/NOTICE.md. Latin-script entries
// match whole words, or whole word sequences for phrases, after folding case, accents and
// the usual look-alike digits and symbols. Entries with Japanese or Korean characters, which
// have no spaces between words, match as substrings of the text with spaces removed.
package wordfilter

import (
	"embed"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

//go:embed lists/*.txt
var listFS embed.FS

// Langs are the bundled languages in display order.
var Langs = []string{"en", "ja", "es", "fr", "de"}

// ExtraLang labels hits from OpenPak's own additions.
const ExtraLang = "x"

// LangName is the display name of a bundled language.
func LangName(l string) string {
	switch l {
	case "en":
		return "English"
	case "ja":
		return "Japanese"
	case "es":
		return "Spanish"
	case "fr":
		return "French"
	case "de":
		return "German"
	}
	return l
}

// ignore lists entries that are ordinary words in game titles or in another bundled language, or
// that are only mildly rude. Measured on the archive (333,830 course titles, 109,654 makers) each
// one hid many clean titles ("Hardcore Parkour", "Escort The Payload", "S.M.B. 1", "Peter's
// World", "Fantasy VII", "country road") and the lists' own wording is kept as published.
var ignore = map[string]bool{
	"hardcore": true, "hard core": true, "escort": true, "butt": true, "suck": true, "sucks": true,
	"bite": true, "bitte": true, "trio": true, "péter": true, "pis": true, "con": true, "cul": true, "tit": true, "xx": true,
	"sm": true, "s&m": true, "s ＆ m": true, "infierno": true, "maldito": true,
	"いたずら": true, "ファンタジー": true, "縛り": true, "殺人事件": true, "両刀": true, "おしり": true, "オシリ": true,
	"お尻": true, "カント": true, "ローター": true, "嫌い": true, "女の子": true, "人種": true, "支配": true,
	"フック": true, "ラバー": true, "グロ": true, "なめ": true, "殺し方": true, "毛深い": true, "誘惑": true,
	"裸": true, "拷問": true, "挿入": true, "革抑制": true, "卍": true,
}

// extra is OpenPak's own short list of hate terms the published lists lack (lang "x").
var extra = []string{"hitler", "nazi", "nazis", "heil hitler", "sieg heil", "white power", "kkk"}

// Hit says which entry of which list matched.
type Hit struct{ Lang, Term string }

// OK reports whether nothing matched.
func (h Hit) OK() bool { return h.Term == "" }

type entry struct {
	lang, term string
	words      []string // latin: the word sequence
	sub        string   // cjk/symbol: substring
}

// Filter holds the compiled lists.
type Filter struct {
	words map[string][]entry // first word -> latin entries
	subs  []entry
	count map[string]int
}

var def *Filter

// Default returns the filter over the bundled lists.
func Default() *Filter {
	if def == nil {
		def = mustLoad()
	}
	return def
}

func mustLoad() *Filter {
	f := &Filter{words: map[string][]entry{}, count: map[string]int{}}
	for _, l := range Langs {
		b, err := listFS.ReadFile("lists/" + l + ".txt")
		if err != nil {
			panic(err)
		}
		for _, line := range strings.Split(string(b), "\n") {
			f.add(l, strings.TrimSpace(line))
		}
	}
	for _, t := range extra {
		f.add(ExtraLang, t)
	}
	return f
}

// New builds a filter from custom lists (tests).
func New(lists map[string][]string) *Filter {
	f := &Filter{words: map[string][]entry{}, count: map[string]int{}}
	for l, ts := range lists {
		for _, t := range ts {
			f.add(l, t)
		}
	}
	return f
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

func (f *Filter) add(lang, term string) {
	if term == "" || ignore[strings.ToLower(term)] {
		return
	}
	f.count[lang]++
	cjk, letters := false, false
	for _, r := range term {
		if isCJK(r) {
			cjk = true
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			letters = true
		}
	}
	if cjk || !letters {
		if s := squash(term); s != "" {
			f.subs = append(f.subs, entry{lang: lang, term: term, sub: s})
		}
		return
	}
	ws := tokens(fold(term, false))
	if len(ws) == 0 {
		return
	}
	f.words[ws[0]] = append(f.words[ws[0]], entry{lang: lang, term: term, words: ws})
}

// Count is the number of active entries of a language.
func (f *Filter) Count(lang string) int { return f.count[lang] }

// squash is NFKC, lower case, with spaces removed: the form substring entries match in.
func squash(s string) string {
	s = strings.ToLower(norm.NFKC.String(s))
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
}

var leet = map[rune]rune{'0': 'o', '1': 'i', '3': 'e', '4': 'a', '5': 's', '7': 't', '@': 'a', '$': 's', '!': 'i'}

// fold lower-cases, drops accents and, when lookalike is set, maps look-alike digits and symbols.
func fold(s string, lookalike bool) string {
	s = norm.NFKD.String(strings.ToLower(s))
	var b strings.Builder
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) && !isCJK(r) && r != 0x3099 && r != 0x309a {
			continue
		}
		if lookalike {
			if m, ok := leet[r]; ok {
				r = m
			}
		}
		b.WriteRune(r)
	}
	return b.String()
}

func tokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// Check returns the first list entry that matches s, or the zero Hit.
func (f *Filter) Check(s string) Hit {
	if strings.TrimSpace(s) == "" {
		return Hit{}
	}
	sq := squash(s)
	for _, e := range f.subs {
		if strings.Contains(sq, e.sub) {
			return Hit{e.lang, e.term}
		}
	}
	for _, look := range []bool{false, true} {
		ws := tokens(fold(s, look))
		for i, w := range ws {
			for _, e := range f.words[w] {
				if i+len(e.words) > len(ws) {
					continue
				}
				ok := true
				for j, x := range e.words {
					if ws[i+j] != x {
						ok = false
						break
					}
				}
				if ok {
					return Hit{e.lang, e.term}
				}
			}
		}
	}
	return Hit{}
}
