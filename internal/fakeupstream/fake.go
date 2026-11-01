// Package fakeupstream implements the upstream read contract (docs/api-contract.md)
// over in-memory data. Tests and examples/fake-upstream use it.
package fakeupstream

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Course is the contract's course record.
type Course struct {
	Game         string   `json:"game"`
	ID           string   `json:"id"`
	DataID       uint64   `json:"data_id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	MakerID      string   `json:"maker_id"`
	MakerName    string   `json:"maker_name"`
	Style        string   `json:"style"`
	Theme        string   `json:"theme"`
	Difficulty   string   `json:"difficulty"`
	Tags         []string `json:"tags"`
	Created      string   `json:"created"`
	Updated      string   `json:"updated"`
	Stars        int      `json:"stars"`
	Plays        int      `json:"plays"`
	Clears       int      `json:"clears"`
	Attempts     int      `json:"attempts"`
	HasThumbnail bool     `json:"has_thumbnail"`
}

// Maker is the contract's maker record.
type Maker struct {
	Game    string `json:"game"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Courses int    `json:"courses"`
	Stars   int    `json:"stars"`
	Plays   int    `json:"plays"`
	Created string `json:"created"`
}

// Fake is the handler; mutate it between requests with Set/Remove.
type Fake struct {
	Key string // required X-Internal-Key when non-empty

	mu       sync.Mutex
	courses  map[string]Course
	makers   map[string]Maker
	Requests int // course-list requests served
	Thumbs   int // thumbnail requests served
}

// New returns an empty fake.
func New(key string) *Fake {
	return &Fake{Key: key, courses: map[string]Course{}, makers: map[string]Maker{}}
}

// Set adds or replaces a course.
func (f *Fake) Set(c Course) { f.mu.Lock(); f.courses[c.ID] = c; f.mu.Unlock() }

// Remove deletes a course.
func (f *Fake) Remove(id string) {
	f.mu.Lock()
	delete(f.courses, id)
	f.mu.Unlock()
}

// SetMaker adds or replaces a maker.
func (f *Fake) SetMaker(m Maker) { f.mu.Lock(); f.makers[m.ID] = m; f.mu.Unlock() }

// Counters returns the request counters.
func (f *Fake) Counters() (list, thumbs int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Requests, f.Thumbs
}

// JPEG returns a tiny valid JPEG.
func JPEG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 16, 9))
	for x := 0; x < 16; x++ {
		for y := 0; y < 9; y++ {
			img.Set(x, y, color.RGBA{uint8(x * 16), uint8(y * 28), 160, 255})
		}
	}
	var b bytes.Buffer
	_ = jpeg.Encode(&b, img, nil)
	return b.Bytes()
}

func intParam(q string, def int) int {
	n, err := strconv.Atoi(q)
	if err != nil || n < 0 {
		return def
	}
	return n
}

// ServeHTTP implements the contract.
func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.Key != "" && r.Header.Get("X-Internal-Key") != f.Key {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case path == "/v1/courses":
		f.Requests++
		f.listCourses(w, r)
	case strings.HasPrefix(path, "/v1/courses/") && strings.HasSuffix(path, "/thumbnail"):
		c, ok := f.courses[strings.TrimSuffix(strings.TrimPrefix(path, "/v1/courses/"), "/thumbnail")]
		if !ok || !c.HasThumbnail {
			http.NotFound(w, r)
			return
		}
		f.Thumbs++
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(JPEG())
	case strings.HasPrefix(path, "/v1/courses/"):
		c, ok := f.courses[strings.TrimPrefix(path, "/v1/courses/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, c)
	case path == "/v1/makers":
		ms := make([]Maker, 0, len(f.makers))
		for _, m := range f.makers {
			ms = append(ms, m)
		}
		sort.Slice(ms, func(i, j int) bool { return ms[i].ID < ms[j].ID })
		lim, off := min(intParam(r.URL.Query().Get("limit"), 50), 200), intParam(r.URL.Query().Get("offset"), 0)
		writeJSON(w, map[string]any{"total": len(ms), "makers": window(ms, off, lim)})
	case strings.HasPrefix(path, "/v1/makers/"):
		m, ok := f.makers[strings.TrimPrefix(path, "/v1/makers/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, m)
	default:
		http.NotFound(w, r)
	}
}

func window[T any](s []T, off, lim int) []T {
	off = min(off, len(s))
	return s[off:min(off+lim, len(s))]
}

func (f *Fake) listCourses(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	since, _ := time.Parse(time.RFC3339, q.Get("updated_since"))
	text := strings.ToLower(q.Get("q"))
	cs := []Course{}
	for _, c := range f.courses {
		if m := q.Get("maker"); m != "" && c.MakerID != m {
			continue
		}
		if text != "" && !strings.Contains(strings.ToLower(c.Name+" "+c.Description+" "+strings.Join(c.Tags, " ")), text) {
			continue
		}
		if u, err := time.Parse(time.RFC3339, c.Updated); !since.IsZero() && err == nil && u.Before(since) {
			continue
		}
		cs = append(cs, c)
	}
	sort.Slice(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		switch q.Get("sort") {
		case "stars":
			if a.Stars != b.Stars {
				return a.Stars > b.Stars
			}
		case "plays":
			if a.Plays != b.Plays {
				return a.Plays > b.Plays
			}
		}
		if a.Created != b.Created {
			return a.Created > b.Created
		}
		return a.ID < b.ID
	})
	lim, off := min(intParam(q.Get("limit"), 50), 200), intParam(q.Get("offset"), 0)
	writeJSON(w, map[string]any{"total": len(cs), "courses": window(cs, off, lim)})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
