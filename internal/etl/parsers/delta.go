package parsers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cloudmilesscouter/internal/storage"
)

// deltaClockLayout matches Delta's rendered times, e.g. "7:35 am", "12:04 pm".
const deltaClockLayout = "3:04 pm"

type deltaResponse struct {
	Flights []deltaFlight `json:"flights"`
}

type deltaFlight struct {
	FlightNumbers   []string    `json:"flightNumbers"`
	Depart          string      `json:"depart"`
	Arrive          string      `json:"arrive"`
	Origin          string      `json:"origin"`
	Destination     string      `json:"destination"`
	Stops           int         `json:"stops"`
	DurationMinutes int         `json:"durationMinutes"`
	Fares           []deltaFare `json:"fares"`
}

type deltaFare struct {
	Cabin     string  `json:"cabin"`
	Available bool    `json:"available"`
	Miles     int     `json:"miles"`
	TaxUSD    float64 `json:"taxUSD"`
}

// Delta parses the DOM-extracted payload produced by
// internal/scraper/airlines/delta.go (Delta's results page has no JSON API).
type Delta struct{}

// Parse emits one NormalizedAward per flight × available cabin. The extractor
// reads each fare column's brand label from the "Shop with Miles" grid
// (Main / Comfort / First, plus Premium Select and Delta One on premium
// routes); mapDeltaCabin maps it. Cabins marked unavailable are skipped.
func (Delta) Parse(raw storage.RawScrape) ([]storage.NormalizedAward, error) {
	var resp deltaResponse
	if err := json.Unmarshal([]byte(raw.RawPayload), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal delta response: %w", err)
	}

	seen := make(map[string]bool)
	var awards []storage.NormalizedAward

	for _, f := range resp.Flights {
		if len(f.FlightNumbers) == 0 {
			slog.Warn("skipping delta flight with no flight number")
			continue
		}

		departTime, ok := deltaClock(raw.SearchDate, f.Depart)
		if !ok {
			slog.Warn("skipping delta flight with unparseable depart time", "raw", f.Depart)
			continue
		}
		arriveTime, ok := deltaClock(raw.SearchDate, f.Arrive)
		if !ok {
			slog.Warn("skipping delta flight with unparseable arrival time", "raw", f.Arrive)
			continue
		}
		// Delta shows both times in local time with no date; an arrival earlier
		// in the day than the departure is the next day.
		if arriveTime.Before(departTime) {
			arriveTime = arriveTime.AddDate(0, 0, 1)
		}

		flightNumber := f.FlightNumbers[0]
		segmentPath := strings.Join(f.FlightNumbers, "-")

		for _, fare := range f.Fares {
			if !fare.Available || fare.Miles <= 0 {
				continue
			}
			cabin, ok := mapDeltaCabin(fare.Cabin)
			if !ok {
				slog.Warn("skipping delta fare with unrecognized cabin", "cabin", fare.Cabin)
				continue
			}

			key := fmt.Sprintf("%s|%s|%s|%d", segmentPath, f.Depart, cabin, fare.Miles)
			if seen[key] {
				continue
			}
			seen[key] = true

			awards = append(awards, storage.NormalizedAward{
				AirlineCode:       "delta",
				AirlineName:       "Delta Air Lines",
				Origin:            raw.Origin,
				Destination:       raw.Destination,
				SearchDate:        raw.SearchDate,
				ScrapedAt:         raw.ScrapedAt,
				Cabin:             cabin,
				AwardType:         "dynamic", // Delta SkyMiles has no saver/chart tier
				Currency:          "USD",
				FlightNumber:      flightNumber,
				FlightOrigin:      f.Origin,
				FlightDestination: f.Destination,
				DepartTime:        departTime,
				ArriveTime:        arriveTime,
				DurationMinutes:   f.DurationMinutes,
				Stops:             f.Stops,
				PointsCost:        fare.Miles,
				TaxesFees:         fare.TaxUSD,
			})
		}
	}

	return awards, nil
}

// deltaClock combines the search date with a "7:35 am" style time, returning a
// timezone-less wall-clock timestamp (matching how United/American times are
// stored).
func deltaClock(date time.Time, clock string) (time.Time, bool) {
	t, err := time.Parse(deltaClockLayout, strings.ToLower(strings.TrimSpace(clock)))
	if err != nil {
		return time.Time{}, false
	}
	return time.Date(date.Year(), date.Month(), date.Day(), t.Hour(), t.Minute(), 0, 0, time.UTC), true
}

// mapDeltaCabin maps a Delta fare-column brand label to a cabins table name.
// The label is what the extractor reads off the results-grid column header:
// "Delta Main", "Delta Comfort Classic", "Delta Premium Select Classic",
// "Delta First Classic", "Delta One® Classic" (other/older pages render the
// bare "Main"/"Comfort"/"First"). Delta decorates the brand with tier words
// ("Classic", "Basic"), a ®, and a "Delta " prefix that vary by fare and
// route, so match on the one distinguishing word rather than the whole string.
func mapDeltaCabin(label string) (string, bool) {
	l := strings.ToLower(strings.TrimSpace(label))
	switch {
	case l == "":
		return "", false
	case strings.Contains(l, "delta one"), strings.Contains(l, "business"):
		return "business", true
	case strings.Contains(l, "premium select"):
		return "premium_economy", true
	case strings.Contains(l, "comfort"):
		return "premium_economy", true
	case strings.Contains(l, "first"):
		return "first", true
	case strings.Contains(l, "main"), strings.Contains(l, "coach"), strings.Contains(l, "basic"):
		return "economy", true
	default:
		return "", false
	}
}
