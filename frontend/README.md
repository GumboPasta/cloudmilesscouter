# CloudMilesScouter Frontend

React + TypeScript + Tailwind (Vite) UI for the Phase 4 Go REST API.

## Prerequisites

- Node 20.19+ (see `.nvmrc` / `package.json` `engines`)
- The Go API running (`go run ./cmd/api` from the repo root) with Postgres up.
  The API must allow this origin — its default `CORS_ALLOWED_ORIGINS` is
  `http://localhost:5173`, which is the port the dev server below uses.

## Setup

```
cd frontend
npm install
cp .env.example .env.local   # optional — only to point at a non-default API
```

`VITE_API_BASE_URL` sets the API base URL. When unset the client falls back to
`http://localhost:8080`.

## Scripts

| command | what it does |
|---|---|
| `npm run dev` | Vite dev server on http://localhost:5173 |
| `npm run build` | type-check and build to `dist/` |
| `npm run preview` | serve the production build locally |
| `npm run typecheck` | `tsc` with no emit |
| `npm test` | run the Vitest suite once |

## Layout

```
public/favicon.svg
src/
├── components/   reusable presentational pieces (ApiStatusBadge, SearchForm,
│                 ResultsTable, EmptyState, ErrorState)
├── pages/        one component per screen (HomePage)
├── hooks/        reusable stateful logic (useApiHealth, useAwardSearch)
├── utils/        framework-free helpers (api.ts — REST client + types;
│                 airports.ts — static list for the search autocomplete;
│                 airlines.ts — static airline→alliance map for the results filter)
├── App.tsx       app shell — header wordmark/tagline + footer
└── main.tsx      React entry point
```

The search form (`components/SearchForm.tsx`) validates the route/date/cabin
inputs client-side, then `hooks/useAwardSearch.ts` fires a best-effort
`POST /api/scrape` followed by `GET /api/search`. `components/ResultsTable.tsx`
renders the returned award options as a table that sorts by any column (points
ascending by default) and filters by airline, cabin, and alliance. Alliance
isn't in the API response, so it comes from the bundled `utils/airlines.ts` map.

While the search runs `HomePage` shows a spinner; then one of `ResultsTable`,
`EmptyState` (search succeeded, no awards for that route/date), or `ErrorState`
(the `GET /api/search` call failed — a panel with a "Try again" button). A failed
`POST /api/scrape` dispatch doesn't block the search: `useAwardSearch` exposes it
as `scrapeWarning`, shown as an amber notice above the results, which are then
whatever was already on file.

The REST client and response types live in `src/utils/api.ts`; the full endpoint
contract is in [`../docs/api.md`](../docs/api.md).

## Deploy (Vercel)

This is a monorepo, so the Vercel project must point at the `frontend/`
subdirectory:

1. **New Project** → import `github.com/GumboPasta/cloudmilesscouter`.
2. **Root Directory: `frontend`** (required). Framework Preset auto-detects as
   **Vite** — Build Command `npm run build`, Output Directory `dist`. The
   committed `package-lock.json` makes the install reproducible (`npm ci`). Node
   comes from `.nvmrc` (22).
3. **Environment Variables** → add `VITE_API_BASE_URL` = the public API URL (see
   the caveat below). It's inlined at build time, so a change needs a redeploy.
4. Deploy. Every push builds a **Preview**; the branch Vercel tracks (`develop`
   after the Phase 5 PR merges) builds **Production**.

### API reachability caveat

The Go API (`cmd/api`) runs locally only — a hosted API is a Phase 7 goal. Until
then, a deployed frontend has nothing to call unless you expose the local API:

- Run a tunnel to it — `cloudflared tunnel --url http://localhost:8080` (or
  `ngrok http 8080`) — and set `VITE_API_BASE_URL` to the tunnel's `https://` URL.
- On the API host, set `CORS_ALLOWED_ORIGINS` to include the Vercel origin
  (e.g. `https://cloudmilesscouter.vercel.app`), comma-separated with any others.

Without a reachable API the site still deploys and loads — the header badge shows
"API unreachable" and a search lands on the `ErrorState` panel.
