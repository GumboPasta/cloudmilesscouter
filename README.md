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
- [ ] Add Kafka + Zookeeper to Docker Compose
- [ ] Create topic: `scrape.jobs`
- [ ] Verify Kafka boots and accepts messages

**Step 2 — Build Job Producer**
- [ ] Write Go code to dispatch one job per airline into Kafka topic
- [ ] Job payload: airline ID, route, dates, cabin class

**Step 3 — Build Worker Pool**
- [ ] Write Go worker pool (5–10 concurrent workers)
- [ ] Each worker pulls a job from Kafka
- [ ] Each worker spawns a Playwright browser instance
- [ ] Worker scrapes airline, stores raw result in MongoDB
- [ ] Worker acknowledges job completion back to Kafka

**Step 4 — Add More Airlines**
- [ ] Add American Airlines parser
- [ ] Add Delta Airlines parser
- [ ] Add Air Canada parser
- [ ] Test all three running in parallel via worker pool

**Step 5 — Add Circuit Breakers & Retry Logic**
- [ ] If airline site is down, fail gracefully
- [ ] Re-queue failed jobs with exponential backoff
- [ ] Log failure reason with structured logging

**Step 6 — Test & Validate**
- [ ] Trigger a search and verify all airlines scraped in parallel
- [ ] Verify all results land in MongoDB
- [ ] Simulate a scraper failure and verify retry works

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

```
cloudmilesscouter/
├── cmd/
│   └── scraper/
│       └── main.go
├── internal/
│   ├── scraper/
│   │   ├── scraper.go
│   │   └── airlines/
│   │       ├── united.go
│   │       ├── american.go
│   │       └── delta.go
│   ├── etl/
│   │   ├── etl.go
│   │   └── parsers/
│   │       ├── united.go
│   │       ├── american.go
│   │       └── delta.go
│   ├── storage/
│   │   ├── mongo.go
│   │   └── postgres.go
│   ├── queue/
│   │   ├── producer.go
│   │   └── consumer.go
│   ├── api/
│   │   ├── router.go
│   │   └── handlers/
│   │       └── search.go
│   └── config/
│       └── config.go
├── frontend/
│   ├── src/
│   │   ├── components/
│   │   ├── pages/
│   │   ├── hooks/
│   │   └── utils/
│   └── package.json
├── docker/
│   └── docker-compose.yml
├── monitoring/
│   ├── prometheus.yml
│   └── grafana/
├── CLAUDE.md
├── .env
├── .gitignore
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
