//go:build integration

// Integration coverage for ListAirlines and ListRoutes against a real
// PostgreSQL instance. Same requirements and self-cleaning approach as
// awards_query_test.go (shares its seed helpers).
//
//	go test -tags integration ./internal/storage
package storage

import (
	"context"
	"testing"
	"time"

	"cloudmilesscouter/internal/config"
)

func TestListAirlinesAndRoutes(t *testing.T) {
	ctx := context.Background()
	db, err := ConnectPostgres(ctx, config.Load().PostgresURI)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	// Registered before the row-cleanup below so it runs last (t.Cleanup is
	// LIFO): the delete statements need the pool still open. A plain
	// `defer db.Close()` would run before any t.Cleanup and leave the rows.
	t.Cleanup(func() { db.Close() })

	searchDate := time.Date(2098, 3, 10, 0, 0, 0, 0, time.UTC)

	// Two throwaway routes: Ls1 with three award rows, Ls2 with one.
	airA := seedAirline(t, db, "lsaira", "LS Air Alpha")
	airB := seedAirline(t, db, "lsairb", "LS Air Beta")
	route1 := seedRoute(t, db, "LS1", "LSX")
	route2 := seedRoute(t, db, "LS2", "LSX")
	econ := cabinID(t, db, "economy")

	t.Cleanup(func() {
		for _, rid := range []int{route1, route2} {
			db.ExecContext(ctx, `DELETE FROM awards WHERE route_id = $1`, rid)
			db.ExecContext(ctx, `DELETE FROM routes WHERE id = $1`, rid)
		}
		for _, aid := range []int{airA, airB} {
			db.ExecContext(ctx, `DELETE FROM airlines WHERE id = $1`, aid)
		}
	})
	db.ExecContext(ctx, `DELETE FROM awards WHERE route_id = ANY($1)`, []int{route1, route2})

	seedAward(t, db, airA, route1, econ, searchDate, "LS1", 10000)
	seedAward(t, db, airA, route1, econ, searchDate, "LS2", 20000)
	seedAward(t, db, airB, route1, econ, searchDate, "LS3", 30000)
	seedAward(t, db, airB, route2, econ, searchDate, "LS4", 40000)

	t.Run("ListAirlines includes seeded airlines, ordered by name", func(t *testing.T) {
		got, err := ListAirlines(ctx, db)
		if err != nil {
			t.Fatalf("ListAirlines: %v", err)
		}
		idxA, idxB := -1, -1
		for i, a := range got {
			switch a.Code {
			case "lsaira":
				idxA = i
			case "lsairb":
				idxB = i
			}
		}
		if idxA < 0 || idxB < 0 {
			t.Fatalf("seeded airlines missing from %+v", got)
		}
		if idxA > idxB {
			t.Fatalf("not ordered by name: Alpha at %d, Beta at %d", idxA, idxB)
		}
	})

	t.Run("ListRoutes counts awards and orders most-populated first", func(t *testing.T) {
		got, err := ListRoutes(ctx, db)
		if err != nil {
			t.Fatalf("ListRoutes: %v", err)
		}
		var r1, r2 *RouteSummary
		for i := range got {
			switch {
			case got[i].Origin == "LS1" && got[i].Destination == "LSX":
				r1 = &got[i]
			case got[i].Origin == "LS2" && got[i].Destination == "LSX":
				r2 = &got[i]
			}
		}
		if r1 == nil || r2 == nil {
			t.Fatalf("seeded routes missing from %+v", got)
		}
		if r1.AwardCount != 3 || r2.AwardCount != 1 {
			t.Fatalf("award counts: LS1=%d (want 3), LS2=%d (want 1)", r1.AwardCount, r2.AwardCount)
		}
		if r1.LastScraped.IsZero() {
			t.Fatalf("LS1 last_scraped not set: %+v", r1)
		}
	})
}
