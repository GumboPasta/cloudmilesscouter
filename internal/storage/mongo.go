package storage

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Connect creates a MongoDB client for the given URI and verifies
// the deployment is reachable via Ping before returning.
func Connect(ctx context.Context, uri string) (*mongo.Client, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}
	return client, nil
}

// RawScrape is one scrape result stored exactly as the airline returned it,
// alongside the metadata needed to find it again before Phase 2 normalizes it.
type RawScrape struct {
	Airline     string    `bson:"airline"`
	Origin      string    `bson:"origin"`
	Destination string    `bson:"destination"`
	SearchDate  time.Time `bson:"search_date"`
	ScrapedAt   time.Time `bson:"scraped_at"`
	RawPayload  string    `bson:"raw_payload"`
}

// StoreRawScrape inserts doc into data.flight_scrapes as-is, no parsing.
func StoreRawScrape(ctx context.Context, client *mongo.Client, doc RawScrape) error {
	collection := client.Database("data").Collection("flight_scrapes")
	_, err := collection.InsertOne(ctx, doc)
	return err
}

// FindRawScrapes returns every raw scrape stored in data.flight_scrapes.
func FindRawScrapes(ctx context.Context, client *mongo.Client) ([]RawScrape, error) {
	collection := client.Database("data").Collection("flight_scrapes")
	cursor, err := collection.Find(ctx, bson.D{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var docs []RawScrape
	if err := cursor.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}
