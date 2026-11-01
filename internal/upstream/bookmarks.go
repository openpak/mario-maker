package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/openpak/mario-maker/internal/model"
)

// Bookmarks (docs/api-contract.md, "Bookmarks"): the title keeps one list per maker. Only the
// Super Mario Maker (Wii U) title implements them; any other upstream answers 404.

// ErrUnknownMaker means the title has no maker by that NNID (the player has not started the game
// on OpenPak yet).
var ErrUnknownMaker = fmt.Errorf("upstream: unknown maker")

type bookmarkBody struct {
	NNID   string `json:"nnid"`
	Course string `json:"course"`
}

func (c *Client) bookmarkCall(ctx context.Context, method, nnid, course string) error {
	b, _ := json.Marshal(bookmarkBody{NNID: nnid, Course: course})
	req, err := http.NewRequestWithContext(ctx, method, c.Base+"/v1/bookmarks", bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Key != "" {
		req.Header.Set("X-Internal-Key", c.Key)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusNotFound:
		// * Either the course or the maker is unknown; the title says which in its error text, but
		// * the site only needs to tell the player to start the game first.
		return ErrUnknownMaker
	}
	return fmt.Errorf("upstream %s bookmarks: status %d", c.Game, resp.StatusCode)
}

// Bookmark adds a course to a maker's in-game bookmark list. Adding twice is fine.
func (c *Client) Bookmark(ctx context.Context, nnid, course string) error {
	return c.bookmarkCall(ctx, http.MethodPut, nnid, course)
}

// Unbookmark removes a course from the list.
func (c *Client) Unbookmark(ctx context.Context, nnid, course string) error {
	return c.bookmarkCall(ctx, http.MethodDelete, nnid, course)
}

// Bookmarks lists a maker's bookmarked courses, newest bookmark first. A maker the title does
// not know has no bookmarks: the answer is empty, not an error.
func (c *Client) Bookmarks(ctx context.Context, nnid string) ([]model.Course, error) {
	resp, err := c.get(ctx, "/v1/bookmarks", url.Values{"nnid": {nnid}})
	if err == ErrNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var body struct {
		Courses []wireCourse `json:"courses"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]model.Course, 0, len(body.Courses))
	for _, w := range body.Courses {
		if w.ID != "" {
			out = append(out, w.model(c.Game))
		}
	}
	return out, nil
}
