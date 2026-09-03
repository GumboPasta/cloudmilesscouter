//go:build integration

// Integration coverage for the Redis search cache against a real Redis.
//
// Requires the docker/ compose stack up (uses REDIS_ADDR, else the localhost
// default). The test writes keys under a throwaway route code and deletes them
// on cleanup, so it does not disturb anything else in the instance.
//
//	go test -tags integration ./internal/storage
package storage

import (
	"context"
	"testing"
	"time"

	"cloudmilesscouter/internal/config"
)

func TestCacheSearchRoundTripAndInvalidate(t *testing.T) {
	ctx := context.Background()
	cache, err := NewCache(ctx, config.Load().RedisAddr)
	if err != nil {
		t.Fatalf("connect redis: %v", err)
	}
	t.Cleanup(func() { cache.Close() })

	search := AwardSearch{
		Origin:      "TST",
		Destination: "ZZZ",
		SearchDate:  time.Date(2099, 1, 15, 0, 0, 0, 0, time.UTC),
		Cabin:       "business",
	}
	anyCabin := search
	anyCabin.Cabin = ""

	clean := func() {
		cache.rdb.Del(ctx,
			searchKey(search.Origin, search.Destination, search.SearchDate, search.Cabin),
			searchKey(anyCabin.Origin, anyCabin.Destination, anyCabin.SearchDate, anyCabin.Cabin),
		)
	}
	clean()
	t.Cleanup(clean)

	want := []AwardResult{
		{AirlineCode: "testair", AirlineName: "Test Air", Cabin: "business", FlightNumber: "TA100", PointsCost: 80000},
	}

	t.Run("miss then hit", func(t *testing.T) {
		if _, hit, err := cache.GetSearch(ctx, search); err != nil || hit {
			t.Fatalf("pre-seed GetSearch: hit=%v err=%v, want miss", hit, err)
		}
		if err := cache.SetSearch(ctx, search, want); err != nil {
			t.Fatalf("SetSearch: %v", err)
		}
		got, hit, err := cache.GetSearch(ctx, search)
		if err != nil || !hit {
			t.Fatalf("post-seed GetSearch: hit=%v err=%v, want hit", hit, err)
		}
		if len(got) != 1 || got[0].FlightNumber != "TA100" || got[0].PointsCost != 80000 {
			t.Fatalf("round-tripped value wrong: %+v", got)
		}
	})

	t.Run("cabin-less search caches under its own key", func(t *testing.T) {
		if err := cache.SetSearch(ctx, anyCabin, want); err != nil {
			t.Fatalf("SetSearch (any): %v", err)
		}
		if _, hit, _ := cache.GetSearch(ctx, anyCabin); !hit {
			t.Fatal("cabin-less search should hit its own key")
		}
	})

	t.Run("InvalidateRoute clears every cabin variant", func(t *testing.T) {
		// Both keys are set from the sub-tests above.
		if err := cache.InvalidateRoute(ctx, RawScrapeKey{
			AirlineCode: "testair",
			Origin:      search.Origin,
			Destination: search.Destination,
			SearchDate:  search.SearchDate,
		}); err != nil {
			t.Fatalf("InvalidateRoute: %v", err)
		}
		if _, hit, _ := cache.GetSearch(ctx, search); hit {
			t.Fatal("business key still cached after InvalidateRoute")
		}
		if _, hit, _ := cache.GetSearch(ctx, anyCabin); hit {
			t.Fatal("cabin-less key still cached after InvalidateRoute")
		}
	})

	t.Run("nil cache is a no-op", func(t *testing.T) {
		var nc *Cache
		if _, hit, err := nc.GetSearch(ctx, search); hit || err != nil {
			t.Fatalf("nil GetSearch: hit=%v err=%v", hit, err)
		}
		if err := nc.SetSearch(ctx, search, want); err != nil {
			t.Fatalf("nil SetSearch: %v", err)
		}
		if err := nc.InvalidateRoute(ctx, RawScrapeKey{}); err != nil {
			t.Fatalf("nil InvalidateRoute: %v", err)
		}
	})
}
