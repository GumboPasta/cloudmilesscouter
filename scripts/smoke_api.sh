#!/usr/bin/env bash
#
# Phase 4 Step 5 — curl smoke test for the REST API.
#
# Exercises every endpoint (happy paths, bad params, empty results), checks the
# response status + JSON shape, and verifies the Redis cache hit/miss and ETL
# invalidation behaviour.
#
# Prereqs:
#   - the compose stack is up          (docker compose -f docker/docker-compose.yml up -d)
#   - Postgres has award data          (run a scrape + `go run ./cmd/etl` first)
#   - the API is running               (`go run ./cmd/api` from the repo root)
#
# Usage:
#   scripts/smoke_api.sh                 # endpoint + cache checks
#   scripts/smoke_api.sh --rate          # also hammer the rate limiter (slow)
#   BASE_URL=http://host:8080 scripts/smoke_api.sh
#
# Env knobs (defaults match config.go / docker-compose):
#   BASE_URL        http://localhost:8080
#   REDIS_CONTAINER docker-redis-1
#   CORS_ORIGIN     http://localhost:5173
#   SEED_ROUTE      "SEA JFK 2026-12-08"   (origin dest date — must have award data)
#   EMPTY_ROUTE     "AAA BBB 2999-01-01"   (a route+date with no data)

set -u

BASE_URL="${BASE_URL:-http://localhost:8080}"
REDIS_CONTAINER="${REDIS_CONTAINER:-docker-redis-1}"
CORS_ORIGIN="${CORS_ORIGIN:-http://localhost:5173}"
read -r SEED_O SEED_D SEED_DATE <<<"${SEED_ROUTE:-SEA JFK 2026-12-08}"
read -r EMPTY_O EMPTY_D EMPTY_DATE <<<"${EMPTY_ROUTE:-AAA BBB 2999-01-01}"
RUN_RATE=false
[[ "${1:-}" == "--rate" ]] && RUN_RATE=true

pass=0
fail=0

# check <name> <expected-status> <actual-status> [jq-filter] [body]
check() {
  local name="$1" want="$2" got="$3" filter="${4:-}" body="${5:-}"
  if [[ "$got" != "$want" ]]; then
    printf 'FAIL  %-45s want HTTP %s, got %s\n' "$name" "$want" "$got"
    [[ -n "$body" ]] && printf '      body: %s\n' "$body"
    ((fail++)); return
  fi
  if [[ -n "$filter" ]]; then
    if ! jq -e "$filter" >/dev/null 2>&1 <<<"$body"; then
      printf 'FAIL  %-45s HTTP %s ok but body failed: %s\n' "$name" "$got" "$filter"
      printf '      body: %s\n' "$body"
      ((fail++)); return
    fi
  fi
  printf 'PASS  %-45s HTTP %s\n' "$name" "$got"
  ((pass++))
}

# req <method> <path> [curl-args...]  -> sets $STATUS and $BODY
req() {
  local method="$1" path="$2"; shift 2
  local out
  out="$(curl -s -w $'\n%{http_code}' -X "$method" "$BASE_URL$path" "$@")"
  STATUS="${out##*$'\n'}"
  BODY="${out%$'\n'*}"
}

redis_cli() { docker exec "$REDIS_CONTAINER" redis-cli "$@" 2>/dev/null; }

echo "== target: $BASE_URL =="
echo "== seed route: $SEED_O-$SEED_D $SEED_DATE =="
echo

# ---------------------------------------------------------------- healthz
req GET /healthz
check "GET /healthz" 200 "$STATUS" '.status == "ok"' "$BODY"

# ---------------------------------------------------------------- /metrics (Phase 6)
# Served from ahead of the chi middleware, so no auth / CORS / rate limit.
req GET /metrics
check "GET /metrics" 200 "$STATUS"
grep -q '^http_requests_total' <<<"$BODY" \
  && { printf 'PASS  %-45s\n' "/metrics exposes http_requests_total"; ((pass++)); } \
  || { printf 'FAIL  %-45s\n' "/metrics exposes http_requests_total"; ((fail++)); }

# ---------------------------------------------------------------- /api/search happy paths
req GET "/api/search?origin=$SEED_O&destination=$SEED_D&date=$SEED_DATE"
check "GET /api/search (all cabins)" 200 "$STATUS" 'type == "array" and length > 0' "$BODY"

# cheapest-first: points_cost non-decreasing
req GET "/api/search?origin=$SEED_O&destination=$SEED_D&date=$SEED_DATE"
check "GET /api/search sorted by points_cost" 200 "$STATUS" \
  '[.[].points_cost] == ([.[].points_cost] | sort)' "$BODY"

req GET "/api/search?origin=$SEED_O&destination=$SEED_D&date=$SEED_DATE&cabin=business"
check "GET /api/search?cabin=business" 200 "$STATUS" \
  'type == "array" and (all(.[]; .cabin == "business"))' "$BODY"

# lower-case / whitespace normalisation on the route codes
req GET "/api/search?origin=$(tr '[:upper:]' '[:lower:]' <<<"$SEED_O")&destination=$SEED_D&date=$SEED_DATE"
check "GET /api/search (lowercase origin)" 200 "$STATUS" 'length > 0' "$BODY"

req GET "/api/search?origin=$EMPTY_O&destination=$EMPTY_D&date=$EMPTY_DATE"
check "GET /api/search (no data -> 200 [])" 200 "$STATUS" '. == []' "$BODY"

# ---------------------------------------------------------------- /api/search bad params
req GET "/api/search?destination=$SEED_D&date=$SEED_DATE"
check "GET /api/search missing origin" 400 "$STATUS" '.error | test("origin")' "$BODY"

req GET "/api/search?origin=$SEED_O&destination=$SEED_D"
check "GET /api/search missing date" 400 "$STATUS" '.error | test("date")' "$BODY"

req GET "/api/search?origin=BOSTON&destination=$SEED_D&date=$SEED_DATE"
check "GET /api/search bad origin code" 400 "$STATUS" '.error | test("3-letter")' "$BODY"

req GET "/api/search?origin=$SEED_O&destination=$SEED_D&date=2026-13-99"
check "GET /api/search bad date format" 400 "$STATUS" '.error | test("YYYY-MM-DD")' "$BODY"

req GET "/api/search?origin=$SEED_O&destination=$SEED_D&date=$SEED_DATE&cabin=couch"
check "GET /api/search bad cabin" 400 "$STATUS" '.error | test("cabin")' "$BODY"

# ---------------------------------------------------------------- /api/airlines
req GET /api/airlines
check "GET /api/airlines" 200 "$STATUS" \
  'type == "array" and length > 0 and (all(.[]; has("code") and has("name")))' "$BODY"
check "GET /api/airlines ordered by name" 200 "$STATUS" \
  '[.[].name] == ([.[].name] | sort)' "$BODY"

# ---------------------------------------------------------------- /api/routes
req GET /api/routes
check "GET /api/routes" 200 "$STATUS" \
  'type == "array" and length > 0 and (all(.[]; has("origin") and has("destination") and has("award_count") and has("last_scraped")))' "$BODY"
check "GET /api/routes ordered by award_count desc" 200 "$STATUS" \
  '[.[].award_count] == ([.[].award_count] | sort | reverse)' "$BODY"

# ---------------------------------------------------------------- POST /api/scrape
req POST /api/scrape -H 'Content-Type: application/json' \
  -d "{\"origin\":\"$SEED_O\",\"destination\":\"$SEED_D\",\"date\":\"$SEED_DATE\",\"airlines\":[\"delta\"]}"
check "POST /api/scrape (delta only)" 202 "$STATUS" '.dispatched == ["delta"]' "$BODY"

req POST /api/scrape -H 'Content-Type: application/json' \
  -d "{\"origin\":\"$SEED_O\",\"destination\":\"$SEED_D\",\"date\":\"$SEED_DATE\"}"
check "POST /api/scrape (default airlines)" 202 "$STATUS" \
  '.dispatched == ["united","american","delta","alaska"]' "$BODY"

req POST /api/scrape -H 'Content-Type: application/json' -d '{ not json'
check "POST /api/scrape bad JSON" 400 "$STATUS" '.error' "$BODY"

req POST /api/scrape -H 'Content-Type: application/json' \
  -d "{\"origin\":\"$SEED_O\",\"destination\":\"$SEED_D\",\"date\":\"$SEED_DATE\",\"bogus\":1}"
check "POST /api/scrape unknown field" 400 "$STATUS" '.error' "$BODY"

req POST /api/scrape -H 'Content-Type: application/json' \
  -d "{\"origin\":\"$SEED_O\",\"destination\":\"$SEED_D\"}"
check "POST /api/scrape missing date" 400 "$STATUS" '.error | test("date")' "$BODY"

req POST /api/scrape -H 'Content-Type: application/json' \
  -d "{\"origin\":\"$SEED_O\",\"destination\":\"$SEED_D\",\"date\":\"$SEED_DATE\",\"airlines\":[\" \",\"\"]}"
check "POST /api/scrape all-blank airlines" 400 "$STATUS" '.error | test("airlines")' "$BODY"

# ---------------------------------------------------------------- CORS
# go-chi/cors answers a preflight with 200 + empty body (its default success status).
req OPTIONS "/api/search" -H "Origin: $CORS_ORIGIN" -H 'Access-Control-Request-Method: GET'
check "CORS preflight (allowed origin)" 200 "$STATUS"
acao="$(curl -s -o /dev/null -D - -X OPTIONS "$BASE_URL/api/search" \
  -H "Origin: $CORS_ORIGIN" -H 'Access-Control-Request-Method: GET' \
  | tr -d '\r' | awk -F': ' 'tolower($1)=="access-control-allow-origin"{print $2}')"
if [[ "$acao" == "$CORS_ORIGIN" ]]; then
  printf 'PASS  %-45s %s\n' "CORS allow-origin echoes allowed origin" "$acao"; ((pass++))
else
  printf 'FAIL  %-45s got "%s"\n' "CORS allow-origin echoes allowed origin" "$acao"; ((fail++))
fi
acao_bad="$(curl -s -o /dev/null -D - -X OPTIONS "$BASE_URL/api/search" \
  -H "Origin: https://evil.example" -H 'Access-Control-Request-Method: GET' \
  | tr -d '\r' | awk -F': ' 'tolower($1)=="access-control-allow-origin"{print $2}')"
if [[ -z "$acao_bad" ]]; then
  printf 'PASS  %-45s (no header)\n' "CORS rejects unlisted origin"; ((pass++))
else
  printf 'FAIL  %-45s got "%s"\n' "CORS rejects unlisted origin" "$acao_bad"; ((fail++))
fi

# ---------------------------------------------------------------- cache hit/miss
if redis_cli PING >/dev/null; then
  key="search:$SEED_O:$SEED_D:$SEED_DATE:any"
  redis_cli DEL "$key" >/dev/null
  [[ "$(redis_cli EXISTS "$key")" == "0" ]] \
    && { printf 'PASS  %-45s\n' "cache key absent before request"; ((pass++)); } \
    || { printf 'FAIL  %-45s\n' "cache key absent before request"; ((fail++)); }

  req GET "/api/search?origin=$SEED_O&destination=$SEED_D&date=$SEED_DATE"
  miss_body="$BODY"
  [[ "$(redis_cli EXISTS "$key")" == "1" ]] \
    && { printf 'PASS  %-45s\n' "miss back-fills cache key"; ((pass++)); } \
    || { printf 'FAIL  %-45s\n' "miss back-fills cache key"; ((fail++)); }

  ttl="$(redis_cli TTL "$key")"
  if [[ "$ttl" -gt 3000 && "$ttl" -le 3600 ]]; then
    printf 'PASS  %-45s TTL=%ss\n' "cache key TTL ~1h" "$ttl"; ((pass++))
  else
    printf 'FAIL  %-45s TTL=%ss\n' "cache key TTL ~1h" "$ttl"; ((fail++))
  fi

  req GET "/api/search?origin=$SEED_O&destination=$SEED_D&date=$SEED_DATE"
  [[ "$BODY" == "$miss_body" ]] \
    && { printf 'PASS  %-45s\n' "cache hit body == miss body"; ((pass++)); } \
    || { printf 'FAIL  %-45s\n' "cache hit body == miss body"; ((fail++)); }

  # ETL invalidation: re-run the ETL and confirm the route's keys are dropped.
  if [[ "${RUN_ETL:-true}" == "true" ]]; then
    echo "-- running ETL to check invalidation (RUN_ETL=false to skip) --"
    if (cd "$(dirname "$0")/.." && go run ./cmd/etl >/dev/null 2>&1); then
      [[ "$(redis_cli EXISTS "$key")" == "0" ]] \
        && { printf 'PASS  %-45s\n' "ETL run invalidates cache key"; ((pass++)); } \
        || { printf 'FAIL  %-45s (key survived ETL)\n' "ETL run invalidates cache key"; ((fail++)); }
    else
      printf 'SKIP  %-45s (etl run failed)\n' "ETL run invalidates cache key"
    fi
  fi
else
  echo "SKIP  Redis checks — cannot reach container $REDIS_CONTAINER"
fi

# ---------------------------------------------------------------- rate limiter (opt-in)
if $RUN_RATE; then
  echo "-- firing 130 requests at /healthz to trip the limiter --"
  limited=0
  for _ in $(seq 1 130); do
    code="$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL/healthz")"
    [[ "$code" == "429" ]] && ((limited++))
  done
  [[ "$limited" -gt 0 ]] \
    && { printf 'PASS  %-45s %s x 429\n' "rate limiter returns 429 past budget" "$limited"; ((pass++)); } \
    || { printf 'FAIL  %-45s no 429s seen\n' "rate limiter returns 429 past budget"; ((fail++)); }
fi

echo
echo "== $pass passed, $fail failed =="
[[ "$fail" -eq 0 ]]
