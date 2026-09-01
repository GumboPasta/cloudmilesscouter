package parsers

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cloudmilesscouter/internal/storage"
)

func loadAlaskaSample(t *testing.T) storage.RawScrape {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "samples", "alaska_pdx_jfk_results.json")
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}
	return storage.RawScrape{
		Airline:     "alaska",
		Origin:      "PDX",
		Destination: "JFK",
		SearchDate:  time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC),
		ScrapedAt:   time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
		RawPayload:  string(payload),
	}
}

func TestAlaskaParse(t *testing.T) {
	awards, err := Alaska{}.Parse(loadAlaskaSample(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// 5 flights, 2 cabins each => 10 awards.
	if len(awards) != 10 {
		t.Fatalf("award count = %d, want 10", len(awards))
	}

	cabinCount := map[string]int{}
	for _, a := range awards {
		if a.AirlineCode != "alaska" || a.AirlineName != "Alaska Airlines" {
			t.Errorf("bad airline fields: %+v", a)
		}
		if a.Origin != "PDX" || a.Destination != "JFK" {
			t.Errorf("bad searched route: %s -> %s", a.Origin, a.Destination)
		}
		if a.PointsCost <= 0 || a.Currency != "USD" || a.AwardType != "dynamic" {
			t.Errorf("bad points/currency/awardType: %+v", a)
		}
		if !a.ArriveTime.After(a.DepartTime) {
			t.Errorf("arrive not after depart for %s: %s .. %s", a.FlightNumber, a.DepartTime, a.ArriveTime)
		}
		cabinCount[a.Cabin]++
	}
	if cabinCount["economy"] != 5 || cabinCount["first"] != 5 {
		t.Errorf("cabin split = %v, want 5/5", cabinCount)
	}

	find := func(flight, cabin string) *storage.NormalizedAward {
		for i := range awards {
			if awards[i].FlightNumber == flight && awards[i].Cabin == cabin {
				return &awards[i]
			}
		}
		return nil
	}

	nonstop := find("AS18", "economy")
	if nonstop == nil {
		t.Fatal("missing AS18 economy")
	}
	if nonstop.PointsCost != 32500 || nonstop.TaxesFees != 6 {
		t.Errorf("AS18 economy = %d + %.2f, want 32500 + 6.00", nonstop.PointsCost, nonstop.TaxesFees)
	}
	if nonstop.Stops != 0 || nonstop.DurationMinutes != 329 {
		t.Errorf("AS18 stops/dur = %d/%d, want 0/329", nonstop.Stops, nonstop.DurationMinutes)
	}
	if got := nonstop.DepartTime.Format("2006-01-02 15:04"); got != "2026-11-24 10:46" {
		t.Errorf("AS18 depart = %q, want 2026-11-24 10:46", got)
	}
	if got := nonstop.ArriveTime.Format("2006-01-02 15:04"); got != "2026-11-24 19:15" {
		t.Errorf("AS18 arrive = %q, want 2026-11-24 19:15", got)
	}
	if first := find("AS18", "first"); first == nil || first.PointsCost != 125000 {
		t.Errorf("AS18 first missing or wrong points")
	}

	// Connecting itineraries get a synthetic "AS <via>" number; two SAN
	// connections differ only by departure time.
	san := find("AS SAN", "economy")
	if san == nil {
		t.Fatal("missing AS SAN economy")
	}
	if san.Stops != 1 {
		t.Errorf("AS SAN stops = %d, want 1", san.Stops)
	}
	sanCount := 0
	for _, a := range awards {
		if a.FlightNumber == "AS SAN" {
			sanCount++
		}
	}
	if sanCount != 4 { // 2 flights x 2 cabins
		t.Errorf("AS SAN award count = %d, want 4", sanCount)
	}

	// Red-eye: departs 9:15am, arrives 6:36am the next day.
	for _, a := range awards {
		if a.FlightNumber == "AS SAN" && a.DepartTime.Format("15:04") == "09:15" {
			if got := a.ArriveTime.Format("2006-01-02 15:04"); got != "2026-11-25 06:36" {
				t.Errorf("AS SAN 09:15 arrive = %q, want 2026-11-25 06:36 (next day)", got)
			}
		}
	}
}

func TestAlaskaParseEmpty(t *testing.T) {
	awards, err := Alaska{}.Parse(storage.RawScrape{
		RawPayload: `{"route":{"origin":"PDX","destination":"JFK"},"flights":[]}`,
	})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(awards) != 0 {
		t.Fatalf("award count = %d, want 0", len(awards))
	}
}
