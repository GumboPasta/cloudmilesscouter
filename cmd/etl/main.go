package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"time"

	"cloudmilesscouter/internal/config"
	"cloudmilesscouter/internal/etl"
	"cloudmilesscouter/internal/storage"
)

func main() {
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := storage.Connect(ctx, cfg.MongoURI)
	if err != nil {
		log.Fatalf("mongo connection failed: %v", err)
	}
	defer client.Disconnect(context.Background())

	log.Println("MongoDB is reachable at", cfg.MongoURI)

	pg, err := storage.ConnectPostgres(ctx, cfg.PostgresURI)
	if err != nil {
		log.Fatalf("postgres connection failed: %v", err)
	}
	defer pg.Close()

	log.Println("PostgreSQL is reachable at", cfg.PostgresURI)

	if err := etl.Run(context.Background(), client, pg); err != nil {
		slog.Error("etl run failed", "err", err)
		os.Exit(1)
	}
}
