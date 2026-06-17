package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/k0kubun/pp/v3"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"bamboo/sms/core"
	"bamboo/sms/utils"
)

// Handler provides HTTP handlers for SMS operations
type Handler struct {
	sender *core.Sender
}

// NewHandler creates a new SMS handler
func NewHandler() (*Handler, error) {
	sender, err := core.NewSender()
	if err != nil {
		return nil, err
	}
	return &Handler{sender: sender}, nil
}

// HandleSendSMS handles sending an SMS message
func (h *Handler) HandleSendSMS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(SendSMSResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	var req SendSMSRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(SendSMSResponse{
			Success: false,
			Error:   "Invalid JSON payload",
		})
		return
	}

	// Validate required fields
	if req.To == "" || req.From == "" || req.Message == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(SendSMSResponse{
			Success: false,
			Error:   "Missing required fields: to, from, or message",
		})
		return
	}

	// Send SMS
	messageID, err := h.sender.SendSMS(req.To, req.From, req.Message, req.Automated, req.TeamID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(SendSMSResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to send SMS: %v", err),
		})
		return
	}

	json.NewEncoder(w).Encode(SendSMSResponse{
		Success:   true,
		MessageID: messageID,
	})
}

// HandleGetConversation handles retrieving SMS conversation between two numbers
func (h *Handler) HandleGetConversation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(GetConversationResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	var req GetConversationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(GetConversationResponse{
			Success: false,
			Error:   "Invalid JSON payload",
		})
		return
	}

	// Validate required fields
	if req.FromNumber == "" || req.ToNumber == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(GetConversationResponse{
			Success: false,
			Error:   "Missing required fields: fromNumber or toNumber",
		})
		return
	}

	// Get conversation
	messages, err := h.sender.GetMessagesBetweenPhoneNumbers(req.FromNumber, req.ToNumber)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(GetConversationResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to get conversation: %v", err),
		})
		return
	}

	json.NewEncoder(w).Encode(GetConversationResponse{
		Success:  true,
		Messages: messages,
	})
}

// HandleGetConfig handles retrieving SMS configuration for a team
func (h *Handler) HandleGetConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(GetConfigResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	var req GetConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(GetConfigResponse{
			Success: false,
			Error:   "Invalid JSON payload",
		})
		return
	}

	// Validate required fields
	if req.TeamID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(GetConfigResponse{
			Success: false,
			Error:   "Missing required field: teamId",
		})
		return
	}

	// Get configuration
	config, err := h.sender.GetTeamSMSConfiguration(req.TeamID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(GetConfigResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to get SMS configuration: %v", err),
		})
		return
	}

	json.NewEncoder(w).Encode(GetConfigResponse{
		Success: true,
		Config:  config,
	})
}

// HandleGetStats handles retrieving SMS statistics for a team
func (h *Handler) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(GetStatsResponse{
			Success: false,
			Error:   "Method not allowed",
		})
		return
	}

	var req GetStatsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(GetStatsResponse{
			Success: false,
			Error:   "Invalid JSON payload",
		})
		return
	}

	// Validate required fields
	if req.TeamID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(GetStatsResponse{
			Success: false,
			Error:   "Missing required field: teamId",
		})
		return
	}

	// Get statistics
	stats, err := h.sender.GetSMSStats(req.TeamID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(GetStatsResponse{
			Success: false,
			Error:   fmt.Sprintf("Failed to get SMS statistics: %v", err),
		})
		return
	}

	json.NewEncoder(w).Encode(GetStatsResponse{
		Success: true,
		Stats:   stats,
	})
}

// HandleWebhook handles incoming webhooks from Twilio
func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Method not allowed",
		})
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to parse form data",
		})
		return
	}

	// Extract webhook data
	webhook := WebhookRequest{
		MessageSid:   r.FormValue("MessageSid"),
		AccountSid:   r.FormValue("AccountSid"),
		MessagingSid: r.FormValue("MessagingServiceSid"),
		From:         r.FormValue("From"),
		To:           r.FormValue("To"),
		Body:         r.FormValue("Body"),
		NumMedia:     r.FormValue("NumMedia"),
		APIVersion:   r.FormValue("ApiVersion"),
		SmsSid:       r.FormValue("SmsSid"),
		SmsStatus:    r.FormValue("SmsStatus"),
		NumSegments:  r.FormValue("NumSegments"),
	}

	// Extract media URLs and types
	for i := 0; i < 10; i++ {
		mediaURL := r.FormValue(fmt.Sprintf("MediaUrl%d", i))
		mediaType := r.FormValue(fmt.Sprintf("MediaContentType%d", i))
		if mediaURL != "" {
			webhook.MediaURLs = append(webhook.MediaURLs, mediaURL)
			webhook.MediaTypes = append(webhook.MediaTypes, mediaType)
		}
	}

	// Store incoming message in database
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		pp.Printf("\x1b[31mFailed to connect to MongoDB: %v\x1b[0m", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to store message",
		})
		return
	}
	defer client.Disconnect(ctx)

	db := client.Database("sms")
	collection := db.Collection("messages")

	// Create SMS message object
	smsMessage := core.SMSMessage{
		MessageID:  webhook.MessageSid,
		Body:       webhook.Body,
		From:       webhook.From,
		To:         webhook.To,
		Status:     webhook.SmsStatus,
		Direction:  "inbound",
		Timestamp:  time.Now(),
		MediaCount: len(webhook.MediaURLs),
		AccountSid: webhook.AccountSid,
		Segments: func() int {
			if webhook.NumSegments != "" {
				var seg int
				fmt.Sscanf(webhook.NumSegments, "%d", &seg)
				return seg
			}
			return 1
		}(),
		Automated: false,
	}

	// Store the message
	_, err = collection.InsertOne(ctx, smsMessage)
	if err != nil {
		pp.Printf("\x1b[31mFailed to store incoming SMS: %v\x1b[0m", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to store message",
		})
		return
	}

	// Update SMS configuration stats
	h.updateSMSReceivedStats(webhook.From)

	// Return TwiML response for automated replies if needed
	w.Header().Set("Content-Type", "text/xml")
	fmt.Fprintf(w, `<?xml version="1.0" encoding="UTF-8"?>
<Response>
	<Message>Message received</Message>
</Response>`)
}

// updateSMSReceivedStats updates the SMS received count for a phone number
func (h *Handler) updateSMSReceivedStats(phoneNumber string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	db := client.Database("Users")
	collection := db.Collection("sms-configurations")

	// Normalize phone number
	normalizedPhone, err := utils.ValidatePhoneNumber(phoneNumber)
	if err != nil {
		normalizedPhone = phoneNumber
	}

	// Find configuration by phone number
	filter := bson.M{"phoneNumber": normalizedPhone}
	update := bson.M{
		"$inc": bson.M{"smsReceived": 1},
		"$set": bson.M{"updatedAt": time.Now()},
	}

	_, err = collection.UpdateOne(ctx, filter, update)
	if err != nil && err != mongo.ErrNoDocuments {
		return fmt.Errorf("error updating SMS stats: %v", err)
	}

	return nil
}

// HandleStatusCallback handles status callbacks from Twilio
func (h *Handler) HandleStatusCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Method not allowed",
		})
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to parse form data",
		})
		return
	}

	messageSid := r.FormValue("MessageSid")
	messageStatus := r.FormValue("MessageStatus")
	errorCode := r.FormValue("ErrorCode")
	errorMessage := r.FormValue("ErrorMessage")

	// Update message status in database
	var errorCodePtr *string
	var errorMsgPtr *string

	if errorCode != "" {
		errorCodePtr = &errorCode
	}
	if errorMessage != "" {
		errorMsgPtr = &errorMessage
	}

	err := h.sender.UpdateSMSMessageStatus(messageSid, messageStatus, errorCodePtr, errorMsgPtr)
	if err != nil {
		pp.Printf("\x1b[31mFailed to update SMS status: %v\x1b[0m", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Failed to update message status",
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": "Status updated successfully",
	})
}

// HandleBulkSend handles sending bulk SMS messages
func (h *Handler) HandleBulkSend(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Method not allowed",
		})
		return
	}

	var req BulkSendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Invalid JSON payload",
		})
		return
	}

	// Validate required fields
	if req.TeamID == "" || req.From == "" || len(req.Messages) == 0 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Missing required fields: teamId, from, or messages",
		})
		return
	}

	// Limit bulk send to 100 messages at a time
	if len(req.Messages) > 100 {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   "Too many messages. Maximum 100 per bulk send",
		})
		return
	}

	// Process messages in parallel with rate limiting
	results := make([]sendResult, len(req.Messages))
	semaphore := make(chan struct{}, 5) // Limit to 5 concurrent sends

	var wg sync.WaitGroup
	for i, msg := range req.Messages {
		wg.Add(1)
		go func(idx int, to, message string) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			messageID, err := h.sender.SendSMS(to, req.From, message, true, req.TeamID)
			result := sendResult{
				To:      to,
				Success: err == nil,
			}

			if err != nil {
				result.Error = err.Error()
			} else {
				result.MessageID = messageID
			}

			results[idx] = result
		}(i, msg.To, msg.Message)
	}

	wg.Wait()

	// Calculate statistics
	successCount := 0
	failedCount := 0
	for _, result := range results {
		if result.Success {
			successCount++
		} else {
			failedCount++
		}
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"results":      results,
		"totalSent":    len(req.Messages),
		"successCount": successCount,
		"failedCount":  failedCount,
		"successRate":  float64(successCount) / float64(len(req.Messages)) * 100,
	})
}
