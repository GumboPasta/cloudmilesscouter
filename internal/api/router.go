// Package api builds the HTTP router for the REST API (Phase 4): the middleware
// chain plus the /api/* endpoints — search, airlines, routes, and a scrape
// trigger that dispatches jobs onto the Kafka queue.
package api

import (
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/httprate"

	"cloudmilesscouter/internal/config"
	"cloudmilesscouter/internal/metrics"
	"cloudmilesscouter/internal/storage"
)

// server holds the dependencies the /api/* handlers share: the loaded config,
// the Postgres pool the read endpoints query, the Redis read cache /api/search
// checks first (nil when Redis was unreachable at startup — caching is then
// skipped), and the queue dispatcher POST /api/scrape enqueues jobs on.
type server struct {
	cfg        config.Config
	db         *sql.DB
	cache      *storage.Cache
	dispatcher scrapeDispatcher
}

// NewRouter assembles the router and its middleware chain. The chain, outermost
// first: RequestID and RealIP populate request context, requestLogger records
// one structured line per request, metricsRecorder feeds the Prometheus request
// counter and latency histogram, Recoverer turns a handler panic into a 500
// instead of a dropped connection, CORS answers browser preflights, and the
// rate limiter caps requests per client IP.
//
// GET /metrics is served from an outer ServeMux, ahead of the chi router, so
// Prometheus scraping it every 15s never touches the request log, the request
// metrics, CORS, or the rate limiter.
func NewRouter(cfg config.Config, db *sql.DB, cache *storage.Cache, dispatcher scrapeDispatcher) http.Handler {
	srv := &server{cfg: cfg, db: db, cache: cache, dispatcher: dispatcher}
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(requestLogger)
	r.Use(metricsRecorder)
	r.Use(middleware.Recoverer)

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSAllowedOrigins,
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	if cfg.RateLimitPerMinute > 0 {
		r.Use(httprate.LimitByIP(cfg.RateLimitPerMinute, time.Minute))
	}

	r.Get("/healthz", healthz)

	r.Route("/api", func(r chi.Router) {
		r.Get("/search", srv.handleSearch)
		r.Get("/airlines", srv.handleAirlines)
		r.Get("/routes", srv.handleRoutes)
		r.Post("/scrape", srv.handleScrape)
	})

	mux := http.NewServeMux()
	mux.Handle("/metrics", metrics.Handler())
	mux.Handle("/", r)
	return mux
}

// healthz is a liveness probe: it reports that the process is up and serving.
// It intentionally does not check MongoDB or PostgreSQL — those get a readiness
// endpoint of their own once the data endpoints need them.
func healthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// writeJSON is the single response encoder shared by every handler.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		slog.Error("response encode failed", "err", err)
	}
}

// requestLogger emits one slog line per request with method, path, status,
// byte count, duration, and the chi request ID, matching the structured
// logging the worker and etl commands use.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		slog.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration", time.Since(start).String(),
			"request_id", middleware.GetReqID(r.Context()),
			"remote", r.RemoteAddr,
		)
	})
}

// metricsRecorder feeds the Prometheus request counter and latency histogram.
// It labels by the chi route pattern ("/api/search", "/api/*" for an unmatched
// path) rather than the raw URL so an attacker spraying random paths cannot
// blow up the metric cardinality.
func metricsRecorder(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		metrics.HTTPRequestDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
		metrics.HTTPRequestsTotal.WithLabelValues(r.Method, route, strconv.Itoa(ww.Status())).Inc()
	})
}
