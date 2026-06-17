package core

import (
	"bamboo/helpers"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/k0kubun/pp/v3"
	twilio "github.com/twilio/twilio-go"
	openapi "github.com/twilio/twilio-go/rest/api/v2010"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"bamboo/sms/utils"
)

// Sender handles sending SMS messages via Twilio
type Sender struct {
	mongoURI string
}

// NewSender creates a new SMS sender
func NewSender() (*Sender, error) {
	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	return &Sender{
		mongoURI: mongoURI,
	}, nil
}

// SendSMS sends a simple SMS message and tracks it in the database
func (s *Sender) SendSMS(to string, from string, message string, automated bool, teamId string) (string, error) {
	// Validate message length (Twilio maximum is 1600 characters)
	if len(message) > 1600 {
		return "", fmt.Errorf("message too long: %d characters (maximum is 1600)", len(message))
	}

	// Check if lead is in closed stage (only for automated messages to leads)
	if automated && teamId != "" {
		// Check if the recipient (to) is a lead in a closed stage
		isClosed, err := s.isLeadInClosedStage(teamId, to)
		if err != nil {
			pp.Printf("\x1b[33mSendSMS: Error checking lead stage for %s: %v\x1b[0m\n", to, err)
			// Continue with sending despite error
		} else if isClosed {
			pp.Printf("\x1b[33mSendSMS: Skipping SMS to %s - lead is in closed stage\x1b[0m\n", to)
			// Return a special error to indicate lead is closed
			return "", fmt.Errorf("lead is in closed stage, skipping SMS")
		}
	}

	// Initialize MongoDB connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(s.mongoURI))
	if err != nil {
		pp.Printf("\x1b[31mFailed to connect to MongoDB: %v\x1b[0m", err)
		return "", err
	}
	defer client.Disconnect(ctx)

	db := client.Database("sms")

	// Initialize Twilio client
	accountSid := os.Getenv("TWILIO_ACCOUNT_SID")
	authToken := os.Getenv("TWILIO_AUTH_TOKEN")

	twilioClient := twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: accountSid,
		Password: authToken,
	})

	params := &openapi.CreateMessageParams{}
	if len(to) == 10 {
		to = "+1" + to
	}
	params.SetTo(to)
	params.SetFrom(from)

	// Truncate message if it's too long (shouldn't happen due to validation above, but just in case)
	messageToSend := message
	if len(messageToSend) > 1600 {
		messageToSend = messageToSend[:1600]
	}

	// Sanitize the message to ensure valid UTF-8 and remove problematic characters
	// This prevents issues with special characters like "��" appearing in SMS
	messageToSend = helpers.CleanText(messageToSend)

	params.SetBody(messageToSend)

	resp, err := twilioClient.Api.CreateMessage(params)

	// Create SMS message object for tracking
	smsMessage := SMSMessage{
		Direction:  "outbound",
		Body:       messageToSend,
		From:       from,
		To:         to,
		Timestamp:  time.Now(),
		MediaCount: 0,
		Segments:   utils.CountSMSSegments(messageToSend),
		Automated:  automated,
	}

	if err != nil {
		// Handle error case
		errMsg := err.Error()
		smsMessage.Status = "failed"
		smsMessage.ErrorMessage = &errMsg

		// Store the failed message attempt
		_, dbErr := db.Collection("messages").InsertOne(context.Background(), smsMessage)
		if dbErr != nil {
			fmt.Printf("Failed to store SMS record: %v\n", dbErr)
		}

		return "", err
	}

	// Successful message sent
	smsMessage.MessageID = *resp.Sid
	smsMessage.Status = *resp.Status
	smsMessage.AccountSid = *resp.AccountSid

	// Store the message in the database
	_, dbErr := db.Collection("messages").InsertOne(context.Background(), smsMessage)
	if dbErr != nil {
		fmt.Printf("Failed to store SMS record: %v\n", dbErr)
	}

	return *resp.Sid, nil
}

// GetTeamSMSConfiguration fetches user config using a teamId
func (s *Sender) GetTeamSMSConfiguration(teamId string) (*SMSConfiguration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(s.mongoURI))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	db := client.Database("Users")

	var config SMSConfiguration
	err = db.Collection("sms-configurations").FindOne(ctx, bson.M{
		"teamId":      teamId,
		"autoRespond": true,
	}).Decode(&config)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, fmt.Errorf("no SMS configuration found for user %s", teamId)
		}
		return nil, fmt.Errorf("error fetching SMS configuration: %v", err)
	}

	return &config, nil
}

// GetMessagesBetweenPhoneNumbers retrieves SMS conversation history between two phone numbers
func (s *Sender) GetMessagesBetweenPhoneNumbers(fromNumber, toNumber string) ([]SMSMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(s.mongoURI))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	db := client.Database("sms")
	collection := db.Collection("messages")

	filter := bson.M{
		"$or": []bson.M{
			{"from": fromNumber, "to": toNumber},
			{"from": toNumber, "to": fromNumber},
		},
	}

	// Fetch last 20 messages sorted newest-first, then reverse to chronological.
	// 20 messages ≈ 10 back-and-forth exchanges — recent context without prompt bloat.
	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(20)
	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("error querying messages: %v", err)
	}
	defer cursor.Close(ctx)

	var messages []SMSMessage
	err = cursor.All(ctx, &messages)
	if err != nil {
		return nil, fmt.Errorf("error retrieving messages: %v", err)
	}

	// Remove duplicates based on MessageID
	seen := make(map[string]bool)
	var uniqueMessages []SMSMessage
	for _, msg := range messages {
		if msg.MessageID != "" && seen[msg.MessageID] {
			continue
		}
		if msg.MessageID != "" {
			seen[msg.MessageID] = true
		}
		uniqueMessages = append(uniqueMessages, msg)
	}

	// Reverse to chronological order (oldest first)
	for i, j := 0, len(uniqueMessages)-1; i < j; i, j = i+1, j-1 {
		uniqueMessages[i], uniqueMessages[j] = uniqueMessages[j], uniqueMessages[i]
	}

	return uniqueMessages, nil
}

// UpdateSMSMessageStatus updates the status of an SMS message in the database
func (s *Sender) UpdateSMSMessageStatus(messageID string, status string, errorCode *string, errorMessage *string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(s.mongoURI))
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	db := client.Database("sms")
	collection := db.Collection("messages")

	filter := bson.M{"messageId": messageID}
	update := bson.M{
		"$set": bson.M{
			"status":       status,
			"errorCode":    errorCode,
			"errorMessage": errorMessage,
			"updatedAt":    time.Now(),
		},
	}

	_, err = collection.UpdateOne(ctx, filter, update)
	if err != nil {
		return fmt.Errorf("error updating SMS message status: %v", err)
	}

	return nil
}

// GetSMSStats retrieves SMS statistics for a team
func (s *Sender) GetSMSStats(teamId string) (map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(s.mongoURI))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	db := client.Database("sms")
	collection := db.Collection("messages")

	// Get total messages sent
	totalSentFilter := bson.M{
		"direction": "outbound",
		"from":      bson.M{"$regex": primitive.Regex{Pattern: ".*" + teamId + ".*", Options: "i"}},
	}
	totalSent, err := collection.CountDocuments(ctx, totalSentFilter)
	if err != nil {
		return nil, fmt.Errorf("error counting sent messages: %v", err)
	}

	// Get automated messages sent
	automatedSentFilter := bson.M{
		"direction": "outbound",
		"automated": true,
		"from":      bson.M{"$regex": primitive.Regex{Pattern: ".*" + teamId + ".*", Options: "i"}},
	}
	automatedSent, err := collection.CountDocuments(ctx, automatedSentFilter)
	if err != nil {
		return nil, fmt.Errorf("error counting automated messages: %v", err)
	}

	// Get failed messages
	failedFilter := bson.M{
		"status": "failed",
		"from":   bson.M{"$regex": primitive.Regex{Pattern: ".*" + teamId + ".*", Options: "i"}},
	}
	failedCount, err := collection.CountDocuments(ctx, failedFilter)
	if err != nil {
		return nil, fmt.Errorf("error counting failed messages: %v", err)
	}

	// Get messages from last 7 days
	sevenDaysAgo := time.Now().AddDate(0, 0, -7)
	recentFilter := bson.M{
		"timestamp": bson.M{"$gte": sevenDaysAgo},
		"from":      bson.M{"$regex": primitive.Regex{Pattern: ".*" + teamId + ".*", Options: "i"}},
	}
	recentCount, err := collection.CountDocuments(ctx, recentFilter)
	if err != nil {
		return nil, fmt.Errorf("error counting recent messages: %v", err)
	}

	stats := map[string]interface{}{
		"teamId":           teamId,
		"totalSent":        totalSent,
		"automatedSent":    automatedSent,
		"failedCount":      failedCount,
		"recent7DaysCount": recentCount,
		"successRate":      float64(totalSent-failedCount) / float64(totalSent) * 100,
	}

	return stats, nil
}

// GetTeamIdByPhoneNumber looks up the teamId associated with a phone number
func GetTeamIdByPhoneNumber(phoneNumber string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return "", fmt.Errorf("failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	// Normalize phone number
	normalizedPhone := phoneNumber
	if len(phoneNumber) == 10 {
		normalizedPhone = "+1" + phoneNumber
	} else if len(phoneNumber) == 11 && phoneNumber[0] == '1' {
		normalizedPhone = "+" + phoneNumber
	}

	// Look up in Users.sms-configurations
	collection := client.Database("Users").Collection("sms-configurations")

	var config SMSConfiguration
	err = collection.FindOne(ctx, bson.M{
		"phoneNumber": normalizedPhone,
	}).Decode(&config)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			// Try without +1 prefix
			if len(normalizedPhone) > 2 && normalizedPhone[:2] == "+1" {
				withoutPrefix := normalizedPhone[2:]
				err = collection.FindOne(ctx, bson.M{
					"phoneNumber": withoutPrefix,
				}).Decode(&config)
				if err == nil {
					return config.TeamID, nil
				}
			}
			return "", fmt.Errorf("no team found for phone number %s", phoneNumber)
		}
		return "", fmt.Errorf("error looking up team: %v", err)
	}

	return config.TeamID, nil
}

// GetMessages retrieves all messages for a phone number
func GetMessages(phoneNumber string) ([]SMSMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	db := client.Database("sms")
	collection := db.Collection("messages")

	filter := bson.M{
		"$or": []bson.M{
			{"from": phoneNumber},
			{"to": phoneNumber},
		},
	}

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("error querying messages: %v", err)
	}
	defer cursor.Close(ctx)

	var messages []SMSMessage
	err = cursor.All(ctx, &messages)
	if err != nil {
		return nil, fmt.Errorf("error retrieving messages: %v", err)
	}

	return messages, nil
}

// isLeadInClosedStage checks if a lead is in a closed stage
func (s *Sender) isLeadInClosedStage(teamId, phoneNumber string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(s.mongoURI))
	if err != nil {
		return false, fmt.Errorf("failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	// Normalize phone number
	normalizedPhone := phoneNumber
	if len(phoneNumber) == 11 && phoneNumber[0] == '1' {
		normalizedPhone = phoneNumber[1:]
	}
	if len(phoneNumber) == 10 {
		normalizedPhone = phoneNumber
	}
	if len(phoneNumber) > 10 && phoneNumber[0] == '+' {
		normalizedPhone = phoneNumber[2:]
	}

	collection := client.Database("teams").Collection("leads")

	filter := bson.M{
		"teamId": teamId,
		"phone":  normalizedPhone,
		"status": bson.M{"$in": []string{"Lost", "Closed", "Withdrawn", "Rejected"}},
	}

	count, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		return false, fmt.Errorf("error checking lead stage: %v", err)
	}

	return count > 0, nil
}
