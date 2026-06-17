package email

import (
	"bamboo/security"
	"bamboo/types"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/k0kubun/pp/v3"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Handler manages email API routes
type Handler struct {
}

// NewHandler creates a new email handler
func NewHandler(_ *mongo.Database) *Handler {
	return &Handler{}
}

// getMongoClient creates a new MongoDB client connection
func (h *Handler) getMongoClient(ctx context.Context) (*mongo.Client, error) {
	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	return client, nil
}

func (h *Handler) getEmailConfiguration(ctx context.Context, configID string) (*types.EmailConfiguration, error) {
	client, err := h.getMongoClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	dbName := os.Getenv("USERS_COLLECTION")
	collName := os.Getenv("EMAIL_CONFIGURATION_COLLECTION")

	if dbName == "" {
		dbName = "Users"
	}
	if collName == "" {
		collName = "email-configurations"
	}

	collection := client.Database(dbName).Collection(collName)

	pp.Println(configID)

	filter := bson.M{
		"configId": configID,
	}

	var result bson.M
	err = collection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		return nil, fmt.Errorf("failed to find email configuration: %w", err)
	}

	var config types.EmailConfiguration

	// Extract userId and teamId
	if userId, ok := result["userId"].(string); ok {
		config.UserID = userId
	}

	// Extract email
	if email, ok := result["email"].(string); ok {
		config.Email = email
	}

	if teamId, ok := result["teamId"].(string); ok {
		config.TeamID = teamId
	}

	// Extract scan
	if scan, ok := result["scan"].(bool); ok {
		config.Scan = &scan
	}

	// Extract tourTimeInterval
	if interval, ok := result["tourTimeInterval"].(int64); ok {
		val := int(interval)
		config.TourTimeInterval = &val
	} else {
		val := 30
		config.TourTimeInterval = &val
	}

	// Extract companyGiven
	if companyGiven, ok := result["companyGiven"].(bool); ok {
		config.CompanyGiven = companyGiven
	}

	// Extract hasAutoRespond
	if hasAutoRespond, ok := result["hasAutoRespond"].(bool); ok {
		config.HasAutoRespond = hasAutoRespond
	}

	// Extract configId
	if configId, ok := result["configId"].(string); ok {
		config.ConfigID = configId
	}

	// Extract name
	if name, ok := result["name"].(string); ok {
		config.Name = &name
	}

	// Extract isDefault
	if isDefault, ok := result["isDefault"].(bool); ok {
		config.IsDefault = &isDefault
	}

	// Extract timestamps
	if created, ok := result["createdAt"].(primitive.DateTime); ok {
		config.CreatedAt = created.Time()
	}
	if updated, ok := result["updatedAt"].(primitive.DateTime); ok {
		config.UpdatedAt = updated.Time()
	}

	for key, value := range result {
		if key == "_id" || key == "userId" || key == "hasAutoRespond" || key == "teamId" || key == "tourTimeInterval" || key == "companyGiven" || key == "createdAt" || key == "updatedAt" || key == "scan" || key == "configId" || key == "name" || key == "isDefault" || key == "email" {
			continue
		}

		if _, ok := value.(primitive.ObjectID); ok {
			continue
		}

		if settingsM, ok := value.(primitive.M); ok {
			if key == "smtp" {
				var smtpConfig types.SMTPSettings
				for settingKey, settingValue := range settingsM {
					if settingKey == "dkim" {
						smtpConfig.DKIM = fmt.Sprint(settingValue)
						continue
					}

					if settingKey == "status" {
						if status, ok := settingValue.(string); ok {
							smtpConfig.Status = status
						}
						continue
					}

					// Default empty string if no value
					decryptedValue := ""
					if settingValue != nil {
						encryptedValue, ok := settingValue.(string)
						if ok {
							var decryptErr error
							decryptedValue, decryptErr = security.Decrypt(encryptedValue)
							if decryptErr != nil {
								return nil, fmt.Errorf("failed to decrypt SMTP %s: %w", settingKey, decryptErr)
							}
						}
					}

					switch settingKey {
					case "host":
						smtpConfig.Host = cleanString(decryptedValue)
					case "port":
						smtpConfig.Port = strings.TrimSpace(decryptedValue)
					case "username":
						smtpConfig.Username = cleanString(decryptedValue)
					case "password":
						smtpConfig.Password = cleanString(decryptedValue)
					}
				}
				config.SMTP = smtpConfig
			}

			if key == "imap" {
				var imapConfig types.IMAPSettings
				for settingKey, settingValue := range settingsM {
					if settingKey == "status" {
						if status, ok := settingValue.(string); ok {
							imapConfig.Status = status
						}
						continue
					}

					// Default empty string if no value
					decryptedValue := ""
					if settingValue != nil {
						encryptedValue, ok := settingValue.(string)
						if ok {
							var decryptErr error
							decryptedValue, decryptErr = security.Decrypt(encryptedValue)
							if decryptErr != nil {
								return nil, fmt.Errorf("failed to decrypt IMAP %s: %w", settingKey, decryptErr)
							}
						}
					}
					switch settingKey {
					case "host":
						imapConfig.Host = cleanString(decryptedValue)
					case "port":
						imapConfig.Port = strings.TrimSpace(decryptedValue)
					case "username":
						imapConfig.Username = cleanString(decryptedValue)
					case "password":
						imapConfig.Password = cleanString(decryptedValue)
					}
				}
				config.IMAP = imapConfig
			}
		}
	}
	pp.Println(config)
	return &config, nil
}

// getDatabase returns the MongoDB database
func (h *Handler) getDatabase(client *mongo.Client) *mongo.Database {
	dbName := os.Getenv("MONGODB_DATABASE")
	if dbName == "" {
		dbName = "teams"
	}
	return client.Database(dbName)
}

// convertPort converts a string port to int
func convertPort(portStr string) int {
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 {
		return 993 // default IMAP port
	}
	return port
}

// GetEmailsRequest represents the request to get emails
type GetEmailsRequest struct {
	ConfigID string `json:"configId"`
	Mailbox  string `json:"mailbox"`
	Page     int    `json:"page"`  // Page number (1-based)
	Limit    int    `json:"limit"` // Items per page (default 20)
}

// GetEmailsResponse represents the response for getting emails
type GetEmailsResponse struct {
	Emails     []EmailMessage `json:"emails"`
	Threads    []EmailThread  `json:"threads"`
	Success    bool           `json:"success"`
	Message    string         `json:"message,omitempty"`
	Page       int            `json:"page,omitempty"`
	Limit      int            `json:"limit,omitempty"`
	TotalCount int            `json:"totalCount,omitempty"`
}

// DeleteEmailRequest represents the request to delete an email
type DeleteEmailRequest struct {
	ConfigID string   `json:"configId"`
	Mailbox  string   `json:"mailbox"`
	UID      imap.UID `json:"uid"`
}

// MarkAsReadRequest represents the request to mark an email as read
type MarkAsReadRequest struct {
	ConfigID string   `json:"configId"`
	Mailbox  string   `json:"mailbox"`
	UID      imap.UID `json:"uid"`
}

// MoveToTrashRequest represents the request to move an email to trash
type MoveToTrashRequest struct {
	ConfigID string   `json:"configId"`
	Mailbox  string   `json:"mailbox"`
	UID      imap.UID `json:"uid"`
}

// ReplyEmailRequest represents the request to reply to an email
type ReplyEmailRequest struct {
	ConfigID   string   `json:"configId"`
	To         []string `json:"to"`
	Subject    string   `json:"subject"`
	Body       string   `json:"body"`
	BodyHTML   string   `json:"bodyHtml"`
	InReplyTo  string   `json:"inReplyTo"`
	References []string `json:"references"`
}

// SearchEmailsRequest represents the request to search emails
type SearchEmailsRequest struct {
	ConfigID string `json:"configId"`
	Mailbox  string `json:"mailbox"`
	Query    string `json:"query"`
	Page     int    `json:"page"`  // Page number (1-based)
	Limit    int    `json:"limit"` // Items per page (default 20)
}

// PreviewConfigRequest represents the request to preview a configuration
type PreviewConfigRequest struct {
	ConfigID string             `json:"configId,omitempty"`
	SMTP     types.SMTPSettings `json:"smtp"`
	IMAP     types.IMAPSettings `json:"imap"`
}

// StandardResponse represents a standard API response
type StandardResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// EmailStats represents analytics for an email configuration
type EmailStats struct {
	ConfigID           string    `json:"configId"`
	TeamID             string    `json:"teamId"`
	TotalEmailsFetched int       `json:"totalEmailsFetched"`
	TotalEmailsSent    int       `json:"totalEmailsSent"`
	TotalEmailsOpened  int       `json:"totalEmailsOpened"`
	LastActivity       time.Time `json:"lastActivity"`
}

// cleanString removes control characters and trims whitespace from a string
func cleanString(s string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			return -1
		}
		return r
	}, s))
}

// getEmailConfiguration retrieves email configuration from database using configId

// HandleGetEmails handles POST /e/emails
func (h *Handler) HandleGetEmails(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req GetEmailsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(GetEmailsResponse{
			Success: false,
			Message: "Invalid request body",
		})
		return
	}

	// Validate request
	if req.ConfigID == "" {
		json.NewEncoder(w).Encode(GetEmailsResponse{
			Success: false,
			Message: "ConfigID is required",
		})
		return
	}

	// Default mailbox
	if req.Mailbox == "" {
		req.Mailbox = "INBOX"
	}

	// Default pagination (for threads)
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit == 0 {
		req.Limit = 10
	}

	// Get email configuration
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := h.getEmailConfiguration(ctx, req.ConfigID)
	if err != nil {
		json.NewEncoder(w).Encode(GetEmailsResponse{
			Success: false,
			Message: "Email configuration not found",
		})
		return
	}

	// Create IMAP client
	// Note: The password field may contain colons, so we need to handle it carefully
	// The error "too many colons in address" suggests the host:port:password format issue
	imapHost := config.IMAP.Host
	imapPort := convertPort(config.IMAP.Port)

	pp.Println(config.IMAP)
	pp.Println(config.SMTP)

	imapClient := NewIMAPClient(
		imapHost,
		imapPort,
		config.IMAP.Username,
		config.IMAP.Password,
	)

	// Connect
	if err := imapClient.Connect(); err != nil {
		json.NewEncoder(w).Encode(GetEmailsResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to connect to IMAP server: %v", err),
		})
		return
	}
	defer imapClient.Disconnect()

	// Fetch recent emails for threading
	// Fetch enough to build complete threads but not too many for performance
	// Most recent ~200 emails should cover most active conversations
	fetchLimit := 200
	emails, err := imapClient.FetchEmails(req.Mailbox, fetchLimit)
	if err != nil {
		json.NewEncoder(w).Encode(GetEmailsResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to fetch emails: %v", err),
		})
		return
	}

	// Group ALL emails into threads FIRST
	threads := groupEmailsIntoThreads(emails)

	// NOW paginate the threads
	totalThreads := len(threads)
	offset := (req.Page - 1) * req.Limit
	start := offset
	if start > totalThreads {
		start = totalThreads
	}
	end := start + req.Limit
	if end > totalThreads {
		end = totalThreads
	}

	paginatedThreads := threads[start:end]

	// Collect all emails from paginated threads for the response
	var paginatedEmails []EmailMessage
	for _, thread := range paginatedThreads {
		paginatedEmails = append(paginatedEmails, thread.Messages...)
	}

	// Track analytics
	go h.trackEmailActivity(context.Background(), req.ConfigID, config.TeamID, "fetch", len(emails))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GetEmailsResponse{
		Success: true,
		// Emails:     paginatedEmails,
		Threads:    paginatedThreads,
		Page:       req.Page,
		Limit:      req.Limit,
		TotalCount: totalThreads,
	})
}

// HandleDeleteEmail handles DELETE /e/email
func (h *Handler) HandleDeleteEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req DeleteEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "Invalid request body",
		})
		return
	}

	// Validate request
	if req.ConfigID == "" || req.UID == 0 {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "ConfigID and UID are required",
		})
		return
	}

	// Default mailbox
	if req.Mailbox == "" {
		req.Mailbox = "INBOX"
	}

	// Get email configuration
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := h.getEmailConfiguration(ctx, req.ConfigID)
	if err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "Email configuration not found",
		})
		return
	}

	// Create IMAP client
	imapClient := NewIMAPClient(
		config.IMAP.Host,
		convertPort(config.IMAP.Port),
		config.IMAP.Username,
		config.IMAP.Password,
	)

	// Connect
	if err := imapClient.Connect(); err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to connect to IMAP server: %v", err),
		})
		return
	}
	defer imapClient.Disconnect()

	// Delete email
	if err := imapClient.DeleteEmail(req.Mailbox, req.UID); err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to delete email: %v", err),
		})
		return
	}

	// Track analytics
	go h.trackEmailActivity(context.Background(), req.ConfigID, config.TeamID, "delete", 1)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(StandardResponse{
		Success: true,
		Message: "Email deleted successfully",
	})
}

// HandleMarkAsRead handles POST /e/email/read
func (h *Handler) HandleMarkAsRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MarkAsReadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "Invalid request body",
		})
		return
	}

	// Validate request
	if req.ConfigID == "" || req.UID == 0 {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "ConfigID and UID are required",
		})
		return
	}

	// Default mailbox
	if req.Mailbox == "" {
		req.Mailbox = "INBOX"
	}

	// Get email configuration
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := h.getEmailConfiguration(ctx, req.ConfigID)
	if err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "Email configuration not found",
		})
		return
	}

	// Create IMAP client
	imapClient := NewIMAPClient(
		config.IMAP.Host,
		convertPort(config.IMAP.Port),
		config.IMAP.Username,
		config.IMAP.Password,
	)

	// Connect
	if err := imapClient.Connect(); err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to connect to IMAP server: %v", err),
		})
		return
	}
	defer imapClient.Disconnect()

	// Mark as read
	if err := imapClient.MarkAsRead(req.Mailbox, req.UID); err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to mark email as read: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(StandardResponse{
		Success: true,
		Message: "Email marked as read",
	})
}

// HandleMoveToTrash handles POST /e/email/trash
func (h *Handler) HandleMoveToTrash(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MoveToTrashRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "Invalid request body",
		})
		return
	}

	// Validate request
	if req.ConfigID == "" || req.UID == 0 {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "ConfigID and UID are required",
		})
		return
	}

	// Default mailbox
	if req.Mailbox == "" {
		req.Mailbox = "INBOX"
	}

	// Get email configuration
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := h.getEmailConfiguration(ctx, req.ConfigID)
	if err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "Email configuration not found",
		})
		return
	}

	// Create IMAP client
	imapClient := NewIMAPClient(
		config.IMAP.Host,
		convertPort(config.IMAP.Port),
		config.IMAP.Username,
		config.IMAP.Password,
	)

	// Connect
	if err := imapClient.Connect(); err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to connect to IMAP server: %v", err),
		})
		return
	}
	defer imapClient.Disconnect()

	// Move to trash
	if err := imapClient.MoveToTrash(req.Mailbox, req.UID); err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to move email to trash: %v", err),
		})
		return
	}

	// Track analytics
	go h.trackEmailActivity(context.Background(), req.ConfigID, config.TeamID, "trash", 1)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(StandardResponse{
		Success: true,
		Message: "Email moved to trash",
	})
}

// HandleReplyEmail handles POST /e/email/reply
func (h *Handler) HandleReplyEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ReplyEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "Invalid request body",
		})
		return
	}

	// Validate request
	if req.ConfigID == "" || len(req.To) == 0 || req.Body == "" {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "ConfigID, To, and Body are required",
		})
		return
	}

	// Get email configuration
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := h.getEmailConfiguration(ctx, req.ConfigID)
	if err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "Email configuration not found",
		})
		return
	}

	// Create SMTP client
	smtpClient := &SMTPClient{
		Host:     config.SMTP.Host,
		Port:     convertPort(config.SMTP.Port),
		Username: config.SMTP.Username,
		Password: config.SMTP.Password,
	}

	// Ensure subject has Re: prefix for replies
	subject := req.Subject
	if !strings.HasPrefix(strings.ToLower(subject), "re:") {
		subject = "Re: " + subject
	}

	// Send email using SMTP username as the actual email address
	senderEmail := config.SMTP.Username
	err = smtpClient.SendEmail(
		senderEmail,
		req.To,
		subject,
		req.Body,
		req.BodyHTML,
		senderEmail,
		req.InReplyTo,
		req.References,
	)

	if err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to send email: %v", err),
		})
		return
	}

	// Store sent email in database for tracking with configId
	go h.storeSentEmail(context.Background(), req, config)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(StandardResponse{
		Success: true,
		Message: "Email sent successfully",
	})
}

// HandleSearchEmails handles POST /e/emails/search
func (h *Handler) HandleSearchEmails(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SearchEmailsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(GetEmailsResponse{
			Success: false,
			Message: "Invalid request body",
		})
		return
	}

	// Validate request
	if req.ConfigID == "" || req.Query == "" {
		json.NewEncoder(w).Encode(GetEmailsResponse{
			Success: false,
			Message: "ConfigID and Query are required",
		})
		return
	}

	// Default mailbox
	if req.Mailbox == "" {
		req.Mailbox = "INBOX"
	}

	// Default pagination (for threads)
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit == 0 {
		req.Limit = 10
	}

	// Get email configuration
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := h.getEmailConfiguration(ctx, req.ConfigID)
	if err != nil {
		json.NewEncoder(w).Encode(GetEmailsResponse{
			Success: false,
			Message: "Email configuration not found",
		})
		return
	}

	// Create IMAP client
	imapClient := NewIMAPClient(
		config.IMAP.Host,
		convertPort(config.IMAP.Port),
		config.IMAP.Username,
		config.IMAP.Password,
	)

	// Connect
	if err := imapClient.Connect(); err != nil {
		json.NewEncoder(w).Encode(GetEmailsResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to connect to IMAP server: %v", err),
		})
		return
	}
	defer imapClient.Disconnect()

	// Search emails
	emails, err := imapClient.SearchEmails(req.Mailbox, req.Query)
	if err != nil {
		json.NewEncoder(w).Encode(GetEmailsResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to search emails: %v", err),
		})
		return
	}

	// Group ALL search results into threads FIRST
	threads := groupEmailsIntoThreads(emails)

	// NOW paginate the threads
	totalThreads := len(threads)
	offset := (req.Page - 1) * req.Limit
	start := offset
	if start > totalThreads {
		start = totalThreads
	}

	// Limit to 15 threads maximum
	maxThreads := 5
	if req.Limit > maxThreads {
		req.Limit = maxThreads
	}

	end := min(start+req.Limit, totalThreads)

	paginatedThreads := threads[start:end]

	// Collect all emails from paginated threads for the response
	var paginatedEmails []EmailMessage
	for _, thread := range paginatedThreads {
		paginatedEmails = append(paginatedEmails, thread.Messages...)
	}

	// Track analytics
	go h.trackEmailActivity(context.Background(), req.ConfigID, config.TeamID, "search", len(emails))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(GetEmailsResponse{
		Success: true,
		// Emails:     paginatedEmails,
		Threads:    paginatedThreads,
		Page:       req.Page,
		Limit:      req.Limit,
		TotalCount: totalThreads,
	})
}

// HandlePreviewConfig handles POST /e/config/preview
func (h *Handler) HandlePreviewConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PreviewConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "Invalid request body",
		})
		return
	}

	// Validate request
	if req.SMTP.Host == "" || req.IMAP.Host == "" {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "SMTP and IMAP configurations are required",
		})
		return
	}

	results := make(map[string]interface{})

	// Test IMAP connection
	imapClient := NewIMAPClient(
		req.IMAP.Host,
		convertPort(req.IMAP.Port),
		req.IMAP.Username,
		req.IMAP.Password,
	)

	imapErr := imapClient.TestConnection()
	if imapErr != nil {
		results["imap"] = map[string]interface{}{
			"success": false,
			"error":   imapErr.Error(),
		}
	} else {
		// Get mailboxes to verify connection
		if err := imapClient.Connect(); err == nil {
			mailboxes, _ := imapClient.GetMailboxes()
			imapClient.Disconnect()
			results["imap"] = map[string]interface{}{
				"success":   true,
				"mailboxes": mailboxes,
			}
		} else {
			results["imap"] = map[string]interface{}{
				"success": true,
			}
		}
	}

	// Test SMTP connection (simplified - just check if we can establish connection)
	smtpClient := &SMTPClient{
		Host:     req.SMTP.Host,
		Port:     convertPort(req.SMTP.Port),
		Username: req.SMTP.Username,
		Password: req.SMTP.Password,
	}

	// We can't fully test SMTP without sending an email, but we can validate the config
	if smtpClient.Host != "" && smtpClient.Port > 0 {
		results["smtp"] = map[string]interface{}{
			"success": true,
			"message": "SMTP configuration appears valid (send test email to fully verify)",
		}
	} else {
		results["smtp"] = map[string]interface{}{
			"success": false,
			"error":   "Invalid SMTP configuration",
		}
	}

	// Determine overall success
	imapSuccess := false
	smtpSuccess := false

	if imapResult, ok := results["imap"].(map[string]interface{}); ok {
		if success, ok := imapResult["success"].(bool); ok {
			imapSuccess = success
		}
	}

	if smtpResult, ok := results["smtp"].(map[string]interface{}); ok {
		if success, ok := smtpResult["success"].(bool); ok {
			smtpSuccess = success
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(StandardResponse{
		Success: imapSuccess && smtpSuccess,
		Message: "Configuration test completed",
		Data:    results,
	})
}

// HandleGetMailboxes handles POST /e/mailboxes
func (h *Handler) HandleGetMailboxes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ConfigID string `json:"configId"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "Invalid request body",
		})
		return
	}

	// Validate request
	if req.ConfigID == "" {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "ConfigID is required",
		})
		return
	}

	// Get email configuration
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := h.getEmailConfiguration(ctx, req.ConfigID)
	if err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "Email configuration not found",
		})
		return
	}

	// Create IMAP client
	imapClient := NewIMAPClient(
		config.IMAP.Host,
		convertPort(config.IMAP.Port),
		config.IMAP.Username,
		config.IMAP.Password,
	)

	// Connect
	if err := imapClient.Connect(); err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to connect to IMAP server: %v", err),
		})
		return
	}
	defer imapClient.Disconnect()

	// Get mailboxes
	mailboxes, err := imapClient.GetMailboxes()
	if err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to get mailboxes: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(StandardResponse{
		Success: true,
		Message: "Mailboxes retrieved successfully",
		Data:    map[string]interface{}{"mailboxes": mailboxes},
	})
}

// HandleGetStats handles GET /e/stats - Get analytics for a configuration
func (h *Handler) HandleGetStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ConfigID string `json:"configId"`
	}

	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			json.NewEncoder(w).Encode(StandardResponse{
				Success: false,
				Message: "Invalid request body",
			})
			return
		}
	} else {
		req.ConfigID = r.URL.Query().Get("configId")
	}

	// Validate request
	if req.ConfigID == "" {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "ConfigID is required",
		})
		return
	}

	// Get email configuration to retrieve teamID
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := h.getEmailConfiguration(ctx, req.ConfigID)
	if err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: "Email configuration not found",
		})
		return
	}

	stats, err := h.getEmailStats(ctx, req.ConfigID, config.TeamID)
	if err != nil {
		json.NewEncoder(w).Encode(StandardResponse{
			Success: false,
			Message: fmt.Sprintf("Failed to get stats: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(StandardResponse{
		Success: true,
		Message: "Stats retrieved successfully",
		Data:    stats,
	})
}

// groupEmailsIntoThreads groups emails by thread ID
func groupEmailsIntoThreads(emails []EmailMessage) []EmailThread {
	// Step 1: Build a map of MessageID -> Email index for quick lookups
	messageMap := make(map[string]int)
	for i := range emails {
		if emails[i].MessageID != "" {
			messageMap[emails[i].MessageID] = i
		}
	}

	// Step 2: Find root message for each email and build thread groups
	threadMap := make(map[string]*EmailThread)
	messageToRootID := make(map[string]string) // Maps message ID to its root thread ID

	// Helper function to find the root of a thread
	findRoot := func(messageID string) string {
		visited := make(map[string]bool)
		currentID := messageID

		for currentID != "" && !visited[currentID] {
			visited[currentID] = true

			// Check if we already know the root for this message
			if rootID, exists := messageToRootID[currentID]; exists {
				return rootID
			}

			// Look up the message
			if idx, exists := messageMap[currentID]; exists {
				email := &emails[idx]

				// Check if this message has a parent
				if len(email.References) > 0 {
					// First reference is the root
					currentID = email.References[0]
				} else if email.InReplyTo != "" {
					currentID = email.InReplyTo
				} else {
					// This is the root
					return currentID
				}
			} else {
				// Referenced message not in our list, use current as root
				return currentID
			}
		}

		return messageID
	}

	for i := range emails {
		email := &emails[i]

		// Find the root of this thread
		var rootThreadID string

		if len(email.References) > 0 {
			rootThreadID = findRoot(email.References[0])
		} else if email.InReplyTo != "" {
			rootThreadID = findRoot(email.InReplyTo)
		} else {
			rootThreadID = email.MessageID
		}

		// Ensure we have a valid thread ID
		if rootThreadID == "" {
			rootThreadID = email.MessageID
		}

		// Store the mapping
		if email.MessageID != "" {
			messageToRootID[email.MessageID] = rootThreadID
		}

		// Get or create the thread
		thread, exists := threadMap[rootThreadID]
		if !exists {
			// Remove Re:, Fwd:, etc. prefixes for cleaner subject
			cleanSubject := strings.TrimSpace(email.Subject)
			for strings.HasPrefix(strings.ToLower(cleanSubject), "re:") ||
				strings.HasPrefix(strings.ToLower(cleanSubject), "fwd:") ||
				strings.HasPrefix(strings.ToLower(cleanSubject), "fw:") {
				if strings.HasPrefix(strings.ToLower(cleanSubject), "re:") {
					cleanSubject = strings.TrimSpace(cleanSubject[3:])
				} else if strings.HasPrefix(strings.ToLower(cleanSubject), "fwd:") {
					cleanSubject = strings.TrimSpace(cleanSubject[4:])
				} else if strings.HasPrefix(strings.ToLower(cleanSubject), "fw:") {
					cleanSubject = strings.TrimSpace(cleanSubject[3:])
				}
			}

			thread = &EmailThread{
				ThreadID: rootThreadID,
				Subject:  cleanSubject,
				Messages: []EmailMessage{},
				LastDate: email.Date,
				IsRead:   true, // Will be set to false if any message is unread
				Snippet:  email.Snippet,
			}
			threadMap[rootThreadID] = thread
		}

		// Add message to thread (avoid duplicates)
		isDuplicate := false
		for _, msg := range thread.Messages {
			if msg.MessageID == email.MessageID {
				isDuplicate = true
				break
			}
		}

		if !isDuplicate {
			thread.Messages = append(thread.Messages, *email)
		}

		// Update thread metadata
		if email.Date.After(thread.LastDate) {
			thread.LastDate = email.Date
			thread.Snippet = email.Snippet // Use most recent message snippet
		}

		// Thread is unread if any message is unread
		if !email.IsRead {
			thread.IsRead = false
		}
	}

	// Step 3: Sort messages within each thread by date (oldest first)
	for _, thread := range threadMap {
		messages := thread.Messages
		for i := 0; i < len(messages)-1; i++ {
			for j := i + 1; j < len(messages); j++ {
				if messages[i].Date.After(messages[j].Date) {
					messages[i], messages[j] = messages[j], messages[i]
				}
			}
		}
		thread.Messages = messages
	}

	// Step 4: Convert map to slice and sort threads by last date (most recent first)
	threads := make([]EmailThread, 0, len(threadMap))
	for _, thread := range threadMap {
		threads = append(threads, *thread)
	}

	// Sort threads by LastDate descending (most recent first)
	for i := 0; i < len(threads)-1; i++ {
		for j := i + 1; j < len(threads); j++ {
			if threads[i].LastDate.Before(threads[j].LastDate) {
				threads[i], threads[j] = threads[j], threads[i]
			}
		}
	}

	return threads
}

// storeSentEmail stores a sent email in the database for tracking with configId
func (h *Handler) storeSentEmail(ctx context.Context, req ReplyEmailRequest, config *types.EmailConfiguration) {
	client, err := h.getMongoClient(ctx)
	if err != nil {
		return
	}
	defer client.Disconnect(ctx)

	db := h.getDatabase(client)
	collection := db.Collection("emailTrackers")

	// Use SMTP username as the actual email address
	senderEmail := config.SMTP.Username
	domain := "localhost"
	if atIndex := strings.Index(senderEmail, "@"); atIndex != -1 {
		domain = senderEmail[atIndex+1:]
	}

	tracker := types.EmailTracker{
		ID:            primitive.NewObjectID().Hex(),
		HasBeenOpened: false,
		EmailReceiver: strings.Join(req.To, ", "),
		EmailSender:   senderEmail, // Use SMTP username as sender
		Subject:       req.Subject,
		HTML:          req.BodyHTML,
		Text:          req.Body,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		IsAutomated:   false,
		MessageID:     fmt.Sprintf("<%s@%s>", primitive.NewObjectID().Hex(), domain),
		TeamID:        config.TeamID,
		ConfigID:      config.ConfigID,
		FollowUpSent:  false,
		Type:          "reply",
	}

	collection.InsertOne(ctx, tracker)
}

// trackEmailActivity tracks email activity for analytics
func (h *Handler) trackEmailActivity(ctx context.Context, configID, teamID, activityType string, count int) {
	client, err := h.getMongoClient(ctx)
	if err != nil {
		return
	}
	defer client.Disconnect(ctx)

	db := h.getDatabase(client)
	collection := db.Collection("emailStats")

	// Update or insert stats
	filter := bson.M{
		"configId": configID,
		"teamId":   teamID,
	}

	update := bson.M{
		"$inc": bson.M{
			fmt.Sprintf("total%s", activityType): count,
		},
		"$set": bson.M{
			"lastActivity": time.Now(),
		},
		"$setOnInsert": bson.M{
			"configId":  configID,
			"teamId":    teamID,
			"createdAt": time.Now(),
		},
	}

	opts := options.Update().SetUpsert(true)
	collection.UpdateOne(ctx, filter, update, opts)
}

// getEmailStats retrieves analytics for an email configuration
func (h *Handler) getEmailStats(ctx context.Context, configID, teamID string) (*EmailStats, error) {
	client, err := h.getMongoClient(ctx)
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	db := h.getDatabase(client)

	// Get stats from emailStats collection
	statsCollection := db.Collection("emailStats")
	var stats EmailStats

	filter := bson.M{
		"configId": configID,
		"teamId":   teamID,
	}

	err = statsCollection.FindOne(ctx, filter).Decode(&stats)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			// Return empty stats if not found
			return &EmailStats{
				ConfigID:     configID,
				TeamID:       teamID,
				LastActivity: time.Now(),
			}, nil
		}
		return nil, err
	}

	// Get count of opened emails from emailTrackers
	trackerCollection := db.Collection("emailTrackers")
	openedCount, _ := trackerCollection.CountDocuments(ctx, bson.M{
		"configId":      configID,
		"teamId":        teamID,
		"hasBeenOpened": true,
	})
	stats.TotalEmailsOpened = int(openedCount)

	// Get count of sent emails from emailTrackers
	sentCount, _ := trackerCollection.CountDocuments(ctx, bson.M{
		"configId": configID,
		"teamId":   teamID,
	})
	stats.TotalEmailsSent = int(sentCount)

	return &stats, nil
}

// HandleGetEmailsCompatible handles GET/POST /e/emails/get
// This endpoint provides compatibility with the Node.js API structure
// It returns messages instead of threads for backwards compatibility
func (h *Handler) HandleGetEmailsCompatible(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req GetEmailsRequest

	if r.Method == http.MethodPost {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"message": "Invalid request body",
				"status":  400,
				"data":    nil,
			})
			return
		}
	} else {
		// GET request - parse query parameters
		req.ConfigID = r.URL.Query().Get("configId")
		req.Mailbox = r.URL.Query().Get("mailbox")
		pageStr := r.URL.Query().Get("page")
		limitStr := r.URL.Query().Get("limit")

		if pageStr != "" {
			page, err := strconv.Atoi(pageStr)
			if err == nil {
				req.Page = page
			}
		}
		if limitStr != "" {
			limit, err := strconv.Atoi(limitStr)
			if err == nil {
				req.Limit = limit
			}
		}
	}

	// Validate request
	if req.ConfigID == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "configId is required",
			"status":  400,
			"data":    nil,
		})
		return
	}

	// Default mailbox
	if req.Mailbox == "" {
		req.Mailbox = "INBOX"
	}

	// Default pagination
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit == 0 {
		req.Limit = 25 // Match Node.js default pageSize
	}

	// Get email configuration
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	config, err := h.getEmailConfiguration(ctx, req.ConfigID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": "Email configuration not found",
			"status":  404,
			"data":    nil,
		})
		return
	}

	// Create IMAP client
	imapHost := config.IMAP.Host
	imapPort := convertPort(config.IMAP.Port)

	imapClient := NewIMAPClient(
		imapHost,
		imapPort,
		config.IMAP.Username,
		config.IMAP.Password,
	)

	// Connect
	if err := imapClient.Connect(); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": fmt.Sprintf("Failed to connect to IMAP server: %v", err),
			"status":  500,
			"data":    nil,
		})
		return
	}
	defer imapClient.Disconnect()

	// Fetch recent emails for threading
	// Fetch enough to build complete threads
	fetchLimit := 200
	emails, err := imapClient.FetchEmails(req.Mailbox, fetchLimit)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"message": fmt.Sprintf("Failed to fetch emails: %v", err),
			"status":  500,
			"data":    []interface{}{},
		})
		return
	}

	// Group ALL emails into threads FIRST
	threads := groupEmailsIntoThreads(emails)

	// NOW paginate the threads
	totalThreads := len(threads)
	offset := (req.Page - 1) * req.Limit
	start := offset
	if start > totalThreads {
		start = totalThreads
	}
	end := start + req.Limit
	if end > totalThreads {
		end = totalThreads
	}

	paginatedThreads := threads[start:end]

	// Collect all emails from paginated threads and flatten them
	// Get the representative message for each thread (most recent)
	var messages []EmailMessage
	for _, thread := range paginatedThreads {
		if len(thread.Messages) > 0 {
			// Get the most recent message in the thread as the representative
			representative := thread.Messages[len(thread.Messages)-1]

			// Add thread metadata to the representative message
			representative.ThreadID = thread.ThreadID
			representative.IsRead = thread.IsRead
			representative.ThreadCount = len(thread.Messages)

			// If this is a multi-message thread, add thread information
			if len(thread.Messages) > 1 {
				// Store all message IDs from the thread
				threadMessageIds := make([]string, 0, len(thread.Messages))
				for _, msg := range thread.Messages {
					if msg.MessageID != "" {
						threadMessageIds = append(threadMessageIds, msg.MessageID)
					}
				}
				representative.ThreadMessageIds = threadMessageIds
			} else {
				// Single message thread
				if representative.MessageID != "" {
					representative.ThreadMessageIds = []string{representative.MessageID}
				}
			}

			messages = append(messages, representative)
		}
	}

	// Calculate total pages
	totalPages := (totalThreads + req.Limit - 1) / req.Limit

	// Track analytics
	go h.trackEmailActivity(context.Background(), req.ConfigID, config.TeamID, "fetch", len(emails))

	// Return in Node.js compatible format
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message": "Connection successful",
		"status":  200,
		"success": true,
		"data": map[string]interface{}{
			"messages":      messages,
			"totalMessages": len(emails),
			"totalThreads":  totalThreads,
			"currentPage":   req.Page,
			"totalPages":    totalPages,
		},
		// Also include top-level fields for compatibility
		"messages":   messages,
		"totalCount": totalThreads,
		"page":       req.Page,
		"limit":      req.Limit,
	})
}

func (h *Handler) HandleGetThread(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ConfigID   string   `json:"configId"`
		Mailbox    string   `json:"mailbox"`
		ThreadID   string   `json:"threadId,omitempty"`
		MessageID  string   `json:"messageId,omitempty"`
		MessageIDs []string `json:"messageIds,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Invalid request body",
		})
		return
	}

	// Validate request
	if req.ConfigID == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "ConfigID is required",
		})
		return
	}

	// At least one identifier is required
	if req.ThreadID == "" && req.MessageID == "" && len(req.MessageIDs) == 0 {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "ThreadID, MessageID, or MessageIDs is required",
		})
		return
	}

	// Default mailbox
	if req.Mailbox == "" {
		req.Mailbox = "INBOX"
	}

	// Get email configuration
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	config, err := h.getEmailConfiguration(ctx, req.ConfigID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Email configuration not found",
		})
		return
	}

	// Create IMAP client
	imapHost := config.IMAP.Host
	imapPort := convertPort(config.IMAP.Port)

	imapClient := NewIMAPClient(
		imapHost,
		imapPort,
		config.IMAP.Username,
		config.IMAP.Password,
	)

	// Connect
	if err := imapClient.Connect(); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("Failed to connect to IMAP server: %v", err),
		})
		return
	}
	defer imapClient.Disconnect()

	// Fetch enough emails to find the thread (last 200)
	fetchLimit := 200
	emails, err := imapClient.FetchEmails(req.Mailbox, fetchLimit)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": fmt.Sprintf("Failed to fetch emails: %v", err),
		})
		return
	}

	if len(emails) == 0 {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":  true,
			"message":  "No emails found",
			"messages": []EmailMessage{},
			"threadId": req.ThreadID,
		})
		return
	}

	// Build message map for quick lookups
	messageMap := make(map[string]*EmailMessage)
	for i := range emails {
		if emails[i].MessageID != "" {
			messageMap[emails[i].MessageID] = &emails[i]
		}
	}

	var threadMessages []EmailMessage
	collectedMessageIDs := make(map[string]bool)

	// Helper function to check if a message belongs to the thread
	belongsToThread := func(msg *EmailMessage, targetIDs map[string]bool) bool {
		// Check if this message is in our target list
		if msg.MessageID != "" && targetIDs[msg.MessageID] {
			return true
		}

		// Check if message references any of our target messages
		for _, ref := range msg.References {
			if targetIDs[ref] {
				return true
			}
		}

		if msg.InReplyTo != "" && targetIDs[msg.InReplyTo] {
			return true
		}

		// Check if any target message references this one
		for targetID := range targetIDs {
			if targetMsg, exists := messageMap[targetID]; exists {
				for _, ref := range targetMsg.References {
					if ref == msg.MessageID {
						return true
					}
				}
				if targetMsg.InReplyTo == msg.MessageID {
					return true
				}
			}
		}

		return false
	}

	// Build initial target message IDs
	targetMessageIDs := make(map[string]bool)

	if len(req.MessageIDs) > 0 {
		// Use provided message IDs
		for _, msgID := range req.MessageIDs {
			targetMessageIDs[msgID] = true
		}
	} else if req.MessageID != "" {
		// Use single message ID
		targetMessageIDs[req.MessageID] = true
	} else if req.ThreadID != "" {
		// Find messages with this thread ID
		for i := range emails {
			if emails[i].ThreadID == req.ThreadID {
				if emails[i].MessageID != "" {
					targetMessageIDs[emails[i].MessageID] = true
				}
			}
		}
	}

	// If we have no target messages, return empty
	if len(targetMessageIDs) == 0 {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Thread not found",
		})
		return
	}

	// Collect all messages that belong to the thread
	// Use multiple passes to ensure we get all related messages
	changed := true
	for changed {
		changed = false
		for i := range emails {
			if emails[i].MessageID == "" {
				continue
			}

			// Skip if already collected
			if collectedMessageIDs[emails[i].MessageID] {
				continue
			}

			// Check if this message belongs to the thread
			if belongsToThread(&emails[i], targetMessageIDs) {
				threadMessages = append(threadMessages, emails[i])
				collectedMessageIDs[emails[i].MessageID] = true
				targetMessageIDs[emails[i].MessageID] = true
				changed = true
			}
		}
	}

	// Sort messages by date (oldest first for chronological order)
	sort.Slice(threadMessages, func(i, j int) bool {
		return threadMessages[i].Date.Before(threadMessages[j].Date)
	})

	// Find the root message (the one without inReplyTo or references)
	var rootThreadID string
	for i := range threadMessages {
		if threadMessages[i].InReplyTo == "" && len(threadMessages[i].References) == 0 {
			rootThreadID = threadMessages[i].MessageID
			break
		}
	}

	// If no root found, use the first message ID or provided thread ID
	if rootThreadID == "" {
		if len(threadMessages) > 0 && threadMessages[0].MessageID != "" {
			rootThreadID = threadMessages[0].MessageID
		} else if req.ThreadID != "" {
			rootThreadID = req.ThreadID
		} else if req.MessageID != "" {
			rootThreadID = req.MessageID
		}
	}

	// Track analytics
	go h.trackEmailActivity(context.Background(), req.ConfigID, config.TeamID, "thread_fetch", len(threadMessages))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"message":      "Thread fetched successfully",
		"messages":     threadMessages,
		"threadId":     rootThreadID,
		"messageCount": len(threadMessages),
	})
}
