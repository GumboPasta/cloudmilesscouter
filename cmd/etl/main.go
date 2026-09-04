package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cloudmilesscouter/internal/config"
	"cloudmilesscouter/internal/etl"
	"cloudmilesscouter/internal/logging"
	"cloudmilesscouter/internal/metrics"
	"cloudmilesscouter/internal/storage"
)

func main() {
	cfg := config.Load()
	logging.Setup("etl", cfg.LogLevel, cfg.LogFormat)

	// Graceful shutdown: SIGINT/SIGTERM cancels the run. storage.WriteAwards is
	// transactional, so a cancel mid-batch rolls back cleanly rather than
	// leaving Postgres half-written.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	client, err := storage.Connect(connectCtx, cfg.MongoURI)
	if err != nil {
		slog.Error("mongo connection failed", "err", err)
		os.Exit(1)
	}
	defer client.Disconnect(context.Background())

	slog.Info("mongo reachable", "uri", cfg.MongoURI)

	pg, err := storage.ConnectPostgres(connectCtx, cfg.PostgresURI)
	if err != nil {
		slog.Error("postgres connection failed", "err", err)
		os.Exit(1)
	}
	defer pg.Close()

	slog.Info("postgres reachable", "uri", cfg.PostgresURI)

	// Optional: drop cached /api/search results for the routes this run
	// rewrites. If Redis is down the ETL still runs; stale keys expire on TTL.
	var cache *storage.Cache
	if c, err := storage.NewCache(connectCtx, cfg.RedisAddr); err != nil {
		slog.Warn("redis unreachable, skipping cache invalidation", "err", err, "addr", cfg.RedisAddr)
	} else {
		cache = c
		defer cache.Close()
	}

	if err := etl.Run(ctx, client, pg, cache); err != nil {
		if errors.Is(err, context.Canceled) {
			slog.Warn("etl run interrupted")
			return
		}
		slog.Error("etl run failed", "err", err)
		os.Exit(1)
	}

	// The ETL is a batch job Prometheus cannot scrape, so push this run's
	// counters to the Pushgateway. Best-effort: a gateway that is down or unset
	// just means this run's parse-failure counts are not recorded.
	if err := metrics.PushETL(cfg.PushgatewayURL); err != nil {
		slog.Warn("metrics push failed", "err", err, "pushgateway", cfg.PushgatewayURL)
	}
}
