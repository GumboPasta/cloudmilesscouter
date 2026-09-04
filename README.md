# CloudMilesScouter ✈️

> An airline award flight tracker that scrapes loyalty program websites to find the best award availability across all major carriers — aggregated, normalized, and searchable in one place.

---

## What It Does
Finding award flight availability across 40+ airlines is painful. Each airline has its own search tool. CloudMilesScouter aggregates them all in one place, triggered on demand, so you can find the best points deal for any route and date without manually checking every airline.

---

## ⚠️ MVP First Mindset
This project is phased intentionally. **Phases 1 and 2 have zero queue infrastructure.** No Kafka, no Redis, no Prometheus yet. The goal is to prove the scraper works and data flows cleanly before layering in complexity. If you're fighting Kafka configs before you've scraped a single flight, you've moved too fast. Build lean, validate, then scale.

---

## Tech Stack

| Layer | Technology |
|---|---|
| Language | Go |
| Browser Automation | Playwright (via `playwright-go`) |
| Job Queue | Apache Kafka *(Phase 3+)* |
| Raw Storage | MongoDB |
| Normalization | Go ETL Service |
| Structured Store | PostgreSQL |
| Cache | Redis *(Phase 4+)* |
| API | Go + Chi Router |
| Frontend | React + TypeScript + Tailwind |
| Frontend Deploy | Vercel |
| Monitoring | Prometheus *(Phase 6 Step 1)* + Grafana *(Phase 6 Step 2)* |
| Logging | `slog` structured JSON *(Phase 6 Step 3)* |
| Containerization | Docker + Docker Compose |

---

## Project Roadmap

### Phase 1 — Foundation & First Scraper
> Goal: Prove you can scrape one airline and store raw data in MongoDB. No queue, no pipeline, no extra infrastructure. Just get data flowing.

> ⚠️ **Phases 1 and 2 intentionally have NO Kafka, NO Redis, NO Prometheus. Add those only in their designated phases. Keep it simple until the scraper is proven.**

**✅ Definition of Done:** Running one command scrapes United Airlines for a given route and date, and I can see the raw JSON stored correctly in MongoDB.

**Step 1 — Project Setup**
- [x] Initialize Go project (`go mod init cloudmilesscouter`)
- [x] Set up folder structure (see below)
- [x] Install dependencies: `playwright-go`, MongoDB driver, Chi router
- [x] Set up Docker Compose with MongoDB only (nothing else yet)
- [x] Verify MongoDB boots and is reachable from Go

**Step 2 — Pick First Airline**
- [x] Start with United Airlines
- [x] Manually browse their award search to understand the flow
- [x] Note what inputs are required (origin, destination, dates, cabin)

**Step 3 — Reverse Engineer the Airline Site**
- [x] Open United award search in Chrome DevTools → Network tab
- [x] Perform a manual award search and watch XHR/Fetch requests
- [x] Internal API found (`FetchFlights`) — but United runs Akamai Bot Manager, so calling it directly via `net/http` isn't viable; drove it through a real Playwright browser instead and captured the response.
- [ ] ~~If no API found → fall back to Playwright browser automation~~ (n/a — API was found, Playwright used for bot-detection reasons instead)

**Step 4 — Build the Scraper**
- [x] Write Go + Playwright code to automate the search
- [x] Handle dynamic page load wait times
- [x] Grab raw HTML or JSON response
- [x] Add structured logging for success/failure

**Step 5 — Store Raw Data in MongoDB**
- [x] Connect Go scraper to local MongoDB instance
- [x] Store raw response as-is (no cleaning yet)
- [x] Include metadata: airline name, route, date scraped, raw payload

**Step 6 — Test & Validate**
- [x] Run scraper manually end to end
- [x] Verify data appears correctly in MongoDB
- [x] Handle basic errors: timeout, blocked, no results
- [x] Save a sample raw JSON document — you'll need it for Phase 2

---

### Phase 2 — Normalization Pipeline
> Goal: Transform messy raw MongoDB data into clean structured data in PostgreSQL using a Go ETL service. No dbt — keep it pure Go so you stay in one language and learn the ETL pattern directly.

**✅ Definition of Done:** Running the ETL service reads raw United data from MongoDB and writes clean, queryable rows into PostgreSQL with a consistent schema.

**Step 1 — Set Up PostgreSQL**
- [x] Add PostgreSQL to Docker Compose
- [x] Design normalized schema: `airlines`, `routes`, `awards`, `cabins`
- [x] Verify connection from Go

**Step 2 — Build Go ETL Service**
- [x] Write a Go service that reads raw documents from MongoDB
- [x] Parse out the key fields: airline, origin, destination, date, cabin, points cost
- [x] Handle missing or malformed fields gracefully
- [x] Write clean rows into PostgreSQL

**Step 3 — Handle Schema Differences Per Airline**
- [x] Each airline's raw data will look different — write a parser per airline
- [x] United parser → normalized struct → Postgres insert
- [x] Design the parser interface so adding new airlines later is just adding a new file

**Step 4 — Test & Validate**
- [x] Run full pipeline: scrape → MongoDB → ETL → PostgreSQL
- [x] Query PostgreSQL directly and verify clean data
- [x] Confirm schema handles data from United cleanly
- [x] Document the normalized schema — Phase 4 API depends on it (see `docs/schema.md`)

---

### Phase 3 — Queue & Worker Pool
> Goal: Add Kafka job queue and parallel worker pool so multiple airlines can be scraped concurrently.

**✅ Definition of Done:** Triggering one search dispatches 3+ airline jobs into Kafka, workers scrape them in parallel, and all results land in MongoDB.

**Step 1 — Set Up Kafka**
- [x] Add Kafka + Zookeeper to Docker Compose
- [x] Create topic: `scrape.jobs` (created on boot by a one-shot `kafka-init` service; 3 partitions, replication-factor 1). Phase 6 Step 4 adds `scrape.jobs.dlq` (1 partition) to the same one-shot.
- [x] Verify Kafka boots and accepts messages

**Step 2 — Build Job Producer**
- [x] Write Go code to dispatch one job per airline into Kafka topic
- [x] Job payload: airline ID, route, dates

**Step 3 — Build Worker Pool**
- [x] Write Go worker pool (5–10 concurrent workers)
- [x] Each worker pulls a job from Kafka
- [x] Each worker spawns a Playwright browser instance
- [x] Worker scrapes airline, stores raw result in MongoDB
- [x] Worker acknowledges job completion back to Kafkalet

**Step 4 — Add More Airlines**
- [x] Add American Airlines parser — anonymous award search, reads the `ng-state` SSR JSON from aa.com; scraper `internal/scraper/airlines/american.go`, parser `internal/etl/parsers/american.go`
- [x] Add Delta Airlines parser — anonymous "Shop with Miles" search; results page has no JSON, so the scraper extracts the DOM in-browser (`internal/scraper/airlines/delta.go`), parser `internal/etl/parsers/delta.go`
- [x] ~~Add Air Canada parser~~ — Aeroplan award search requires a login (no anonymous access), so swapped for **Alaska Airlines**: anonymous "Use points" search via a deep-link URL, DOM extraction, and it runs headless. Scraper `internal/scraper/airlines/alaska.go`, parser `internal/etl/parsers/alaska.go`. Air Canada deferred to Phase 7.
- [x] Test all four running in parallel via worker pool — one `producer` search fans out to 4 jobs; the worker pool scrapes American, Delta and Alaska concurrently and all land in MongoDB → ETL → PostgreSQL. (United's login expired mid-Phase 3 — see the note below Step 6 for the fix and why United needs a login at all, unlike the other three.)

**Step 5 — Add Circuit Breakers & Retry Logic**
- [x] If airline site is down, fail gracefully — in-memory per-airline circuit breaker (`internal/breaker`): after 5 consecutive failures for an airline (kept above `MAX_SCRAPE_ATTEMPTS` so one job's own retry run can't trip it), its jobs are dropped fast for a 60s cooldown instead of launching a browser; the producer re-dispatches on its next cadence. (Phase 6 Step 4 adds the half-open probe state + metrics + config knobs.)
- [x] Re-queue failed jobs with exponential backoff — worker re-enqueues a failed scrape/store with an incremented `attempt` after `RETRY_BACKOFF_BASE` × 2ⁿ (capped 30s), up to `MAX_SCRAPE_ATTEMPTS` (3) tries, then (Phase 6 Step 4) writes it to the `scrape.jobs.dlq` dead-letter topic.
- [x] Log failure reason with structured logging — every failure/re-queue line carries a coarse `reason` (`timeout`, `blocked`, `browser`, `store`, `circuit_open`, `other`) via `slog`

**Step 6 — Test & Validate**
- [x] Trigger a search and verify all airlines scraped in parallel — one `producer -origin BOS -destination SFO -date 2026-12-20` fans out to 4 jobs; the 5-worker pool scrapes **all four** (United, American, Delta, Alaska) concurrently — `scrape started` for all within 3s, all `scrape succeeded` in 5–16s.
- [x] Verify all results land in MongoDB — all four wrote `data.flight_scrapes` docs (United ~65 KB, American ~716 KB, Delta ~8 KB, Alaska ~3 KB) with airline / route / `search_date` / `scraped_at` metadata.
- [x] Simulate a scraper failure and verify retry works — with United's login expired, its scrape timed out (30s Playwright); the worker re-queued it with an incremented `attempt` after exponential backoff (2s → 4s), gave up after `MAX_SCRAPE_ATTEMPTS` (3) with a structured `giving up` log, and the per-airline circuit breaker opened for a 60s cooldown — later United jobs deferred with `reason=circuit_open` without launching a browser. (United was subsequently fixed via `bootstrap-auto` — see the note below.)

> **United's expired login, and why it needs one at all:** unlike American/Delta/Alaska, United only shows award (miles) pricing to a signed-in MileagePlus account — confirmed live: an anonymous search prompts "You must be signed-in to see flight results with miles." Once a device is trusted, re-auth is password-only (no OTP) via `ensureLoggedIn`, so the only human-in-the-loop step is establishing that trust once. `scraper bootstrap-auto` (`internal/scraper/airlines/united.go`, `internal/mailotp`) does that setup end to end with no one at the browser: it fills the MileagePlus number/password and, when United emails a verification code, reads it via IMAP instead of waiting on stdin. `scraper bootstrap` (manual, stdin-driven) still exists as a fallback. United's login is a modal off a search page (the old `/mileageplus/login` URL 404s now, and the modal container has no `role="dialog"` — it's `.atm-c-modal__body`); the flow — optional "Continue shopping?" interstitial → email-first *or* remembered-password step → optional OTP — and the field ids (`#password`, `#MPIDEmailField`) were reverse-engineered live. Each step is submitted by pressing Enter in the field (implicit form submission) rather than hunting for the buttons, which have no stable id and re-render on input. The **OTP step's markup is still unverified** (it sits behind a password submit). `bootstrap-auto` runs a 3-minute loop — trigger the search, then handle whichever appears first, the sign-in modal or the `FetchFlights` payload — and on failure logs a screenshot plus a dump of the page's form controls so a fix lands in one iteration. **Verified working:** a run fills the password, presses Enter, hits no OTP (device trusted from a prior login), and confirms miles authorization by pulling a ~1.9 MB `FetchFlights` payload — then United scrapes clean alongside the other three in the worker pool.
>
> MileagePlus only offers email or SMS for its second factor (no third-party authenticator app support, confirmed — [travelersunited.org](https://www.travelersunited.org/two-factor-authentication-too-much-for-uniteds-best-customers/)), so `bootstrap-auto` needs read access to *an* inbox. To avoid handing the scraper a credential for the real inbox, it's pointed at a **dedicated throwaway Gmail account** instead:
> 1. Create a new Gmail account used for nothing else.
> 2. On the *real* account (the one on the MileagePlus login), Settings → Forwarding and POP/IMAP → add the throwaway address as a forwarding address, confirm it via the code Gmail sends there.
> 3. Add a filter on the real account: `from:(united.com)` → Forward it to the throwaway address.
> 4. On the throwaway account, turn on 2-Step Verification, then generate an App Password (myaccount.google.com/apppasswords).
> 5. Set `.env`'s `GMAIL_ADDRESS` / `GMAIL_APP_PASSWORD` to the throwaway account's address and that App Password.
>
> The throwaway inbox then holds nothing but forwarded United mail — if its App Password ever leaked, the blast radius is United OTP codes with a ~90s validity window, not the real inbox.

**Running the binaries.** `config.Load()` reads `.env` from the current working
directory, and the `UNITED_PROFILE_DIR` / `AMERICAN_PROFILE_DIR` / etc. defaults
(`.united-profile`, …) are relative too. Run `producer` / `worker` / `etl` /
`scraper` / `api` from the repo root, or set `MONGO_URI`, `POSTGRES_URI`,
`KAFKA_BROKERS`, `REDIS_ADDR`, the `*_PROFILE_DIR` vars, and the United/Gmail
creds explicitly in the real environment — otherwise you silently get the
localhost defaults and an empty United credential. `etl` and `api` both use
`REDIS_ADDR` (default `localhost:6379`) for the search cache, but neither fails
if Redis is down.

**Metrics (Phase 6).** The `api` exposes Prometheus metrics at `/metrics` on
`API_PORT`; the `worker` runs a dedicated `/metrics` listener on `METRICS_ADDR`
(default `:2112`); the `etl` pushes its counters to `PUSHGATEWAY_URL` (default
`http://localhost:9091`) once per run. `docker compose -f
docker/docker-compose.yml up -d prometheus pushgateway grafana` brings up
Prometheus on `localhost:9090` and Grafana on `localhost:3000` — Prometheus's
targets and the metric names are in [`docs/api.md`](docs/api.md#get-metrics). None
of the three binaries fail if the Pushgateway or a scrape target is unreachable.

**Logging (Phase 6 Step 3).** Every Go binary logs one JSON object per line to
stderr, each carrying a `service` field (`api`, `worker`, `etl`, `scraper`,
`producer`). `LOG_LEVEL` (`debug` | `info` | `warn` | `error`, default `info`)
and `LOG_FORMAT` (`json` default, or `text` for readable local runs) control it.
`internal/logging.Setup` wires this up as the `slog` default in each `main`, so
lines from the internal packages carry the same shape. Key events: the scrapers
log `scrape started` / `scrape succeeded` / `scrape failed`; the worker logs
`job started` / `job done`, retries (`re-queueing job after backoff`,
`job failed permanently, giving up` → `dead-lettered job`), and `circuit opened
for airline` / `circuit half-open, probing airline`; the API logs one `request`
line per call.

**Resilience (Phase 6 Step 4).** Per-airline circuit breaker (`internal/breaker`,
closed → open → half-open): `CIRCUIT_BREAKER_THRESHOLD` (5) consecutive failures
open it, jobs are then dropped fast for `CIRCUIT_BREAKER_COOLDOWN` (60s), and the
first job after the cooldown probes as half-open — success closes it, failure
re-opens it for another cooldown. The worker exposes the state as
`scrape_circuit_state{airline}` (0/1/2). Permanently failed jobs (retries
exhausted) are written to the `scrape.jobs.dlq` Kafka topic with their failure
reason and last error, and counted by `dlq_messages_total{airline,reason}`;
inspect it with `kafka-console-consumer ... --topic scrape.jobs.dlq
--from-beginning`. Every binary handles SIGINT/SIGTERM gracefully — the API drains
in-flight requests, the worker finishes in-flight scrapes before exiting, the
producer stops its dispatch loop, and the ETL cancels mid-run (transactional, so
it rolls back).

**Validating it (Phase 6 Step 5).** The worker reads `SCRAPER_FORCE_FAILURE` (a
comma-separated airline list) and fails those scrapes immediately without a
browser, so an outage can be simulated on the live stack. Run the worker with it
set and logging to a file, then `scripts/validate_resilience.sh` (see its header
for prereqs and knobs; `--half-open` also waits out the cooldown to check the
probe). `go test -tags e2e -run TestResilience ./cmd/worker` covers the
closed→open→half-open walk and the DLQ write deterministically against a live
Kafka.

**Dashboards (Phase 6 Step 2).** Grafana at `localhost:3000` opens straight onto
the **CloudMilesScouter** dashboard (anonymous Viewer; admin `admin`/`admin` for
edits). Its Prometheus datasource and the dashboard JSON are provisioned from
`docker/grafana/` on boot — no manual setup. Panels are grouped API performance /
scraper health / queue depth + ETL; they stay empty until the `api` and `worker`
are running on the host for Prometheus to scrape.

**Running the API.** Full endpoint reference for the frontend is in
[`docs/api.md`](docs/api.md); `scripts/smoke_api.sh` curl-tests every endpoint
plus the cache behaviour against a live stack. `go run ./cmd/api` from the repo
root starts the REST API on
`API_PORT` (default `8080`); it connects to Postgres (`POSTGRES_URI`) on boot and
exits if that fails. `POST /api/scrape` enqueues onto Kafka (`KAFKA_BROKERS`), but
the writer connects lazily on first use, so a missing broker only fails that one
endpoint, not startup. `GET /healthz` returns `{"status":"ok"}`. Browser callers
must be listed in `CORS_ALLOWED_ORIGINS` (comma-separated; default
`http://localhost:5173` for the Phase 5 Vite dev server — add the deployed
frontend's `https://…vercel.app` origin here when serving it); each client IP gets
`RATE_LIMIT_PER_MINUTE` requests/min (default 120, `0` disables the limiter).

**Running the frontend.** `cd frontend && npm install && npm run dev` starts the
Vite dev server on `http://localhost:5173` (the API's default CORS origin). It
reads `VITE_API_BASE_URL` (see `frontend/.env.example`; falls back to
`http://localhost:8080`), so the Go API needs to be running too. `npm test` runs
the Vitest suite, `npm run build` type-checks and builds to `dist/`. See
[`frontend/README.md`](frontend/README.md) — including its **Deploy (Vercel)**
section (project Root Directory `frontend`, `VITE_API_BASE_URL`, and the
local-API-tunnel caveat).

Results are cached in Redis (`REDIS_ADDR`, default `localhost:6379`) for 1 hour,
keyed by route + date + cabin; the ETL drops a route's cached results when it
writes a fresh scrape for it. If Redis is unreachable the API still serves,
reading Postgres on every call.

`GET /api/search?origin=BOS&destination=SFO&date=2026-12-20&cabin=business` — the
award-search endpoint. `origin`/`destination` (3-letter airport or metro codes,
matched against the *searched* route codes, not the flight's own airports),
`date` (`YYYY-MM-DD`) are required; `cabin` (`economy` | `premium_economy` |
`business` | `first`) is optional and omitting it returns all cabins. Returns a
JSON array of award options sorted cheapest-first (`points_cost` asc, then
duration), `200 []` when nothing matches, `400 {"error": ...}` on a bad
parameter. Served from the Redis cache on a hit (see above); a miss reads
Postgres and back-fills the cache with a 1h TTL.

`GET /api/airlines` — JSON array of `{code, name}` for every airline present in
the awards data, ordered by name.

`GET /api/routes` — JSON array of `{origin, destination, award_count,
last_scraped}` for routes that have award data, most-populated first (there is no
popularity signal in the schema, so `award_count` stands in for it).

`POST /api/scrape` — trigger a scrape. JSON body `{origin, destination, date,
airlines?}`; `origin`/`destination` (3-letter codes) and `date` (`YYYY-MM-DD`)
required, `airlines` optional (defaults to `united, american, delta, alaska`).
Dispatches one `scrape.jobs` message per airline and returns `202 {"dispatched":
[...], ...}`; `400 {"error": ...}` on a bad body/param, `502` if the dispatch to
Kafka fails. It does not wait for the scrape — the worker pool picks the jobs up.

`go test -tags integration ./internal/storage ./internal/api` runs `SearchAwards`,
`ListAirlines` and `ListRoutes` against a real Postgres, plus the `storage.Cache`
round-trip and the `/api/search` cache-hit path against a real Redis (needs the
compose stack up); each test seeds and cleans up its own rows/keys.

**End-to-end smoke test.** `go test -tags e2e ./cmd/worker` runs the whole pipeline — producer → Kafka → worker `process` → MongoDB → ETL → PostgreSQL — with a stub scraper and stub parser, so no browser or live airline site is involved. Needs the compose stack up and `cmd/worker` **not** running (the test joins the real `scrape-workers` group and drains any queued jobs as a setup step). It re-runs the ETL over all real MongoDB docs too, so it also catches ETL regressions on real data.

**DOM-extractor tests.** `go test -tags browser ./internal/scraper/airlines` loads a hand-built minimal grid fixture (`testdata/samples/{delta,alaska}_grid_fixture.html`) into headless Chromium via `page.SetContent` and runs the real `DeltaExtractJS` / `AlaskaExtractJS` against it — the only coverage the in-browser extractors have. Needs `playwright install chromium` once; no compose stack.

---

### Phase 4 — REST API
> Goal: Expose the data via a Go REST API so the frontend can query results.

**✅ Definition of Done:** `GET /api/search?origin=JFK&destination=LAX&date=2025-06-01&cabin=business` returns a sorted JSON list of award options from PostgreSQL.

**Step 1 — Set Up Chi Router**
- [x] Initialize Chi router in Go — `internal/api/router.go` (`NewRouter`), served by `cmd/api/main.go` with explicit `http.Server` timeouts + SIGINT/SIGTERM graceful shutdown; `GET /healthz` liveness probe. No `/api/*` endpoints yet (Steps 2–3).
- [x] Add middleware: logging, CORS, rate limiting — `RequestID` → `RealIP` → structured `slog` request logger → `Recoverer` → CORS (`go-chi/cors`, origins from `CORS_ALLOWED_ORIGINS`, default `http://localhost:5173`) → per-IP rate limit (`go-chi/httprate`, `RATE_LIMIT_PER_MINUTE`, default 120; 0 disables).

**Step 2 — Build Search Endpoint**
- [x] `GET /api/search?origin=JFK&destination=LAX&date=2025-01-15&cabin=business` — `internal/api/search.go`, mounted under `/api` in `router.go`. `origin`/`destination`/`date` required, `cabin` optional (omit → all cabins); bad params return `400 {"error": ...}`.
- [x] Query PostgreSQL for matching award results — `storage.SearchAwards` (`internal/storage/awards_query.go`): one join over `awards`/`airlines`/`routes`/`cabins`, filtered on the searched route + `search_date` (+ cabin), using the `(route_id, search_date)` index.
- [x] Check Redis cache first before hitting Postgres — done in **Step 4**: `handleSearch` calls `storage.Cache.GetSearch` before `storage.SearchAwards` and back-fills on a miss.
- [x] Return sorted JSON response — array ordered by `points_cost ASC`, then `duration_minutes ASC`; no match returns `200 []`.

**Step 3 — Build Supporting Endpoints**
- [x] `GET /api/airlines` — `internal/api/airlines.go` + `storage.ListAirlines`; the airlines present in the awards data (`SELECT code, name FROM airlines`), ordered by name.
- [x] `GET /api/routes` — `internal/api/routes.go` + `storage.ListRoutes`; routes that have award data, with an `award_count` and `last_scraped`, most-populated first. No popularity signal exists in the schema, so "popular" ≈ how much data a route has.
- [x] `POST /api/scrape` — `internal/api/scrape.go`; validates the search and dispatches one `queue.ScrapeJob` per airline onto Kafka (same path as `cmd/producer`), returns `202` with the dispatched list. Fire-and-forget — the worker pool must be running for the scrape to actually happen.

**Step 4 — Add Redis Caching**
- [x] Add Redis to Docker Compose — `redis:7-alpine` service on `localhost:6379`, `redis-data` volume, `redis-cli ping` healthcheck.
- [x] Cache search results for 1 hour — `internal/storage/cache.go` (`storage.Cache`). `GET /api/search` reads Redis first under `search:{origin}:{destination}:{date}:{cabin}` (`cabin` = `any` when unset), on a miss queries Postgres and writes the JSON back with a 1h TTL. A `REDIS_ADDR` that is unreachable at startup is logged and the API runs cache-less (a nil `*storage.Cache` is a no-op), and a Redis error on any single request falls through to Postgres — the cache degrades to slower, never broken.
- [x] Invalidate cache when new scrape completes — the ETL owns invalidation, since `POST /api/scrape` is fire-and-forget and fresh rows only land when `etl.Run` → `storage.WriteAwards` commits. After the write, `Run` calls `cache.InvalidateRoute` for every route+date it rewrote (its `clearKeys`), deleting all cabin variants. Best-effort: a Redis failure there just leaves those keys to expire on their TTL. `cmd/etl` builds the cache from `REDIS_ADDR`; Redis being down doesn't fail the ETL.

**Step 5 — Test & Validate**
- [x] Test all endpoints with Postman or curl — `scripts/smoke_api.sh` curls every endpoint against a live API: `/healthz`, `/api/search` (all-cabins, per-cabin, `200 []` for an empty route, and `400` on each bad param), `/api/airlines`, `/api/routes` (both with order assertions), `POST /api/scrape` (`202` + dispatched list, `400` on bad JSON / unknown field / missing param / all-blank airlines), CORS preflight (allowed vs unlisted origin), and — with `--rate` — the per-IP `429`. 30/30 pass against the compose stack.
- [x] Verify cache hit/miss behavior — the script clears the Redis key, confirms a miss back-fills it with a ~3600s TTL, a repeat request returns the identical body, and a `go run ./cmd/etl` pass invalidates the key. Graceful degradation checked separately: with `REDIS_ADDR` pointed at a dead port the API logs `redis unreachable, running without a search cache` and still serves `/api/search` `200` from Postgres.
- [x] Document API endpoints for frontend integration — `docs/api.md`: base URL / env knobs, conventions (auth, error envelope, CORS, rate limiting, `X-Request-Id`), and per-endpoint params, example request/response, field tables, and status codes.

---

### Phase 5 — Frontend UI
> Goal: Build a clean React frontend that calls the REST API and displays award results.

**✅ Definition of Done:** Searching a route in the browser returns a live results table pulled from the Go API, deployed and accessible on Vercel. *(The Vercel deploy is live; live API data is served through a tunnel to the local API until Phase 7 hosts it — see Step 5.)*

**Step 1 — Initialize React Project**
- [x] Create React + TypeScript + Tailwind project with Vite — `frontend/`, Vite 7 + React 19 + Tailwind 4 (`@tailwindcss/vite`, no config file). Dev server pinned to `:5173` to match the API's default `CORS_ALLOWED_ORIGINS`. Vitest + Testing Library for tests.
- [x] Set up folder structure: components, pages, hooks, utils — `frontend/src/{components,pages,hooks,utils}/`, each with one real file: `utils/api.ts` (REST client + types), `hooks/useApiHealth.ts`, `components/ApiStatusBadge.tsx`, `pages/HomePage.tsx`.
- [x] Connect to local Go API via environment variable — `VITE_API_BASE_URL` (`frontend/.env.example`; falls back to `http://localhost:8080`). `utils/api.ts` wraps every endpoint in `docs/api.md` and unwraps the `{"error": ...}` envelope into an `ApiError`. `HomePage` shows a live `GET /healthz` reachability badge. `frontend/src/utils/api.test.ts` covers URL/query building and error handling with `fetch` mocked.

**Step 2 — Build Search Form**
- [ ] Origin airport input (with autocomplete)
- [ ] Destination airport input (with autocomplete)
- [ ] Date picker
- [ ] Cabin class selector (Economy, Business, First)
- [ ] Submit button that triggers scrape + search

**Step 3 — Build Results Table**
- [x] Display airline, route, cabin, points cost, availability — `frontend/src/components/ResultsTable.tsx`, one row per `GET /api/search` result: airline, route (`flight_origin→flight_destination` + stops + depart time), cabin, duration, points, `award_type`, taxes. The schema has no seats-remaining field, so `award_type` (`saver` / `dynamic` / …) stands in for "availability".
- [x] Sort by points cost (ascending by default) — every column header is a sort toggle; the table opens on `points_cost` ascending (matching the API's own order), ties broken by points.
- [x] Filter by airline, cabin class, alliance — three dropdowns above the table; options are derived from the current results. Alliance isn't in the API response, so it's resolved via a bundled `frontend/src/utils/airlines.ts` map (the four scraped carriers → Star Alliance / oneworld / SkyTeam).
- [x] Loading state while scrape runs — `HomePage` shows "Searching…" while `useAwardSearch` is in flight (`POST /api/scrape` then `GET /api/search`); the table renders only on success, with an explicit empty state when the route/date has no awards.

**Step 4 — Polish UI**
- [x] Responsive design (mobile + desktop) — `max-w-4xl` shell with `sm:` padding steps; the search form already stacks 1-col below `sm`; the results table keeps its columns and scrolls inside an `overflow-x-auto` wrapper (`min-w-[44rem]`, `whitespace-nowrap`) instead of squashing on mobile.
- [x] Empty state when no results found — `frontend/src/components/EmptyState.tsx`: shown when `GET /api/search` succeeds with `[]`, names the searched route/date/cabin and notes a scrape may still be running. Distinct from `ResultsTable`'s "no rows match these filters" message.
- [x] Error state when scrape fails — `frontend/src/components/ErrorState.tsx`: a blocking panel with a "Try again" button (re-runs the last search) when `GET /api/search` itself fails. A failed `POST /api/scrape` dispatch is non-blocking — `useAwardSearch` exposes it as `scrapeWarning`, rendered as an amber notice above the (stale) results.
- [x] CloudMilesScouter branding — `App.tsx` header with a plane wordmark + tagline and a footer (non-commercial disclaimer); `frontend/public/favicon.svg` + `<link>` and a `<meta name="description">` in `index.html`.

**Step 5 — Deploy to Vercel**
- [x] Push frontend to GitHub repo — `frontend/` committed on `feature/phase-5-implement-frontend-ui`; Node pinned via `frontend/.nvmrc` (22) + `package.json` `engines`.
- [x] Connect GitHub repo to Vercel — monorepo, so the project's **Root Directory is `frontend`**; Vite preset auto-detects `npm run build` → `dist/`. Steps in [`frontend/README.md`](frontend/README.md#deploy-vercel).
- [x] Set environment variable for API URL — `VITE_API_BASE_URL` in the Vercel project settings (build-time inlined).
- [x] Deploy and verify live — the Vercel build + hosting is live. **Live award data needs a reachable API:** the Go API is local-only until Phase 7, so `VITE_API_BASE_URL` points at a `cloudflared`/`ngrok` tunnel to it (and that origin is added to the API's `CORS_ALLOWED_ORIGINS`). Without the tunnel the site loads but shows "API unreachable" / `ErrorState`.

---

### Phase 6 — Observability & Resilience
> Goal: Make the system production-grade with monitoring, alerting, and robust error handling.

**✅ Definition of Done:** Grafana dashboard shows scraper health, queue depth, and API latency in real time. Circuit breakers fire correctly when an airline is down. *(Met — validated in Step 5 via `scripts/validate_resilience.sh` and `go test -tags e2e -run TestResilience ./cmd/worker`.)*

**Step 1 — Add Prometheus**
- [x] Add Prometheus to Docker Compose — `prom/prometheus` (`:9090`) + `prom/pushgateway` (`:9091`) services; scrape config in `docker/prometheus/prometheus.yml`. The `api` and `worker` run on the host so Prometheus reaches them via `host.docker.internal`; the `etl` is a batch job, so it pushes to the Pushgateway on completion and Prometheus scrapes that.
- [x] Instrument Go API with Prometheus metrics — all collectors live in one place, `internal/metrics`. The API serves `/metrics` on `API_PORT` from an outer `ServeMux` ahead of the chi middleware (no CORS / rate limit / request log, and `/metrics` isn't self-counted); a `metricsRecorder` middleware records every other request. The worker runs its own `/metrics` listener on `METRICS_ADDR` (default `:2112`); `cmd/etl` calls `metrics.PushETL(PUSHGATEWAY_URL)` after `etl.Run`.
- [x] Track: scraper success rate, parse failures, queue lag, API latency, cache hit rate — `http_requests_total` + `http_request_duration_seconds` (by method + chi route pattern), `search_cache_requests_total{result}`, `scrape_attempts_total` / `scrape_failures_total{airline,reason}` / `scrape_duration_seconds` / `scrape_empty_results_total`, `kafka_consumer_lag` (worker gauge, refreshed every 15s from the reader stats), and `etl_parse_failures_total{airline}` (+ `etl_docs_processed_total`, `etl_awards_written_total`). Grafana dashboards over these are Step 2. Step 4 adds `scrape_circuit_state{airline}` and `dlq_messages_total{airline,reason}`.

**Step 2 — Add Grafana**
- [x] Add Grafana to Docker Compose — `grafana/grafana:11.4.0` (`:3000`), `depends_on: prometheus`, `grafana-data` volume. Anonymous access is on (`GF_AUTH_ANONYMOUS_ENABLED`, Viewer role) so the dashboard opens with no login; admin is `admin`/`admin` for edits.
- [x] Connect Grafana to Prometheus — provisioned, not click-ops: `docker/grafana/provisioning/datasources/prometheus.yml` wires the `http://prometheus:9090` datasource (fixed `uid: prometheus`, default, read-only).
- [x] Build dashboard: scraper health, queue depth, API performance — `docker/grafana/dashboards/cloudmilesscouter.json` (`uid: cloudmilesscouter`), file-provisioned via `docker/grafana/provisioning/dashboards/dashboards.yml`, 15s refresh. Three rows: **API performance** (request rate + p95 latency by route, 5xx ratio, `/api/search` cache hit rate, rate by status), **Scraper health** (per-airline success rate, failures by `reason`, p95 scrape duration, empty-result scrapes), **Queue depth & ETL** (`kafka_consumer_lag`, ETL parse failures / docs processed / awards written from the last run).

**Step 3 — Structured Logging**
- [x] Add structured JSON logging across all Go services — `internal/logging`
  (`Setup(service, level, format)`) installs a `slog` JSON handler as the process
  default, tagged with a `service` field (`api` / `worker` / `etl` / `scraper` /
  `producer`); each `cmd/*/main.go` calls it straight after `config.Load()`.
  `LOG_LEVEL` (default `info`) and `LOG_FORMAT` (`json` default; `text` for local
  dev) are config knobs. The internal packages already logged through the `slog`
  default, so they emit JSON unchanged — the rest was swapping the leftover
  `log.Fatalf` / `log.Println` startup lines in the mains for `slog`.
- [x] Log: scrape start/end, errors, retries, job completions — already emitted
  by the scrapers (`scrape started` / `scrape succeeded` / `scrape failed`, with
  `airline` + `duration_ms`) and the worker (`job started` / `job done` /
  `scrape failed` with `reason`, `re-queueing job after backoff`, `job failed
  permanently, giving up`); the producer logs `job dispatched` per airline. Now
  they land as JSON with a `service` tag.

**Step 4 — Harden Resilience**
- [x] Circuit breaker per airline scraper — `internal/breaker` reworked from a two-state (closed/open) into a three-state machine: after `CIRCUIT_BREAKER_THRESHOLD` (5) consecutive failures for an airline the breaker **opens** and jobs are dropped fast for `CIRCUIT_BREAKER_COOLDOWN` (60s); the first job after the cooldown is let through as a **half-open** probe (further jobs still blocked) — it closes the breaker on success or re-opens it for a fresh cooldown on failure, so a still-down site is never hammered the instant its cooldown lapses. The worker publishes the per-airline state as the `scrape_circuit_state` gauge and logs `circuit opened` / `circuit half-open, probing`.
- [x] Dead letter queue in Kafka for permanently failed jobs — new `scrape.jobs.dlq` topic (created on boot by `kafka-init` alongside `scrape.jobs`, 1 partition). When a job exhausts its `MAX_SCRAPE_ATTEMPTS` (3) retries the worker writes a `queue.DeadLetterJob` there — the original job plus `reason`, `attempts`, last `error`, and `failed_at` — instead of only logging "giving up", and bumps `dlq_messages_total{airline,reason}`. Poison (undecodable) messages are still just logged and dropped. Inspect the queue with `docker compose -f docker/docker-compose.yml exec kafka kafka-console-consumer --bootstrap-server localhost:9092 --topic scrape.jobs.dlq --from-beginning`.
- [x] Graceful shutdown handling in all Go services — `cmd/api` (already) drains in-flight requests via `http.Server.Shutdown`. `cmd/worker` (already) stops fetching, lets in-flight scrapes finish and store, then `wg.Wait`s. Added: `cmd/producer` and `cmd/etl` and `cmd/scraper` now install `signal.NotifyContext(SIGINT/SIGTERM)` — the producer stops its dispatch loop, the ETL cancels mid-run (`storage.WriteAwards` is transactional, so it rolls back), and the scraper skips the store if interrupted after the scrape. Post-scrape Mongo writes use a bounded `context.Background()` so a Ctrl-C never throws away a completed 30–45s scrape.

**Step 5 — Test & Validate**
- [x] Simulate airline site going down — verify circuit breaker fires — the worker's `SCRAPER_FORCE_FAILURE=<airline>` knob (`internal/config`, `cmd/worker`) fails that airline's scrape instantly, no browser, straight into the real retry/breaker/DLQ path. `scripts/validate_resilience.sh` fires repeated scrapes and confirms `scrape_circuit_state{airline}` goes to 1 (open), jobs are then dropped fast (`circuit open for airline, dropping job`), and `--half-open` waits out the cooldown and checks the single probe (`circuit half-open, probing airline`) re-opens it while the site is still down. `go test -tags e2e -run TestResilience ./cmd/worker` asserts the same closed→open→half-open walk deterministically against a live Kafka.
- [x] Check Grafana dashboard updates in real time — with the worker + API on the host, `up{job="worker"}` / `up{job="api"}` are `1` and the **CloudMilesScouter** dashboard's Scraper-health row tracks the forced failures live (per-airline success rate drops, failures-by-`reason` and `scrape_circuit_state` climb) on its 15s refresh; `validate_resilience.sh` asserts Prometheus has the tripped-breaker series and prints the dashboard URL to eyeball.
- [x] Verify dead letter queue catches unrecoverable failures — once a forced-failure job exhausts `MAX_SCRAPE_ATTEMPTS` the worker writes a `queue.DeadLetterJob` to `scrape.jobs.dlq` (`reason`, `attempts`, `error`, `failed_at`) and bumps `dlq_messages_total{airline,reason}`; the script consumes the topic and validates the message shape, the e2e test asserts the message lands with the right fields.

---

### Phase 7 — Scale & Polish
> Goal: Add remaining airlines, clean up the codebase, and prepare for open source release.

**✅ Definition of Done:** Single `docker-compose up` boots the entire system. All 41 airlines are supported. Project is published on GitHub and documented for contributors.

**Step 1 — Add Remaining Airlines**
- [ ] Add all remaining major carriers (up to 41 total)
- [ ] Test each parser individually before adding to pool

**Step 2 — Docker Compose Everything**
- [ ] Ensure all services start with a single `docker-compose up`
- [ ] Add health checks for each container
- [ ] Write setup documentation

**Step 3 — Performance Optimization**
- [ ] Profile Go scrapers for bottlenecks
- [ ] Tune Kafka consumer group settings
- [ ] Optimize PostgreSQL indexes for common queries

**Step 4 — Open Source Prep**
- [ ] Clean up code and add comments
- [ ] Write contribution guidelines
- [ ] Add license file (MIT recommended)
- [ ] Publish to GitHub

**Step 5 — Future Ideas**
- [ ] Date flexibility / calendar view
- [ ] Price drop alerts via email or SMS
- [ ] Multi-leg trip optimizer
- [ ] gRPC upgrade for API
- [ ] Cloud deployment (AWS / GCP)

---

## Folder Structure

Current tree (through Phase 6 Step 5 — validation: one script + one e2e test).

```
cloudmilesscouter/
├── frontend/                  # Phase 5 — React + TS + Tailwind (Vite), deployed on Vercel
│   ├── index.html            # favicon + meta description
│   ├── package.json          # "engines": node >=20.19
│   ├── .nvmrc                 # 22 — Vercel + local Node version
│   ├── vite.config.ts
│   ├── tsconfig*.json
│   ├── .env.example           # VITE_API_BASE_URL (+ Vercel prod note)
│   ├── public/favicon.svg
│   └── src/
│       ├── main.tsx
│       ├── App.tsx            # app shell — header wordmark/tagline + footer
│       ├── index.css          # @import "tailwindcss"
│       ├── components/
│       │   ├── ApiStatusBadge.tsx
│       │   ├── SearchForm.tsx      # Step 2 — route/date/cabin form
│       │   ├── ResultsTable.tsx    # Step 3 — sortable + filterable award table
│       │   ├── EmptyState.tsx      # Step 4 — no-results panel
│       │   └── ErrorState.tsx      # Step 4 — search-failed panel + retry
│       ├── pages/HomePage.tsx
│       ├── hooks/
│       │   ├── useApiHealth.ts
│       │   └── useAwardSearch.ts   # POST /api/scrape then GET /api/search; scrapeWarning
│       └── utils/
│           ├── api.ts         # REST client + response types
│           ├── airports.ts    # static airport list for the search autocomplete
│           └── airlines.ts    # static airline→alliance map for the results filter
├── cmd/
│   ├── scraper/main.go     # one-off: scrape a single route, store to Mongo
│   ├── producer/main.go    # dispatch one ScrapeJob per airline to Kafka
│   ├── worker/main.go      # worker pool: Kafka → scrape → Mongo (SCRAPER_FORCE_FAILURE outage knob, Phase 6 Step 5)
│   ├── worker/resilience_test.go  # -tags e2e — breaker closed→open→half-open + DLQ against live Kafka (Phase 6 Step 5)
│   ├── etl/main.go         # Mongo (raw) → Postgres (normalized)
│   └── api/main.go         # Chi REST API over Postgres (Phase 4)
├── internal/
│   ├── scraper/
│   │   ├── scraper.go          # Playwright session (persistent context)
│   │   └── airlines/
│   │       ├── airlines.go     # Scrapers registry + HasResults dispatch
│   │       ├── united.go
│   │       ├── american.go
│   │       ├── delta.go
│   │       └── alaska.go
│   ├── etl/
│   │   ├── etl.go
│   │   └── parsers/
│   │       ├── united.go
│   │       ├── american.go
│   │       ├── delta.go
│   │       └── alaska.go
│   ├── api/                    # Phase 4 REST API
│   │   ├── router.go           # Chi router + middleware chain
│   │   ├── search.go           # GET /api/search
│   │   ├── airlines.go         # GET /api/airlines
│   │   ├── routes.go           # GET /api/routes
│   │   └── scrape.go           # POST /api/scrape → Kafka
│   ├── storage/
│   │   ├── mongo.go
│   │   ├── postgres.go
│   │   ├── awards_query.go     # SearchAwards — read path for /api/search
│   │   ├── airlines_query.go   # ListAirlines — read path for /api/airlines
│   │   ├── routes_query.go     # ListRoutes — read path for /api/routes
│   │   └── cache.go            # Redis search cache: read-through + ETL invalidation
│   ├── queue/
│   │   ├── producer.go          # scrape.jobs + scrape.jobs.dlq dead-letter writer (Phase 6 Step 4)
│   │   └── consumer.go
│   ├── breaker/breaker.go        # per-airline circuit breaker: closed → open → half-open (Phase 6 Step 4)
│   ├── metrics/metrics.go     # Phase 6 — all Prometheus collectors + /metrics handler, worker listener, ETL push
│   ├── logging/logging.go     # Phase 6 Step 3 — slog JSON handler setup, per-service tag
│   ├── mailotp/imap.go        # IMAP OTP reader (United bootstrap-auto only)
│   └── config/config.go
├── docker/
│   ├── docker-compose.yml
│   ├── postgres/init/001_schema.sql
│   ├── prometheus/prometheus.yml  # Phase 6 Step 1 — scrape config (api, worker, pushgateway)
│   ├── grafana/                   # Phase 6 Step 2 — provisioned datasource + dashboard
│   │   ├── provisioning/{datasources,dashboards}/*.yml
│   │   └── dashboards/cloudmilesscouter.json
│   └── pgadmin/
├── docs/
│   ├── schema.md
│   └── api.md                 # REST API reference for the frontend
├── scripts/
│   ├── smoke_api.sh           # curl smoke test for the REST API
│   └── validate_resilience.sh # Phase 6 Step 5 — breaker / DLQ / Grafana-freshness checks on the live stack
├── testdata/samples/         # real scraped payloads, used by the parser tests
├── CLAUDE.md
├── .env                      # gitignored
├── go.mod
├── go.sum
└── README.md
```

---

## Machines
- **Development**: MacBook Pro M3 (24GB RAM) — write code here
- **Running scrapers**: Gaming PC (RTX 3080, 5800X3D, 32GB RAM) — heavy lifting here

---

## Notes
- All scraping is manually triggered — not automated background jobs
- Scraping airline sites may violate their Terms of Service — keep personal and non-commercial
- All infrastructure runs locally for now — cloud deployment is a Phase 7 goal
- Goal: learn industry-standard Big Tech patterns while building something real and useful
- See `CLAUDE.md` for Claude Code context and working instructions
