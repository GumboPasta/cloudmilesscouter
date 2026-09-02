package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// ConnectPostgres opens a connection pool for the given DSN and verifies
// the database is reachable via Ping before returning.
func ConnectPostgres(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// NormalizedAward is one flight-product option parsed out of a raw scrape,
// ready to insert as a row in the awards table.
type NormalizedAward struct {
	AirlineCode, AirlineName        string
	Origin, Destination             string // searched route, e.g. DFW -> NYC
	SearchDate, ScrapedAt           time.Time
	Cabin, AwardType, Currency      string
	FlightNumber                    string
	FlightOrigin, FlightDestination string // this flight option's actual origin/final destination
	DepartTime, ArriveTime          time.Time
	DurationMinutes, Stops          int
	PointsCost                      int
	TaxesFees                       float64
}

// RawScrapeKey identifies one searched route+date for one airline program,
// using the same lowercase program name as RawScrape.Airline and the
// airlines.code column ("united", "delta", ...).
type RawScrapeKey struct {
	AirlineCode         string
	Origin, Destination string
	SearchDate          time.Time
}

// WriteAwards upserts the airline/route/cabin dimensions and replaces the
// awards rows for every (airline, route, search_date) present in awards,
// so re-running the ETL for the same search updates it cleanly instead of
// piling up duplicates.
//
// clearKeys is the set of (airline, route, search_date) the ETL parsed this
// run, including docs that yielded zero award rows (selector drift on a DOM
// extractor returns an empty result that still parses as a success). Every
// clear key's stale rows are deleted before the inserts, so an empty scrape
// clears last run's prices instead of leaving them to be served as current.
func WriteAwards(ctx context.Context, db *sql.DB, awards []NormalizedAward, clearKeys []RawScrapeKey) error {
	if len(awards) == 0 && len(clearKeys) == 0 {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	type searchKey struct {
		airlineID, routeID int
		searchDate         time.Time
	}
	deleted := make(map[searchKey]bool)

	// Clear stale rows for every parsed key first. Stale rows can only exist if
	// a previous run inserted them, which means the airline and route rows are
	// already present; if they are not, there is nothing to clear, so a missing
	// dimension row is not an error here (no name is available to upsert one for
	// a zero-award key anyway). A key that also has award rows below is deleted
	// once, here, and skipped by the insert loop via the shared deleted map.
	for _, k := range clearKeys {
		var airlineID int
		err := tx.QueryRowContext(ctx, `SELECT id FROM airlines WHERE code = $1`, k.AirlineCode).Scan(&airlineID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}

		var routeID int
		err = tx.QueryRowContext(ctx, `SELECT id FROM routes WHERE origin = $1 AND destination = $2`, k.Origin, k.Destination).Scan(&routeID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}

		key := searchKey{airlineID, routeID, k.SearchDate}
		if !deleted[key] {
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM awards WHERE airline_id = $1 AND route_id = $2 AND search_date = $3`,
				airlineID, routeID, k.SearchDate); err != nil {
				return err
			}
			deleted[key] = true
		}
	}

	for _, a := range awards {
		var airlineID int
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO airlines (code, name) VALUES ($1, $2)
			ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name
			RETURNING id`, a.AirlineCode, a.AirlineName).Scan(&airlineID); err != nil {
			return err
		}

		var routeID int
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO routes (origin, destination) VALUES ($1, $2)
			ON CONFLICT (origin, destination) DO UPDATE SET origin = EXCLUDED.origin
			RETURNING id`, a.Origin, a.Destination).Scan(&routeID); err != nil {
			return err
		}

		var cabinID int
		if err := tx.QueryRowContext(ctx, `SELECT id FROM cabins WHERE name = $1`, a.Cabin).Scan(&cabinID); err != nil {
			return err
		}

		key := searchKey{airlineID, routeID, a.SearchDate}
		if !deleted[key] {
			if _, err := tx.ExecContext(ctx, `
				DELETE FROM awards WHERE airline_id = $1 AND route_id = $2 AND search_date = $3`,
				airlineID, routeID, a.SearchDate); err != nil {
				return err
			}
			deleted[key] = true
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO awards (
				airline_id, route_id, cabin_id, search_date, scraped_at,
				flight_number, flight_origin, flight_destination,
				depart_time, arrive_time, duration_minutes, stops,
				award_type, points_cost, taxes_fees, currency
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
			airlineID, routeID, cabinID, a.SearchDate, a.ScrapedAt,
			a.FlightNumber, a.FlightOrigin, a.FlightDestination,
			a.DepartTime, a.ArriveTime, a.DurationMinutes, a.Stops,
			a.AwardType, a.PointsCost, a.TaxesFees, a.Currency,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}
