//go:build integration

// Integration coverage for the cache-first path of handleSearch against a real
// Redis (uses REDIS_ADDR, else the localhost default; needs the compose stack).
//
//	go test -tags integration ./internal/api
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cloudmilesscouter/internal/config"
	"cloudmilesscouter/internal/storage"
)

func TestSearchServesFromCache(t *testing.T) {
	ctx := context.Background()
	cache, err := storage.NewCache(ctx, config.Load().RedisAddr)
	if err != nil {
		t.Fatalf("connect redis: %v", err)
	}
	t.Cleanup(func() { cache.Close() })

	search := storage.AwardSearch{
		Origin:      "TST",
		Destination: "ZZZ",
		SearchDate:  time.Date(2099, 3, 4, 0, 0, 0, 0, time.UTC),
		Cabin:       "economy",
	}
	seeded := []storage.AwardResult{
		{AirlineCode: "testair", AirlineName: "Test Air", Cabin: "economy", FlightNumber: "TA1", PointsCost: 11000},
	}
	if err := cache.SetSearch(ctx, search, seeded); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	t.Cleanup(func() {
		_ = cache.InvalidateRoute(ctx, storage.RawScrapeKey{
			Origin: search.Origin, Destination: search.Destination, SearchDate: search.SearchDate,
		})
	})

	cfg := testConfig()
	cfg.RateLimitPerMinute = 0
	// db is nil: a cache hit must not touch Postgres.
	handler := NewRouter(cfg, nil, cache, nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/api/search?origin=TST&destination=ZZZ&date=2099-03-04&cabin=economy", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got []storage.AwardResult
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(got) != 1 || got[0].FlightNumber != "TA1" || got[0].PointsCost != 11000 {
		t.Fatalf("cached result not served: %+v", got)
	}
}
