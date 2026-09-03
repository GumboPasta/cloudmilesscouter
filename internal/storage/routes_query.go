package storage

import (
	"context"
	"database/sql"
	"time"
)

// RouteSummary is one route the frontend can offer as a shortcut, as returned
// by GET /api/routes. AwardCount is how many award rows currently exist for the
// route across all search dates; LastScraped is the newest of their scrape
// timestamps.
type RouteSummary struct {
	Origin      string    `json:"origin"`
	Destination string    `json:"destination"`
	AwardCount  int       `json:"award_count"`
	LastScraped time.Time `json:"last_scraped"`
}

// ListRoutes returns the routes that have award data, most-populated first.
// Routes with no awards are omitted (the INNER JOIN drops them). There is no
// popularity signal in the schema, so "popular" is approximated by how much
// data a route has. An empty result is not an error.
func ListRoutes(ctx context.Context, db *sql.DB) ([]RouteSummary, error) {
	const query = `
		SELECT r.origin, r.destination, COUNT(a.id), MAX(a.scraped_at)
		FROM routes r
		JOIN awards a ON a.route_id = r.id
		GROUP BY r.origin, r.destination
		ORDER BY COUNT(a.id) DESC, r.origin ASC, r.destination ASC`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := make([]RouteSummary, 0)
	for rows.Next() {
		var s RouteSummary
		if err := rows.Scan(&s.Origin, &s.Destination, &s.AwardCount, &s.LastScraped); err != nil {
			return nil, err
		}
		routes = append(routes, s)
	}
	return routes, rows.Err()
}
