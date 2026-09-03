package storage

import (
	"context"
	"database/sql"
	"time"
)

// AwardSearch is the filter for a single /api/search query: an exact
// origin/destination pair and search date, optionally narrowed to one cabin.
// The codes match the routes table (the codes supplied to the scrape job), so
// they are compared as-is.
type AwardSearch struct {
	Origin, Destination string
	SearchDate          time.Time
	Cabin               string // "" means any cabin
}

// AwardResult is one award option returned to the API caller. It flattens the
// awards row with its airline and cabin names already joined in.
type AwardResult struct {
	AirlineCode       string    `json:"airline_code"`
	AirlineName       string    `json:"airline_name"`
	Cabin             string    `json:"cabin"`
	FlightNumber      string    `json:"flight_number"`
	FlightOrigin      string    `json:"flight_origin"`
	FlightDestination string    `json:"flight_destination"`
	DepartTime        time.Time `json:"depart_time"`
	ArriveTime        time.Time `json:"arrive_time"`
	DurationMinutes   int       `json:"duration_minutes"`
	Stops             int       `json:"stops"`
	AwardType         string    `json:"award_type"`
	PointsCost        int       `json:"points_cost"`
	TaxesFees         float64   `json:"taxes_fees"`
	Currency          string    `json:"currency"`
	ScrapedAt         time.Time `json:"scraped_at"`
}

// SearchAwards returns every award option for the given route and search date,
// cheapest first (ties broken by the shorter itinerary). It hits the
// (route_id, search_date) index via the routes lookup. A route/date with no
// rows is not an error — the result is an empty slice.
func SearchAwards(ctx context.Context, db *sql.DB, f AwardSearch) ([]AwardResult, error) {
	const query = `
		SELECT al.code, al.name, c.name,
		       a.flight_number, a.flight_origin, a.flight_destination,
		       a.depart_time, a.arrive_time, a.duration_minutes, a.stops,
		       a.award_type, a.points_cost, a.taxes_fees, a.currency, a.scraped_at
		FROM awards a
		JOIN airlines al ON al.id = a.airline_id
		JOIN routes   r  ON r.id  = a.route_id
		JOIN cabins   c  ON c.id  = a.cabin_id
		WHERE r.origin = $1 AND r.destination = $2 AND a.search_date = $3
		  AND ($4 = '' OR c.name = $4)
		ORDER BY a.points_cost ASC, a.duration_minutes ASC`

	rows, err := db.QueryContext(ctx, query, f.Origin, f.Destination, f.SearchDate, f.Cabin)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make([]AwardResult, 0)
	for rows.Next() {
		var r AwardResult
		if err := rows.Scan(
			&r.AirlineCode, &r.AirlineName, &r.Cabin,
			&r.FlightNumber, &r.FlightOrigin, &r.FlightDestination,
			&r.DepartTime, &r.ArriveTime, &r.DurationMinutes, &r.Stops,
			&r.AwardType, &r.PointsCost, &r.TaxesFees, &r.Currency, &r.ScrapedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
