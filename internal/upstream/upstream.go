// Package upstream is the client for the read API both Mario Maker titles
// implement (docs/api-contract.md).
package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/openpak/mario-maker/internal/model"
)

// PageSize is the largest page the contract allows.
const PageSize = 200

// maxThumb bounds a proxied thumbnail.
const maxThumb = 8 << 20

// ErrNotFound is returned for an upstream 404.
var ErrNotFound = errors.New("upstream: not found")

// Client talks to one title's read API.
type Client struct {
	Game string
	Base string
	Key  string
	HTTP *http.Client
}

// New builds a client; base may carry a trailing slash.
func New(game, base, key string) *Client {
	return &Client{Game: game, Base: strings.TrimRight(base, "/"), Key: key, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

type wireCourse struct {
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

type wireMaker struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Courses int    `json:"courses"`
	Stars   int    `json:"stars"`
	Plays   int    `json:"plays"`
	Created string `json:"created"`
}

func parseTime(s string) time.Time {
	for _, l := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05"} {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}

func (w wireCourse) model(game string) model.Course {
	return model.Course{
		Game: game, ID: w.ID, DataID: strconv.FormatUint(w.DataID, 10), Name: w.Name, Description: w.Description,
		MakerID: w.MakerID, MakerName: w.MakerName, Style: w.Style, Theme: w.Theme, Difficulty: w.Difficulty,
		Tags: w.Tags, Created: parseTime(w.Created), Updated: parseTime(w.Updated),
		Stars: w.Stars, Plays: w.Plays, Clears: w.Clears, Attempts: w.Attempts, HasThumbnail: w.HasThumbnail,
	}
}

func (w wireMaker) model(game string) model.Maker {
	return model.Maker{Game: game, ID: w.ID, Name: w.Name, Courses: w.Courses, Stars: w.Stars, Plays: w.Plays, Created: parseTime(w.Created)}
}

func (c *Client) get(ctx context.Context, path string, q url.Values) (*http.Response, error) {
	u := c.Base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if c.Key != "" {
		req.Header.Set("X-Internal-Key", c.Key)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("upstream %s%s: status %d", c.Game, path, resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) json(ctx context.Context, path string, q url.Values, out any) error {
	resp, err := c.get(ctx, path, q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(io.LimitReader(resp.Body, 64<<20)).Decode(out)
}

// Courses lists one page. updatedSince is zero for no filter.
func (c *Client) Courses(ctx context.Context, limit, offset int, updatedSince time.Time) ([]model.Course, int, error) {
	q := url.Values{"sort": {"newest"}, "limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	if !updatedSince.IsZero() {
		q.Set("updated_since", updatedSince.UTC().Format(time.RFC3339))
	}
	var body struct {
		Total   int          `json:"total"`
		Courses []wireCourse `json:"courses"`
	}
	if err := c.json(ctx, "/v1/courses", q, &body); err != nil {
		return nil, 0, err
	}
	out := make([]model.Course, 0, len(body.Courses))
	for _, w := range body.Courses {
		if w.ID != "" {
			out = append(out, w.model(c.Game))
		}
	}
	return out, body.Total, nil
}

// Course fetches one course; ErrNotFound when it is gone.
func (c *Client) Course(ctx context.Context, id string) (model.Course, error) {
	var w wireCourse
	if err := c.json(ctx, "/v1/courses/"+url.PathEscape(id), nil, &w); err != nil {
		return model.Course{}, err
	}
	if w.ID == "" {
		w.ID = id
	}
	return w.model(c.Game), nil
}

// Thumbnail fetches a course's JPEG; ErrNotFound when it has none.
func (c *Client) Thumbnail(ctx context.Context, id string) ([]byte, error) {
	resp, err := c.get(ctx, "/v1/courses/"+url.PathEscape(id)+"/thumbnail", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxThumb+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxThumb {
		return nil, fmt.Errorf("upstream %s: thumbnail too large", c.Game)
	}
	return b, nil
}

// Makers lists one page of makers.
func (c *Client) Makers(ctx context.Context, limit, offset int) ([]model.Maker, int, error) {
	q := url.Values{"limit": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}
	var body struct {
		Total  int         `json:"total"`
		Makers []wireMaker `json:"makers"`
	}
	if err := c.json(ctx, "/v1/makers", q, &body); err != nil {
		return nil, 0, err
	}
	out := make([]model.Maker, 0, len(body.Makers))
	for _, w := range body.Makers {
		if w.ID != "" {
			out = append(out, w.model(c.Game))
		}
	}
	return out, body.Total, nil
}
