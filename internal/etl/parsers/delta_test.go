package parsers

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"cloudmilesscouter/internal/storage"
)

func loadDeltaSample(t *testing.T) storage.RawScrape {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "samples", "delta_dfw_jfk_results.json")
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}
	return storage.RawScrape{
		Airline:     "delta",
		Origin:      "DFW",
		Destination: "JFK",
		SearchDate:  time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC),
		ScrapedAt:   time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC),
		RawPayload:  string(payload),
	}
}

func TestDeltaParse(t *testing.T) {
	awards, err := Delta{}.Parse(loadDeltaSample(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// 6 flights in the fixture, 3 available cabins each => 18 awards.
	if len(awards) != 18 {
		t.Fatalf("award count = %d, want 18", len(awards))
	}

	cabinCount := map[string]int{}
	for _, a := range awards {
		if a.AirlineCode != "delta" || a.AirlineName != "Delta Air Lines" {
			t.Errorf("bad airline fields: %+v", a)
		}
		if a.Origin != "DFW" || a.Destination != "JFK" {
			t.Errorf("bad searched route: %s -> %s", a.Origin, a.Destination)
		}
		if a.PointsCost <= 0 || a.TaxesFees <= 0 {
			t.Errorf("non-positive points/taxes: %+v", a)
		}
		if a.Currency != "USD" || a.AwardType != "Dynamic" {
			t.Errorf("currency/awardType = %q/%q", a.Currency, a.AwardType)
		}
		if !a.ArriveTime.After(a.DepartTime) {
			t.Errorf("arrive not after depart: %s .. %s (%s)", a.DepartTime, a.ArriveTime, a.FlightNumber)
		}
		cabinCount[a.Cabin]++
	}
	if cabinCount["economy"] != 6 || cabinCount["premium_economy"] != 6 || cabinCount["first"] != 6 {
		t.Errorf("cabin split = %v, want 6/6/6", cabinCount)
	}

	find := func(flight, cabin string) *storage.NormalizedAward {
		for i := range awards {
			if awards[i].FlightNumber == flight && awards[i].Cabin == cabin {
				return &awards[i]
			}
		}
		return nil
	}

	nonstop := find("DL454", "economy")
	if nonstop == nil {
		t.Fatal("missing DL454 economy")
	}
	if nonstop.PointsCost != 22600 || nonstop.TaxesFees != 6 {
		t.Errorf("DL454 economy = %d + %.2f, want 22600 + 6.00", nonstop.PointsCost, nonstop.TaxesFees)
	}
	if nonstop.Stops != 0 || nonstop.DurationMinutes != 209 {
		t.Errorf("DL454 stops/dur = %d/%d, want 0/209", nonstop.Stops, nonstop.DurationMinutes)
	}
	if got := nonstop.DepartTime.Format("2006-01-02 15:04"); got != "2026-11-22 07:35" {
		t.Errorf("DL454 depart = %q, want 2026-11-22 07:35", got)
	}
	if got := nonstop.ArriveTime.Format("2006-01-02 15:04"); got != "2026-11-22 12:04" {
		t.Errorf("DL454 arrive = %q, want 2026-11-22 12:04", got)
	}

	// DAL-origin "nearby airport" result: searched route stays DFW->JFK, flight
	// origin is DAL.
	nearby := find("DL516", "first")
	if nearby == nil {
		t.Fatal("missing DL516 first")
	}
	if nearby.FlightOrigin != "DAL" || nearby.FlightDestination != "JFK" {
		t.Errorf("DL516 flight route = %s -> %s, want DAL -> JFK", nearby.FlightOrigin, nearby.FlightDestination)
	}
	if nearby.Origin != "DFW" {
		t.Errorf("DL516 searched origin = %s, want DFW", nearby.Origin)
	}

	// Red-eye DL2143 departs 5:20pm, arrives 12:59am the next day.
	redeye := find("DL2143", "economy")
	if redeye == nil {
		t.Fatal("missing DL2143 economy")
	}
	if got := redeye.ArriveTime.Format("2006-01-02 15:04"); got != "2026-11-23 00:59" {
		t.Errorf("DL2143 arrive = %q, want 2026-11-23 00:59 (next day)", got)
	}
	if redeye.Stops != 1 {
		t.Errorf("DL2143 stops = %d, want 1", redeye.Stops)
	}
}

func TestDeltaParseUnavailableAndEmpty(t *testing.T) {
	raw := storage.RawScrape{
		Airline:    "delta",
		SearchDate: time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC),
		RawPayload: `{"route":{"origin":"DFW","destination":"JFK"},"flights":[
			{"flightNumbers":["DL999"],"depart":"8:00 am","arrive":"11:00 am","origin":"DFW","destination":"JFK","stops":0,"durationMinutes":180,"fares":[
				{"cabin":"Main","available":true,"miles":20000,"taxUSD":5.6},
				{"cabin":"Comfort","available":false,"miles":0,"taxUSD":0},
				{"cabin":"First","available":true,"miles":90000,"taxUSD":5.6}
			]}
		]}`,
	}
	awards, err := Delta{}.Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(awards) != 2 {
		t.Fatalf("award count = %d, want 2 (unavailable Comfort skipped)", len(awards))
	}

	empty, err := Delta{}.Parse(storage.RawScrape{RawPayload: `{"route":{"origin":"DFW","destination":"JFK"},"flights":[]}`})
	if err != nil {
		t.Fatalf("Parse empty: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty award count = %d, want 0", len(empty))
	}
}
