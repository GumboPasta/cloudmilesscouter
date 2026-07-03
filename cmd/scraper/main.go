package main

import (
	"context"
	"log"
	"time"

	"cloudmilesscouter/internal/config"
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
}
