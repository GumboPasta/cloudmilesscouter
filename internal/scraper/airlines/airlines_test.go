package airlines

import "testing"

func TestHasResultsFor(t *testing.T) {
	tests := []struct {
		name    string
		airline string
		body    string
		want    bool
	}{
		{"united empty", "united", `{"data":{"Trips":[{"Flights":[]}]}}`, false},
		{"united has flights", "united", `{"data":{"Trips":[{"Flights":[{}]}]}}`, true},
		{"american empty", "american", `{"SearchData":{"itineraryResult":{"error":"No available flights","slices":[]}}}`, false},
		{"american has slices", "american", `{"SearchData":{"itineraryResult":{"slices":[{}]}}}`, true},
		{"delta empty", "delta", `{"flights":[]}`, false},
		{"delta has flights", "delta", `{"flights":[{}]}`, true},
		{"alaska empty", "alaska", `{"flights":[]}`, false},
		{"alaska has flights", "alaska", `{"flights":[{}]}`, true},
		{"unknown airline assumed non-empty", "southwest", `{}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := HasResultsFor(tt.airline, []byte(tt.body))
			if err != nil {
				t.Fatalf("HasResultsFor: %v", err)
			}
			if got != tt.want {
				t.Errorf("HasResultsFor(%q) = %v, want %v", tt.airline, got, tt.want)
			}
		})
	}
}

func TestHasResultsForBadJSON(t *testing.T) {
	if _, err := HasResultsFor("united", []byte("not json")); err == nil {
		t.Error("want error on malformed body, got nil")
	}
	// An unknown airline never inspects the body, so malformed JSON is fine.
	if ok, err := HasResultsFor("southwest", []byte("not json")); err != nil || !ok {
		t.Errorf("unknown airline: got (%v, %v), want (true, nil)", ok, err)
	}
}
