//go:build integration

// Integration coverage for SearchAwards against a real PostgreSQL instance.
//
// Requires the docker/ compose stack up (uses POSTGRES_URI, else the localhost
// default). The test seeds its own airline/route/award rows under a throwaway
// route code and deletes them on cleanup, so it does not disturb real data.
//
//	go test -tags integration ./internal/storage
package storage

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"cloudmilesscouter/internal/config"
)

func TestSearchAwards(t *testing.T) {
	ctx := context.Background()
	db, err := ConnectPostgres(ctx, config.Load().PostgresURI)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	// Registered before t.Cleanup(clean) so it runs last (t.Cleanup is LIFO):
	// clean's DELETEs need the pool still open. A plain `defer db.Close()` runs
	// before any t.Cleanup and would leave the seeded rows behind.
	t.Cleanup(func() { db.Close() })

	const (
		origin      = "TST"
		destination = "ZZZ"
	)
	searchDate := time.Date(2099, 1, 15, 0, 0, 0, 0, time.UTC)

	airlineID := seedAirline(t, db, "testair", "Test Air")
	routeID := seedRoute(t, db, origin, destination)
	clean := func() {
		db.ExecContext(ctx, `DELETE FROM awards WHERE route_id = $1`, routeID)
		db.ExecContext(ctx, `DELETE FROM routes WHERE id = $1`, routeID)
		db.ExecContext(ctx, `DELETE FROM airlines WHERE id = $1`, airlineID)
	}
	db.ExecContext(ctx, `DELETE FROM awards WHERE route_id = $1`, routeID) // clear any rows a prior aborted run left
	t.Cleanup(clean)

	// Two economy rows (insert priciest first to prove the ORDER BY) and one
	// business row that the cabin filter must exclude.
	seedAward(t, db, airlineID, routeID, cabinID(t, db, "economy"), searchDate, "TA200", 44000)
	seedAward(t, db, airlineID, routeID, cabinID(t, db, "economy"), searchDate, "TA100", 12500)
	seedAward(t, db, airlineID, routeID, cabinID(t, db, "business"), searchDate, "TA300", 80000)

	t.Run("all cabins, cheapest first", func(t *testing.T) {
		got, err := SearchAwards(ctx, db, AwardSearch{Origin: origin, Destination: destination, SearchDate: searchDate})
		if err != nil {
			t.Fatalf("SearchAwards: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("got %d results, want 3", len(got))
		}
		if got[0].PointsCost != 12500 || got[1].PointsCost != 44000 || got[2].PointsCost != 80000 {
			t.Fatalf("not sorted ascending by points: %d, %d, %d", got[0].PointsCost, got[1].PointsCost, got[2].PointsCost)
		}
		if got[0].AirlineName != "Test Air" || got[0].Cabin != "economy" {
			t.Fatalf("join fields wrong: %+v", got[0])
		}
	})

	t.Run("cabin filter", func(t *testing.T) {
		got, err := SearchAwards(ctx, db, AwardSearch{Origin: origin, Destination: destination, SearchDate: searchDate, Cabin: "business"})
		if err != nil {
			t.Fatalf("SearchAwards: %v", err)
		}
		if len(got) != 1 || got[0].Cabin != "business" {
			t.Fatalf("cabin filter: got %+v, want one business row", got)
		}
	})

	t.Run("no match is empty not nil", func(t *testing.T) {
		got, err := SearchAwards(ctx, db, AwardSearch{Origin: origin, Destination: destination, SearchDate: searchDate.AddDate(1, 0, 0)})
		if err != nil {
			t.Fatalf("SearchAwards: %v", err)
		}
		if got == nil || len(got) != 0 {
			t.Fatalf("want empty non-nil slice, got %#v", got)
		}
	})
}

func seedAirline(t *testing.T, db *sql.DB, code, name string) int {
	t.Helper()
	var id int
	err := db.QueryRow(`
		INSERT INTO airlines (code, name) VALUES ($1, $2)
		ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name RETURNING id`, code, name).Scan(&id)
	if err != nil {
		t.Fatalf("seed airline: %v", err)
	}
	return id
}

func seedRoute(t *testing.T, db *sql.DB, origin, destination string) int {
	t.Helper()
	var id int
	err := db.QueryRow(`
		INSERT INTO routes (origin, destination) VALUES ($1, $2)
		ON CONFLICT (origin, destination) DO UPDATE SET origin = EXCLUDED.origin RETURNING id`, origin, destination).Scan(&id)
	if err != nil {
		t.Fatalf("seed route: %v", err)
	}
	return id
}

func cabinID(t *testing.T, db *sql.DB, name string) int {
	t.Helper()
	var id int
	if err := db.QueryRow(`SELECT id FROM cabins WHERE name = $1`, name).Scan(&id); err != nil {
		t.Fatalf("cabin %q: %v", name, err)
	}
	return id
}

func seedAward(t *testing.T, db *sql.DB, airlineID, routeID, cabinID int, searchDate time.Time, flightNo string, points int) {
	t.Helper()
	depart := searchDate.Add(9 * time.Hour)
	_, err := db.Exec(`
		INSERT INTO awards (
			airline_id, route_id, cabin_id, search_date, scraped_at,
			flight_number, flight_origin, flight_destination,
			depart_time, arrive_time, duration_minutes, stops,
			award_type, points_cost, taxes_fees, currency
		) VALUES ($1, $2, $3, $4, now(), $5, 'TST', 'ZZZ', $6, $7, 360, 0, 'dynamic', $8, 5.60, 'USD')`,
		airlineID, routeID, cabinID, searchDate, flightNo, depart, depart.Add(6*time.Hour), points)
	if err != nil {
		t.Fatalf("seed award: %v", err)
	}
}
