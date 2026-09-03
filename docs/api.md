# REST API — Frontend Integration Reference

The Phase 4 Go + Chi API over the normalized PostgreSQL data. Source: `internal/api/`,
served by `cmd/api/main.go`. Read `docs/schema.md` for the underlying tables.

## Running it

```
go run ./cmd/api        # from the repo root, so config.Load() finds .env
```

Needs Postgres up (it exits on boot if it can't connect). Redis and Kafka are
optional at startup — see *Caching* and `POST /api/scrape` below.

| env var | default | purpose |
|---|---|---|
| `API_PORT` | `8080` | listen port |
| `POSTGRES_URI` | `postgres://cloudmilesscouter:cloudmilesscouter@localhost:5432/cloudmilesscouter?sslmode=disable` | read database |
| `REDIS_ADDR` | `localhost:6379` | `/api/search` cache (degrades gracefully if down) |
| `KAFKA_BROKERS` | `localhost:9092` | `POST /api/scrape` target |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:5173` | comma-separated exact origins allowed from a browser |
| `RATE_LIMIT_PER_MINUTE` | `120` | per-client-IP budget; `0` disables the limiter |

## Conventions

- **Base URL:** `http://localhost:8080` in development.
- **Auth:** none.
- **Content type:** all responses are `application/json`.
- **Errors:** non-2xx responses carry `{"error": "<message>"}`. `4xx` messages are
  safe to show; `5xx` are generic (`"search failed"` etc.) with detail only in the
  server log.
- **CORS:** browsers must call from an origin in `CORS_ALLOWED_ORIGINS`. Allowed
  methods: `GET`, `POST`, `OPTIONS`. A preflight from an allowed origin returns
  `200` with an empty body and the `Access-Control-Allow-*` headers; an unlisted
  origin gets no `Access-Control-Allow-Origin` header (the browser blocks it).
- **Rate limiting:** past `RATE_LIMIT_PER_MINUTE` requests/min a client IP gets
  `429 Too Many Requests` until the window rolls over.
- **Request ID:** every response has an `X-Request-Id` header, echoed in the
  server's structured log line for that request.

---

## `GET /healthz`

Liveness probe. Does **not** check Postgres or Redis.

```
$ curl -s localhost:8080/healthz
{"status":"ok"}
```

`200` always (if the process is up).

---

## `GET /api/search`

Award options for one route and date, cheapest first. This is the endpoint the
search results table is built on.

| param | required | format | notes |
|---|---|---|---|
| `origin` | yes | 3 letters | IATA airport or metro code, case-insensitive. Matched against the **searched** route code, not each flight's own airport. |
| `destination` | yes | 3 letters | same |
| `date` | yes | `YYYY-MM-DD` | the outbound search date |
| `cabin` | no | `economy` \| `premium_economy` \| `business` \| `first` | omit for all cabins |

**Response:** `200` with a JSON array, ordered by `points_cost` ascending, ties
broken by `duration_minutes` ascending. A route/date with no data is `200 []`,
not an error.

```
$ curl -s 'localhost:8080/api/search?origin=SEA&destination=JFK&date=2026-12-08&cabin=economy'
[
  {
    "airline_code": "alaska",
    "airline_name": "Alaska Airlines",
    "cabin": "economy",
    "flight_number": "AS22",
    "flight_origin": "SEA",
    "flight_destination": "JFK",
    "depart_time": "2026-12-08T22:50:00Z",
    "arrive_time": "2026-12-09T07:00:00Z",
    "duration_minutes": 310,
    "stops": 0,
    "award_type": "dynamic",
    "points_cost": 12500,
    "taxes_fees": 6,
    "currency": "USD",
    "scraped_at": "2026-09-01T14:02:13.643-05:00"
  }
]
```

| field | type | notes |
|---|---|---|
| `airline_code` | string | e.g. `alaska` |
| `airline_name` | string | e.g. `Alaska Airlines` |
| `cabin` | string | one of the four cabin names |
| `flight_number` | string | may be a first-leg number when `stops > 0` |
| `flight_origin` / `flight_destination` | string | the flight's own airports (can differ from the searched codes when metro codes were searched) |
| `depart_time` / `arrive_time` | RFC 3339 timestamp | |
| `duration_minutes` | int | total itinerary duration |
| `stops` | int | `0` = nonstop |
| `award_type` | string | e.g. `dynamic`, `saver` — as reported by the airline |
| `points_cost` | int | miles/points for the award |
| `taxes_fees` | number | cash portion |
| `currency` | string | ISO code for `taxes_fees`, e.g. `USD` |
| `scraped_at` | RFC 3339 timestamp | when this row was scraped |

**`400`** on: missing `origin`/`destination`/`date`, a non-3-letter code, a
malformed `date`, an unrecognized `cabin`.

**Caching:** a hit is served from Redis; a miss reads Postgres and back-fills
Redis with a 1-hour TTL, keyed by `origin:destination:date:cabin` (`cabin` = `any`
when unset). A completed scrape's ETL run drops the affected route+date keys, so
fresh prices show up before the TTL. If Redis is unreachable the API reads
Postgres on every call — slower, never an error.

---

## `GET /api/airlines`

Every airline present in the awards data, ordered by name. For a filter dropdown.

```
$ curl -s localhost:8080/api/airlines
[
  {"code":"alaska","name":"Alaska Airlines"},
  {"code":"american","name":"American Airlines"},
  {"code":"delta","name":"Delta Air Lines"},
  {"code":"united","name":"United Airlines"}
]
```

`200` always (empty array if there's no data).

---

## `GET /api/routes`

Routes that have award data, most-populated first. For "popular routes" shortcuts
— there is no popularity signal in the schema, so `award_count` (total award rows
across all search dates) stands in for it.

```
$ curl -s localhost:8080/api/routes
[
  {
    "origin": "BOS",
    "destination": "SFO",
    "award_count": 611,
    "last_scraped": "2026-09-02T10:32:38.758-05:00"
  }
]
```

| field | type | notes |
|---|---|---|
| `origin` / `destination` | string | route codes as scraped (may be metro names like `NEW YORK, NY, US (ALL AIRPORTS)`) |
| `award_count` | int | total award rows for the route, all dates |
| `last_scraped` | RFC 3339 timestamp | newest `scraped_at` among them |

`200` always.

---

## `POST /api/scrape`

Trigger a scrape. Fire-and-forget: it dispatches one Kafka job per airline and
returns immediately — the **worker pool must be running** for anything to happen.
Fresh rows land in Postgres only after the worker scrapes and the ETL runs.

**Body:**

| field | required | format | notes |
|---|---|---|---|
| `origin` | yes | 3 letters | |
| `destination` | yes | 3 letters | |
| `date` | yes | `YYYY-MM-DD` | |
| `airlines` | no | string array | defaults to `["united","american","delta","alaska"]`; blanks and duplicates are dropped; the worker skips any airline without a registered scraper |

```
$ curl -s -X POST localhost:8080/api/scrape \
    -H 'Content-Type: application/json' \
    -d '{"origin":"SEA","destination":"JFK","date":"2026-12-08","airlines":["delta"]}'
{"dispatched":["delta"],"origin":"SEA","destination":"JFK","date":"2026-12-08"}
```

**`202`** with the dispatched list on success.
**`400`** on a bad/oversized body (>4 KiB), an unknown JSON field, a missing/bad
param, or an all-blank `airlines` list.
**`502`** if dispatching to Kafka fails (broker down); any jobs already enqueued
before the failure are listed in the server log.

---

## Testing

`scripts/smoke_api.sh` runs curl against a live API covering every case above
plus the cache hit/miss/invalidation path. `go test -tags integration
./internal/storage ./internal/api` covers the query and cache layers against a
real Postgres + Redis.
