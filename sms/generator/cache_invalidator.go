package generator

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// StartPropertyChangeWatcher watches the teams.properties collection for
// changes (insert/update/delete) and invalidates caches so the AI never
// serves stale property data. Runs as a background goroutine with automatic
// reconnection on failure.
func StartPropertyChangeWatcher(mongoURI string) {
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	dbName := os.Getenv("MONGODB_DATABASE")
	if dbName == "" {
		dbName = "teams"
	}
	collName := os.Getenv("MONGODB_COLLECTION_PROPERTIES")
	if collName == "" {
		collName = "properties"
	}

	for {
		err := watchPropertyChanges(mongoURI, dbName, collName)
		if err != nil {
			log.Printf("⚠️  Property change watcher: %v — reconnecting in 5s", err)
		} else {
			log.Printf("⚠️  Property change watcher: stream ended — reconnecting in 5s")
		}
		time.Sleep(5 * time.Second)
	}
}

// watchPropertyChanges runs a single change stream session. Returns when the
// stream ends (connection drop, error, or clean close) so the caller can retry.
func watchPropertyChanges(mongoURI, dbName, collName string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer client.Disconnect(ctx)

	if err := client.Ping(ctx, nil); err != nil {
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	coll := client.Database(dbName).Collection(collName)
	fmt.Printf("🔄 Property change watcher: connected, watching %s.%s\n", dbName, collName)

	changeStream, err := coll.Watch(ctx, mongo.Pipeline{})
	if err != nil {
		return fmt.Errorf("failed to open change stream: %w", err)
	}
	defer changeStream.Close(ctx)

	for changeStream.Next(ctx) {
		var change bson.M
		if err := changeStream.Decode(&change); err != nil {
			log.Printf("⚠️  Property change watcher: decode error: %v", err)
			continue
		}

		// Extract teamId and property id from the change document.
		// For insert/update they're in fullDocument; for delete in documentKey.
		var teamID, propID string

		if fullDoc, ok := change["fullDocument"].(bson.M); ok {
			if t, ok := fullDoc["teamId"].(string); ok {
				teamID = t
			}
			if p, ok := fullDoc["id"].(string); ok {
				propID = p
			}
		} else if docKey, ok := change["documentKey"].(bson.M); ok {
			if id, ok := docKey["_id"]; ok {
				propID = fmt.Sprintf("%v", id)
			}
		}

		if teamID == "" || propID == "" {
			continue
		}

		InvalidateCaches(teamID, propID)
		fmt.Printf("🔄 Property changed (%s) — caches invalidated for team %s, property %s\n",
			change["operationType"], teamID[:8], propID[:8])
	}

	return changeStream.Err()
}
