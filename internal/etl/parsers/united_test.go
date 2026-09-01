package parsers

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cloudmilesscouter/internal/storage"
)

func loadUnitedSample(t *testing.T) storage.RawScrape {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "samples", "united_dfw_nyc_results.json")
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}
	return storage.RawScrape{
		Airline:     "united",
		Origin:      "DFW",
		Destination: "NYC",
		SearchDate:  time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC),
		ScrapedAt:   time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC),
		RawPayload:  string(payload),
	}
}

func TestUnitedParse(t *testing.T) {
	awards, err := United{}.Parse(loadUnitedSample(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// One raw document -> 142 rows (many itinerary/cabin/fare combinations per
	// search); the "-NOT-MIXED" duplicate products are deduped out.
	if len(awards) != 142 {
		t.Fatalf("award count = %d, want 142", len(awards))
	}

	cabins := map[string]bool{"economy": true, "premium_economy": true, "business": true, "first": true}
	awardType := map[string]bool{"saver": true, "standard": true, "dynamic": true}
	awardTypeCount := map[string]int{}
	for _, a := range awards {
		if a.AirlineCode != "united" || a.AirlineName != "United Airlines" {
			t.Errorf("bad airline fields: %+v", a)
		}
		if a.Origin != "DFW" || a.Destination != "NYC" {
			t.Errorf("bad searched route: %s -> %s (want the searched DFW -> NYC)", a.Origin, a.Destination)
		}
		if !cabins[a.Cabin] {
			t.Errorf("unexpected cabin %q", a.Cabin)
		}
		if !awardType[a.AwardType] {
			t.Errorf("unexpected award type %q, want one of saver/standard/dynamic", a.AwardType)
		}
		if a.PointsCost <= 0 {
			t.Errorf("non-positive points: %+v", a)
		}
		if a.Currency != "USD" {
			t.Errorf("currency = %q, want USD", a.Currency)
		}
		if a.DurationMinutes <= 0 || a.Stops < 0 {
			t.Errorf("bad duration/stops: %+v", a)
		}
		if a.ArriveTime.Before(a.DepartTime) {
			t.Errorf("arrive before depart: %+v", a)
		}
		awardTypeCount[a.AwardType]++
	}

	if awardTypeCount["saver"] == 0 || awardTypeCount["standard"] == 0 {
		t.Errorf("award type split = %v, want both saver and standard present", awardTypeCount)
	}
}
