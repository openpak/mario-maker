// Command fake-upstream serves a small catalogue over the upstream read contract,
// for running the site locally:
//
//	go run ./examples/fake-upstream -addr :20170 -game smm1
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/openpak/mario-maker/internal/fakeupstream"
)

func main() {
	addr := flag.String("addr", ":20170", "listen address")
	game := flag.String("game", "smm1", "smm1 or smm2")
	key := flag.String("key", "", "required X-Internal-Key (empty: none)")
	n := flag.Int("n", 30, "number of courses")
	flag.Parse()

	f := fakeupstream.New(*key)
	now := time.Now().UTC()
	names := []string{"Sky Ride", "Pipe Maze", "Koopa Castle", "Ghost Hall", "Underwater Dash", "Desert Run"}
	for i := 0; i < *n; i++ {
		id := fmt.Sprintf("%016X", 0xA0B0C0D000000000+uint64(i)*7919)
		if *game == "smm2" {
			id = fmt.Sprintf("%03X-%03X-%03X", 0x100+i, 0x200+i*3, 0x300+i*5)
		}
		maker := fmt.Sprintf("MAKER%02d", i%4)
		f.Set(fakeupstream.Course{
			Game: *game, ID: id, DataID: uint64(1000 + i), Name: fmt.Sprintf("%s %d", names[i%len(names)], i),
			Description: "A sample course.", MakerID: maker, MakerName: "Maker " + maker[5:],
			Style: "smb1", Theme: "overworld", Difficulty: "normal", Tags: []string{"speedrun", "standard"},
			Created: now.Add(-time.Duration(i) * time.Hour).Format(time.RFC3339), Updated: now.Format(time.RFC3339),
			Stars: (i * 37) % 101, Plays: (i * 91) % 503, Clears: i, Attempts: i * 3, HasThumbnail: i%5 != 0,
		})
	}
	for i := 0; i < 4; i++ {
		f.SetMaker(fakeupstream.Maker{Game: *game, ID: fmt.Sprintf("MAKER%02d", i), Name: fmt.Sprintf("Maker %02d", i),
			Courses: *n / 4, Stars: 100 * (i + 1), Plays: 400 * (i + 1), Created: now.Add(-72 * time.Hour).Format(time.RFC3339)})
	}
	log.Printf("fake %s upstream on %s", *game, *addr)
	log.Fatal(http.ListenAndServe(*addr, f))
}
