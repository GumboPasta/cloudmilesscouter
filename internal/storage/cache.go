package storage

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// searchTTL is how long a cached /api/search result stays fresh. Award space
// moves slowly enough that an hour of staleness is fine, and a completed scrape
// invalidates the affected route's keys anyway (see InvalidateRoute).
const searchTTL = time.Hour

// cabinKeyParts are the cabin values a search can be cached under: the four
// seeded cabin names plus "any" for a cabin-less search. InvalidateRoute walks
// this list to clear every variant a route+date could have been cached as.
var cabinKeyParts = []string{"any", "economy", "premium_economy", "business", "first"}

// Cache is the Redis-backed read cache for /api/search. A nil *Cache is a valid
// "caching disabled" value: every method is a no-op on it, so a caller that
// could not reach Redis at startup can carry a nil and skip the nil checks.
type Cache struct {
	rdb *redis.Client
}

// NewCache dials Redis at addr (host:port) and verifies it with a PING. A
// connection failure is returned so the caller can decide whether to run
// without a cache.
func NewCache(ctx context.Context, addr string) (*Cache, error) {
	rdb := redis.NewClient(&redis.Options{Addr: addr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		rdb.Close()
		return nil, err
	}
	return &Cache{rdb: rdb}, nil
}

// Close releases the Redis connection pool.
func (c *Cache) Close() error {
	if c == nil {
		return nil
	}
	return c.rdb.Close()
}

// GetSearch returns the cached results for f and true on a hit. A miss returns
// (nil, false, nil); a Redis or decode error is returned so the caller can log
// it and fall through to Postgres.
func (c *Cache) GetSearch(ctx context.Context, f AwardSearch) ([]AwardResult, bool, error) {
	if c == nil {
		return nil, false, nil
	}
	raw, err := c.rdb.Get(ctx, searchKey(f.Origin, f.Destination, f.SearchDate, f.Cabin)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var results []AwardResult
	if err := json.Unmarshal(raw, &results); err != nil {
		return nil, false, err
	}
	return results, true, nil
}

// SetSearch stores results for f with the search TTL. Best-effort: the caller
// should log any error and carry on, since the response is already in hand.
func (c *Cache) SetSearch(ctx context.Context, f AwardSearch, results []AwardResult) error {
	if c == nil {
		return nil
	}
	raw, err := json.Marshal(results)
	if err != nil {
		return err
	}
	return c.rdb.Set(ctx, searchKey(f.Origin, f.Destination, f.SearchDate, f.Cabin), raw, searchTTL).Err()
}

// InvalidateRoute drops every cached search for one route+date, across all
// cabins. The ETL calls this after writing fresh awards for the key so the next
// /api/search re-reads Postgres instead of serving the pre-scrape prices.
func (c *Cache) InvalidateRoute(ctx context.Context, k RawScrapeKey) error {
	if c == nil {
		return nil
	}
	keys := make([]string, len(cabinKeyParts))
	for i, cabin := range cabinKeyParts {
		keys[i] = searchKey(k.Origin, k.Destination, k.SearchDate, cabin)
	}
	return c.rdb.Del(ctx, keys...).Err()
}

// searchKey is the Redis key for one search: route, date, and cabin. An empty
// cabin (a search that did not pin one) is stored as "any" — the same token
// InvalidateRoute clears via cabinKeyParts.
func searchKey(origin, destination string, date time.Time, cabin string) string {
	if cabin == "" {
		cabin = "any"
	}
	return "search:" + origin + ":" + destination + ":" + date.Format("2006-01-02") + ":" + cabin
}
