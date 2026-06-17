package report

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"runtime"

	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Error struct {
	Location string `json:"location"`
	Message  string `json:"message"`
}

func GetErrorLocation() string {
	_, file, line, _ := runtime.Caller(1)
	return fmt.Sprintf("%s:%d", file, line)
}

func GetErrorMessage(err error) string {
	return err.Error()
}

func GetError(err error) Error {
	return Error{
		Location: GetErrorLocation(),
		Message:  GetErrorMessage(err),
	}
}

type ErrorCollection struct {
	ID        string    `bson:"id"`
	CreatedAt time.Time `bson:"createdAt"`
	Errors    []Error   `bson:"errors"`
}

func reportToDiscord(errorMsg string) {
	webhookURL := os.Getenv("ERROR_WEBHOOK_URL")
	if webhookURL == "" {
		fmt.Println("Discord webhook URL not set in .env file")
		return
	}

	embed := map[string]interface{}{
		"title":       "Error Report",
		"description": errorMsg,
		"color":       16711680, // Red color
		"timestamp":   time.Now().Format(time.RFC3339),
		"footer": map[string]string{
			"text": "rentbamboo-charles-api",
		},
	}

	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{embed},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		fmt.Printf("Error marshaling Discord payload: %v", err)
		return
	}

	req, err := http.NewRequest("POST", webhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		fmt.Printf("Error creating Discord request: %v", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error sending to Discord: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		fmt.Printf("Discord webhook returned error status: %d", resp.StatusCode)
	}
}

func InsertError(error string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		fmt.Printf("Error connecting to MongoDB: %v", err)
		return
	}
	defer client.Disconnect(ctx)

	dbName := os.Getenv("ERROR")
	collName := os.Getenv("ERROR_COLLECTION")
	if dbName == "" || collName == "" {
		fmt.Println("Database or collection name not set in .env file")
		return
	}

	collection := client.Database(dbName).Collection(collName)

	errorCollection := ErrorCollection{
		ID:        primitive.NewObjectID().Hex(),
		CreatedAt: time.Now(),
		Errors: []Error{{
			Location: GetErrorLocation(),
			Message:  error,
		}},
	}

	_, err = collection.InsertOne(ctx, errorCollection)
	if err != nil {
		fmt.Printf("\x1b[31mError inserting error: %v\x1b[0m", err)
		reportToDiscord(fmt.Sprintf("MongoDB Insert Error: %v", err))
		return
	}
	fmt.Printf("\x1b[32mSuccessfully inserted error record\x1b[0m")
}
