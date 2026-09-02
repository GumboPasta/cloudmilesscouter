//go:build browser

// DOM-extractor tests for the two airlines whose results page has no JSON
// payload (Delta, Alaska): load a hand-built minimal grid fixture into a real
// headless Chromium via page.SetContent, run the actual *ExtractJS string, and
// check the structured output. These are the only coverage the in-browser
// extractors have.
//
//	playwright install chromium   # one-time
//	go test -tags browser ./internal/scraper/airlines
package airlines

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/playwright-community/playwright-go"

	"cloudmilesscouter/internal/etl/parsers"
	"cloudmilesscouter/internal/storage"
)

func evalExtractor(t *testing.T, fixtureFile, extractJS string) []byte {
	t.Helper()
	html, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "samples", fixtureFile))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	pw, err := playwright.Run()
	if err != nil {
		t.Skipf("playwright not available (run `playwright install chromium`): %v", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	if err != nil {
		t.Fatalf("launch chromium: %v", err)
	}
	defer browser.Close()

	page, err := browser.NewPage()
	if err != nil {
		t.Fatalf("new page: %v", err)
	}
	if err := page.SetContent(string(html)); err != nil {
		t.Fatalf("set content: %v", err)
	}

	res, err := page.Evaluate(extractJS)
	if err != nil {
		t.Fatalf("evaluate extractor: %v", err)
	}
	s, ok := res.(string)
	if !ok {
		t.Fatalf("extractor returned %T, want string", res)
	}
	return []byte(s)
}

func TestDeltaExtractJS(t *testing.T) {
	body := evalExtractor(t, "delta_grid_fixture.html", DeltaExtractJS)

	var out struct {
		Route    *struct{ Origin, Destination string } `json:"route"`
		DateText string                                `json:"dateText"`
		Flights  []struct {
			FlightNumbers   []string `json:"flightNumbers"`
			Depart          string   `json:"depart"`
			Stops           int      `json:"stops"`
			DurationMinutes int      `json:"durationMinutes"`
			Fares           []struct {
				Cabin     string  `json:"cabin"`
				Available bool    `json:"available"`
				Miles     int     `json:"miles"`
				TaxUSD    float64 `json:"taxUSD"`
			} `json:"fares"`
		} `json:"flights"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal extractor output: %v\n%s", err, body)
	}

	if out.Route == nil || out.Route.Origin != "JFK" || out.Route.Destination != "LAX" {
		t.Fatalf("route = %+v, want JFK -> LAX", out.Route)
	}
	if len(out.Flights) != 2 {
		t.Fatalf("flight count = %d, want 2", len(out.Flights))
	}

	f0 := out.Flights[0]
	if len(f0.FlightNumbers) != 1 || f0.FlightNumbers[0] != "DL742" {
		t.Errorf("flight 0 numbers = %v, want [DL742]", f0.FlightNumbers)
	}
	if f0.Stops != 0 || f0.DurationMinutes != 375 || f0.Depart != "7:00 am" {
		t.Errorf("flight 0 = stops %d, dur %d, depart %q", f0.Stops, f0.DurationMinutes, f0.Depart)
	}
	wantCabins0 := []string{"Delta Main", "Delta Comfort Classic", "Delta Premium Select Classic", "Delta One® Classic"}
	wantMiles0 := []int{46700, 55100, 102600, 147200}
	for i, fare := range f0.Fares {
		if fare.Cabin != wantCabins0[i] || !fare.Available || fare.Miles != wantMiles0[i] {
			t.Errorf("flight 0 fare %d = %+v, want cabin %q miles %d available", i, fare, wantCabins0[i], wantMiles0[i])
		}
	}

	// Row 1's third column is First, not Premium Select — the per-row swap the
	// old positional ['Main','Comfort','First'] mapping mislabeled.
	f1 := out.Flights[1]
	if len(f1.Fares) != 4 {
		t.Fatalf("flight 1 fare count = %d, want 4", len(f1.Fares))
	}
	if f1.Fares[2].Cabin != "Delta First Classic" || f1.Fares[2].Miles != 120400 {
		t.Errorf("flight 1 fare 2 = %+v, want Delta First Classic / 120400", f1.Fares[2])
	}
	if f1.Fares[3].Available {
		t.Errorf("flight 1 fare 3 (Delta One, Sold Out) should be unavailable: %+v", f1.Fares[3])
	}

	// End to end through the parser: the swap must land row 1's premium fare in
	// `first`, and both premium columns of row 0 in `premium_economy`.
	awards, err := parsers.Delta{}.Parse(storage.RawScrape{
		Airline: "delta", Origin: "JFK", Destination: "LAX",
		SearchDate: time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC), RawPayload: string(body),
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cabinCount := map[string]int{}
	for _, a := range awards {
		cabinCount[a.Cabin]++
	}
	// row0: economy, premium_economy (Comfort), premium_economy (Premium Select), business
	// row1: economy, premium_economy (Comfort), first  (Delta One sold out -> skipped)
	if cabinCount["first"] != 1 || cabinCount["business"] != 1 || cabinCount["premium_economy"] != 3 || cabinCount["economy"] != 2 {
		t.Errorf("cabin split = %v, want economy 2 / premium_economy 3 / first 1 / business 1", cabinCount)
	}
}

func TestAlaskaExtractJS(t *testing.T) {
	body := evalExtractor(t, "alaska_grid_fixture.html", AlaskaExtractJS)

	var out struct {
		Route   *struct{ Origin, Destination string } `json:"route"`
		Flights []struct {
			FlightNumber    string   `json:"flightNumber"`
			Via             []string `json:"via"`
			Stops           int      `json:"stops"`
			DurationMinutes int      `json:"durationMinutes"`
			ArrivesNextDay  int      `json:"arrivesNextDay"`
			Fares           []struct {
				Cabin  string  `json:"cabin"`
				Miles  int     `json:"miles"`
				TaxUSD float64 `json:"taxUSD"`
			} `json:"fares"`
		} `json:"flights"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("unmarshal extractor output: %v\n%s", err, body)
	}

	if out.Route == nil || out.Route.Origin != "SEA" || out.Route.Destination != "SAN" {
		t.Fatalf("route = %+v, want SEA -> SAN", out.Route)
	}
	if len(out.Flights) != 2 {
		t.Fatalf("flight count = %d, want 2", len(out.Flights))
	}

	f0 := out.Flights[0]
	if f0.FlightNumber != "AS 305" || f0.Stops != 0 || len(f0.Via) != 0 || f0.DurationMinutes != 168 || f0.ArrivesNextDay != 0 {
		t.Errorf("flight 0 = %+v", f0)
	}
	if len(f0.Fares) != 2 || f0.Fares[0].Cabin != "Main" || f0.Fares[0].Miles != 17500 ||
		f0.Fares[1].Cabin != "First" || f0.Fares[1].Miles != 32500 {
		t.Errorf("flight 0 fares = %+v", f0.Fares)
	}

	// Connecting itinerary: stop count from the data-testid, via airport from
	// the [aria-label*="flight path"] widget.
	f1 := out.Flights[1]
	if f1.Stops != 1 || len(f1.Via) != 1 || f1.Via[0] != "PDX" || f1.ArrivesNextDay != 1 || f1.DurationMinutes != 482 {
		t.Errorf("flight 1 = %+v, want stops 1 / via [PDX] / nextDay 1 / dur 482", f1)
	}
	if len(f1.Fares) != 1 || f1.Fares[0].Miles != 32500 || f1.Fares[0].TaxUSD != 12 {
		t.Errorf("flight 1 fares = %+v", f1.Fares)
	}
}
