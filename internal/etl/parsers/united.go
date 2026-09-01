package parsers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"cloudmilesscouter/internal/storage"
)

// unitedDateTimeLayout matches United's local, timezone-less timestamps,
// e.g. "2026-08-10 06:00".
const unitedDateTimeLayout = "2006-01-02 15:04"

type unitedResponse struct {
	Data struct {
		Trips []struct {
			Flights []unitedFlight `json:"Flights"`
		} `json:"Trips"`
	} `json:"data"`
}

type unitedFlight struct {
	FlightNumber        string             `json:"FlightNumber"`
	MarketingCarrier    string             `json:"MarketingCarrier"`
	Origin              string             `json:"Origin"`
	Destination         string             `json:"Destination"`
	DepartDateTime      string             `json:"DepartDateTime"`
	DestinationDateTime string             `json:"DestinationDateTime"`
	TravelMinutesTotal  int                `json:"TravelMinutesTotal"`
	Connections         []unitedConnection `json:"Connections"`
	Products            []unitedProduct    `json:"Products"`
}

type unitedConnection struct {
	Destination         string `json:"Destination"`
	DestinationDateTime string `json:"DestinationDateTime"`
}

type unitedProduct struct {
	CabinType   string        `json:"CabinType"`
	Description string        `json:"Description"`
	AwardType   string        `json:"AwardType"`
	Prices      []unitedPrice `json:"Prices"`
}

type unitedPrice struct {
	Currency    string  `json:"Currency"`
	Amount      float64 `json:"Amount"`
	PricingType string  `json:"PricingType"`
}

// United parses raw FetchFlights payloads scraped from united.com.
type United struct{}

// Parse decodes raw.RawPayload and emits one NormalizedAward per distinct
// flight-product option: every flight number keeps its own row, one per
// cabin/award-type combination, skipping unavailable placeholders and
// United's "-NOT-MIXED" duplicate products.
func (United) Parse(raw storage.RawScrape) ([]storage.NormalizedAward, error) {
	var resp unitedResponse
	if err := json.Unmarshal([]byte(raw.RawPayload), &resp); err != nil {
		return nil, fmt.Errorf("unmarshal united response: %w", err)
	}

	seen := make(map[string]bool)
	var awards []storage.NormalizedAward

	for _, trip := range resp.Data.Trips {
		for _, f := range trip.Flights {
			departTime, err := time.Parse(unitedDateTimeLayout, f.DepartDateTime)
			if err != nil {
				slog.Warn("skipping flight with unparseable depart time", "flight_number", f.FlightNumber, "err", err)
				continue
			}

			flightDestination := f.Destination
			arriveRaw := f.DestinationDateTime
			stops := len(f.Connections)
			if stops > 0 {
				last := f.Connections[stops-1]
				flightDestination = last.Destination
				arriveRaw = last.DestinationDateTime
			}

			arriveTime, err := time.Parse(unitedDateTimeLayout, arriveRaw)
			if err != nil {
				slog.Warn("skipping flight with unparseable arrival time", "flight_number", f.FlightNumber, "err", err)
				continue
			}

			flightNumber := f.MarketingCarrier + f.FlightNumber

			for _, p := range f.Products {
				if len(p.Prices) == 0 {
					continue // unavailable placeholder product
				}

				cabin, ok := mapCabin(p.CabinType, p.Description)
				if !ok {
					slog.Warn("skipping product with unrecognized cabin type", "cabin_type", p.CabinType)
					continue
				}

				var pointsCost int
				var taxesFees float64
				var currency string
				havePoints := false
				for _, price := range p.Prices {
					switch price.PricingType {
					case "Award":
						pointsCost = int(price.Amount)
						havePoints = true
					case "Tax":
						taxesFees = price.Amount
						currency = price.Currency
					}
				}
				if !havePoints {
					continue
				}
				if currency == "" {
					currency = "USD"
				}

				awardType := normalizeUnitedAwardType(p.AwardType)

				// Dedupe United's "-NOT-MIXED" duplicate products, which
				// repeat the same cabin/award-type/price under a different
				// ProductType.
				key := fmt.Sprintf("%s|%s|%s|%d", flightNumber, cabin, awardType, pointsCost)
				if seen[key] {
					continue
				}
				seen[key] = true

				awards = append(awards, storage.NormalizedAward{
					AirlineCode:       "united",
					AirlineName:       "United Airlines",
					Origin:            raw.Origin,
					Destination:       raw.Destination,
					SearchDate:        raw.SearchDate,
					ScrapedAt:         raw.ScrapedAt,
					Cabin:             cabin,
					AwardType:         awardType,
					Currency:          currency,
					FlightNumber:      flightNumber,
					FlightOrigin:      f.Origin,
					FlightDestination: flightDestination,
					DepartTime:        departTime,
					ArriveTime:        arriveTime,
					DurationMinutes:   f.TravelMinutesTotal,
					Stops:             stops,
					PointsCost:        pointsCost,
					TaxesFees:         taxesFees,
				})
			}
		}
	}

	return awards, nil
}

// normalizeUnitedAwardType maps United's payload AwardType to the lowercase
// vocabulary shared across parsers: "Saver" → saver (a further-discounted chart
// tier), "Standard" → standard (the fixed award chart). United is the only
// airline whose award type comes from the scraped payload rather than a literal,
// so an unrecognized value is lowercased and logged rather than dropped.
func normalizeUnitedAwardType(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "saver":
		return "saver"
	case "standard":
		return "standard"
	default:
		slog.Warn("united: unrecognized award type", "award_type", v)
		return strings.ToLower(strings.TrimSpace(v))
	}
}

// mapCabin maps United's CabinType (and, where CabinType is ambiguous,
// Description) to the cabin names seeded in the cabins table.
func mapCabin(cabinType, description string) (string, bool) {
	switch strings.ToLower(cabinType) {
	case "coach":
		return "economy", true
	case "premium coach", "premium economy":
		return "premium_economy", true
	case "business":
		return "business", true
	case "first":
		// International Polaris business is reported under CabinType
		// "First" with a "Polaris" description; domestic First is its own
		// cabin.
		if strings.Contains(strings.ToLower(description), "polaris") {
			return "business", true
		}
		return "first", true
	default:
		return "", false
	}
}
