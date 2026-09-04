#!/usr/bin/env bash
#
# Phase 6 Step 5 — resilience validation against the live stack.
#
# Simulates an airline outage (the worker's SCRAPER_FORCE_FAILURE knob), then
# checks the Phase 6 definition of done:
#   1. the per-airline circuit breaker opens once the failure threshold is hit
#      and drops further jobs fast during its cooldown (and, with --half-open,
#      lets exactly one probe through afterwards);
#   2. Prometheus is scraping the worker so Grafana updates in real time;
#   3. permanently failed jobs land on the scrape.jobs.dlq dead-letter topic
#      with their failure context, counted by dlq_messages_total.
#
# Prereqs:
#   - the full compose stack is up, including prometheus + grafana:
#       docker compose -f docker/docker-compose.yml up -d
#   - the worker is running on the host with the outage knob set, logging to a
#     file so this script can assert on its log lines:
#       SCRAPER_FORCE_FAILURE=delta go run ./cmd/worker 2>&1 | tee /tmp/worker.log
#   - the API is running on the host (for POST /api/scrape):
#       go run ./cmd/api
#
# Usage:
#   scripts/validate_resilience.sh                     # breaker + DLQ + Prometheus checks
#   scripts/validate_resilience.sh --half-open         # also wait out the cooldown and check the probe
#   AIRLINE=delta WORKER_LOG=/tmp/worker.log scripts/validate_resilience.sh
#
# Env knobs (defaults match config.go / docker-compose):
#   BASE_URL        http://localhost:8080     REST API
#   WORKER_METRICS  http://localhost:2112     worker /metrics listener (METRICS_ADDR)
#   PROM_URL        http://localhost:9090     Prometheus
#   GRAFANA_URL     http://localhost:3000     Grafana
#   KAFKA_CONTAINER docker-kafka-1            container running the broker
#   AIRLINE         delta                     must match the worker's SCRAPER_FORCE_FAILURE
#   WORKER_LOG      (unset)                   path to the worker log; enables log-line assertions
#   ROUTE          "SEA JFK 2026-12-20"       origin dest date to dispatch
#   DISPATCHES     8                          how many scrapes to fire (> breaker threshold)
#   COOLDOWN       60                         CIRCUIT_BREAKER_COOLDOWN seconds (for --half-open)

set -u

BASE_URL="${BASE_URL:-http://localhost:8080}"
WORKER_METRICS="${WORKER_METRICS:-http://localhost:2112}"
PROM_URL="${PROM_URL:-http://localhost:9090}"
GRAFANA_URL="${GRAFANA_URL:-http://localhost:3000}"
KAFKA_CONTAINER="${KAFKA_CONTAINER:-docker-kafka-1}"
AIRLINE="${AIRLINE:-delta}"
WORKER_LOG="${WORKER_LOG:-}"
read -r ROUTE_O ROUTE_D ROUTE_DATE <<<"${ROUTE:-SEA JFK 2026-12-20}"
DISPATCHES="${DISPATCHES:-8}"
COOLDOWN="${COOLDOWN:-60}"
HALF_OPEN=false
[[ "${1:-}" == "--half-open" ]] && HALF_OPEN=true

pass=0
fail=0
ok()   { printf 'PASS  %s\n' "$1"; ((pass++)); }
bad()  { printf 'FAIL  %s\n' "$1"; ((fail++)); }
note() { printf '      %s\n' "$1"; }

# scrape a numeric metric value off the worker's /metrics for $AIRLINE, summed
# across label permutations (e.g. dlq_messages_total has one line per reason).
worker_metric() { # <metric-name>
  curl -s "$WORKER_METRICS/metrics" \
    | grep -E "^$1\{[^}]*airline=\"$AIRLINE\"" \
    | awk '{s+=$2} END{printf "%d", s+0}'
}

# run an instant query against the Prometheus HTTP API, print the scalar value.
prom_query() { # <promql>
  curl -s --data-urlencode "query=$1" "$PROM_URL/api/v1/query" \
    | jq -r '.data.result[0].value[1] // "none"'
}

log_has() { # <substring>
  [[ -n "$WORKER_LOG" && -f "$WORKER_LOG" ]] && grep -qF "$1" "$WORKER_LOG"
}

echo "== resilience validation =="
echo "== airline under test: $AIRLINE   route: $ROUTE_O-$ROUTE_D $ROUTE_DATE =="
echo

# ---------------------------------------------------------------- preflight
curl -sf "$WORKER_METRICS/metrics" >/dev/null \
  && ok "worker /metrics reachable ($WORKER_METRICS)" \
  || { bad "worker /metrics unreachable ($WORKER_METRICS) — is the worker running?"; echo; echo "== aborted =="; exit 1; }

curl -sf "$PROM_URL/-/healthy" >/dev/null \
  && ok "Prometheus healthy ($PROM_URL)" \
  || bad "Prometheus unhealthy ($PROM_URL)"

curl -sf "$GRAFANA_URL/api/health" >/dev/null \
  && ok "Grafana healthy ($GRAFANA_URL)" \
  || bad "Grafana unhealthy ($GRAFANA_URL)"

if [[ -n "$WORKER_LOG" ]]; then
  if grep -q "\"force_failure\":\"[^\"]*$AIRLINE" "$WORKER_LOG" 2>/dev/null; then
    ok "worker started with SCRAPER_FORCE_FAILURE including $AIRLINE"
  else
    bad "worker log does not show SCRAPER_FORCE_FAILURE=$AIRLINE — the scrape will not fail"
  fi
else
  note "WORKER_LOG unset — skipping log-line assertions (circuit-open / dead-letter / probe)"
fi

dlq_before="$(worker_metric dlq_messages_total)"
note "baseline dlq_messages_total{airline=\"$AIRLINE\"} = $dlq_before"
echo

# ---------------------------------------------------------------- drive the outage
echo "-- firing $DISPATCHES scrapes at $AIRLINE --"
for i in $(seq 1 "$DISPATCHES"); do
  code="$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE_URL/api/scrape" \
    -H 'Content-Type: application/json' \
    -d "{\"origin\":\"$ROUTE_O\",\"destination\":\"$ROUTE_D\",\"date\":\"$ROUTE_DATE\",\"airlines\":[\"$AIRLINE\"]}")"
  [[ "$code" == "202" ]] || bad "POST /api/scrape #$i returned $code (want 202)"
  sleep 3
done
[[ "$fail" -eq 0 ]] && ok "$DISPATCHES scrapes dispatched (202)"

# each failed job retries MAX_SCRAPE_ATTEMPTS times (~6s of backoff) before it is
# dead-lettered; give the last dispatch time to walk that ladder.
echo "-- waiting 20s for retries + dead-lettering to settle --"
sleep 20
echo

# ---------------------------------------------------------------- 1. circuit breaker
state="$(worker_metric scrape_circuit_state)"
case "$state" in
  1) ok "scrape_circuit_state{airline=\"$AIRLINE\"} = 1 (open)" ;;
  2) ok "scrape_circuit_state{airline=\"$AIRLINE\"} = 2 (half-open) — breaker has tripped" ;;
  *) bad "scrape_circuit_state{airline=\"$AIRLINE\"} = ${state:-<absent>}, want 1 (open)" ;;
esac

if [[ -n "$WORKER_LOG" ]]; then
  log_has "circuit opened for airline" && ok "worker logged 'circuit opened for airline'" \
    || bad "worker log missing 'circuit opened for airline'"
  log_has "circuit open for airline, dropping job" && ok "worker dropped jobs while the breaker was open" \
    || note "no 'dropping job' line yet — depends on dispatch timing vs cooldown"
fi

# ---------------------------------------------------------------- 2. Grafana / Prometheus live
up="$(prom_query "up{job=\"worker\"}")"
[[ "$up" == "1" ]] && ok "Prometheus is scraping the worker (up{job=\"worker\"} = 1)" \
  || bad "Prometheus target up{job=\"worker\"} = ${up} — Grafana will not update"

pstate="$(prom_query "scrape_circuit_state{airline=\"$AIRLINE\"}")"
[[ "$pstate" == "1" || "$pstate" == "2" ]] \
  && ok "Prometheus has the tripped breaker (scrape_circuit_state = $pstate)" \
  || bad "Prometheus scrape_circuit_state{airline=\"$AIRLINE\"} = $pstate"

note "eyeball the live dashboard: $GRAFANA_URL/d/cloudmilesscouter (Scraper health row)"

# ---------------------------------------------------------------- 3. dead-letter queue
dlq_after="$(worker_metric dlq_messages_total)"
if [[ "$dlq_after" -gt "$dlq_before" ]]; then
  ok "dlq_messages_total{airline=\"$AIRLINE\"} rose $dlq_before -> $dlq_after"
else
  bad "dlq_messages_total{airline=\"$AIRLINE\"} did not rise (still $dlq_after)"
fi

log_has "dead-lettered job" && ok "worker logged 'dead-lettered job'" \
  || { [[ -n "$WORKER_LOG" ]] && bad "worker log missing 'dead-lettered job'"; }

echo "-- reading scrape.jobs.dlq from the broker --"
dlq_dump="$(docker exec "$KAFKA_CONTAINER" kafka-console-consumer \
  --bootstrap-server localhost:9092 --topic scrape.jobs.dlq \
  --from-beginning --timeout-ms 8000 2>/dev/null)"
mine="$(jq -c "select(.job.airline == \"$AIRLINE\")" <<<"$dlq_dump" 2>/dev/null | tail -5)"
if [[ -n "$mine" ]]; then
  ok "scrape.jobs.dlq has DeadLetterJob(s) for $AIRLINE"
  last="$(tail -1 <<<"$mine")"
  note "latest: $last"
  jq -e '.reason and .attempts and .error and .failed_at and .job' >/dev/null 2>&1 <<<"$last" \
    && ok "DLQ message carries reason / attempts / error / failed_at / job" \
    || bad "DLQ message is missing expected fields"
else
  bad "no DeadLetterJob for $AIRLINE on scrape.jobs.dlq"
fi
echo

# ---------------------------------------------------------------- half-open probe (opt-in)
if $HALF_OPEN; then
  echo "-- --half-open: waiting ${COOLDOWN}s for the cooldown, then firing one probe --"
  sleep "$((COOLDOWN + 3))"
  curl -s -o /dev/null -X POST "$BASE_URL/api/scrape" -H 'Content-Type: application/json' \
    -d "{\"origin\":\"$ROUTE_O\",\"destination\":\"$ROUTE_D\",\"date\":\"$ROUTE_DATE\",\"airlines\":[\"$AIRLINE\"]}"
  sleep 15
  if [[ -n "$WORKER_LOG" ]]; then
    log_has "circuit half-open, probing airline" \
      && ok "worker logged the half-open probe" \
      || bad "worker log missing 'circuit half-open, probing airline'"
  fi
  state="$(worker_metric scrape_circuit_state)"
  [[ "$state" == "1" ]] \
    && ok "breaker re-opened after the failed probe (scrape_circuit_state = 1)" \
    || bad "scrape_circuit_state = ${state:-<absent>} after the probe, want 1 (still down -> re-open)"
  echo
fi

# ---------------------------------------------------------------- done
echo "== $pass passed, $fail failed =="
echo
echo "teardown: stop the worker, drop SCRAPER_FORCE_FAILURE, restart it."
echo "          to clear the DLQ topic:"
echo "          docker exec $KAFKA_CONTAINER kafka-topics --bootstrap-server localhost:9092 \\"
echo "            --delete --topic scrape.jobs.dlq  (kafka-init recreates it on next 'up')"
[[ "$fail" -eq 0 ]]
