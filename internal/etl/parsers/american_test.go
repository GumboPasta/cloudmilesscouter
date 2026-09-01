package parsers

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cloudmilesscouter/internal/storage"
)

func loadAmericanSample(t *testing.T) storage.RawScrape {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "samples", "american_dfw_jfk_results.json")
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}
	return storage.RawScrape{
		Airline:     "american",
		Origin:      "DFW",
		Destination: "JFK",
		SearchDate:  time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC),
		ScrapedAt:   time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC),
		RawPayload:  string(payload),
	}
}

func TestAmericanParse(t *testing.T) {
	awards, err := American{}.Parse(loadAmericanSample(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// 40 slices in the fixture, each with up to 3 cabins; sold-out cabins are
	// skipped, leaving 76 distinct flight/cabin awards.
	if len(awards) != 76 {
		t.Fatalf("award count = %d, want 76", len(awards))
	}

	cabins := map[string]bool{"economy": true, "premium_economy": true, "business": true, "first": true}
	awardTypes := map[string]int{}
	for _, a := range awards {
		if a.AirlineCode != "american" || a.AirlineName != "American Airlines" {
			t.Errorf("bad airline fields: %+v", a)
		}
		if a.Origin != "DFW" || a.Destination != "JFK" {
			t.Errorf("bad searched route: %s -> %s", a.Origin, a.Destination)
		}
		if !cabins[a.Cabin] {
			t.Errorf("unexpected cabin %q", a.Cabin)
		}
		if a.PointsCost <= 0 {
			t.Errorf("non-positive points on available award: %+v", a)
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
		awardTypes[a.AwardType]++
	}

	if awardTypes["Dynamic"] == 0 || awardTypes["Standard"] == 0 {
		t.Errorf("award type split = %v, want both Dynamic and Standard present", awardTypes)
	}
	if awardTypes["Dynamic"]+awardTypes["Standard"] != 76 {
		t.Errorf("award types other than Dynamic/Standard present: %v", awardTypes)
	}

	find := func(flight, cabin string) *storage.NormalizedAward {
		for i := range awards {
			if awards[i].FlightNumber == flight && awards[i].Cabin == cabin {
				return &awards[i]
			}
		}
		return nil
	}

	// Sold-out cabins must not surface (AA1766 business, AA859 first).
	if find("AA1766", "business") != nil {
		t.Error("AA1766 business is sold out in the fixture but was emitted")
	}
	if find("AA859", "first") != nil {
		t.Error("AA859 first is sold out in the fixture but was emitted")
	}

	nonstop := find("AA1766", "economy")
	if nonstop == nil {
		t.Fatal("missing AA1766 economy award")
	}
	if nonstop.PointsCost != 33000 || nonstop.TaxesFees != 5.6 {
		t.Errorf("AA1766 economy = %d pts + %.2f, want 33000 + 5.60", nonstop.PointsCost, nonstop.TaxesFees)
	}
	if nonstop.Stops != 0 || nonstop.DurationMinutes != 215 {
		t.Errorf("AA1766 economy stops/dur = %d/%d, want 0/215", nonstop.Stops, nonstop.DurationMinutes)
	}
	if got := nonstop.DepartTime.Format("2006-01-02 15:04"); got != "2026-11-20 08:25" {
		t.Errorf("AA1766 depart = %q, want 2026-11-20 08:25 (local wall time)", got)
	}
	if nonstop.AwardType != "Dynamic" {
		t.Errorf("AA1766 economy award type = %q, want Dynamic", nonstop.AwardType)
	}

	connecting := find("AA855", "first")
	if connecting == nil {
		t.Fatal("missing AA855 first award")
	}
	if connecting.Stops < 1 {
		t.Errorf("AA855 first stops = %d, want >= 1", connecting.Stops)
	}
	if connecting.FlightOrigin != "DFW" || connecting.FlightDestination != "JFK" {
		t.Errorf("AA855 flight route = %s -> %s, want DFW -> JFK", connecting.FlightOrigin, connecting.FlightDestination)
	}

	business := find("AA859", "business")
	if business == nil {
		t.Fatal("missing AA859 business award")
	}
	if business.PointsCost != 150000 {
		t.Errorf("AA859 business = %d pts, want 150000", business.PointsCost)
	}
}

func TestAmericanParseEmpty(t *testing.T) {
	raw := storage.RawScrape{
		Airline:    "american",
		RawPayload: `{"SearchData":{"itineraryResult":{"error":"No available flights","slices":[]}}}`,
	}
	awards, err := American{}.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(awards) != 0 {
		t.Fatalf("award count = %d, want 0", len(awards))
	}
}
