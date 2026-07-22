# Normalized PostgreSQL Schema

Source: `docker/postgres/init/001_schema.sql`. This is what the Phase 4 API queries — any future schema change here is a breaking change for that layer.

## Tables

### `airlines`
One row per airline. Upserted by the ETL from `NormalizedAward.AirlineCode`/`AirlineName`.

| column | type | notes |
|---|---|---|
| id | serial pk | |
| code | text, unique | e.g. `united` |
| name | text | e.g. `United Airlines` |

### `routes`
One row per searched origin/destination pair, as reported by the airline (may be a multi-airport metro code, e.g. `NYC`, not a single airport).

| column | type | notes |
|---|---|---|
| id | serial pk | |
| origin | text | |
| destination | text | |
| | | unique on (origin, destination) |

### `cabins`
Fixed lookup, seeded once: `economy`, `premium_economy`, `business`, `first`. Not written by the ETL.

### `awards`
One row per distinct flight/cabin/award-type option returned by a scrape. This is the table the API reads from.

| column | type | notes |
|---|---|---|
| id | serial pk | |
| airline_id | fk → airlines | |
| route_id | fk → routes | the *searched* route, not necessarily the flight's actual origin/destination |
| cabin_id | fk → cabins | |
| search_date | date | the date searched |
| scraped_at | timestamptz | when the scrape ran |
| flight_number | text | marketing carrier + number, e.g. `UA2260` |
| flight_origin | text | this flight option's actual origin airport |
| flight_destination | text | this flight option's actual final-leg destination airport (last connection if any) |
| depart_time | timestamp | local, timezone-less (matches airline's own timestamps) |
| arrive_time | timestamp | local, timezone-less |
| duration_minutes | integer | |
| stops | integer | number of connections |
| award_type | text | e.g. `Saver`, `Standard` |
| points_cost | integer | |
| taxes_fees | numeric(10,2) | |
| currency | text | defaults to `USD` |
| created_at | timestamptz | |

Indexed on `(route_id, search_date)` for the Phase 4 search query pattern.

## Write pattern

`storage.WriteAwards` upserts the `airlines`/`routes` dimensions, then **deletes and re-inserts** all `awards` rows for every `(airline_id, route_id, search_date)` touched by the batch, inside one transaction. Re-running the ETL for the same search replaces that search's rows rather than duplicating them — confirmed by running the ETL twice against the same raw scrape and seeing the row count stay constant.

## Per-airline parsing notes

### United (`internal/etl/parsers/united.go`)
- One row per flight number × cabin × award type. A single scraped search can produce many rows (one raw document → 142 rows in testing) because United returns many itinerary/cabin/fare combinations per search.
- `flight_destination`/`arrive_time` come from the *last connection* when the flight has one, not the first leg.
- Products with zero `Prices` are unavailable placeholders and are skipped, as are United's `-NOT-MIXED` duplicate products (deduped on flight number + cabin + award type + points cost).
- `CabinType: "First"` with a "Polaris" description is international business, not domestic first, and is mapped to `business`.
- Domestic-only searches may legitimately have no `business`/`premium_economy` rows if United didn't offer those cabins on that route — this is not a parser bug (verified against raw payload during Phase 2 Step 4 testing).
