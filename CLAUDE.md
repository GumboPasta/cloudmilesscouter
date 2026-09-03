# CloudMilesScouter — Claude Code Context

## Project
Airline award flight scraper. Scrapes loyalty program websites, stores raw data in MongoDB, normalizes into PostgreSQL, exposes via Go REST API, React frontend on Vercel.

## Current Phase
**Phase 5 — Frontend UI — complete.** React + TS + Tailwind (Vite) app in
`frontend/`, all 5 steps done: API wiring, search form, sortable/filterable
results table, UI polish, and the Vercel deploy (project Root Directory
`frontend`, Node from `.nvmrc`, `VITE_API_BASE_URL` env var). The Go API is still
local-only (Phase 7 goal), so live award data on the deployed site comes through
a `cloudflared`/`ngrok` tunnel to the local API for now — see `frontend/README.md`.

**Next: Phase 6 — Observability & Resilience** (Prometheus + Grafana, structured
JSON logging, DLQ). First infra phase since Phase 4.

## Tech Stack
- Language: Go
- Browser automation: Playwright (`playwright-go`)
- Raw storage: MongoDB
- Structured storage: PostgreSQL
- Queue: Kafka (Phase 3 — in progress)
- Cache: Redis (Phase 4 Step 4 — `internal/storage/cache.go`, caches `/api/search`)
- API: Go + Chi Router
- Frontend: React + TypeScript + Tailwind + Vercel
- Monitoring: Prometheus + Grafana (Phase 6+, not yet)
- Containers: Docker + Docker Compose

## Folder Structure
Through Phase 5 (frontend built + deployed to Vercel). `monitoring/` is Phase 6.
```
cloudmilesscouter/
├── frontend/               # Phase 5 — React + TS + Tailwind (Vite), deployed on Vercel (root dir = frontend/, .nvmrc = 22)
│   └── src/{components,pages,hooks,utils}/  # SearchForm, ResultsTable, Empty/ErrorState; useAwardSearch; utils/api.ts (REST client + types)
├── cmd/
│   ├── scraper/main.go     # one-off single-route scrape → Mongo
│   ├── producer/main.go    # dispatch ScrapeJobs to Kafka
│   ├── worker/main.go      # worker pool: Kafka → scrape → Mongo
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
│   ├── queue/{producer,consumer}.go
│   ├── breaker/breaker.go
│   ├── mailotp/imap.go     # United bootstrap-auto only
│   └── config/config.go
├── docker/{docker-compose.yml,postgres/init/001_schema.sql,pgadmin/}
├── docs/{schema.md,api.md}
├── scripts/smoke_api.sh    # curl smoke test for the REST API
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
