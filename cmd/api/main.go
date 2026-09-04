// Command api serves the REST API that the frontend queries (Phase 4). Run it
// from the repo root so config.Load() picks up .env; it listens on cfg.APIPort.
//
// It serves /healthz plus the /api/* endpoints over the normalized Postgres
// data. POST /api/scrape dispatches jobs onto Kafka (KAFKA_BROKERS), so the
// worker pool must be running for a triggered scrape to actually happen.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cloudmilesscouter/internal/api"
	"cloudmilesscouter/internal/config"
	"cloudmilesscouter/internal/logging"
	"cloudmilesscouter/internal/queue"
	"cloudmilesscouter/internal/storage"
)

// shutdownTimeout bounds how long a graceful shutdown waits for in-flight
// requests to finish before the process exits anyway.
const shutdownTimeout = 10 * time.Second

func main() {
	cfg := config.Load()
	logging.Setup("api", cfg.LogLevel, cfg.LogFormat)

	connectCtx, cancelConnect := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelConnect()
	db, err := storage.ConnectPostgres(connectCtx, cfg.PostgresURI)
	if err != nil {
		slog.Error("postgres connection failed", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	// The read cache is optional: if Redis is unreachable at startup the API
	// still serves, reading Postgres on every /api/search. A nil *storage.Cache
	// is a no-op cache, so nothing downstream needs to branch on this.
	redisCtx, cancelRedis := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelRedis()
	var cache *storage.Cache
	if c, err := storage.NewCache(redisCtx, cfg.RedisAddr); err != nil {
		slog.Warn("redis unreachable, running without a search cache", "err", err, "addr", cfg.RedisAddr)
	} else {
		cache = c
		defer cache.Close()
	}

	// The Kafka writer connects lazily on the first Enqueue, so building it here
	// does not fail startup or require a broker to be up until a scrape is
	// triggered.
	producer := queue.NewProducer(cfg.KafkaBrokers)
	defer producer.Close()

	srv := &http.Server{
		Addr:              ":" + cfg.APIPort,
		Handler:           api.NewRouter(cfg, db, cache, producer),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		slog.Info("api listening", "addr", srv.Addr, "cors_origins", cfg.CORSAllowedOrigins,
			"rate_limit_per_minute", cfg.RateLimitPerMinute, "postgres", cfg.PostgresURI,
			"kafka_brokers", cfg.KafkaBrokers, "search_cache", cache != nil)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("api server failed", "err", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("api shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("api shutdown failed", "err", err)
		os.Exit(1)
	}
	slog.Info("api stopped")
}
