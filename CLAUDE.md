# CloudMilesScouter — Claude Code Context

## Project
Airline award flight scraper. Scrapes loyalty program websites, stores raw data in MongoDB, normalizes into PostgreSQL, exposes via Go REST API, React frontend on Vercel.

## Current Phase
**Phase 6 — Observability & Resilience — COMPLETE (Step 5, Test & Validate, done).**
Step 1: `prometheus` + `pushgateway` in Docker Compose
(`docker/prometheus/prometheus.yml`); all collectors in `internal/metrics`. The
API serves `/metrics` on `API_PORT` from an outer `ServeMux` (ahead of chi
middleware); the worker runs its own `/metrics` listener on `METRICS_ADDR`
(`:2112`); `cmd/etl` pushes to `PUSHGATEWAY_URL` after each run. Metrics: API
request count/latency, `/api/search` cache hit rate, per-airline scrape
attempts/failures/duration, `kafka_consumer_lag`, ETL parse failures.
Step 2: `grafana` service (`:3000`, anonymous Viewer) with a provisioned
Prometheus datasource and one file-provisioned dashboard, all under
`docker/grafana/`. `docker/grafana/dashboards/cloudmilesscouter.json` — three
rows (API performance / scraper health / queue depth + ETL) over the Step 1
metrics. See README Phase 6 Steps 1–2 and `docs/api.md#get-metrics`.
Step 3: `internal/logging` (`Setup(service, level, format)`) installs a slog
JSON handler as the process default, tagged with a `service` field; every
`cmd/*/main.go` calls it right after `config.Load()`. `LOG_LEVEL` (default
`info`) and `LOG_FORMAT` (`json` default, `text` for local dev) are config
fields. The internal packages already logged via the slog default, so they emit
JSON unchanged; the only other change was swapping the leftover `log.Fatalf` /
`log.Println` startup lines in the mains for `slog`.

Step 4: `internal/breaker` reworked to a closed→open→half-open machine —
`CIRCUIT_BREAKER_THRESHOLD` (5) / `CIRCUIT_BREAKER_COOLDOWN` (60s) config fields,
one half-open probe job after the cooldown, state published as the
`scrape_circuit_state{airline}` gauge. New `scrape.jobs.dlq` Kafka topic (added
to the `kafka-init` one-shot): the worker writes a `queue.DeadLetterJob` there
once a job exhausts `MAX_SCRAPE_ATTEMPTS`, counted by
`dlq_messages_total{airline,reason}`. Graceful shutdown added to `cmd/producer`,
`cmd/etl`, `cmd/scraper` (`signal.NotifyContext`); `cmd/api` and `cmd/worker`
already had it.

Step 5: validation, no product code beyond one knob. `cmd/worker` reads
`SCRAPER_FORCE_FAILURE` (comma-separated airline IDs, `internal/config`) and fails
those scrapes instantly without a browser, straight into the real
retry/breaker/DLQ path — the outage simulator. `scripts/validate_resilience.sh`
drives the live stack: fires repeated scrapes, asserts `scrape_circuit_state`
opens, `up{job="worker"}` is 1 (Grafana is live), and `scrape.jobs.dlq` fills
with well-formed `DeadLetterJob`s; `--half-open` waits the cooldown and checks the
probe. `cmd/worker/resilience_test.go` (`-tags e2e`, joins the
`TestResilience` run) asserts the closed→open→half-open walk and the DLQ write
deterministically against a live Kafka.

**Next: Phase 7 Step 1** — add remaining airlines / scale & polish (see README
Phase 7).

Phase 5 (Frontend UI) is complete — React + TS + Tailwind app in `frontend/`,
deployed to Vercel; live award data flows through a `cloudflared`/`ngrok` tunnel
to the local API until Phase 7 hosts it.

## Tech Stack
- Language: Go
- Browser automation: Playwright (`playwright-go`)
- Raw storage: MongoDB
- Structured storage: PostgreSQL
- Queue: Kafka (Phase 3 — in progress)
- Cache: Redis (Phase 4 Step 4 — `internal/storage/cache.go`, caches `/api/search`)
- API: Go + Chi Router
- Frontend: React + TypeScript + Tailwind + Vercel
- Monitoring: Prometheus (Phase 6 Step 1 — `internal/metrics`, `docker/prometheus/`) + Grafana (Step 2 — `docker/grafana/`, provisioned datasource + dashboard)
- Logging: `slog` JSON to stderr, per-service tag (Phase 6 Step 3 — `internal/logging`)
- Resilience: per-airline circuit breaker + Kafka DLQ (`scrape.jobs.dlq`) + graceful shutdown in every binary (Phase 6 Step 4 — `internal/breaker`, `internal/queue`); validated in Step 5 (`SCRAPER_FORCE_FAILURE` knob, `scripts/validate_resilience.sh`, `cmd/worker/resilience_test.go`)
- Containers: Docker + Docker Compose

## Folder Structure
Through Phase 6 Step 5 (validation — adds `scripts/validate_resilience.sh` and
`cmd/worker/resilience_test.go`).
```
cloudmilesscouter/
├── frontend/               # Phase 5 — React + TS + Tailwind (Vite), deployed on Vercel (root dir = frontend/, .nvmrc = 22)
│   └── src/{components,pages,hooks,utils}/  # SearchForm, ResultsTable, Empty/ErrorState; useAwardSearch; utils/api.ts (REST client + types)
├── cmd/
│   ├── scraper/main.go     # one-off single-route scrape → Mongo
│   ├── producer/main.go    # dispatch ScrapeJobs to Kafka
│   ├── worker/main.go      # worker pool: Kafka → scrape → Mongo (+ SCRAPER_FORCE_FAILURE outage knob, Step 5)
│   ├── worker/resilience_test.go  # -tags e2e — breaker + DLQ validation vs live Kafka (Step 5)
│   ├── etl/main.go         # Mongo → Postgres
│   └── api/main.go         # Chi REST API over Postgres (Phase 4)
├── internal/
│   ├── api/router.go       # Chi router + middleware (logging, CORS, rate limit)
│   ├── api/search.go       # GET /api/search handler
│   ├── api/airlines.go     # GET /api/airlines handler
│   ├── api/routes.go       # GET /api/routes handler
│   ├── api/scrape.go       # POST /api/scrape — dispatches ScrapeJobs to Kafka
│   ├── storage/awards_query.go    # SearchAwards: read path for /api/search
│   ├── storage/airlines_query.go  # ListAirlines: read path for /api/airlines
│   ├── storage/routes_query.go    # ListRoutes: read path for /api/routes
│   ├── storage/cache.go           # Redis search cache: read-through + ETL invalidation
│   ├── scraper/scraper.go
│   ├── scraper/airlines/{airlines,united,american,delta,alaska}.go
│   ├── etl/etl.go
│   ├── etl/parsers/{united,american,delta,alaska}.go
│   ├── storage/{mongo,postgres}.go
│   ├── queue/{producer,consumer}.go  # producer also writes scrape.jobs.dlq (Phase 6 Step 4)
│   ├── breaker/breaker.go  # per-airline circuit breaker: closed→open→half-open (Phase 6 Step 4)
│   ├── metrics/metrics.go  # Phase 6 — Prometheus collectors, /metrics handler, worker listener, ETL push
│   ├── logging/logging.go  # Phase 6 Step 3 — slog JSON handler setup, per-service tag
│   ├── mailotp/imap.go     # United bootstrap-auto only
│   └── config/config.go
├── docker/{docker-compose.yml,postgres/init/001_schema.sql,prometheus/prometheus.yml,grafana/,pgadmin/}
├── docs/{schema.md,api.md}
├── scripts/{smoke_api.sh,validate_resilience.sh}  # REST API smoke test; Phase 6 Step 5 resilience validation
├── testdata/samples/       # real payloads for the parser tests
├── CLAUDE.md
└── README.md
```

## Development Rules — Read Before Writing Any Code
- This is an MVP — simplest solution that works
- No premature abstractions, no over-engineering
- No Kafka, Redis, or Prometheus until their designated phases
- Prefer single-file implementations where feasible
- No complex error handling for unlikely edge cases yet
- If in doubt: write less code, not more
- Each phase has a Definition of Done — don't move on until it's met

## How to Work With Me Efficiently
- One task per session — don't mix unrelated work
- Run `/clear` between steps to reset context
- Use Sonnet for most coding, Opus only for hard architecture decisions
- Reference files by path — don't paste large files into chat
- Before multi-file changes: ask for a plan first, review it, then execute
