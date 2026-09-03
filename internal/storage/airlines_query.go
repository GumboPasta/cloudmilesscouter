package storage

import (
	"context"
	"database/sql"
)

// Airline is one supported airline, as returned by GET /api/airlines.
type Airline struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// ListAirlines returns every airline that has landed in the awards data,
// ordered by name. An empty database is not an error — the result is an
// empty slice.
func ListAirlines(ctx context.Context, db *sql.DB) ([]Airline, error) {
	rows, err := db.QueryContext(ctx, `SELECT code, name FROM airlines ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	airlines := make([]Airline, 0)
	for rows.Next() {
		var a Airline
		if err := rows.Scan(&a.Code, &a.Name); err != nil {
			return nil, err
		}
		airlines = append(airlines, a)
	}
	return airlines, rows.Err()
}
