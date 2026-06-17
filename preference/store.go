package preference

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const collName = "lead_preferences"

// Store handles MongoDB operations for lead preferences
type Store struct {
	client     *mongo.Client
	collection *mongo.Collection
}

// NewStore creates a new preference store
func NewStore() (*Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Verify connection
	err = client.Ping(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	// Get database name at runtime (after env is loaded)
	dbName := os.Getenv("MONGODB_DATABASE")
	if dbName == "" {
		return nil, fmt.Errorf("database name cannot be empty: MONGODB_DATABASE not set")
	}

	database := client.Database(dbName)
	collection := database.Collection(collName)

	return &Store{
		client:     client,
		collection: collection,
	}, nil
}

// Close closes the MongoDB connection
func (s *Store) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

// GetBySessionID retrieves preferences by session ID
func (s *Store) GetBySessionID(ctx context.Context, sessionID string) (*LeadPreference, error) {
	var pref LeadPreference
	err := s.collection.FindOne(ctx, bson.M{"sessionId": sessionID}).Decode(&pref)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}

		// Check if it's a decode error (corrupted data)
		if strings.Contains(err.Error(), "cannot decode array into an integer type") ||
			strings.Contains(err.Error(), "decode error") {
			// Return empty preferences for corrupted data
			return &LeadPreference{
				SessionID:         sessionID,
				Preferences:       Preferences{},
				MatchedProperties: []string{},
			}, nil
		}

		return nil, fmt.Errorf("failed to get preference: %w", err)
	}
	return &pref, nil
}

// GetByLeadID retrieves preferences by lead ID
func (s *Store) GetByLeadID(ctx context.Context, leadID string) (*LeadPreference, error) {
	var pref LeadPreference
	err := s.collection.FindOne(ctx, bson.M{"leadId": leadID}).Decode(&pref)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get preference: %w", err)
	}
	return &pref, nil
}

// Upsert creates or updates a preference record
func (s *Store) Upsert(ctx context.Context, pref *LeadPreference) error {
	pref.UpdatedAt = time.Now()

	filter := bson.M{"sessionId": pref.SessionID}

	// First try to find existing document
	var existing LeadPreference
	err := s.collection.FindOne(ctx, filter).Decode(&existing)

	if err == mongo.ErrNoDocuments {
		// No existing document - insert new one with createdAt
		pref.CreatedAt = time.Now()
		_, err = s.collection.InsertOne(ctx, pref)
		if err != nil {
			return fmt.Errorf("failed to insert preference: %w", err)
		}
		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to check existing preference: %w", err)
	}

	// Document exists - update using $set for all fields
	update := bson.M{
		"$set": bson.M{
			"preferences":       pref.Preferences,
			"matchedProperties": pref.MatchedProperties,
			"updatedAt":         pref.UpdatedAt,
		},
	}

	_, err = s.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update preference: %w", err)
	}
	return nil
}

// UpdatePreferences updates just the preferences part
func (s *Store) UpdatePreferences(ctx context.Context, sessionID string, prefs Preferences) error {
	filter := bson.M{"sessionId": sessionID}
	update := bson.M{
		"$set": bson.M{
			"preferences": prefs,
			"updatedAt":   time.Now(),
		},
	}

	_, err := s.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to update preferences: %w", err)
	}
	return nil
}

// SetMatchedProperties updates the matched properties list
func (s *Store) SetMatchedProperties(ctx context.Context, sessionID string, propertyIDs []string) error {
	filter := bson.M{"sessionId": sessionID}
	update := bson.M{
		"$set": bson.M{
			"matchedProperties": propertyIDs,
			"updatedAt":         time.Now(),
		},
	}

	_, err := s.collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("failed to set matched properties: %w", err)
	}
	return nil
}

// Delete removes a preference record
func (s *Store) Delete(ctx context.Context, sessionID string) error {
	_, err := s.collection.DeleteOne(ctx, bson.M{"sessionId": sessionID})
	if err != nil {
		return fmt.Errorf("failed to delete preference: %w", err)
	}
	return nil
}
