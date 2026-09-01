package airlines

import (
	"strings"
	"testing"
	"time"

	"cloudmilesscouter/internal/scraper"
)

func TestUnitedAirportName(t *testing.T) {
	cases := map[string]string{
		"DFW":                           "Dallas, TX, US (DFW)",
		"dfw":                           "Dallas, TX, US (DFW)",
		" nyc ":                         "New York, NY, US (All Airports)",
		"XXX":                           "XXX",                           // unknown code: passed through
		"Dallas, TX, US (ALL AIRPORTS)": "Dallas, TX, US (ALL AIRPORTS)", // already a display string: passed through
	}
	for in, want := range cases {
		if got := unitedAirportName(in); got != want {
			t.Errorf("unitedAirportName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildChooseFlightsURL(t *testing.T) {
	url := BuildChooseFlightsURL(scraper.SearchParams{
		Origin:      "DFW",
		Destination: "NYC",
		Date:        time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC),
	})

	if !strings.HasPrefix(url, chooseFlightsBase+"?") {
		t.Fatalf("unexpected base: %s", url)
	}
	// IATA codes resolved to display strings; spaces as %20 and parens left
	// literal (the captured working pattern), commas still %2C-encoded.
	if !strings.Contains(url, "f=Dallas%2C%20TX%2C%20US%20(DFW)") {
		t.Errorf("origin not resolved/encoded as expected: %s", url)
	}
	if !strings.Contains(url, "t=New%20York%2C%20NY%2C%20US%20(All%20Airports)") {
		t.Errorf("destination not resolved/encoded as expected: %s", url)
	}
	if !strings.Contains(url, "d=2026-11-20") {
		t.Errorf("missing date: %s", url)
	}
	if strings.ContainsAny(url, "+") || strings.Contains(url, "%28") || strings.Contains(url, "%29") {
		t.Errorf("url still has +, %%28 or %%29: %s", url)
	}
}
