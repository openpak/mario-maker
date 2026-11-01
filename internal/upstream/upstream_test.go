package upstream_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openpak/mario-maker/internal/fakeupstream"
	"github.com/openpak/mario-maker/internal/upstream"
)

func TestClient(t *testing.T) {
	f := fakeupstream.New("k3y")
	for i := 0; i < 5; i++ {
		f.Set(fakeupstream.Course{ID: fmt.Sprintf("C%d", i), Name: "n", Created: time.Now().Format(time.RFC3339), Updated: "", DataID: 1 << 63, HasThumbnail: i == 0})
	}
	f.SetMaker(fakeupstream.Maker{ID: "M1", Name: "Mia", Created: "not a time"})
	ts := httptest.NewServer(f)
	defer ts.Close()
	ctx := context.Background()

	bad := upstream.New("smm1", ts.URL, "wrong")
	if _, _, err := bad.Courses(ctx, 10, 0, time.Time{}); err == nil {
		t.Fatal("wrong key accepted")
	}
	c := upstream.New("smm1", ts.URL+"/", "k3y")
	cs, total, err := c.Courses(ctx, 2, 2, time.Time{})
	if err != nil || total != 5 || len(cs) != 2 || cs[0].Game != "smm1" {
		t.Fatalf("page: %v total=%d n=%d", err, total, len(cs))
	}
	if cs[0].DataID != "9223372036854775808" || !cs[0].Updated.IsZero() {
		t.Fatalf("lenient parse: %+v", cs[0])
	}
	if _, err := c.Course(ctx, "nope"); !errors.Is(err, upstream.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if b, err := c.Thumbnail(ctx, "C0"); err != nil || len(b) < 100 {
		t.Fatalf("thumb: %v %d", err, len(b))
	}
	if _, err := c.Thumbnail(ctx, "C1"); !errors.Is(err, upstream.ErrNotFound) {
		t.Fatalf("want ErrNotFound for no thumbnail, got %v", err)
	}
	ms, _, err := c.Makers(ctx, 10, 0)
	if err != nil || len(ms) != 1 || !ms[0].Created.IsZero() {
		t.Fatalf("makers: %v %+v", err, ms)
	}
}
