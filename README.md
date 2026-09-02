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
| Monitoring | Prometheus + Grafana *(Phase 6+)* |
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
- [x] Create topic: `scrape.jobs` (created on boot by a one-shot `kafka-init` service; 3 partitions, replication-factor 1)
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
- [x] If airline site is down, fail gracefully — in-memory per-airline circuit breaker (`internal/breaker`): after 5 consecutive failures for an airline (kept above `MAX_SCRAPE_ATTEMPTS` so one job's own retry run can't trip it), its jobs are dropped fast for a 60s cooldown instead of launching a browser; the producer re-dispatches on its next cadence
- [x] Re-queue failed jobs with exponential backoff — worker re-enqueues a failed scrape/store with an incremented `attempt` after `RETRY_BACKOFF_BASE` × 2ⁿ (capped 30s), up to `MAX_SCRAPE_ATTEMPTS` (3) tries, then drops it with a "giving up" log. Kafka dead-letter topic deferred to Phase 6.
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
`scraper` from the repo root, or set `MONGO_URI`, `POSTGRES_URI`,
`KAFKA_BROKERS`, the `*_PROFILE_DIR` vars, and the United/Gmail creds explicitly
in the real environment — otherwise you silently get the localhost defaults and
an empty United credential.

**End-to-end smoke test.** `go test -tags e2e ./cmd/worker` runs the whole pipeline — producer → Kafka → worker `process` → MongoDB → ETL → PostgreSQL — with a stub scraper and stub parser, so no browser or live airline site is involved. Needs the compose stack up and `cmd/worker` **not** running (the test joins the real `scrape-workers` group and drains any queued jobs as a setup step). It re-runs the ETL over all real MongoDB docs too, so it also catches ETL regressions on real data.

**DOM-extractor tests.** `go test -tags browser ./internal/scraper/airlines` loads a hand-built minimal grid fixture (`testdata/samples/{delta,alaska}_grid_fixture.html`) into headless Chromium via `page.SetContent` and runs the real `DeltaExtractJS` / `AlaskaExtractJS` against it — the only coverage the in-browser extractors have. Needs `playwright install chromium` once; no compose stack.

---

### Phase 4 — REST API
> Goal: Expose the data via a Go REST API so the frontend can query results.

**✅ Definition of Done:** `GET /api/search?origin=JFK&destination=LAX&date=2025-06-01&cabin=business` returns a sorted JSON list of award options from PostgreSQL.

**Step 1 — Set Up Chi Router**
- [ ] Initialize Chi router in Go
- [ ] Add middleware: logging, CORS, rate limiting

**Step 2 — Build Search Endpoint**
- [ ] `GET /api/search?origin=JFK&destination=LAX&date=2025-01-15&cabin=business`
- [ ] Query PostgreSQL for matching award results
- [ ] Check Redis cache first before hitting Postgres
- [ ] Return sorted JSON response

**Step 3 — Build Supporting Endpoints**
- [ ] `GET /api/airlines` — list all supported airlines
- [ ] `GET /api/routes` — list popular routes
- [ ] `POST /api/scrape` — manually trigger a scrape job

**Step 4 — Add Redis Caching**
- [ ] Add Redis to Docker Compose
- [ ] Cache search results for 1 hour
- [ ] Invalidate cache when new scrape completes

**Step 5 — Test & Validate**
- [ ] Test all endpoints with Postman or curl
- [ ] Verify cache hit/miss behavior
- [ ] Document API endpoints for frontend integration

---

### Phase 5 — Frontend UI
> Goal: Build a clean React frontend that calls the REST API and displays award results.

**✅ Definition of Done:** Searching a route in the browser returns a live results table pulled from the Go API, deployed and accessible on Vercel.

**Step 1 — Initialize React Project**
- [ ] Create React + TypeScript + Tailwind project with Vite
- [ ] Set up folder structure: components, pages, hooks, utils
- [ ] Connect to local Go API via environment variable

**Step 2 — Build Search Form**
- [ ] Origin airport input (with autocomplete)
- [ ] Destination airport input (with autocomplete)
- [ ] Date picker
- [ ] Cabin class selector (Economy, Business, First)
- [ ] Submit button that triggers scrape + search

**Step 3 — Build Results Table**
- [ ] Display airline, route, cabin, points cost, availability
- [ ] Sort by points cost (ascending by default)
- [ ] Filter by airline, cabin class, alliance
- [ ] Loading state while scrape runs

**Step 4 — Polish UI**
- [ ] Responsive design (mobile + desktop)
- [ ] Empty state when no results found
- [ ] Error state when scrape fails
- [ ] CloudMilesScouter branding

**Step 5 — Deploy to Vercel**
- [ ] Push frontend to GitHub repo
- [ ] Connect GitHub repo to Vercel
- [ ] Set environment variable for API URL
- [ ] Deploy and verify live

---

### Phase 6 — Observability & Resilience
> Goal: Make the system production-grade with monitoring, alerting, and robust error handling.

**✅ Definition of Done:** Grafana dashboard shows scraper health, queue depth, and API latency in real time. Circuit breakers fire correctly when an airline is down.

**Step 1 — Add Prometheus**
- [ ] Add Prometheus to Docker Compose
- [ ] Instrument Go API with Prometheus metrics
- [ ] Track: scraper success rate, parse failures, queue lag, API latency, cache hit rate

**Step 2 — Add Grafana**
- [ ] Add Grafana to Docker Compose
- [ ] Connect Grafana to Prometheus
- [ ] Build dashboard: scraper health, queue depth, API performance

**Step 3 — Structured Logging**
- [ ] Add structured JSON logging across all Go services
- [ ] Log: scrape start/end, errors, retries, job completions

**Step 4 — Harden Resilience**
- [ ] Circuit breaker per airline scraper
- [ ] Dead letter queue in Kafka for permanently failed jobs
- [ ] Graceful shutdown handling in all Go services

**Step 5 — Test & Validate**
- [ ] Simulate airline site going down — verify circuit breaker fires
- [ ] Check Grafana dashboard updates in real time
- [ ] Verify dead letter queue catches unrecoverable failures

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

Current tree (through Phase 3). `internal/api/` + `frontend/` land in Phase 4,
`monitoring/` in Phase 6.

```
cloudmilesscouter/
├── cmd/
│   ├── scraper/main.go     # one-off: scrape a single route, store to Mongo
│   ├── producer/main.go    # dispatch one ScrapeJob per airline to Kafka
│   ├── worker/main.go      # worker pool: Kafka → scrape → Mongo
│   └── etl/main.go         # Mongo (raw) → Postgres (normalized)
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
│   ├── storage/
│   │   ├── mongo.go
│   │   └── postgres.go
│   ├── queue/
│   │   ├── producer.go
│   │   └── consumer.go
│   ├── breaker/breaker.go
│   ├── mailotp/imap.go        # IMAP OTP reader (United bootstrap-auto only)
│   └── config/config.go
├── docker/
│   ├── docker-compose.yml
│   ├── postgres/init/001_schema.sql
│   └── pgadmin/
├── docs/
│   └── schema.md
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
