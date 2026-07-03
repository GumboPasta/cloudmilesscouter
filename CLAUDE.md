# CloudMilesScouter — Claude Code Context

## Project
Airline award flight scraper. Scrapes loyalty program websites, stores raw data in MongoDB, normalizes into PostgreSQL, exposes via Go REST API, React frontend on Vercel.

## Current Phase
**Phase 1 — Foundation & First Scraper**
Working on: United Airlines scraper → MongoDB storage

## Tech Stack
- Language: Go
- Browser automation: Playwright (`playwright-go`)
- Raw storage: MongoDB
- Structured storage: PostgreSQL
- Queue: Kafka (Phase 3+, not yet)
- Cache: Redis (Phase 4+, not yet)
- API: Go + Chi Router
- Frontend: React + TypeScript + Tailwind + Vercel
- Monitoring: Prometheus + Grafana (Phase 6+, not yet)
- Containers: Docker + Docker Compose

## Folder Structure
```
cloudmilesscouter/
├── cmd/scraper/main.go
├── internal/
│   ├── scraper/scraper.go
│   ├── scraper/airlines/united.go
│   ├── etl/etl.go
│   ├── etl/parsers/united.go
│   ├── storage/mongo.go
│   ├── storage/postgres.go
│   ├── queue/producer.go
│   ├── queue/consumer.go
│   ├── api/router.go
│   ├── api/handlers/search.go
│   └── config/config.go
├── frontend/
├── docker/docker-compose.yml
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
