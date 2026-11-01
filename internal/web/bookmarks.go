package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"

	"github.com/openpak/mario-maker/internal/auth"
	"github.com/openpak/mario-maker/internal/model"
	"github.com/openpak/mario-maker/internal/store"
	"github.com/openpak/mario-maker/internal/upstream"
)

// Bookmarks: a signed-in player bookmarks a Super Mario Maker (Wii U) course from the site and it
// is added to that player's in-game bookmark list. The list lives in the title; the site only
// forwards (internal/upstream/bookmarks.go). It needs three things at once: sign-in (core
// session), a Wii U link (config.Link), and the title's API. Without any of them the course page
// just shows no bookmark control.

// bookmarkView is what the course page needs to draw the control.
type bookmarkView struct {
	// State: "" no control, "signin", "link", "ready", "saved".
	State string
	CSRF  string
}

const bookmarkGame = model.SMM1

func (s *Server) bookmarkEnabled() bool {
	return s.cfg.Link != nil && s.cfg.Upstream[bookmarkGame] != nil
}

// csrfToken ties a form to the session cookie it was rendered for.
func (s *Server) csrfToken(r *http.Request) string {
	ck, err := r.Cookie(s.cfg.SessionCookie)
	if err != nil || ck.Value == "" {
		return ""
	}
	m := hmac.New(sha256.New, s.csrfKey)
	m.Write([]byte("bookmark\x00" + ck.Value))
	return hex.EncodeToString(m.Sum(nil))
}

func (s *Server) csrfOK(r *http.Request) bool {
	want := s.csrfToken(r)
	got := r.PostFormValue("csrf")
	return want != "" && hmac.Equal([]byte(want), []byte(got))
}

func newCSRFKey(configured string) []byte {
	if configured != "" {
		return []byte(configured)
	}
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return b
}

// linkedNNID returns the NNID tied to the signed-in user, "" when there is none.
func (s *Server) linkedNNID(r *http.Request, u *auth.User) string {
	nnid, err := s.cfg.Link.NNID(r.Context(), u.ID)
	if err != nil {
		s.cfg.Log.Warn("link lookup", "err", err)
		return ""
	}
	return nnid
}

func (s *Server) bookmarkView(r *http.Request, c model.Course) bookmarkView {
	if c.Game != bookmarkGame || !s.bookmarkEnabled() || s.cfg.Auth == nil {
		return bookmarkView{}
	}
	u := s.cfg.Auth.User(r)
	if u == nil {
		return bookmarkView{State: "signin"}
	}
	nnid := s.linkedNNID(r, u)
	if nnid == "" {
		return bookmarkView{State: "link"}
	}
	v := bookmarkView{State: "ready", CSRF: s.csrfToken(r)}
	if list, err := s.cfg.Upstream[bookmarkGame].Bookmarks(r.Context(), nnid); err == nil {
		for _, b := range list {
			if b.ID == c.ID {
				v.State = "saved"
			}
		}
	}
	return v
}

func (s *Server) bookmarkRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /bookmark/{game}/{id}", func(w http.ResponseWriter, r *http.Request) { s.bookmarkWrite(w, r, true) })
	mux.HandleFunc("POST /unbookmark/{game}/{id}", func(w http.ResponseWriter, r *http.Request) { s.bookmarkWrite(w, r, false) })
	mux.HandleFunc("GET /bookmarks", s.bookmarkList)
}

func (s *Server) bookmarkWrite(w http.ResponseWriter, r *http.Request, add bool) {
	game, id := r.PathValue("game"), r.PathValue("id")
	if game != bookmarkGame || !idRe.MatchString(id) || !s.bookmarkEnabled() || s.cfg.Auth == nil {
		s.notFound(w, r, "That course")
		return
	}
	u := s.cfg.Auth.User(r)
	if u == nil || !s.csrfOK(r) {
		http.Error(w, "sign in again and retry", http.StatusForbidden)
		return
	}
	c, err := s.cfg.Store.Course(r.Context(), game, id)
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r, "That course")
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	nnid := s.linkedNNID(r, u)
	if nnid == "" {
		http.Redirect(w, r, "/course/"+game+"/"+url.PathEscape(c.ID), http.StatusSeeOther)
		return
	}
	cl := s.cfg.Upstream[game]
	if add {
		err = cl.Bookmark(r.Context(), nnid, c.ID)
	} else {
		err = cl.Unbookmark(r.Context(), nnid, c.ID)
	}
	switch {
	case errors.Is(err, upstream.ErrUnknownMaker):
		s.render(w, r, "error", http.StatusConflict, page{Title: "Start the game first",
			Data: "Super Mario Maker on your Wii U has not signed in to OpenPak with this Network ID yet. Play it once, then bookmark again."})
		return
	case err != nil:
		s.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/course/"+game+"/"+url.PathEscape(c.ID), http.StatusSeeOther)
}

type bookmarksData struct {
	Courses []model.Course
	State   string // "signin", "link", "ok"
}

func (s *Server) bookmarkList(w http.ResponseWriter, r *http.Request) {
	if !s.bookmarkEnabled() || s.cfg.Auth == nil {
		s.notFound(w, r, "That page")
		return
	}
	d := bookmarksData{State: "signin"}
	if u := s.cfg.Auth.User(r); u != nil {
		d.State = "link"
		if nnid := s.linkedNNID(r, u); nnid != "" {
			list, err := s.cfg.Upstream[bookmarkGame].Bookmarks(r.Context(), nnid)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			d.State, d.Courses = "ok", list
		}
	}
	s.render(w, r, "bookmarks", http.StatusOK, page{Title: "Your bookmarks", Desc: "Courses you bookmarked for Super Mario Maker on Wii U.", Data: d})
}
