# CloudMilesScouter — Claude Code Context

## Project
Airline award flight scraper. Scrapes loyalty program websites, stores raw data in MongoDB, normalizes into PostgreSQL, exposes via Go REST API, React frontend on Vercel.

## Current Phase
**Phase 3 — Queue & Worker Pool**
Working on: Kafka job queue + parallel worker pool for concurrent multi-airline scraping

## Tech Stack
- Language: Go
- Browser automation: Playwright (`playwright-go`)
- Raw storage: MongoDB
- Structured storage: PostgreSQL
- Queue: Kafka (Phase 3 — in progress)
- Cache: Redis (Phase 4+, not yet)
- API: Go + Chi Router
- Frontend: React + TypeScript + Tailwind + Vercel
- Monitoring: Prometheus + Grafana (Phase 6+, not yet)
- Containers: Docker + Docker Compose

## Folder Structure
Through Phase 3. `internal/api/` + `frontend/` are Phase 4; `monitoring/` is Phase 6.
```
cloudmilesscouter/
├── cmd/
│   ├── scraper/main.go     # one-off single-route scrape → Mongo
│   ├── producer/main.go    # dispatch ScrapeJobs to Kafka
│   ├── worker/main.go      # worker pool: Kafka → scrape → Mongo
│   └── etl/main.go         # Mongo → Postgres
├── internal/
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
├── docs/schema.md
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
