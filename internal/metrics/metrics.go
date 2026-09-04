// Package metrics is the single place every binary's Prometheus instruments are
// declared (Phase 6 Step 1). Collectors register on the default registry at
// import time; the API scrapes them via Handler, the worker via ListenAndServe,
// and the short-lived ETL ships them to a Pushgateway with PushETL.
package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/push"
)

// metricsPath is where every binary exposes its exposition endpoint.
const metricsPath = "/metrics"

var (
	// HTTPRequestsTotal and HTTPRequestDuration are the API's request counters,
	// labelled by method, the chi route pattern (not the raw path, to keep
	// cardinality bounded), and the response status.
	HTTPRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Total API requests by method, route pattern, and status code.",
	}, []string{"method", "route", "status"})

	HTTPRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "API request latency by method and route pattern.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})

	// SearchCacheRequests counts /api/search cache lookups by outcome
	// ("hit" or "miss"); the hit rate is hit / (hit + miss).
	SearchCacheRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "search_cache_requests_total",
		Help: "GET /api/search cache lookups by result (hit or miss).",
	}, []string{"result"})

	// ScrapeAttemptsTotal and ScrapeFailuresTotal give the per-airline scraper
	// success rate: 1 - (failures / attempts). reason mirrors the worker's
	// coarse failure buckets (timeout, blocked, browser, store, other).
	ScrapeAttemptsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "scrape_attempts_total",
		Help: "Scrape attempts by airline (one per job the worker starts).",
	}, []string{"airline"})

	ScrapeFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "scrape_failures_total",
		Help: "Failed scrapes by airline and coarse reason.",
	}, []string{"airline", "reason"})

	ScrapeDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "scrape_duration_seconds",
		Help:    "Wall time of a scrape attempt by airline, success or failure.",
		Buckets: []float64{1, 2, 5, 10, 15, 20, 30, 45, 60},
	}, []string{"airline"})

	// ScrapeEmptyResultsTotal counts stored scrapes that parsed to zero flights
	// — legitimate for a route with no award space, but also what selector
	// drift on the DOM extractors looks like.
	ScrapeEmptyResultsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "scrape_empty_results_total",
		Help: "Successful scrapes that stored an empty result set, by airline.",
	}, []string{"airline"})

	// KafkaConsumerLag is the worker pool's lag on the scrape.jobs topic:
	// messages produced but not yet fetched by the group.
	KafkaConsumerLag = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "kafka_consumer_lag",
		Help: "Worker consumer-group lag in messages, by topic.",
	}, []string{"topic"})

	// ScrapeCircuitState is the per-airline circuit breaker state as seen by the
	// worker pool: 0 = closed, 1 = open, 2 = half-open (Phase 6 Step 4).
	ScrapeCircuitState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "scrape_circuit_state",
		Help: "Per-airline circuit breaker state (0=closed, 1=open, 2=half-open).",
	}, []string{"airline"})

	// DLQMessagesTotal counts jobs the worker gave up on and wrote to the Kafka
	// dead-letter topic, by airline and the coarse failure reason (Phase 6 Step 4).
	DLQMessagesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "dlq_messages_total",
		Help: "Permanently failed jobs written to the Kafka DLQ, by airline and reason.",
	}, []string{"airline", "reason"})

	// ETL counters. The ETL is a batch process that starts fresh each run and
	// PUTs its final values to the Pushgateway (which stores, never sums), so
	// these read as "the most recent run" rather than an all-time total —
	// ETLAwardsWritten and ETLDocsProcessed are the context a run's parse
	// failures sit against.
	ETLParseFailuresTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "etl_parse_failures_total",
		Help: "Raw scrapes the most recent ETL run failed to parse, by airline.",
	}, []string{"airline"})

	ETLAwardsWrittenTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "etl_awards_written_total",
		Help: "Normalized award rows written to Postgres in the most recent ETL run.",
	})

	ETLDocsProcessedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "etl_docs_processed_total",
		Help: "Deduped raw scrape docs processed in the most recent ETL run.",
	})
)

// Handler serves the Prometheus exposition format. The API mounts it at
// /metrics outside its CORS and rate-limit middleware.
func Handler() http.Handler {
	return promhttp.Handler()
}

// ListenAndServe runs a minimal HTTP server exposing /metrics on addr, for
// binaries that are not already HTTP servers (the worker pool). It blocks, so
// callers run it in a goroutine; a bind failure is returned, not fatal.
func ListenAndServe(addr string) error {
	mux := http.NewServeMux()
	mux.Handle(metricsPath, Handler())
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	return srv.ListenAndServe()
}

// PushETL ships the current metric values to a Pushgateway under job "etl". The
// ETL is a short-lived batch job Prometheus cannot scrape directly, so it
// pushes once on completion. Best-effort: the caller logs any error.
func PushETL(pushgatewayURL string) error {
	return push.New(pushgatewayURL, "etl").
		Gatherer(prometheus.DefaultGatherer).
		Push()
}
