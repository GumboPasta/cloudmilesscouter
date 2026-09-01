package parsers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cloudmilesscouter/internal/storage"
)

// americanDateTimeLayout matches American's ISO-8601 slice/segment timestamps,
// e.g. "2026-11-20T08:25:00.000-06:00". time.RFC3339 accepts the fractional
// seconds even though it does not spell them out.
const americanDateTimeLayout = time.RFC3339

type americanResponse struct {
	SearchData struct {
		ItineraryResult struct {
			Error  string          `json:"error"`
			Slices []americanSlice `json:"slices"`
		} `json:"itineraryResult"`
	} `json:"SearchData"`
}

type americanAirport struct {
	Code string `json:"code"`
}

type americanSlice struct {
	Origin            americanAirport   `json:"origin"`
	Destination       americanAirport   `json:"destination"`
	DepartureDateTime string            `json:"departureDateTime"`
	ArrivalDateTime   string            `json:"arrivalDateTime"`
	DurationInMinutes int               `json:"durationInMinutes"`
	Segments          []americanSegment `json:"segments"`
	PricingDetail     []americanPricing `json:"pricingDetail"`
}

type americanSegment struct {
	Flight struct {
		CarrierCode  string `json:"carrierCode"`
		FlightNumber string `json:"flightNumber"`
	} `json:"flight"`
}

type americanPricing struct {
	ProductType              string `json:"productType"`
	ProductAvailable         bool   `json:"productAvailable"`
	DynamicFare              bool   `json:"dynamicFare"`
	PerPassengerAwardPoints  int    `json:"perPassengerAwardPoints"`
	PerPassengerTaxesAndFees struct {
		Amount   float64 `json:"amount"`
		Currency string  `json:"currency"`
	} `json:"perPassengerTaxesAndFees"`
}

// American parses raw ng-state blobs scraped from aa.com's award search.
type American struct{}

// Parse decodes raw.RawPayload and emits one NormalizedAward per
// slice × available cabin. American returns one pricingDetail entry per cabin
// per itinerary; entries with productAvailable == false are sold-out
// placeholders (points 0) and are skipped, mirroring the United parser's
// handling of price-less products.
func (American) Parse(raw storage.RawScrape) ([]storage.NormalizedAward, error) {
	var resp americanResponse
	if err := json.Unmarshal([]byte(raw.RawPayload), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal american response: %w", err)
	}

	ir := resp.SearchData.ItineraryResult

	seen := make(map[string]bool)
	var awards []storage.NormalizedAward

	for _, s := range ir.Slices {
		if len(s.Segments) == 0 {
			continue
		}

		departTime, err := americanLocalTime(s.DepartureDateTime)
		if err != nil {
			slog.Warn("skipping american slice with unparseable depart time", "raw", s.DepartureDateTime, "err", err)
			continue
		}
		arriveTime, err := americanLocalTime(s.ArrivalDateTime)
		if err != nil {
			slog.Warn("skipping american slice with unparseable arrival time", "raw", s.ArrivalDateTime, "err", err)
			continue
		}

		flightNumbers := make([]string, len(s.Segments))
		for i, seg := range s.Segments {
			flightNumbers[i] = seg.Flight.CarrierCode + seg.Flight.FlightNumber
		}
		flightNumber := flightNumbers[0] // marketing flight of the first segment, per the schema convention
		segmentPath := strings.Join(flightNumbers, "-")
		flightOrigin := s.Origin.Code
		flightDestination := s.Destination.Code
		stops := len(s.Segments) - 1

		for _, p := range s.PricingDetail {
			if !p.ProductAvailable {
				continue
			}

			cabin, ok := mapAmericanCabin(p.ProductType)
			if !ok {
				slog.Warn("skipping american product with unrecognized cabin", "product_type", p.ProductType)
				continue
			}

			// American prices most awards dynamically today; a false flag marks
			// the older fixed-chart ("standard") pricing, which still shows up.
			awardType := "dynamic"
			if !p.DynamicFare {
				awardType = "standard"
			}
			currency := p.PerPassengerTaxesAndFees.Currency
			if currency == "" {
				currency = "USD"
			}

			// Dedupe on the full itinerary, not just the first flight — many
			// distinct slices share a first-segment flight number but connect
			// through different cities.
			key := fmt.Sprintf("%s|%s|%s|%s|%d", segmentPath, s.DepartureDateTime, cabin, awardType, p.PerPassengerAwardPoints)
			if seen[key] {
				continue
			}
			seen[key] = true

			awards = append(awards, storage.NormalizedAward{
				AirlineCode:       "american",
				AirlineName:       "American Airlines",
				Origin:            raw.Origin,
				Destination:       raw.Destination,
				SearchDate:        raw.SearchDate,
				ScrapedAt:         raw.ScrapedAt,
				Cabin:             cabin,
				AwardType:         awardType,
				Currency:          currency,
				FlightNumber:      flightNumber,
				FlightOrigin:      flightOrigin,
				FlightDestination: flightDestination,
				DepartTime:        departTime,
				ArriveTime:        arriveTime,
				DurationMinutes:   s.DurationInMinutes,
				Stops:             stops,
				PointsCost:        p.PerPassengerAwardPoints,
				TaxesFees:         p.PerPassengerTaxesAndFees.Amount,
			})
		}
	}

	return awards, nil
}

// americanLocalTime parses one of American's ISO-8601 timestamps and returns it
// as a timezone-less wall-clock time (the departure/arrival airport's own local
// time), matching how the awards table stores United's already-tz-less
// timestamps.
func americanLocalTime(s string) (time.Time, error) {
	t, err := time.Parse(americanDateTimeLayout, s)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC), nil
}

// mapAmericanCabin maps American's productType to the cabin names seeded in the
// cabins table.
func mapAmericanCabin(productType string) (string, bool) {
	switch strings.ToUpper(productType) {
	case "COACH":
		return "economy", true
	case "PREMIUM_ECONOMY", "PREMIUMECONOMY":
		return "premium_economy", true
	case "BUSINESS":
		return "business", true
	case "FIRST":
		return "first", true
	default:
		return "", false
	}
}
