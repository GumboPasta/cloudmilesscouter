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
One row per searched origin/destination pair — the codes the user/producer supplied to the scrape job, not whatever the airline echoed back. All four parsers set this from the job's `origin`/`destination` so a row is always found by the codes Phase 4's API will query with. May be a multi-airport metro code, e.g. `NYC`, if that is what was searched.

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
| route_id | fk → routes | the *searched* route (the codes supplied to the scrape job), not necessarily the flight's actual origin/destination |
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
| award_type | text | one of `saver`, `standard`, `dynamic` (normalized by the parsers) |
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
- `award_type`: United is the one airline whose value comes from the payload (`AwardType`), mapped `Saver` → `saver`, `Standard` → `standard`. An unrecognized value is lowercased and `slog.Warn`ed rather than dropped.
- `routes` origin/destination are the searched codes from the scrape job (`raw.Origin`/`raw.Destination`), not United's echoed `DepartureAirports`/`ArrivalAirports`.
- `CabinType: "First"` with a "Polaris" description is international business, not domestic first, and is mapped to `business`.
- Domestic-only searches may legitimately have no `business`/`premium_economy` rows if United didn't offer those cabins on that route — this is not a parser bug (verified against raw payload during Phase 2 Step 4 testing).

### American (`internal/etl/parsers/american.go`)
- Raw payload is the `<script id="ng-state">` JSON from aa.com's server-rendered results page; the parser reads `SearchData.itineraryResult`.
- One row per `slice` (itinerary) × available cabin. Each slice carries a `pricingDetail` array with one entry per cabin (`COACH`/`BUSINESS`/`FIRST`, plus `PREMIUM_ECONOMY` on international); entries with `productAvailable == false` are sold-out placeholders (points `0`) and are skipped.
- `flight_number` is the **first segment's** marketing flight (e.g. `AA855`); `stops` is `len(segments) - 1`; `flight_origin`/`flight_destination` are the slice endpoints. Many distinct slices share a first flight but connect through different cities, so dedup is on the full segment path + departure time, not the flight number.
- `routes` origin/destination are the searched codes from the scrape job (`raw.Origin`/`raw.Destination`), not aa.com's resolved `responseMetadata` codes.
- `depart_time`/`arrive_time`: American's timestamps are ISO-8601 with a UTC offset (`2026-11-20T08:25:00.000-06:00`); the parser keeps the local wall-clock components and drops the offset, matching United's already-tz-less values.
- `award_type`: `dynamic` when `pricingDetail[].dynamicFare` is true (American's dynamically-priced awards — most of them today), else `standard` (the older fixed-chart level, still present on some flights). American exposes no separate "Web Special" marker in the captured payload.
- Scraper (`internal/scraper/airlines/american.go`): no login needed, but Akamai hard-blocks headless Chromium, so it must run headed (`HEADLESS=false`). The form's URL params don't pre-fill a cold session, so it drives the fields directly (trip type → one way, IATA airports, date, "Redeem miles"), tolerating values the persistent profile remembers from the previous search.

### Delta (`internal/etl/parsers/delta.go`)
- Delta's `flightsearch/search-results` page is server-rendered and carries **no JSON payload** — the flight/fare data lives only in the DOM. So `internal/scraper/airlines/delta.go` extracts it in-browser via `page.Evaluate` (`DeltaExtractJS`) and stores that structured JSON; the parser just maps it.
- One row per flight × available cabin. The extractor reads each fare column's brand label off the results grid (`.fare-cell-desktop-header .brand-name`) instead of assuming a fixed column order — the same search mixes a "Delta First Classic" column and a "Delta Premium Select Classic" column depending on the aircraft, so position is not the cabin. `mapDeltaCabin` maps the label: **Main → economy, Comfort → premium_economy, First → first**, plus **Delta One → business** and **Premium Select → premium_economy** on premium routes (e.g. transcon widebody JFK–LAX). Comfort+ isn't strictly premium economy but it's the closest seeded cabin.
- `flight_number` is the first segment (e.g. `DL454`); dedup is on the full segment path + departure time. `flight_origin`/`flight_destination` are the flight's own endpoints — Delta returns "nearby airport" results (e.g. a DAL departure for a DFW search). `routes` origin/destination are the searched codes from the scrape job (`raw.Origin`/`raw.Destination`).
- `depart_time`/`arrive_time`: Delta shows local clock times with no date; an arrival earlier in the day than the departure is treated as next-day.
- `award_type` is always `dynamic` — Delta SkyMiles has no saver/award-chart tier.
- Scraper: no login, Akamai present so headed only. The airport pickers are an overlay (click trigger → type IATA → pick first option); the calendar shows two months and is paged forward to the target date. Tolerates form values the persistent profile remembers.

### Alaska (`internal/etl/parsers/alaska.go`)
- Alaska's award search (`ShoppingMethod=onlineaward`) is anonymous **and deep-linkable** — `internal/scraper/airlines/alaska.go` just navigates `alaskaair.com/search/results?O=…&D=…&OD=…&RT=false&ShoppingMethod=onlineaward` with no form. The Svelte results grid has no JSON payload, so it extracts the DOM via `page.Evaluate` (`AlaskaExtractJS`). Alaska does not hard-block headless, so this scraper can run `HEADLESS=true`.
- Two cabin columns: **Main → economy, First → first** (Saver also → economy). `award_type` is always `dynamic`. `routes` origin/destination are the searched codes from the scrape job (`raw.Origin`/`raw.Destination`).
- Connecting itineraries render as "Multiple flights" with **no per-segment flight number**; those get a synthetic `AS <via>` (e.g. `AS SEA`, `AS SAN`) so rows stay distinguishable, deduped with the departure time.
- `depart_time`/`arrive_time`: local clock times; the grid marks overnight arrivals with "+N day", and an arrival earlier than the departure is also treated as next-day.
