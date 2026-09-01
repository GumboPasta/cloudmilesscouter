package parsers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"cloudmilesscouter/internal/storage"
)

type alaskaResponse struct {
	Route struct {
		Origin      string `json:"origin"`
		Destination string `json:"destination"`
	} `json:"route"`
	Flights []alaskaFlight `json:"flights"`
}

type alaskaFlight struct {
	FlightNumber    string       `json:"flightNumber"`
	Via             []string     `json:"via"`
	Depart          string       `json:"depart"`
	Arrive          string       `json:"arrive"`
	DurationMinutes int          `json:"durationMinutes"`
	ArrivesNextDay  int          `json:"arrivesNextDay"`
	Origin          string       `json:"origin"`
	Destination     string       `json:"destination"`
	Stops           int          `json:"stops"`
	Fares           []alaskaFare `json:"fares"`
}

type alaskaFare struct {
	Cabin  string  `json:"cabin"`
	Miles  int     `json:"miles"`
	TaxUSD float64 `json:"taxUSD"`
}

var alaskaFlightNumRe = regexp.MustCompile(`^AS\s?\d+$`)

// Alaska parses the DOM-extracted payload produced by
// internal/scraper/airlines/alaska.go (Alaska's Svelte results grid has no
// JSON API).
type Alaska struct{}

// Parse emits one NormalizedAward per flight × cabin. Alaska's award grid shows
// two cabins, Main → economy and First → first. Connecting itineraries render
// without a real flight number ("Multiple flights"); those get a synthetic
// "AS <via>" number so rows stay distinguishable.
func (Alaska) Parse(raw storage.RawScrape) ([]storage.NormalizedAward, error) {
	var resp alaskaResponse
	if err := json.Unmarshal([]byte(raw.RawPayload), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal alaska response: %w", err)
	}

	origin := resp.Route.Origin
	if origin == "" {
		origin = raw.Origin
	}
	destination := resp.Route.Destination
	if destination == "" {
		destination = raw.Destination
	}

	seen := make(map[string]bool)
	var awards []storage.NormalizedAward

	for _, f := range resp.Flights {
		departTime, ok := deltaClock(raw.SearchDate, f.Depart)
		if !ok {
			slog.Warn("skipping alaska flight with unparseable depart time", "raw", f.Depart)
			continue
		}
		arriveTime, ok := deltaClock(raw.SearchDate, f.Arrive)
		if !ok {
			slog.Warn("skipping alaska flight with unparseable arrival time", "raw", f.Arrive)
			continue
		}
		nextDay := f.ArrivesNextDay
		if nextDay == 0 && arriveTime.Before(departTime) {
			nextDay = 1
		}
		arriveTime = arriveTime.AddDate(0, 0, nextDay)

		flightNumber := alaskaFlightNumber(f)

		for _, fare := range f.Fares {
			if fare.Miles <= 0 {
				continue
			}
			cabin, ok := mapAlaskaCabin(fare.Cabin)
			if !ok {
				slog.Warn("skipping alaska fare with unrecognized cabin", "cabin", fare.Cabin)
				continue
			}

			key := fmt.Sprintf("%s|%s|%s|%d", flightNumber, f.Depart, cabin, fare.Miles)
			if seen[key] {
				continue
			}
			seen[key] = true

			awards = append(awards, storage.NormalizedAward{
				AirlineCode:       "alaska",
				AirlineName:       "Alaska Airlines",
				Origin:            origin,
				Destination:       destination,
				SearchDate:        raw.SearchDate,
				ScrapedAt:         raw.ScrapedAt,
				Cabin:             cabin,
				AwardType:         "Dynamic",
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

// alaskaFlightNumber returns "AS123" for a single-flight itinerary, or a
// synthetic "AS <via>" for a connecting one that Alaska labels "Multiple
// flights".
func alaskaFlightNumber(f alaskaFlight) string {
	if alaskaFlightNumRe.MatchString(strings.TrimSpace(f.FlightNumber)) {
		return strings.ReplaceAll(strings.TrimSpace(f.FlightNumber), " ", "")
	}
	if len(f.Via) > 0 {
		return "AS " + strings.Join(f.Via, "-")
	}
	return "AS"
}

// mapAlaskaCabin maps Alaska's fare-column label to the cabins table names.
func mapAlaskaCabin(cabin string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(cabin)) {
	case "saver", "main", "main cabin", "coach", "economy":
		return "economy", true
	case "first", "first class":
		return "first", true
	case "business":
		return "business", true
	default:
		return "", false
	}
}
