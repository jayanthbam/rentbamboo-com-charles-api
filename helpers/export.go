package helpers

import (
	"bamboo/ai"
	"bamboo/preference"
	"bamboo/report"
	"bamboo/security"
	smsproperty "bamboo/sms/property"
	"bamboo/types"
	"regexp"
	"sort"

	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/k0kubun/pp/v3"
	resend "github.com/resend/resend-go/v2"
	"github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	empty = ""
	tab   = "\t"
)

var (
	trainingCache []types.TrainingExample
	cacheOnce     sync.Once

	// Maximum number of messages to include in chat history
	maxChatHistory = 10
	// Maximum characters per message
	maxMessageLength = 500

	// Property cache for faster embedding lookups
	propertyCache     = make(map[string]propertyCacheEntry)
	propertyCacheLock sync.RWMutex

	// QA system cache for reusing embeddings across requests
	qaSystemCache     = make(map[string]qaCacheEntry)
	qaSystemCacheLock sync.RWMutex

	// Session preferences cache - IN-MEMORY, not MongoDB
	sessionPreferencesCache     = make(map[string]sessionPreferenceEntry)
	sessionPreferencesCacheLock sync.RWMutex
)

type sessionPreferenceEntry struct {
	preferences *preference.Preferences
	expiresAt   time.Time
}

type propertyCacheEntry struct {
	properties []bson.M
	expiresAt  time.Time
}

type qaCacheEntry struct {
	qa        *ai.QASystem
	expiresAt time.Time
}

// getCachedProperties returns cached properties if available and not expired
func getCachedProperties(teamId string) ([]bson.M, bool) {
	propertyCacheLock.RLock()
	defer propertyCacheLock.RUnlock()

	entry, exists := propertyCache[teamId]
	if !exists {
		return nil, false
	}

	if time.Now().After(entry.expiresAt) {
		return nil, false
	}

	return entry.properties, true
}

// setCachedProperties caches properties for 5 minutes
func setCachedProperties(teamId string, properties []bson.M) {
	propertyCacheLock.Lock()
	defer propertyCacheLock.Unlock()

	propertyCache[teamId] = propertyCacheEntry{
		properties: properties,
		expiresAt:  time.Now().Add(5 * time.Minute),
	}
}

// ClearPropertyCache clears the cache for a specific team
func ClearPropertyCache(teamId string) {
	propertyCacheLock.Lock()
	defer propertyCacheLock.Unlock()
	delete(propertyCache, teamId)
}

// GetCachedQASystem returns a cached QA system if available
func GetCachedQASystem(teamId string) (*ai.QASystem, bool) {
	qaSystemCacheLock.RLock()
	defer qaSystemCacheLock.RUnlock()

	entry, exists := qaSystemCache[teamId]
	if !exists {
		return nil, false
	}

	if time.Now().After(entry.expiresAt) {
		return nil, false
	}

	return entry.qa, true
}

// SetCachedQASystem caches a QA system for 10 minutes
func SetCachedQASystem(teamId string, qa *ai.QASystem) {
	qaSystemCacheLock.Lock()
	defer qaSystemCacheLock.Unlock()

	qaSystemCache[teamId] = qaCacheEntry{
		qa:        qa,
		expiresAt: time.Now().Add(10 * time.Minute),
	}
}

// ClearQASystemCache clears the QA system cache for a specific team
func ClearQASystemCache(teamId string) {
	qaSystemCacheLock.Lock()
	defer qaSystemCacheLock.Unlock()
	delete(qaSystemCache, teamId)
}

type TeamInfo struct {
	Name        string `bson:"name"`
	Description string `bson:"description"`
	City        string `bson:"city"`
	State       string `bson:"state"`
	LogoURL     string `bson:"logoUrl"`
	TeamID      string `bson:"teamId"`
	Domain      string `bson:"domain"`
	PhoneNumber string `bson:"phoneNumber"`
}

type ShowCaseProperty struct {
	ID                string   `bson:"id"`
	Location          string   `bson:"location"`
	Title             string   `bson:"title"`
	Description       string   `bson:"description"`
	Prompt            string   `bson:"prompt"`
	Photos            []string `bson:"photos"`
	CustomScheduleUrl string   `bson:"customScheduleUrl" json:"CustomScheduleUrl,omitempty"`
}

type CharlesCommandCenter struct {
	Questions          string             `bson:"questions"`
	Priorities         string             `bson:"priorities"`
	Personality        string             `bson:"personality"`
	Name               string             `bson:"name"`
	KeyInfo            string             `bson:"keyInfo"`
	Highlights         string             `bson:"highlights"`
	ApplicationNeeds   string             `bson:"applicationNeeds"`
	TeamID             string             `bson:"teamId"`
	ApplicationSending bool               `bson:"applicationSending"`
	TourScheduling     bool               `bson:"tourScheduling"`
	Domain             string             `bson:"domain"`
	Color              string             `bson:"color"`
	DefaultMessage     string             `bson:"defaultMessage"`
	Align              string             `bson:"align"`
	CustomLink         string             `bson:"customLink"`
	ShowCaseProperty   []ShowCaseProperty `bson:"showCaseProperty"`
	UserID             string             `bson:"userId"`
	CreatedAt          time.Time          `bson:"createdAt"`
	UpdatedAt          time.Time          `bson:"updatedAt"`
}

type ChatSummary struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Lead        *struct {
		FirstName  string `json:"firstName"`
		LastName   string `json:"lastName"`
		Email      string `json:"email"`
		Phone      string `json:"phone"`
		PropertyID string `json:"propertyId,omitempty"`
		UnitID     string `json:"unitId,omitempty"`
		JobTitle   string `json:"jobTitle,omitempty"`
		Industry   string `json:"industry,omitempty"`
		Budget     string `json:"budget,omitempty"`
	} `json:"lead,omitempty"`
}

// PropertyURLs holds programmatically resolved URLs for a property
type PropertyURLs struct {
	PropertyID         string `json:"propertyId"`
	CustomScheduleURL  string `json:"customScheduleUrl,omitempty"`
	ApplicationURL     string `json:"applicationUrl,omitempty"`
	DefaultScheduleURL string `json:"defaultScheduleUrl"`
}

func check(err error, tweakOut bool) {
	if err != nil {
		fmt.Printf("\x1b[31mError: %v\x1b[0m\n", err)
		report.InsertError(string(err.Error()))
		if tweakOut {
			panic(err)
		}
	}
}

// SanitizeUTF8 ensures text is valid UTF-8 by replacing invalid characters
// This prevents issues with special characters like "��" appearing in text
func SanitizeUTF8(text string) string {
	if utf8.ValidString(text) {
		return text
	}

	// Replace invalid UTF-8 sequences with replacement character
	var result strings.Builder
	for i, r := range text {
		if r == utf8.RuneError {
			// Check if this is a valid rune that was decoded incorrectly
			_, size := utf8.DecodeRuneInString(text[i:])
			if size == 1 {
				// Invalid byte sequence, replace with Unicode replacement character
				result.WriteRune('�')
			} else {
				// Valid rune, copy it
				result.WriteRune(r)
			}
		} else {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// CleanText removes or replaces problematic Unicode characters
// that can cause display issues in SMS or other text channels
func CleanText(text string) string {
	// First ensure valid UTF-8
	text = SanitizeUTF8(text)

	// Replace specific problematic characters
	replacements := map[string]string{
		// Common problematic Unicode characters
		"\uFFFD": "?", // Unicode replacement character
		"\u0000": "",  // Null character
		"\u0001": "",  // Start of heading
		"\u0002": "",  // Start of text
		"\u0003": "",  // End of text
		"\u0004": "",  // End of transmission
		"\u0005": "",  // Enquiry
		"\u0006": "",  // Acknowledge
		"\u0007": "",  // Bell
		"\u0008": "",  // Backspace
		"\u000B": "",  // Vertical tab
		"\u000C": "",  // Form feed
		"\u000E": "",  // Shift out
		"\u000F": "",  // Shift in
		"\u0010": "",  // Data link escape
		"\u0011": "",  // Device control 1
		"\u0012": "",  // Device control 2
		"\u0013": "",  // Device control 3
		"\u0014": "",  // Device control 4
		"\u0015": "",  // Negative acknowledge
		"\u0016": "",  // Synchronous idle
		"\u0017": "",  // End of transmission block
		"\u0018": "",  // Cancel
		"\u0019": "",  // End of medium
		"\u001A": "",  // Substitute
		"\u001B": "",  // Escape
		"\u001C": "",  // File separator
		"\u001D": "",  // Group separator
		"\u001E": "",  // Record separator
		"\u001F": "",  // Unit separator
		"\u007F": "",  // Delete
		"\u0080": "",  // Padding character
		"\u0081": "",  // High octet preset
		"\u0082": "",  // Break permitted here
		"\u0083": "",  // No break here
		"\u0084": "",  // Index
		"\u0085": "",  // Next line
		"\u0086": "",  // Start of selected area
		"\u0087": "",  // End of selected area
		"\u0088": "",  // Character tabulation set
		"\u0089": "",  // Character tabulation with justification
		"\u008A": "",  // Line tabulation set
		"\u008B": "",  // Partial line forward
		"\u008C": "",  // Partial line backward
		"\u008D": "",  // Reverse line feed
		"\u008E": "",  // Single shift two
		"\u008F": "",  // Single shift three
		"\u0090": "",  // Device control string
		"\u0091": "",  // Private use one
		"\u0092": "",  // Private use two
		"\u0093": "",  // Set transmit state
		"\u0094": "",  // Cancel character
		"\u0095": "",  // Message waiting
		"\u0096": "",  // Start of guarded area
		"\u0097": "",  // End of guarded area
		"\u0098": "",  // Start of string
		"\u0099": "",  // Single graphic character introducer
		"\u009A": "",  // Single character introducer
		"\u009B": "",  // Control sequence introducer
		"\u009C": "",  // String terminator
		"\u009D": "",  // Operating system command
		"\u009E": "",  // Privacy message
		"\u009F": "",  // Application program command
	}

	// Apply replacements
	for old, new := range replacements {
		text = strings.ReplaceAll(text, old, new)
	}

	return strings.TrimSpace(text)
}

// EnforceLocationPhrasing is a post-processing safety net that rewrites
// first-person location phrases ("we are located at", "we're located at",
// "our location is", etc.) to third-person ("the property is located at" / "it's located at").
// This runs AFTER the AI generates its response, catching any violations
// the prompt rules didn't prevent.
func EnforceLocationPhrasing(text string) string {
	if text == "" {
		return text
	}

	// Case-insensitive replacements using regexp
	replacements := []struct {
		pattern     string
		replacement string
	}{
		{`(?i)\bwe are located at\b`, "the property is located at"},
		{`(?i)\bwe're located at\b`, "the property is located at"},
		{`(?i)\bwe are located in\b`, "the property is located in"},
		{`(?i)\bwe're located in\b`, "the property is located in"},
		{`(?i)\bour location is\b`, "the property is located at"},
		{`(?i)\bour address is\b`, "the property address is"},
		{`(?i)\bwe are at\b`, "the property is at"},
		{`(?i)\bwe're at\b`, "the property is at"},
		{`(?i)\bfind us at\b`, "the property is located at"},
	}

	for _, r := range replacements {
		re := regexp.MustCompile(r.pattern)
		text = re.ReplaceAllString(text, r.replacement)
	}

	return text
}

// ReportInternalAnonTelemetry sends internal tracking notifications for outreach activities
func ReportInternalAnonTelemetry(activityType, title, description, teamId string) error {
	webhookURL := os.Getenv("OUTREACH_WEBHOOK_URL")
	if webhookURL == "" {
		return nil
	}

	webhookURL = strings.TrimSpace(webhookURL)

	// Get team name from team ID
	teamName := getTeamName(teamId)
	if teamName == "" {
		teamName = teamId // Fallback to team ID if name not found
	}

	// Determine embed color based on activity type
	color := getEmbedColorForActivity(activityType)

	// Create embed with activity information
	embed := map[string]any{
		"title": fmt.Sprintf("Live Feed Telemetry: %s", activityType),
		"color": color,
		"fields": []map[string]any{
			{
				"name":   "Activity Type",
				"value":  getActivityTypeWithEmoji(activityType),
				"inline": true,
			},
			{
				"name":   "Title",
				"value":  title,
				"inline": true,
			},
			{
				"name":   "Team",
				"value":  teamName,
				"inline": true,
			},
			{
				"name":   "Description",
				"value":  description,
				"inline": true,
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
		"footer": map[string]any{
			"text": "🤖 Jake Outreach Tracker",
		},
	}

	payload := map[string]any{
		"embeds": []map[string]any{embed},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		pp.Printf("\x1b[31mFailed to marshal outreach webhook payload: %v\x1b[0m\n", err)
		report.InsertError(fmt.Sprintf("Outreach webhook marshal error: %v", err))
		return err
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Retry logic for webhook call
	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := client.Post(webhookURL, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			pp.Printf("\x1b[33mOutreach webhook attempt %d/%d failed: %v\x1b[0m\n", attempt, maxRetries, err)
			if attempt == maxRetries {
				pp.Printf("\x1b[31mAll outreach webhook attempts failed, giving up\x1b[0m\n")
				report.InsertError(fmt.Sprintf("Outreach webhook error after %d attempts: %v", maxRetries, err))
				return err
			}
			time.Sleep(2 * time.Second) // Wait before retry
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
			pp.Printf("\x1b[33mOutreach webhook attempt %d/%d returned status: %d\x1b[0m\n", attempt, maxRetries, resp.StatusCode)
			if attempt == maxRetries {
				pp.Printf("\x1b[31mOutreach webhook returned non-success status after all attempts: %d\x1b[0m\n", resp.StatusCode)
				report.InsertError(fmt.Sprintf("Outreach webhook returned status %d after %d attempts", resp.StatusCode, maxRetries))
				return err
			}
			time.Sleep(2 * time.Second) // Wait before retry
			continue
		}

		pp.Printf("\x1b[32mOutreach webhook completed successfully on attempt %d\n\x1b[0m", attempt)
		return nil
	}

	return nil
}

// AlertHITLTourIntent fires a high-priority notification when a lead tries
// to schedule a specific tour time in conversation. This is the human-in-
// the-loop escalation path for the tour-intent deflector.
//
// channel should be "sms", "chat", "charles", or similar so the receiving
// agent can tell where the request came from. fromIdentifier is the lead's
// phone number (SMS) or client/session ID (chat).
// HITLStatus represents the human-in-the-loop status for a lead
type HITLStatus struct {
	Enabled             bool      `bson:"enabled"`
	AwaitingHuman       bool      `bson:"awaitingHuman"`
	LastNotifiedAt      time.Time `bson:"lastNotifiedAt,omitempty"`
	NotificationsSent   int       `bson:"notificationsSent"`
	EscalationReason    string    `bson:"escalationReason,omitempty"`
	LastConfidenceScore float64   `bson:"lastConfidenceScore,omitempty"`
	TriggerMessage      string    `bson:"triggerMessage,omitempty"`
	SmsFromNumber       string    `bson:"smsFromNumber,omitempty"`
	SmsToNumber         string    `bson:"smsToNumber,omitempty"`
}

// TeamNotificationSetting represents a single notification type setting
type TeamNotificationSetting struct {
	Enabled bool     `bson:"enabled"`
	Roles   []string `bson:"roles"`
}

// TeamNotificationSettings represents team-level notification preferences
type TeamNotificationSettings struct {
	NewLead        TeamNotificationSetting `bson:"new_lead"`
	TourScheduled  TeamNotificationSetting `bson:"tour_scheduled"`
	HumanInTheLoop TeamNotificationSetting `bson:"human_in_the_loop"`
}

// UserNotificationSettings represents per-user notification preferences
type UserNotificationSettings struct {
	TaskDue               bool `bson:"task_due"`
	ClientMessage         bool `bson:"client_message"`
	NewLead               bool `bson:"new_lead"`
	DocumentViewed        bool `bson:"document_viewed"`
	DocumentDownloaded    bool `bson:"document_downloaded"`
	ImportantStatusChange bool `bson:"important_status_change"`
	TourScheduled         bool `bson:"tour_scheduled"`
	TourReminder          bool `bson:"tour_reminder"`
	EmailViewed           bool `bson:"email_viewed"`
	HumanInTheLoop        bool `bson:"human_in_the_loop"`
}

// EmailRecipient represents an email recipient with their details
type EmailRecipient struct {
	Email  string
	Name   string
	UserID string
}

// TeamMember represents a member of a team
type TeamMember struct {
	ID       string    `bson:"id"`
	UserID   string    `bson:"userId"`
	Email    string    `bson:"email"`
	Name     string    `bson:"name"`
	Role     string    `bson:"role"`
	JoinedAt time.Time `bson:"joinedAt"`
}

func AlertHITLTourIntent(teamId, channel, fromIdentifier, leadId, rawMessage string) {
	title := fmt.Sprintf("Tour intent via %s from %s", channel, fromIdentifier)

	leadDisplay := leadId
	if leadDisplay == "" {
		leadDisplay = "(no lead record)"
	}

	// Truncate the raw message so we don't blow past Discord's field limit.
	truncated := rawMessage
	if len(truncated) > 500 {
		truncated = truncated[:500] + "..."
	}

	desc := fmt.Sprintf("Lead: %s\nChannel: %s\nFrom: %s\nMessage: %s\n\nAction required: an agent should reach out to schedule this tour.",
		leadDisplay, channel, fromIdentifier, truncated)

	go func() {
		// Send Discord telemetry
		if err := ReportInternalAnonTelemetry("Tour Intent - Needs Agent", title, desc, teamId); err != nil {
			pp.Printf("\x1b[31mAlertHITLTourIntent failed: %v\x1b[0m\n", err)
		}

		// Update lead's HITL status if we have a lead ID
		if leadId != "" {
			if err := updateLeadHITLStatus(leadId, teamId, HITLStatus{
				Enabled:          true,
				AwaitingHuman:    true,
				LastNotifiedAt:   time.Now(),
				EscalationReason: fmt.Sprintf("Tour intent via %s", channel),
				TriggerMessage:   truncated,
				SmsFromNumber:    fromIdentifier,
			}); err != nil {
				pp.Printf("\x1b[31mAlertHITLTourIntent: failed to update lead HITL status: %v\x1b[0m\n", err)
			}
		}

		// Create inbox notifications and send emails to opted-in team members
		if err := notifyTeamMembersForHITL(teamId, leadId, channel, fromIdentifier, truncated); err != nil {
			pp.Printf("\x1b[31mAlertHITLTourIntent: failed to notify team members: %v\x1b[0m\n", err)
		}
	}()
}

// updateLeadHITLStatus updates the HITL status for a lead in MongoDB
func updateLeadHITLStatus(leadId, teamId string, status HITLStatus) error {
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

	collection := client.Database("teams").Collection("leads")

	// First check if lead exists and has hitl field
	var lead bson.M
	err = collection.FindOne(ctx, bson.M{"id": leadId, "teamId": teamId}).Decode(&lead)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			pp.Printf("\x1b[33mupdateLeadHITLStatus: lead %s not found\x1b[0m\n", leadId)
			return nil
		}
		return fmt.Errorf("failed to find lead: %v", err)
	}

	// Build the update object
	updateObj := bson.M{
		"updatedAt": time.Now(),
	}

	// Check if hitl field exists
	if _, hasHITL := lead["hitl"]; !hasHITL {
		// Create the entire hitl object if it doesn't exist
		updateObj["hitl"] = bson.M{
			"enabled":           status.Enabled,
			"awaitingHuman":     status.AwaitingHuman,
			"notificationsSent": 1,
			"lastNotifiedAt":    status.LastNotifiedAt,
			"escalationReason":  status.EscalationReason,
			"triggerMessage":    status.TriggerMessage,
			"smsFromNumber":     status.SmsFromNumber,
			"smsToNumber":       status.SmsToNumber,
		}
	} else {
		// Update individual fields using dot notation
		updateObj["hitl.enabled"] = true
		updateObj["hitl.awaitingHuman"] = status.AwaitingHuman
		updateObj["hitl.lastNotifiedAt"] = status.LastNotifiedAt
		updateObj["hitl.escalationReason"] = status.EscalationReason
		updateObj["hitl.triggerMessage"] = status.TriggerMessage
		if status.SmsFromNumber != "" {
			updateObj["hitl.smsFromNumber"] = status.SmsFromNumber
		}
		if status.SmsToNumber != "" {
			updateObj["hitl.smsToNumber"] = status.SmsToNumber
		}
		// Increment notifications sent
		updateObj["hitl.notificationsSent"] = bson.M{"$inc": 1}
	}

	// Use $set for most fields, but handle the increment separately
	update := bson.M{"$set": updateObj}
	if _, hasHITL := lead["hitl"]; hasHITL {
		// If hitl exists, use $inc for notificationsSent
		delete(updateObj, "hitl.notificationsSent")
		update = bson.M{
			"$set": updateObj,
			"$inc": bson.M{"hitl.notificationsSent": 1},
		}
	}

	_, err = collection.UpdateOne(ctx, bson.M{"id": leadId, "teamId": teamId}, update)
	if err != nil {
		return fmt.Errorf("failed to update lead HITL status: %v", err)
	}

	pp.Printf("\x1b[32mupdateLeadHITLStatus: HITL status updated for lead %s\x1b[0m\n", leadId)
	return nil
}

// getTeamNotificationSettings retrieves team-level notification settings
func getTeamNotificationSettings(ctx context.Context, client *mongo.Client, teamId string) TeamNotificationSettings {
	defaultSettings := TeamNotificationSettings{
		NewLead: TeamNotificationSetting{
			Enabled: true,
			Roles:   []string{"owner", "admin", "member"},
		},
		TourScheduled: TeamNotificationSetting{
			Enabled: true,
			Roles:   []string{"owner", "admin", "member"},
		},
		HumanInTheLoop: TeamNotificationSetting{
			Enabled: true,
			Roles:   []string{"owner", "admin", "member"},
		},
	}

	collection := client.Database("teams").Collection("team_notifications")
	var teamNotifications bson.M
	err := collection.FindOne(ctx, bson.M{"teamId": teamId}).Decode(&teamNotifications)
	if err != nil {
		return defaultSettings
	}

	// Parse human_in_the_loop settings
	if hitlRaw, ok := teamNotifications["human_in_the_loop"]; ok {
		if hitlMap, ok := hitlRaw.(bson.M); ok {
			if enabled, ok := hitlMap["enabled"].(bool); ok {
				defaultSettings.HumanInTheLoop.Enabled = enabled
			}
			if rolesRaw, ok := hitlMap["roles"]; ok {
				if rolesArr, ok := rolesRaw.(primitive.A); ok {
					roles := make([]string, 0, len(rolesArr))
					for _, r := range rolesArr {
						if roleStr, ok := r.(string); ok {
							roles = append(roles, roleStr)
						}
					}
					if len(roles) > 0 {
						defaultSettings.HumanInTheLoop.Roles = roles
					}
				}
			}
		}
	}

	return defaultSettings
}

// getTeamMembers retrieves all members of a team
func getTeamMembers(ctx context.Context, client *mongo.Client, teamId string) []TeamMember {
	collection := client.Database("teams").Collection("teams")

	var team bson.M
	err := collection.FindOne(ctx, bson.M{"teamId": teamId}).Decode(&team)
	if err != nil || team["members"] == nil {
		return nil
	}

	membersRaw, ok := team["members"].(primitive.A)
	if !ok {
		return nil
	}

	members := make([]TeamMember, 0, len(membersRaw))
	for _, m := range membersRaw {
		memberMap, ok := m.(bson.M)
		if !ok {
			continue
		}

		member := TeamMember{}
		if id, ok := memberMap["id"].(string); ok {
			member.ID = id
		}
		if userId, ok := memberMap["userId"].(string); ok {
			member.UserID = userId
		}
		if email, ok := memberMap["email"].(string); ok {
			member.Email = email
		}
		if name, ok := memberMap["name"].(string); ok {
			member.Name = name
		}
		if role, ok := memberMap["role"].(string); ok {
			member.Role = role
		}
		if joinedAt, ok := memberMap["joinedAt"].(primitive.DateTime); ok {
			member.JoinedAt = joinedAt.Time()
		}

		members = append(members, member)
	}

	return members
}

// getUserNotificationSettings retrieves per-user notification preferences
func getUserNotificationSettings(ctx context.Context, client *mongo.Client, userId string) UserNotificationSettings {
	// Default: all notifications enabled
	defaultSettings := UserNotificationSettings{
		TaskDue:               true,
		ClientMessage:         true,
		NewLead:               true,
		DocumentViewed:        true,
		DocumentDownloaded:    true,
		ImportantStatusChange: true,
		TourScheduled:         true,
		TourReminder:          true,
		EmailViewed:           true,
		HumanInTheLoop:        true,
	}

	collection := client.Database("Users").Collection("notifications")
	var doc bson.M
	err := collection.FindOne(ctx, bson.M{"userId": userId}).Decode(&doc)
	if err != nil {
		return defaultSettings
	}

	// Parse individual settings
	if v, ok := doc["task_due"].(bool); ok {
		defaultSettings.TaskDue = v
	}
	if v, ok := doc["client_message"].(bool); ok {
		defaultSettings.ClientMessage = v
	}
	if v, ok := doc["new_lead"].(bool); ok {
		defaultSettings.NewLead = v
	}
	if v, ok := doc["document_viewed"].(bool); ok {
		defaultSettings.DocumentViewed = v
	}
	if v, ok := doc["document_downloaded"].(bool); ok {
		defaultSettings.DocumentDownloaded = v
	}
	if v, ok := doc["important_status_change"].(bool); ok {
		defaultSettings.ImportantStatusChange = v
	}
	if v, ok := doc["tour_scheduled"].(bool); ok {
		defaultSettings.TourScheduled = v
	}
	if v, ok := doc["tour_reminder"].(bool); ok {
		defaultSettings.TourReminder = v
	}
	if v, ok := doc["email_viewed"].(bool); ok {
		defaultSettings.EmailViewed = v
	}
	if v, ok := doc["human_in_the_loop"].(bool); ok {
		defaultSettings.HumanInTheLoop = v
	}

	return defaultSettings
}

// getEmailRecipientsForHITL returns eligible email recipients for HITL notifications
func getEmailRecipientsForHITL(ctx context.Context, client *mongo.Client, teamId string) []EmailRecipient {
	settings := getTeamNotificationSettings(ctx, client, teamId)

	// Check if HITL notifications are enabled at team level
	if !settings.HumanInTheLoop.Enabled {
		pp.Printf("\x1b[33mgetEmailRecipientsForHITL: HITL notifications disabled for team %s\x1b[0m\n", teamId)
		return nil
	}

	members := getTeamMembers(ctx, client, teamId)
	if len(members) == 0 {
		return nil
	}

	// Filter members by role
	eligibleRoles := make(map[string]bool)
	for _, role := range settings.HumanInTheLoop.Roles {
		eligibleRoles[role] = true
	}

	recipients := make([]EmailRecipient, 0)
	seenEmails := make(map[string]bool)

	for _, member := range members {
		// Check if member's role is eligible
		if !eligibleRoles[member.Role] {
			continue
		}

		if member.Email == "" {
			continue
		}

		// Check per-user notification preferences
		userId := member.UserID
		if userId == "" {
			userId = member.ID
		}
		if userId != "" {
			userSettings := getUserNotificationSettings(ctx, client, userId)
			if !userSettings.HumanInTheLoop {
				pp.Printf("\x1b[33mgetEmailRecipientsForHITL: user %s opted out of HITL notifications\x1b[0m\n", userId)
				continue
			}
		}

		// Deduplicate by email
		if seenEmails[member.Email] {
			continue
		}
		seenEmails[member.Email] = true

		recipients = append(recipients, EmailRecipient{
			Email:  member.Email,
			Name:   member.Name,
			UserID: userId,
		})
	}

	return recipients
}

// notifyTeamMembersForHITL creates inbox notifications and sends emails to opted-in team members
func notifyTeamMembersForHITL(teamId, leadId, channel, fromIdentifier, messagePreview string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
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

	// Get team notification settings to check if HITL is enabled
	settings := getTeamNotificationSettings(ctx, client, teamId)
	if !settings.HumanInTheLoop.Enabled {
		pp.Printf("\x1b[33mnotifyTeamMembersForHITL: HITL notifications disabled for team %s\x1b[0m\n", teamId)
		return nil
	}

	// Get eligible recipients
	recipients := getEmailRecipientsForHITL(ctx, client, teamId)
	if len(recipients) == 0 {
		pp.Printf("\x1b[33mnotifyTeamMembersForHITL: no eligible recipients for team %s\x1b[0m\n", teamId)
		return nil
	}

	// Create inbox notifications for each recipient
	inboxCollection := client.Database("Users").Collection("inbox")
	for _, recipient := range recipients {
		notification := bson.M{
			"id":          uuid.New().String(),
			"type":        "HITL_REQUIRED",
			"title":       "Tour Scheduling - Human Response Required",
			"description": fmt.Sprintf("Lead via %s needs your attention: %s", channel, messagePreview),
			"createdAt":   time.Now(),
			"readAt":      nil,
			"teamId":      teamId,
			"userId":      recipient.UserID,
			"extra": bson.M{
				"leadId":        leadId,
				"smsFromNumber": fromIdentifier,
				"channel":       channel,
				"priority":      "urgent",
			},
		}

		if _, err := inboxCollection.InsertOne(ctx, notification); err != nil {
			pp.Printf("\x1b[31mnotifyTeamMembersForHITL: failed to create inbox notification for %s: %v\x1b[0m\n", recipient.Email, err)
		}
	}

	pp.Printf("\x1b[32mnotifyTeamMembersForHITL: created %d inbox notifications for team %s\x1b[0m\n", len(recipients), teamId)

	// Send email notifications
	resendAPIKey := os.Getenv("RESEND_API_KEY")
	if resendAPIKey == "" {
		pp.Printf("\x1b[33mnotifyTeamMembersForHITL: RESEND_API_KEY not set, skipping email notifications\x1b[0m\n")
		return nil
	}

	resendClient := resend.NewClient(resendAPIKey)

	// Get team name for email
	teamName := getTeamName(teamId)
	if teamName == "" {
		teamName = "Your Team"
	}

	emailSubject := fmt.Sprintf("🚨 Tour Intent Alert - Human Response Required - %s", fromIdentifier)

	for _, recipient := range recipients {
		recipientName := recipient.Name
		if recipientName == "" {
			recipientName = recipient.Email
		}

		plainBody := fmt.Sprintf(
			"Hi %s,\n\nA lead requires your attention for tour scheduling.\n\nChannel: %s\nFrom: %s\nMessage: %s\n\nPlease log in to the dashboard to respond.\n\nBest,\n%s",
			recipientName, channel, fromIdentifier, messagePreview, teamName,
		)

		htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
</head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Oxygen-Sans,Ubuntu,Cantarell,'Helvetica Neue',sans-serif;background-color:#f4f4f8;margin:0;padding:20px;">
  <div style="max-width:600px;margin:0 auto;background-color:#ffffff;border-radius:8px;overflow:hidden;box-shadow:0 2px 10px rgba(0,0,0,0.1);">

    <!-- Header -->
    <div style="background-color:#dc2626;padding:24px;text-align:center;">
      <span style="color:white;font-size:24px;">🚨</span>
      <h1 style="color:white;margin:8px 0 0 0;font-size:20px;font-weight:600;">Human Response Required</h1>
    </div>

    <!-- Content -->
    <div style="padding:24px;">
      <p style="font-size:16px;color:#374151;margin:0 0 16px 0;">Hi %s,</p>
      <p style="font-size:16px;color:#374151;margin:0 0 24px 0;">
        A lead is trying to schedule a tour and needs your attention.
      </p>

      <!-- Details Card -->
      <div style="background-color:#fef2f2;border:1px solid #fecaca;border-radius:8px;padding:16px;margin-bottom:24px;">
        <table style="width:100%%;border-collapse:collapse;">
          <tr>
            <td style="padding:8px 0;color:#6b7280;font-size:14px;width:100px;">Channel:</td>
            <td style="padding:8px 0;color:#111827;font-size:14px;font-weight:600;">%s</td>
          </tr>
          <tr>
            <td style="padding:8px 0;color:#6b7280;font-size:14px;">From:</td>
            <td style="padding:8px 0;color:#111827;font-size:14px;font-weight:600;">%s</td>
          </tr>
          <tr>
            <td style="padding:8px 0;color:#6b7280;font-size:14px;vertical-align:top;">Message:</td>
            <td style="padding:8px 0;color:#111827;font-size:14px;">%s</td>
          </tr>
        </table>
      </div>

      <!-- CTA -->
      <div style="text-align:center;margin-bottom:24px;">
        <a href="https://rentbamboo.com/dashboard/inbox" style="display:inline-block;background-color:#4f46e5;color:white;padding:12px 24px;border-radius:6px;text-decoration:none;font-weight:600;">
          View in Dashboard
        </a>
      </div>

      <p style="font-size:14px;color:#6b7280;margin:0;">
        Please respond to this lead as soon as possible to provide a great experience.
      </p>
    </div>

    <!-- Footer -->
    <div style="background-color:#f9fafb;padding:16px 24px;text-align:center;border-top:1px solid #e5e7eb;">
      <p style="font-size:12px;color:#9ca3af;margin:0;">
        %s<br/>
        This is an automated notification from <a href="https://rentbamboo.com" style="color:#3b82f6;text-decoration:none;">RentBamboo</a>
      </p>
    </div>
  </div>
</body>
</html>`,
			recipientName, channel, fromIdentifier, messagePreview, teamName,
		)

		params := &resend.SendEmailRequest{
			From:    "RentBamboo AI <notifications@rentbamboo.com>",
			To:      []string{recipient.Email},
			Subject: emailSubject,
			Text:    plainBody,
			Html:    htmlBody,
		}

		if _, err := resendClient.Emails.Send(params); err != nil {
			pp.Printf("\x1b[31mnotifyTeamMembersForHITL: failed to send email to %s: %v\x1b[0m\n", recipient.Email, err)
		} else {
			pp.Printf("\x1b[32mnotifyTeamMembersForHITL: sent email to %s\x1b[0m\n", recipient.Email)
		}
	}

	return nil
}

// ReportAIReplyTelemetry sends Discord telemetry for AI-generated replies
func ReportAIReplyTelemetry(aiResponse, userMessage, teamId, sessionId, propertyId string) error {
	webhookURL := os.Getenv("OUTREACH_WEBHOOK_URL")
	if webhookURL == "" {
		return nil
	}

	webhookURL = strings.TrimSpace(webhookURL)

	// Get team name from team ID
	teamName := getTeamName(teamId)
	if teamName == "" {
		teamName = teamId // Fallback to team ID if name not found
	}

	// Determine category and color
	category := "AI SMS Reply"
	color := 9109504 // Light Purple (same as SMS Response)

	// Truncate messages if too long (Discord field limit is 1024 chars)
	displayAIReply := aiResponse
	if len(aiResponse) > 500 {
		displayAIReply = aiResponse[:500] + "... (truncated)"
	}

	displayUserMessage := userMessage
	if len(userMessage) > 300 {
		displayUserMessage = userMessage[:300] + "... (truncated)"
	}

	// Create embed with AI reply information
	embed := map[string]any{
		"title": fmt.Sprintf("🤖 AI Reply Generated: %s", category),
		"color": color,
		"fields": []map[string]any{
			{
				"name":   "Category",
				"value":  category,
				"inline": true,
			},
			{
				"name":   "Team",
				"value":  teamName,
				"inline": true,
			},
			{
				"name":   "Team ID",
				"value":  "```" + teamId + "```",
				"inline": true,
			},
			{
				"name":   "Session ID",
				"value":  "```" + sessionId + "```",
				"inline": true,
			},
			{
				"name":   "Property ID",
				"value":  "```" + propertyId + "```",
				"inline": true,
			},
			{
				"name":   "User Message",
				"value":  displayUserMessage,
				"inline": false,
			},
			{
				"name":   "AI Response",
				"value":  displayAIReply,
				"inline": false,
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
		"footer": map[string]any{
			"text": "🤖 Jake AI Tracker",
		},
	}

	payload := map[string]any{
		"embeds": []map[string]any{embed},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		pp.Printf("\x1b[31mFailed to marshal AI reply telemetry payload: %v\x1b[0m\n", err)
		report.InsertError(fmt.Sprintf("AI reply telemetry marshal error: %v", err))
		return err
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Retry logic for webhook call
	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := client.Post(webhookURL, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			pp.Printf("\x1b[33mAI reply telemetry webhook attempt %d/%d failed: %v\x1b[0m\n", attempt, maxRetries, err)
			if attempt == maxRetries {
				pp.Printf("\x1b[31mAll AI reply telemetry webhook attempts failed, giving up\x1b[0m\n")
				report.InsertError(fmt.Sprintf("AI reply telemetry webhook error after %d attempts: %v", maxRetries, err))
				return err
			}
			time.Sleep(2 * time.Second) // Wait before retry
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
			pp.Printf("\x1b[33mAI reply telemetry webhook attempt %d/%d returned status: %d\x1b[0m\n", attempt, maxRetries, resp.StatusCode)
			if attempt == maxRetries {
				pp.Printf("\x1b[31mAI reply telemetry webhook returned non-success status after all attempts: %d\x1b[0m\n", resp.StatusCode)
				report.InsertError(fmt.Sprintf("AI reply telemetry webhook returned status %d after %d attempts", resp.StatusCode, maxRetries))
				return err
			}
			time.Sleep(2 * time.Second) // Wait before retry
			continue
		}

		pp.Printf("AI reply telemetry webhook completed successfully on attempt %d\n", attempt)
		return nil
	}

	return nil
}

// getTeamName retrieves the team name from the database using team ID
func getTeamName(teamId string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return ""
	}
	defer client.Disconnect(ctx)

	collection := client.Database("teams").Collection("teams")
	filter := bson.M{"teamId": teamId}

	var result bson.M
	err = collection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		return ""
	}

	if name, ok := result["name"].(string); ok {
		return name
	}

	return ""
}

func getActivityTypeWithEmoji(activityType string) string {
	emojiMap := map[string]string{
		"Email Response":            "📧 " + activityType,
		"SMS Response":              "💬 " + activityType,
		"Lead Created":              "🎯 " + activityType,
		"SMS Sent":                  "💬 " + activityType,
		"Email Sent":                "📧 " + activityType,
		"Tour Scheduled":            "🏠 " + activityType,
		"Follow Up":                 "📞 " + activityType,
		"Charles New Chat":          "💬 " + activityType,
		"Voice Out (From Browser)":  "☎️ " + activityType,
		"Voice In (Automated)":      "☎️ " + activityType,
		"Tour Intent - Needs Agent": "🚨 " + activityType,
	}

	if emojiValue, exists := emojiMap[activityType]; exists {
		return emojiValue
	}

	return "📋 " + activityType // Default emoji for unknown activity types
}

// getEmbedColorForActivity returns a color based on the activity type
func getEmbedColorForActivity(activityType string) int {
	colorMap := map[string]int{
		"Email Response":            3447003,  // Blue
		"SMS Response":              9109504,  // Light Purple
		"Lead Created":              65280,    // Green
		"SMS Sent":                  16776960, // Yellow
		"Email Sent":                5793266,  // Dark Blue
		"Tour Scheduled":            16711935, // Magenta
		"Follow Up":                 16750848, // Orange
		"Charles New Chat":          8388736,  // Teal
		"Voice Out (From Browser)":  16750848, // Orange
		"Voice In (Automated)":      10181046, // Light Green
		"Tour Intent - Needs Agent": 15158332, // Red - high-priority HITL alert
	}

	if color, exists := colorMap[activityType]; exists {
		return color
	}

	return 3447003 // Default blue color
}

func StoreEvent(eventType string, sessionId string, timestamp time.Time, propertyId string, propertyTitle string) error {
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

	collection := client.Database("teams").Collection("charles-events")

	eventData := bson.M{
		"type":      eventType,
		"sessionId": sessionId,
		"timestamp": timestamp,
	}

	// Only add property fields if they exist
	if propertyId != "" {
		eventData["propertyId"] = propertyId
	}
	if propertyTitle != "" {
		eventData["propertyTitle"] = propertyTitle
	}

	_, err = collection.InsertOne(ctx, eventData)
	if err != nil {
		return fmt.Errorf("failed to insert event: %v", err)
	}

	return nil
}

func GetTeamInfo(email string) ([]bson.M, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		check(err, false)
		return nil, err
	}

	defer client.Disconnect(ctx)

	dbName := os.Getenv("MONGODB_DATABASE")
	collName := os.Getenv("MONGODB_DATABASE")
	if dbName == "" || collName == "" {
		return nil, fmt.Errorf("database or collection name not set in environment")
	}

	collection := client.Database(dbName).Collection(collName)

	filter := bson.M{
		"members": bson.M{
			"$elemMatch": bson.M{
				"email":    email,
				"joinedAt": bson.M{"$exists": true},
			},
		},
	}

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		check(err, false)
		return nil, err
	}

	defer cursor.Close(ctx)

	var teams []bson.M
	err = cursor.All(ctx, &teams)
	if err != nil {
		check(err, false)
		return nil, err
	}

	// Remove duplicates
	seen := make(map[string]bool)
	uniqueTeams := make([]bson.M, 0)
	for _, team := range teams {
		if teamID, ok := team["teamId"].(string); ok {
			if !seen[teamID] {
				seen[teamID] = true

				// Remove sensitive data
				uniqueTeams = append(uniqueTeams, team)
			}
		}
	}

	return uniqueTeams, nil
}

func GetTeamProperties(teamId string) ([]bson.M, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		check(err, false)
		return nil, err
	}

	defer client.Disconnect(ctx)

	dbName := os.Getenv("MONGODB_DATABASE")
	collName := os.Getenv("MONGODB_COLLECTION_PROPERTIES")

	if dbName == "" || collName == "" {
		return nil, fmt.Errorf("database or collection name not set in environment")
	}

	collection := client.Database(dbName).Collection(collName)

	filter := bson.M{
		"teamId":   teamId,
		"isPublic": true,
	}

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		check(err, false)
		return nil, err
	}

	defer cursor.Close(ctx)

	var properties []bson.M
	err = cursor.All(ctx, &properties)
	if err != nil {
		check(err, false)
		return nil, err
	}

	// Filter out non-vacant units
	var filteredProperties []bson.M
	for _, property := range properties {
		propertyType, _ := property["type"].(string)

		if propertyType == "single" {
			// For single properties, check isVacant at root level
			if isVacant, ok := property["isVacant"].(bool); ok && isVacant {
				filteredProperties = append(filteredProperties, property)
			}
		} else {
			// For other property types, filter units array
			if units, ok := property["units"].(primitive.A); ok {
				var vacantUnits []interface{}
				for _, unit := range units {
					if unitMap, ok := unit.(primitive.M); ok {
						if isVacant, ok := unitMap["isVacant"].(bool); ok && isVacant {
							vacantUnits = append(vacantUnits, unit)
						}
					}
				}
				// Only include property if it has vacant units
				if len(vacantUnits) > 0 {
					// Create a copy of the property with only vacant units
					filteredProperty := make(bson.M)
					for k, v := range property {
						filteredProperty[k] = v
					}
					filteredProperty["units"] = vacantUnits
					filteredProperties = append(filteredProperties, filteredProperty)
				}
			}
		}
	}

	// TODO // Check STRIPE Limits.

	return filteredProperties, nil
}

// GetPropertyByID fetches a property by ID
func GetPropertyByID(teamId string, propertyId string) (bson.M, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	dbName := os.Getenv("MONGODB_DATABASE")
	collName := os.Getenv("MONGODB_COLLECTION_PROPERTIES")

	if dbName == "" || collName == "" {
		return nil, fmt.Errorf("database or collection name not set in environment")
	}

	collection := client.Database(dbName).Collection(collName)

	var property bson.M
	err = collection.FindOne(ctx, bson.M{
		"id":     propertyId,
		"teamId": teamId,
	}).Decode(&property)

	if err != nil {
		return nil, err
	}

	return property, nil
}

// GetPropertyPhotos fetches photos for a given property ID
func GetPropertyPhotos(teamId string, propertyId string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	dbName := os.Getenv("MONGODB_DATABASE")
	collName := os.Getenv("MONGODB_COLLECTION_PROPERTIES")

	if dbName == "" || collName == "" {
		return nil, fmt.Errorf("database or collection name not set in environment")
	}

	collection := client.Database(dbName).Collection(collName)

	var property bson.M
	err = collection.FindOne(ctx, bson.M{
		"id":     propertyId,
		"teamId": teamId,
	}).Decode(&property)

	if err != nil {
		return nil, err
	}

	// Extract photos from property
	var photos []string
	if photosData, ok := property["photos"].(primitive.A); ok {
		for _, photo := range photosData {
			if photoStr, ok := photo.(string); ok && photoStr != "" {
				photos = append(photos, photoStr)
			}
		}
	}

	return photos, nil
}

// GetPhotosForPropertyIDs fetches photos for multiple property IDs, returning up to maxPhotos total
func GetPhotosForPropertyIDs(teamId string, propertyIDs []string, maxPhotos int) []string {
	var allPhotos []string

	for _, propID := range propertyIDs {
		if len(allPhotos) >= maxPhotos {
			break
		}

		photos, err := GetPropertyPhotos(teamId, propID)
		if err != nil {
			pp.Printf("Error fetching photos for property %s: %v\n", propID, err)
			continue
		}

		// Add photos up to the max
		for _, photo := range photos {
			if len(allPhotos) >= maxPhotos {
				break
			}
			allPhotos = append(allPhotos, photo)
		}
	}

	return allPhotos
}

func GenerateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	if _, err := rand.Read(b); err != nil {
		check(err, false)
		return ""
	}
	for i := range b {
		b[i] = charset[b[i]%byte(len(charset))]
	}
	return string(b)
}

func FetchCommandCenter(teamId string) (bson.M, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, err
	}

	defer client.Disconnect(ctx)

	collection := client.Database("teams").Collection("command-centers")

	filter := bson.M{
		"teamId": teamId,
	}

	var result bson.M
	err = collection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}

	return result, nil
}

// FetchJakeTraining fetches the team's Jake training configuration from MongoDB
func FetchJakeTraining(teamId string) (*types.JakeTraining, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, err
	}

	defer client.Disconnect(ctx)

	collection := client.Database("teams").Collection("jake-training")

	filter := bson.M{
		"teamId": teamId,
	}

	var result types.JakeTraining
	err = collection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}

	return &result, nil
}

// buildEmailTrainingContext builds a training context string from JakeEmail training files
func buildEmailTrainingContext(training *types.JakeTraining) string {
	if training == nil || len(training.JakeEmail.Files) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("=== EMAIL RESPONSE TRAINING EXAMPLES ===\n")
	sb.WriteString("Study these examples carefully. They represent the EXACT style, tone, emoji usage, and formatting that should be used in responses.\n")
	sb.WriteString("Pay special attention to: emoji placement, section organization, greeting warmth, and sign-off style.\n\n")

	for i, file := range training.JakeEmail.Files {
		if file.Content != "" {
			sb.WriteString(fmt.Sprintf("--- Example %d", i+1))
			if file.Name != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", file.Name))
			}
			sb.WriteString(" ---\n")
			sb.WriteString(">>> BEGIN EXAMPLE <<<\n")
			sb.WriteString(file.Content)
			sb.WriteString("\n>>> END EXAMPLE <<<\n\n")
		}
	}

	return sb.String()
}

// GenerateAIResponseSMS generates AI response (backward compatible wrapper)
func GenerateAIResponseSMS(client *openai.Client, chatHistory string, inquiry string, qa *ai.QASystem, teamId string, sessionId string) (string, string, error) {
	return GenerateAIResponseSMSWithContext(client, chatHistory, inquiry, qa, teamId, sessionId, nil, nil, nil, "")
}

// GenerateAIResponseSMSWithContext generates AI response with pre-matched properties and URLs
// If propertyIDs and resolvedURLs are provided, use those instead of doing a new search
// If lastDiscussedPropertyID is provided, use that property's contact info when user asks about contacting someone
// If resolvedApplicationURLs is provided, use those for application links instead of resolving internally
func GenerateAIResponseSMSWithContext(client *openai.Client, chatHistory string, inquiry string, qa *ai.QASystem, teamId string, sessionId string, matchedPropertyIDs []string, resolvedURLs []string, resolvedApplicationURLs []string, lastDiscussedPropertyID string) (string, string, error) {
	pp.Println("Generating AI response for SMS...")

	// Get stored preferences for this session
	userPrefs, err := GetSessionPreferences(teamId, sessionId)
	if err != nil {
		pp.Printf("Error getting session preferences: %v\n", err)
	}

	// Use provided property IDs if available, otherwise determine how to search
	var propertyIDs []string
	var contextInfo string

	if len(matchedPropertyIDs) > 0 {
		// Use pre-matched properties
		propertyIDs = matchedPropertyIDs
		pp.Printf("Using %d pre-matched properties\n", len(propertyIDs))

		// Get context for these specific properties
		for _, propID := range propertyIDs {
			// Generate property summary using property summarizer (no embeddings)
			propContext := GeneratePropertySummary(teamId, propID, inquiry)
			if propContext != "" {
				contextInfo += propContext + "\n\n"
			}
		}
	} else if userPrefs != nil && userPrefs.HasPreferences() {
		// STRICT QUERY: Use MongoDB filter based on preferences FIRST
		pp.Printf("Using STRICT query with preferences: bedrooms=%d, bathrooms=%d, type=%s, budget=%.0f-%.0f\n",
			userPrefs.Bedrooms, userPrefs.Bathrooms, userPrefs.PropertyType, userPrefs.BudgetMin, userPrefs.BudgetMax)

		// Check cache first
		cachedProps, found := preference.GetCachedProperties(teamId, userPrefs)
		if found && len(cachedProps) > 0 {
			pp.Printf("Using %d cached properties for preferences\n", len(cachedProps))
			// Convert cached preference.Property to IDs
			for _, prop := range cachedProps {
				propertyIDs = append(propertyIDs, prop.ID)
				// Generate property summary using property summarizer (no embeddings)
				propContext := GeneratePropertySummary(teamId, prop.ID, inquiry)
				if propContext != "" {
					contextInfo += propContext + "\n\n"
				}
			}
		} else {
			// Query MongoDB with strict filters
			filterEngine, err := preference.NewFilterEngine()
			if err == nil {
				ctx := context.Background()
				result, err := filterEngine.FilterByPreferences(ctx, teamId, userPrefs, 10)
				if err == nil && len(result.Properties) > 0 {
					pp.Printf("Found %d properties matching strict preferences\n", len(result.Properties))

					// Cache the results
					preference.SetCachedProperties(teamId, userPrefs, result.Properties)

					// Convert to property IDs and get context
					for _, prop := range result.Properties {
						propertyIDs = append(propertyIDs, prop.ID)
						// Generate property summary using property summarizer (no embeddings)
						propContext := GeneratePropertySummary(teamId, prop.ID, inquiry)
						if propContext != "" {
							contextInfo += propContext + "\n\n"
						}
					}
				} else {
					pp.Printf("No properties found with strict query - returning empty (no semantic search fallback)\n")
				}
			} else {
				pp.Printf("Failed to create filter engine: %v - returning empty (no semantic search fallback)\n", err)
			}
		}
	}

	// NOTE: No fallback to semantic search when no strict matches found
	// AI will explain no matches and suggest adjusting preferences
	if len(propertyIDs) == 0 {
		pp.Printf("No properties found from strict query - AI will explain no matches and suggest alternatives\n")
	}

	// Use provided URLs if available, otherwise resolve them
	var resolvedScheduleURLs []string
	var resolvedAppURLs []string

	// Always resolve URLs when we have property IDs (don't skip this block!)
	if len(propertyIDs) > 0 {
		// If scheduling URLs are provided, use them; otherwise resolve programmatically
		if len(resolvedURLs) > 0 {
			resolvedScheduleURLs = resolvedURLs
			pp.Printf("Using %d pre-resolved scheduling URLs\n", len(resolvedScheduleURLs))
		} else {
			// Resolve scheduling URLs programmatically (NO AI HALLUCINATION!)
			for _, propID := range propertyIDs {
				url := ResolveScheduleURL(teamId, propID)
				resolvedScheduleURLs = append(resolvedScheduleURLs, url)
				pp.Printf("Resolved scheduling URL for property %s: %s\n", propID, url)
			}
		}

		// Use pre-resolved application URLs if provided, otherwise resolve programmatically
		if len(resolvedApplicationURLs) > 0 {
			resolvedAppURLs = resolvedApplicationURLs
			pp.Printf("Using %d pre-resolved application URLs\n", len(resolvedAppURLs))
		} else {
			// Resolve application URLs programmatically
			for _, propID := range propertyIDs {
				appURL := ResolveApplicationURL(teamId, propID)
				resolvedAppURLs = append(resolvedAppURLs, appURL)
				if appURL != "" {
					pp.Printf("Resolved application URL for property %s: %s\n", propID, appURL)
				}
			}
		}

		// Add summary log for resolved URLs
		if len(resolvedScheduleURLs) > 0 {
			pp.Printf("Resolved %d scheduling URLs total\n", len(resolvedScheduleURLs))
		}
		if len(resolvedAppURLs) > 0 {
			pp.Printf("Resolved %d application URLs total\n", len(resolvedAppURLs))
		}
	}

	// fetch cmd center for admin instructions
	cmdCenter, err := FetchCommandCenter(teamId)
	if err != nil {
		// Handle error but continue with defaults
		check(err, false)
		pp.Printf("Error fetching command center: %v, using defaults\n", err)
	}

	// Build system messages based on command center configuration
	var signingName string = "Jake" // Default name
	var sendTourSendStatus bool = true
	var sendApplicationStatus bool = true
	var highlights, questions, personality, name string

	if cmdCenter != nil {
		// Get values directly from command center
		if nameStr, ok := cmdCenter["name"].(string); ok && nameStr != "" {
			signingName = nameStr
			name = nameStr
		}

		if tourScheduling, ok := cmdCenter["tourScheduling"].(bool); ok {
			sendTourSendStatus = tourScheduling
		}

		if applicationSending, ok := cmdCenter["applicationSending"].(bool); ok {
			sendApplicationStatus = applicationSending
		}

		if highlightsStr, ok := cmdCenter["highlights"].(string); ok {
			highlights = highlightsStr
		}

		if questionsStr, ok := cmdCenter["questions"].(string); ok {
			questions = questionsStr
		}

		if personalityStr, ok := cmdCenter["personality"].(string); ok {
			personality = personalityStr
		}
	}

	// Define structured response format
	type AIResponse struct {
		Response string `json:"response"`
	}

	// Fetch Jake SMS training data for style matching
	jakeTraining, err := FetchJakeTraining(teamId)
	if err != nil {
		pp.Printf("Error fetching Jake training: %v, continuing without training context\n", err)
	}

	// Build SMS training examples context
	var smsTrainingContext strings.Builder
	if jakeTraining != nil && len(jakeTraining.JakeSMS.Files) > 0 {
		smsTrainingContext.WriteString("SMS Response Style Examples - Match this tone and style:\n\n")

		// Include up to 5 training examples to avoid token overflow
		maxExamples := 5
		if len(jakeTraining.JakeSMS.Files) < 5 {
			maxExamples = len(jakeTraining.JakeSMS.Files)
		}

		for i := 0; i < maxExamples; i++ {
			file := jakeTraining.JakeSMS.Files[i]
			if file.Content != "" {
				// Truncate long examples
				content := file.Content
				if len(content) > 500 {
					content = content[:500] + "..."
				}
				fmt.Fprintf(&smsTrainingContext, "Example %d (%s):\n%s\n\n", i+1, file.Name, content)
			}
		}

		// Add common inquiries if available
		if jakeTraining.CommonInquiries != "" {
			commonInq := jakeTraining.CommonInquiries
			if len(commonInq) > 300 {
				commonInq = commonInq[:300] + "..."
			}
			fmt.Fprintf(&smsTrainingContext, "Common Inquiries to handle:\n%s\n", commonInq)
		}
	}

	// Generate JSON schema for structured response
	schema, err := jsonschema.GenerateSchemaForType(AIResponse{})
	if err != nil {
		check(err, false)
		return "", "", fmt.Errorf("schema generation error: %v", err)
	}

	// Get property contact info (from property.contact) instead of team phone
	// Priority: 1. lastDiscussedPropertyID (if provided), 2. first property in the list
	propertyContactInfo := ""
	if lastDiscussedPropertyID != "" && len(propertyIDs) > 0 {
		// Check if lastDiscussedPropertyID is in the propertyIDs list
		isInList := false
		for _, pid := range propertyIDs {
			if pid == lastDiscussedPropertyID {
				isInList = true
				break
			}
		}
		if isInList {
			contactInfo := getPropertyContactInfo(teamId, []string{lastDiscussedPropertyID})
			if contactInfo != "" {
				pp.Printf("Using last discussed property's contact info: %s\n", lastDiscussedPropertyID)
				propertyContactInfo = contactInfo
			}
		}
	}

	// Fallback to first property if no last discussed property or contact info not found
	if propertyContactInfo == "" && len(propertyIDs) > 0 {
		contactInfo := getPropertyContactInfo(teamId, propertyIDs)
		if contactInfo != "" {
			propertyContactInfo = contactInfo
		}
	}

	// Get team info for fallback phone number context
	teamInfo, err := GetTeamInfoByTeamId(teamId)
	var teamContactPhone string
	if err == nil && teamInfo.PhoneNumber != "" {
		teamContactPhone = teamInfo.PhoneNumber
	}

	// Build phone number instructions - use property contact first, fallback to team phone
	phoneNumberInstructions := ""
	if propertyContactInfo != "" {
		phoneNumberInstructions = fmt.Sprintf(`

Contact Information Guidelines:
- Use the property-specific contact info provided below when discussing a specific property
- When asked for contact information, phone number, email, or to speak with someone about a property, use: %s
- Do NOT make up or hallucinate contact information - only use what is provided below
- Format phone numbers as plain digits (no parentheses, dashes, or special formatting)`, propertyContactInfo)
	} else if teamContactPhone != "" {
		// Fallback to team phone if no property contact info
		phoneNumberInstructions = fmt.Sprintf(`

Phone Number Guidelines:
- When asked for contact information, phone number, or to speak with someone, provide this phone number: %s
- This is the main contact number for the leasing office
- Do NOT make up or hallucinate phone numbers - only use this one
- Format phone numbers as plain digits: %s (no parentheses, dashes, or special formatting)`, teamContactPhone, teamContactPhone)
	}

	// Default System Message for SMS
	systemMessages := []openai.ChatCompletionMessage{
		{
			Role: openai.ChatMessageRoleSystem,
			Content: fmt.Sprintf(`You are %s, an AI real estate leasing agent responding via SMS. Your responses MUST be:
- Short and concise (SMS format - keep under 250 characters when possible)
- Natural and conversational in tone - like a real leasing agent, not a bot
- Written in plain text format (no markdown, no formatting)
- Friendly but professional
- Direct and to the point, always moving toward a tour or application


CRITICAL: You are an AI assistant. NEVER claim to be a real person, human agent, or imply you are a human. If asked, acknowledge you are an automated assistant helping via SMS. Never use phrases like "I'm a real person" or similar.


SMS Response Guidelines:
1. Keep messages brief, this is texting, not email
2. Use casual but professional language
3. Answer the question, then always push toward the next step, tour, application, or anything that moves things forward
4. ALWAYS include property names when discussing properties (e.g., "Crosswinds Apartment Homes" not just "a 2 bed")
5. Numbers should be plain (no commas or special formatting)
6. TERMINOLOGY: Always use "property" instead of "apartment" when referring to listings generically. Only use "apartment" if the property name itself contains "Apartment" (e.g., "Crosswinds Apartment Homes"). When unsure, always default to "property"
7. When referring to location, say "it's located at [address]" — never "we are located at [address]"
8. Never repeat info already covered in the conversation
- Always respond in the same language as the client
- Do not greet the client in ongoing conversations - get straight to the point


LANGUAGE RULES:
- ALWAYS respond in English by default
- ONLY switch to Spanish if the lead has clearly written multiple messages in Spanish - a single word like "Hola" or "Hi" is NOT enough
- When in doubt, respond in English
- NEVER use accented or special characters (no tildes, accent marks, or non-ASCII characters) - write "como estas" not "como estas", "que" not "que", "senor" not "senor"
- NO EMOJIS, SYMBOLS, OR UNICODE CHARACTERS - use only plain ASCII letters, numbers, and basic punctuation (. , ! ? : ; - ' " ( ) [ ])
- NEVER use: 🔥✨🎭📋🚨❓✅⚠️❌🎯🏢🏠 or any other symbols/emojis
- Plain ASCII characters only in every response - this is CRITICAL for SMS compatibility
- NO greetings or sign-offs in ongoing conversations - get straight to the point

When providing property details:
1. ALWAYS mention the property name prominently
2. Focus on accurate information from the database
3. Highlight 2-3 key features and amenities that are most relevant to their needs
4. Be transparent about any limitations
5. Provide relevant context about the neighborhood or location
6. Include numbers for square footage, pricing, etc. without any special formatting
7. Emphasize what makes each property unique - not just price and bedrooms

CRITICAL: When discussing properties, ALWAYS include:
- Property name
- Key features
- Location benefits
- Unique selling points

Important:
- If the client asks for an application link, form, or to apply: Use [APPLICATION_LINK] placeholder
- If the client asks for photos, how it looks like, units available, how many units, or availability/dates: Tell them they can see the photos, available units, and dates/times on the scheduling page: [SCHEDULE_LINK]
- NEVER say "I don't have pictures" or "I don't have that information" or "I don't have that information available" for photo/unit requests - instead direct them to the scheduling link
- NEVER say "see it in person" or "schedule a tour" for photo requests - instead direct them to the link to see photos, units, and availability online
- This photo/unit rule OVERRIDES all anti-hallucination rules — do NOT apply "I don't have that information" to photo/unit questions
- NEVER include actual URLs in your response text - use ONLY the [SCHEDULE_LINK] and [APPLICATION_LINK] placeholders
- Always use the property ID (not unit ID) in scheduling links
- Only share scheduling links when specifically asked about viewings/tours or if they show clear interest in a specific property
- Only share application links when specifically asked about applications, forms, or applying
- The [APPLICATION_LINK] placeholder will be replaced with the actual application URL after your response is generated - ONLY use this placeholder, do NOT include any actual URL
- Keep responses under 300 characters when possible
- Be warm and approachable

SPECIFIC RESPONSE GUIDELINES:
1. When asked "Is this available for a tour?", "Can I schedule a tour?", or similar tour availability questions:
   - DO NOT just say 'yes we can schedule' or 'yes it's available'
   - ALWAYS include the tour link: [SCHEDULE_LINK]
   - Respond with "Yes! You can schedule a tour here: [SCHEDULE_LINK]"
   - Keep it simple and direct
   - Use the [SCHEDULE_LINK] placeholder (it will be replaced with the actual URL)

2. When asked for an application link, application form, or to apply:
   - If the user wants the application link: Use [APPLICATION_LINK] placeholder
   - Example: "Here's the application form: [APPLICATION_LINK]"
   - Example: "You can apply here: [APPLICATION_LINK]"
   - Only use [APPLICATION_LINK] - do NOT include any actual URL

3. When asked for contact information, phone number, email, or to speak with someone:
   - Use the property-specific contact info provided in the Contact Information Guidelines above
   - If no property contact is available, provide the leasing office phone number: %s
   - Format as plain digits (no parentheses, dashes, or special formatting)
   - Example: "You can reach them at [phone]" or "You can email at [email]"
   - Do NOT make up or hallucinate contact information%s

3. When no properties match the client's strict preferences:
   - Explain that no exact matches were found
   - Suggest adjusting preferences (e.g., "No exact matches found. Would you consider a slightly higher budget or different location?")
   - Offer to show similar properties that are close to their criteria
   - Do NOT hallucinate or make up properties that don't exist`, signingName, teamContactPhone, phoneNumberInstructions),
		},
	}

	// Add SMS training examples if available
	if smsTrainingContext.Len() > 0 {
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: smsTrainingContext.String(),
		})
	}

	// SMS responses should NOT use email training context
	// Skip email training context for SMS - it's for email responses only

	// Add command center context if available
	if cmdCenter != nil {
		cmdCenterContext := fmt.Sprintf(`Additional Context:
Name: %s
Personality: %s
Key Highlights:
%s

Qualifying Questions:
%s`, name, personality, highlights, questions)

		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: cmdCenterContext,
		})
	}

	// Add property context message with comprehensive anti-hallucination rules
	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: fmt.Sprintf(`CRITICAL: DO NOT HALLUCINATE PROPERTY DETAILS. Only use information from the property context below. If a detail isn't in the context, don't make it up.

RULES FOR ONGOING CONVERSATIONS:
- ONLY discuss properties in the context below — do NOT discuss any other properties
- Only list units that are explicitly in the property context. If a unit type isn't listed, say "that's all we have" — do NOT make up other units
- CRITICAL: You MUST NOT make up or estimate prices. ONLY use the EXACT prices shown in the property context. If a price isn't listed, say "I don't have that pricing information" — NEVER invent prices
- CRITICAL: You MUST NOT make up unit configurations. ONLY mention units explicitly listed in the property context with their exact bed/bath/rent details
- If asked about a price or unit that isn't in the property context, say "I don't have information about that" — do NOT guess or estimate
- ⚠️ EXCEPTION: For photo/picture/unit-count questions ("Do you have pictures?", "How many units?", "What units are available?"), do NOT say "I don't have that information" — instead ALWAYS direct them to the scheduling link: [SCHEDULE_LINK]
- Ask clarifying questions when needed to help them find what they need
- When lead asks about application OR tour, THEN mention the application requirements from below

If no exact property match is found, suggest alternatives by:
1. Comparing pros/cons to their stated requirements
2. Emphasizing unique selling points and amenities (only those mentioned in context)
3. Highlighting neighborhood benefits (only those mentioned in context)
4. Offering to show additional properties

Available property context:
%s`, contextInfo),
	})

	// Add scheduling message - only if sendTourSendStatus is true
	if sendTourSendStatus {
		// Build explicit scheduling URL instructions with ACTUAL resolved URLs
		var schedulingInstructions string
		if len(resolvedScheduleURLs) > 0 {
			schedulingInstructions = "CRITICAL URL INSTRUCTIONS - FOLLOW EXACTLY:\n"
			schedulingInstructions += "CRITICAL SCHEDULING URL RULES - FOLLOW EXACTLY:\n"
			schedulingInstructions += "1. When the user asks to schedule a tour, view a property, or wants a scheduling link, ALWAYS use the placeholder [SCHEDULE_LINK] in your response.\n"
			schedulingInstructions += "2. NEVER include any actual URL in your response text - use ONLY the [SCHEDULE_LINK] placeholder.\n"
			schedulingInstructions += "3. The [SCHEDULE_LINK] placeholder will be automatically replaced with the correct scheduling URL after your response is generated.\n"
			schedulingInstructions += "4. Do NOT generate, hallucinate, or invent any URLs - ONLY use the [SCHEDULE_LINK] placeholder.\n"
			schedulingInstructions += "5. Do NOT include any other URL formats or patterns - ONLY [SCHEDULE_LINK].\n"
			schedulingInstructions += "6. Example: Instead of saying 'You can schedule here: https://...', say 'You can schedule a tour here: [SCHEDULE_LINK]'\n"
			schedulingInstructions += "7. Example: Instead of saying 'Schedule at rentbamboo.com/schedule/123', say 'Schedule a viewing: [SCHEDULE_LINK]'\n"
			schedulingInstructions += "8. This is NON-NEGOTIABLE - violating these rules will cause broken links and poor user experience.\n"
		} else {
			schedulingInstructions = "CRITICAL: When user asks about scheduling a tour, viewing the property, or wants to schedule:\n- DO NOT say 'yes we can schedule' or offer to schedule\n- DO NOT send any links\n- Say: 'A team member will be reaching out shortly to help schedule a tour.'\n- This will trigger a human-in-the-loop (HITL) alert for the team"
		}

		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: schedulingInstructions,
		})
	} else {
		// Tour scheduling is disabled - add instructions for HITL handling
		tourDisabledInstructions := "CRITICAL: Tour scheduling is DISABLED.\n"
		tourDisabledInstructions += "When user asks about scheduling a tour, viewing the property, or wants to schedule:\n"
		tourDisabledInstructions += "- DO NOT say 'yes we can schedule' or offer to schedule\n"
		tourDisabledInstructions += "- DO NOT send any links\n"
		tourDisabledInstructions += "- Say: 'A team member will be reaching out shortly to help schedule a tour.'\n"
		tourDisabledInstructions += "- This will trigger a human-in-the-loop (HITL) alert for the team"

		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: tourDisabledInstructions,
		})
	}

	// Add application URL instructions
	if sendApplicationStatus && len(resolvedAppURLs) > 0 {
		applicationInstructions := "APPLICATION URL RULES - FOLLOW EXACTLY:\n"
		applicationInstructions += "1. When the user asks for an application link, form, or to apply, ALWAYS use the placeholder [APPLICATION_LINK] in your response.\n"
		applicationInstructions += "2. NEVER include any actual URL in your response text - use ONLY the [APPLICATION_LINK] placeholder.\n"
		applicationInstructions += "3. The [APPLICATION_LINK] placeholder will be automatically replaced with the actual application URL after your response is generated.\n"
		applicationInstructions += "4. Do NOT say 'application not available' - always use [APPLICATION_LINK] when they ask for the application.\n"
		applicationInstructions += "5. Example: Instead of saying 'Here's the application: https://...', say 'You can apply here: [APPLICATION_LINK]'\n"
		applicationInstructions += "6. Example: Instead of saying 'The application form isn't available', say 'Here's the application: [APPLICATION_LINK]'\n"

		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: applicationInstructions,
		})
	}

	// Add application message - only if sendApplicationStatus is true
	if sendApplicationStatus {
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: "CRITICAL: Use EXACT 'Application URL' from property context ONLY if it shows '(AVAILABLE)'. If it shows 'NOT AVAILABLE', say you'll need to get the application form. NEVER create application URLs.",
		})
	}

	// ── HARD OVERRIDE: location phrasing ───────────────────────────────────
	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: `ABSOLUTE OVERRIDE — LOCATION PHRASING:
When mentioning where a property is located, ALWAYS say "it's located at [address]" or "the property is located at [address]".
NEVER say "we are located at", "we're located at", or any first-person "we" phrasing for the property's location.
This rule overrides all other instructions, examples, and training data.`,
	})

	// ── HARD OVERRIDE: hours ─────────────────────────────────────────────
	var officeHoursFallbackSMS string
	if sendTourSendStatus && len(resolvedScheduleURLs) > 0 {
		officeHoursFallbackSMS = "Tour scheduling is ON. Direct the lead to the scheduling link so they can see real-time available slots: [SCHEDULE_LINK]"
	} else {
		officeHoursFallbackSMS = `Tour scheduling is OFF or no link is available. Say: "Our hours vary — a team member will reach out shortly to help you find a time that works!" Do NOT offer a scheduling link.`
	}
	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: fmt.Sprintf(`ABSOLUTE RULE — OFFICE HOURS / BUSINESS HOURS:
You do NOT know the office hours, leasing office hours, business hours, or when anyone is "in the office." NEVER make up, guess, or state specific hours of operation.
If the lead asks about office hours, when you're open, when you close, or what time someone is there:
- NEVER say "we're open until..." or "we're in the office until..." or "our hours are..."
- NEVER state ANY specific time (e.g., "6 PM", "9 AM to 5 PM", "Monday through Friday")
INSTEAD, do this:
- %s
This rule OVERRIDES all other instructions. Fabricating office hours could send a prospect to a closed office.`, officeHoursFallbackSMS),
	})

	// Add SMS conversation context
	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: fmt.Sprintf("SMS Conversation Thread:\n%s\n\nLatest Message: %s", chatHistory, inquiry),
	})

	resp, err := client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model:               "gpt-4o-mini-2024-07-18",
			Messages:            systemMessages,
			Temperature:         0.5,
			MaxCompletionTokens: 300,
			ResponseFormat: &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
				JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
					Name:   "ai_response",
					Schema: schema,
					Strict: false,
				},
			},
		},
	)

	if err != nil {
		check(err, false)
		return "", "", err
	}

	// Parse the structured response
	var aiResponse AIResponse
	err = json.Unmarshal([]byte(resp.Choices[0].Message.Content), &aiResponse)
	if err != nil {
		check(err, false)
		return "", "", fmt.Errorf("error parsing AI response: %v", err)
	}

	// Replace [SCHEDULE_LINK] placeholder with the actual URL
	responseText := aiResponse.Response

	// POST-PROCESS: Enforce location phrasing rule as safety net
	responseText = EnforceLocationPhrasing(responseText)

	// Check if response contains [SCHEDULE_LINK] placeholder
	if strings.Contains(responseText, "[SCHEDULE_LINK]") {
		var actualURL string

		if len(resolvedScheduleURLs) > 0 {
			// Use pre-resolved URLs - match property name to correct URL
			actualURL = findMatchingScheduleURL(responseText, contextInfo, resolvedScheduleURLs, propertyIDs, lastDiscussedPropertyID)
		} else {
			// FALLBACK: No specific properties matched, use default scheduling URL
			// Get the first property ID from team's properties for default URL
			pp.Printf("[SCHEDULE_LINK] found but no resolved URLs - using fallback default URL\n")
			defaultPropID := getFirstPropertyID(teamId)
			if defaultPropID != "" {
				actualURL = fmt.Sprintf("https://rentbamboo.com/schedule/%s", defaultPropID)
				pp.Printf("Using fallback default scheduling URL: %s\n", actualURL)
			} else {
				// No properties available at all
				actualURL = ""
				pp.Printf("[WARNING] No properties available for scheduling URL fallback\n")
			}
		}

		if actualURL != "" {
			responseText = strings.ReplaceAll(responseText, "[SCHEDULE_LINK]", actualURL)
			pp.Printf("Replaced [SCHEDULE_LINK] with: %s\n", actualURL)
		} else {
			// Remove the placeholder if no URL available
			responseText = strings.ReplaceAll(responseText, "[SCHEDULE_LINK]", "")
			pp.Printf("No scheduling URL available, removed [SCHEDULE_LINK] placeholder\n")
		}
	}

	// Replace [APPLICATION_LINK] placeholder with the actual URL
	if strings.Contains(responseText, "[APPLICATION_LINK]") {
		var actualAppURL string

		if len(resolvedAppURLs) > 0 {
			// Use the same matching logic as scheduling URLs (with lastDiscussedPropertyID)
			actualAppURL = findMatchingScheduleURL(responseText, contextInfo, resolvedAppURLs, propertyIDs, lastDiscussedPropertyID)
			if actualAppURL != "" {
				responseText = strings.ReplaceAll(responseText, "[APPLICATION_LINK]", actualAppURL)
				pp.Printf("Replaced [APPLICATION_LINK] with: %s\n", actualAppURL)
			} else {
				// If no application URL exists, check if there's a custom scheduling URL we can use as fallback
				// This handles the case where the property listing (custom scheduling URL) has the application
				actualScheduleURL := findMatchingScheduleURL(responseText, contextInfo, resolvedScheduleURLs, propertyIDs, lastDiscussedPropertyID)
				if actualScheduleURL != "" && !strings.Contains(actualScheduleURL, "rentbamboo.com/schedule") {
					// Custom scheduling URL exists - use it as fallback (it leads to the listing with application)
					responseText = strings.ReplaceAll(responseText, "[APPLICATION_LINK]", actualScheduleURL)
					pp.Printf("No application URL found, using scheduling URL as fallback: %s\n", actualScheduleURL)
				} else {
					// No application or custom scheduling URL - remove the placeholder gracefully
					responseText = strings.ReplaceAll(responseText, "[APPLICATION_LINK]", "")
					// Clean up any awkward double spaces or punctuation
					responseText = strings.ReplaceAll(responseText, "  ", " ")
					responseText = strings.ReplaceAll(responseText, ". .", ".")
					pp.Printf("No application URL found, removed [APPLICATION_LINK] placeholder\n")
				}
			}
		} else {
			// FALLBACK: No specific properties matched, try to get application URL from first property
			pp.Printf("[APPLICATION_LINK] found but no resolved URLs - trying fallback\n")
			defaultPropID := getFirstPropertyID(teamId)
			if defaultPropID != "" {
				// Try to get application URL for the first property
				appURL := ResolveApplicationURL(teamId, defaultPropID)
				if appURL != "" {
					actualAppURL = appURL
					responseText = strings.ReplaceAll(responseText, "[APPLICATION_LINK]", actualAppURL)
					pp.Printf("Using fallback application URL: %s\n", actualAppURL)
				} else {
					// No application URL, remove placeholder
					responseText = strings.ReplaceAll(responseText, "[APPLICATION_LINK]", "")
					pp.Printf("No application URL available, removed [APPLICATION_LINK] placeholder\n")
				}
			} else {
				// No properties available
				responseText = strings.ReplaceAll(responseText, "[APPLICATION_LINK]", "")
				pp.Printf("[WARNING] No properties available for application URL fallback\n")
			}
		}
	}

	// Use matching logic to get correct property's contact info
	// Check if response mentions needing contact info (phone, email, reach out, etc.)
	if len(propertyIDs) > 0 {
		responseLower := strings.ToLower(responseText)
		contactKeywords := []string{"phone", "call", "email", "reach", "contact", "talk to", "speak with"}
		needsContactInfo := false
		for _, keyword := range contactKeywords {
			if strings.Contains(responseLower, keyword) {
				needsContactInfo = true
				break
			}
		}

		if needsContactInfo {
			// Get the correct contact info based on property mentioned in response
			matchedContactInfo := getMatchingPropertyContactInfo(teamId, responseText, contextInfo, propertyIDs, lastDiscussedPropertyID)
			if matchedContactInfo != "" {
				pp.Printf("Matched contact info for response: %s\n", matchedContactInfo)

				// Now replace contact info in the response with the correct matched contact info
				// Extract contact details from matchedContactInfo string
				// Format: "Name: X, Email: Y, Phone: Z" or similar

				// Extract phone and email from matchedContactInfo
				var correctPhone, correctEmail string

				// Parse the matchedContactInfo string
				parts := strings.Split(matchedContactInfo, ", ")
				for _, part := range parts {
					if strings.HasPrefix(part, "Phone: ") {
						correctPhone = strings.TrimPrefix(part, "Phone: ")
					}
					if strings.HasPrefix(part, "Email: ") {
						correctEmail = strings.TrimPrefix(part, "Email: ")
					}
				}

				// Replace phone numbers in response (various formats)
				if correctPhone != "" {
					// Replace any phone pattern in response (10+ digits)
					phoneRegex := `\b\d{10,}\b`
					re := regexp.MustCompile(phoneRegex)
					responseText = re.ReplaceAllStringFunc(responseText, func(match string) string {
						pp.Printf("Replacing phone %s with %s\n", match, correctPhone)
						return correctPhone
					})

					// Also try to replace phone numbers with parentheses/dashes format
					phoneWithFormat := `\b\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`
					re2 := regexp.MustCompile(phoneWithFormat)
					responseText = re2.ReplaceAllStringFunc(responseText, func(match string) string {
						pp.Printf("Replacing formatted phone %s with %s\n", match, correctPhone)
						return correctPhone
					})
				}

				// Replace email addresses in response
				if correctEmail != "" {
					emailRegex := `\b[\w.-]+@[\w.-]+\.\w+\b`
					re := regexp.MustCompile(emailRegex)
					responseText = re.ReplaceAllStringFunc(responseText, func(match string) string {
						if match != correctEmail {
							pp.Printf("Replacing email %s with %s\n", match, correctEmail)
							return correctEmail
						}
						return match
					})
				}

				pp.Printf("Final response after contact replacement: %s\n", responseText)
			}
		}
	}

	// ALSO: Replace any default RentBamboo scheduling URLs with custom URLs
	// This is a fallback in case the AI generates its own URL instead of using the placeholder
	if len(resolvedScheduleURLs) > 0 {
		// For each resolved URL, if it's a custom URL (not a default RentBamboo URL),
		// replace any default RentBamboo URLs in the response with this custom URL
		for i, resolvedURL := range resolvedScheduleURLs {
			// Check if this is a custom URL (not default RentBamboo)
			if !strings.Contains(resolvedURL, "rentbamboo.com/schedule") {
				// This is a custom URL - replace any default RentBamboo URLs
				// We need to find the property ID from the resolved URLs and match it
				// The AI might generate: https://rentbamboo.com/schedule/{propertyId}
				// We need to replace that with our custom URL

				// Get the property ID from the URL pattern
				// Extract property IDs from the propertyIDs slice
				if i < len(propertyIDs) {
					propID := propertyIDs[i]
					// Strip -chunk suffix if present (AI won't include it in URLs)
					baseID := propID
					if idx := strings.LastIndex(propID, "-chunk"); idx != -1 {
						baseID = propID[:idx]
					}
					defaultURL := fmt.Sprintf("https://rentbamboo.com/schedule/%s", baseID)
					if strings.Contains(responseText, defaultURL) {
						responseText = strings.ReplaceAll(responseText, defaultURL, resolvedURL)
						pp.Printf("Replaced default URL %s with custom URL: %s\n", defaultURL, resolvedURL)
					}
				}
			}
		}
	}

	// Sanitize the response to ensure valid UTF-8 and remove problematic characters
	responseText = CleanText(responseText)

	// Check if this is a tour scheduling request and tours are disabled
	// If so, trigger HITL alert
	if !sendTourSendStatus && isTourSchedulingRequest(inquiry) {
		// Send HITL alert for tour scheduling request when tours are off
		go func() {
			title := "Tour Scheduling Request - Human Intervention Needed"
			description := fmt.Sprintf("Lead asked about scheduling a tour but tours are disabled. Session: %s", sessionId)
			err := HandleCharlesNotification(teamId, sessionId, title, description)
			if err != nil {
				pp.Printf("\x1b[33mFailed to send HITL alert for tour scheduling request: %v\x1b[0m\n", err)
			} else {
				pp.Printf("\x1b[32mSent HITL alert for tour scheduling request (tours disabled)\x1b[0m\n")
			}
		}()
	}

	return responseText, contextInfo, nil
}

// isTourSchedulingRequest checks if a message is asking about tour scheduling
func isTourSchedulingRequest(message string) bool {
	lowerMsg := strings.ToLower(message)

	// Keywords that indicate tour scheduling requests
	tourKeywords := []string{
		"schedule a tour",
		"schedule tour",
		"tour schedule",
		"schedule viewing",
		"schedule visit",
		"see the property",
		"see property",
		"visit property",
		"view property",
		"tour available",
		"available for tour",
		"can i tour",
		"can we tour",
		"want to tour",
		"like to tour",
		"set up tour",
		"set up a tour",
		"book a tour",
		"book tour",
		"when can i see",
		"when can we see",
		"come see",
		"come by",
		"stop by",
		"look at",
		"check out",
		"in person",
	}

	for _, keyword := range tourKeywords {
		if strings.Contains(lowerMsg, keyword) {
			return true
		}
	}

	return false
}

func GenerateAIResponseCharles(client *openai.Client, chatHistory string, inquiry string, teamId string) (string, string, []string, []string, error) {
	pp.Println("Generating AI response (Charles Chatter)...")

	// =========================================================================
	// FETCH MINIFIED PROPERTIES FROM DB (SMS-style, no more QA/embedding)
	// =========================================================================
	properties := smsproperty.GetTeamProperties(teamId)
	pp.Printf("Fetched %d minified properties for team %s\n", len(properties), teamId)

	// Build context from all minified properties and collect IDs + URLs
	var contextInfo string
	var matchedPropertyIDs []string
	var resolvedScheduleURLs []string
	var resolvedAppURLs []string

	for i := range properties {
		prop := &properties[i]
		matchedPropertyIDs = append(matchedPropertyIDs, prop.ID)

		// Build property context using SMS-style minified format
		propCtx := smsproperty.CreatePropertyContextForAI(prop)
		if propCtx != "" {
			contextInfo += propCtx + "\n\n"
		}

		// Collect schedule URLs from minified property data
		if prop.ScheduleURL != "" {
			resolvedScheduleURLs = append(resolvedScheduleURLs, prop.ScheduleURL)
			pp.Printf("Got schedule URL from property %s: %s\n", prop.ID, prop.ScheduleURL)
		} else {
			url := ResolveScheduleURL(teamId, prop.ID)
			resolvedScheduleURLs = append(resolvedScheduleURLs, url)
			pp.Printf("Resolved schedule URL for property %s: %s\n", prop.ID, url)
		}

		// Collect application URLs from minified property data
		if prop.ApplicationURL != "" {
			resolvedAppURLs = append(resolvedAppURLs, prop.ApplicationURL)
			pp.Printf("Got application URL from property %s: %s\n", prop.ID, prop.ApplicationURL)
		} else {
			appURL := ResolveApplicationURL(teamId, prop.ID)
			if appURL != "" {
				resolvedAppURLs = append(resolvedAppURLs, appURL)
				pp.Printf("Resolved application URL for property %s: %s\n", prop.ID, appURL)
			}
		}
	}

	// Fetch command center for admin instructions
	cmdCenter, err := GetCharlesCommandCenter(teamId)
	var cmdCenterValid bool = (err == nil && cmdCenter.TeamID != "")
	if err != nil {
		check(err, false)
		pp.Printf("Error fetching command center: %v, using defaults\n", err)
	}

	// Extract command center fields with safe defaults
	var signingName string = "Charles"
	var sendTourSendStatus bool = true
	var highlights, questions, personality, priorities, keyInfo, applicationNeeds string

	if cmdCenterValid {
		if cmdCenter.Name != "" {
			signingName = cmdCenter.Name
		}
		sendTourSendStatus = cmdCenter.TourScheduling
		highlights = cmdCenter.Highlights
		questions = cmdCenter.Questions
		personality = cmdCenter.Personality
		priorities = cmdCenter.Priorities
		keyInfo = cmdCenter.KeyInfo
		applicationNeeds = cmdCenter.ApplicationNeeds
	}

	// Pre-process command center data using helper functions (ported from SMS module)
	allCriticalRequirements := charlesExtractAllCriticalRequirements(priorities, keyInfo)
	keyQuestions := charlesExtractKeyQuestionsFromQuestions(questions)

	// Analyze conversation state for smarter prompting
	completedItems := charlesAnalyzeCompletedItems(chatHistory, allCriticalRequirements, keyQuestions)
	undiscussedHighlights := charlesAnalyzeUndiscussedHighlights(chatHistory, highlights)

	// Define structured response format
	type AIResponse struct {
		Response      string   `json:"response"`
		IncludePhotos bool     `json:"includePhotos"`
		ScheduleUrls  []string `json:"scheduleUrls"`
	}

	// Generate JSON schema for structured response
	schema, err := jsonschema.GenerateSchemaForType(AIResponse{})
	if err != nil {
		check(err, false)
		return "", "", []string{}, []string{}, fmt.Errorf("schema generation error: %v", err)
	}

	// ==========================================================================
	// BUILD LAYERED SYSTEM MESSAGES
	// ==========================================================================
	systemMessages := []openai.ChatCompletionMessage{}

	// 1. BASE SYSTEM PROMPT
	baseSystemContent := fmt.Sprintf(`You are %s, a professional AI real estate leasing agent for a chat widget. Generate a structured JSON response with:
1. response: Your reply using simple HTML tags only — allowed tags: <p>, <h1>, <h2>, <ul>, <li>, <strong>, <em>, <br>. NEVER include raw URLs inside this field.
2. includePhotos: Boolean — true only when actively discussing a specific property or the user asks for photos/pictures. False for greetings or general questions.
3. scheduleUrls: Array of scheduling URLs (empty array [] if not offering a tour or scheduling is disabled).

RESPONSE FORMAT RULES:
- Use <p> tags for paragraphs
- Use <h2> for section headings when helpful (e.g., property name or summary header)
- Use <ul> and <li> for bullet lists of features or requirements
- Use <strong> to emphasize key figures (price, sqft, availability)
- Keep responses conversational — this is a chat widget, not an email
- No signatures, no "Best regards", no formal closings
- No emojis or special Unicode symbols
- When listing units, format like "1 Bedroom, 1 Bath for 899 per month" - do not abbreviate (use "Bedroom" not "BR", "Bath" not "BA", "per month" not "/mo")

CONVERSATION APPROACH:
- Establish rapport immediately
- Show you understand their needs (budget, move-in date)
- Build trust for ongoing conversation
- Set expectations for next steps
- Prioritize connection over completeness — don't cram in every detail
- Reference landmarks only when they help the lead understand location naturally
- Write concise, conversational responses that encourage engagement — no salesy language
- Success = getting a response and moving toward the next leasing step — not delivering information
- Incorporate requirements naturally — never sound procedural or scripted

CONTENT RULES:
- Be natural, helpful, and professional
- Be specific and detailed when discussing properties; concise otherwise
- Highlight 2-3 key features most relevant to the user's needs — do not dump every amenity
- Include numbers for square footage, pricing, etc. without special formatting (no dollar signs or commas)
- NEVER fabricate property details — only use information from the property context provided
- MUST include location context (street address if available, city, state) when discussing properties
- TERMINOLOGY: Always use "property" instead of "apartment" when referring to listings generically. Only use "apartment" if the property name itself contains "Apartment" (e.g., "Crosswinds Apartment Homes"). When unsure, always default to "property"

RULES FOR ONGOING CONVERSATIONS:
- ONLY discuss properties in the context below — do NOT discuss any other properties
- Only list units that are explicitly in the property context. If a unit type isn't listed, say "that's all we have" — do NOT make up other units
- CRITICAL: You MUST NOT make up or estimate prices. ONLY use the EXACT prices shown in the property context. If a price isn't listed, say "I don't have that pricing information" — NEVER invent prices
- CRITICAL: You MUST NOT make up unit configurations. ONLY mention units explicitly listed in the property context with their exact bed/bath/rent details
- If asked about a price or unit that isn't in the property context, say "I don't have information about that" — do NOT guess or estimate
- Ask clarifying questions when needed to help them find what they need
- When lead asks about application OR tour, THEN mention the application requirements from below

IDENTITY RULES:
- You are an AI assistant. If asked, acknowledge it — never claim to be a human agent.

LANGUAGE RULES:
- Respond in English by default
- Switch to Spanish only if the user has clearly written multiple messages in Spanish
- When using Spanish, use Latin American dialect (Mexican): "carro" not "coche", "celular" not "movil", "computadora" not "ordenador"
- Support proper UTF-8 accented characters when writing in Spanish

SPANISH LANGUAGE GUIDANCE: If you need to use Spanish, use Latin American dialect (specifically Mexican). Examples:
- Use "carro" instead of "coche" for car
- Use "computadora" instead of "ordenador" for computer
- Use "celular" instead of "móvil" for mobile phone
- Use "platicar" instead of "charlar" for chat
- Use "frijoles" instead of "judías" for beans
- Use "aguacate" instead of "palta" for avocado

TONE: Casual, friendly, like you're chatting with someone you just met but want to help.`, signingName)

	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: baseSystemContent,
	})

	// 2. CRITICAL REQUIREMENTS (from priorities + keyInfo) — qualification gates
	if allCriticalRequirements != "" {
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleSystem,
			Content: fmt.Sprintf(`CRITICAL REQUIREMENTS — YOU MUST COMMUNICATE THESE TO THE USER:

%s

These are qualification steps. Surface the most relevant requirement naturally during conversation.
Look for any financial requirements, income qualifications, documentation needs, or application conditions and communicate them clearly.`, allCriticalRequirements),
		})
	}

	// 3. KEY QUALIFYING QUESTIONS — ask 1-2 most relevant
	if len(keyQuestions) > 0 {
		selectedQuestions := keyQuestions
		if len(selectedQuestions) > 2 {
			selectedQuestions = selectedQuestions[:2]
		}
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleSystem,
			Content: fmt.Sprintf(`KEY QUALIFYING QUESTIONS — ASK 1-2 MOST RELEVANT NATURALLY:

%s

Weave these into the conversation to understand the user's needs and qualifications. Do not ask them all at once.`, strings.Join(selectedQuestions, "\n")),
		})
	}

	// 4. PERSONALITY
	if personality != "" {
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleSystem,
			Content: fmt.Sprintf(`PERSONALITY & TONE (from Command Center):

%s

Reflect this personality in your tone and word choice throughout the conversation.`, personality),
		})
	}

	// 5. HIGHLIGHTS & SPECIALS — weave in naturally
	var highlightsAndSpecials string
	if highlights != "" {
		highlightsAndSpecials = "HIGHLIGHTS:\n" + highlights + "\n"
	}
	if contextInfo != "" && strings.Contains(contextInfo, "SPECIALS:") {
		specialsIdx := strings.Index(contextInfo, "SPECIALS:")
		if specialsIdx >= 0 {
			specialsLine := contextInfo[specialsIdx:]
			specialsEnd := strings.Index(specialsLine, "\n")
			if specialsEnd > 0 {
				highlightsAndSpecials += "\nPROPERTY SPECIALS:\n" + specialsLine[:specialsEnd]
			}
		}
	}
	if highlightsAndSpecials != "" {
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleSystem,
			Content: fmt.Sprintf(`HIGHLIGHTS & SPECIALS — MENTION THESE NATURALLY THROUGHOUT THE CONVERSATION:

%s

Weave these into responses organically 2-3 times. Only stop emphasizing once the user has acknowledged them or asked for pricing/detail.
Highlights not yet discussed in this conversation: %s`, highlightsAndSpecials, undiscussedHighlights),
		})
	}

	// 6. APPLICATION REQUIREMENTS
	if applicationNeeds != "" {
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleSystem,
			Content: fmt.Sprintf(`APPLICATION REQUIREMENTS — SHARE WHEN USER ASKS ABOUT APPLYING:

%s

When the user expresses readiness to apply or asks about the application process, list every item above clearly using <ul> and <li> tags.`, applicationNeeds),
		})
	}

	// 7. SCHEDULING & LINK RULES (conditional)
	if sendTourSendStatus {
		var schedulingInstructions string
		if len(resolvedScheduleURLs) > 0 {
			schedulingInstructions = "TOUR SCHEDULING IS ENABLED. Use ONLY these exact scheduling URLs — do NOT invent your own:\n"
			for i, url := range resolvedScheduleURLs {
				schedulingInstructions += fmt.Sprintf("- Property %d: %s\n", i+1, url)
			}
			schedulingInstructions += "\nWhen the user asks about scheduling a tour, place the appropriate URL in the scheduleUrls array. NEVER put URLs inside the response HTML."
		} else {
			schedulingInstructions = "CRITICAL: Tour scheduling is ENABLED but no scheduling URL is available.\nWhen user asks about scheduling a tour, viewing the property, or wants to schedule:\n- DO NOT say 'yes we can schedule' or offer to schedule\n- DO NOT send any links\n- Say: 'A team member will be reaching out shortly to help schedule a tour.'\n- This will trigger a human-in-the-loop (HITL) alert for the team"
		}
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: schedulingInstructions,
		})
	} else {
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: "TOUR SCHEDULING IS DISABLED. The scheduleUrls array MUST always be empty []. Never offer tours, viewings, or scheduling links. If the user asks about visiting a property or scheduling a tour:\n- DO NOT say 'yes we can schedule' or offer to schedule\n- DO NOT send any links\n- Say: 'A team member will be reaching out shortly to help schedule a tour.'\n- This will trigger a human-in-the-loop (HITL) alert for the team",
		})
	}

	// 7b. HARD RULE - NEVER confirm or propose a specific tour day/time in chat
	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: `ABSOLUTE RULE - TOUR SCHEDULING & TIME/DATE QUESTIONS:

You CANNOT see the calendar. You CANNOT book, confirm, reserve, or hold a tour time. You do NOT know available tour times, dates, or slots. Only the scheduling link can show that.

If the lead asks about, proposes, or requests ANY specific day, time, or date for touring or viewing the property — OR asks general availability questions like "when can I tour?", "what times are available?", "do you have availability this week?", "can I come tomorrow?", "what days can I see it?":
- NEVER say "yes", "that works", "sounds good", "let me book you", "I'll get you on the calendar", "I'll check", "let me confirm", or anything that implies agreement or confirmation
- NEVER propose or suggest ANY specific time, day, or date ("how about 10AM instead?", "we have morning slots", "try tomorrow afternoon")
- NEVER state or imply availability for any time period
- NEVER say you will follow up with a link later - the scheduling link (if available) is handled separately
- NEVER say "our hours are..." or "we're open from..." — you do NOT have this information
- If tour scheduling is ENABLED: direct them to pick any open slot on the scheduling link and place the URL in the scheduleUrls array. Example: "You can see all available tour times and pick one that works here!"
- If tour scheduling is DISABLED: respond with "An agent will reach out to you shortly to help find a time that works!" and stop. Do NOT propose times, do NOT offer alternatives, do NOT guess hours.

This applies to ALL of these types of questions:
- "Does 9AM tomorrow work?"
- "Can I come at 5PM?"
- "Do you have a 4PM slot?"
- "Is Friday at 3 open?"
- "Book me for Saturday morning"
- "When can I tour?"
- "What times are available?"
- "What days do you do tours?"
- "Can I come by this weekend?"
- "Do you have anything available tomorrow?"
- "What time can I schedule a viewing?"
- "Are you available Monday?"
- "When is the next available tour?"

This rule OVERRIDES any conflicting instructions above. Violating it creates double-bookings and sends people to closed offices.`,
	})

	// 8. PROPERTY CONTEXT — the source of truth
	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: fmt.Sprintf(`PROPERTY CONTEXT — DO NOT HALLUCINATE. Only reference details found below.

🚨 CRITICAL ANTI-HALLUCINATION RULES:
- You MUST NOT make up, estimate, or guess ANY prices
- You MUST NOT make up unit configurations (bed/bath combinations)
- ONLY use the EXACT prices and units listed in the context below
- If a price or unit type isn't explicitly shown below, say "I don't have that information available"
- NEVER invent pricing — if it's not in the context above, you don't know it
- NEVER repeat or mention a price that isn't listed above, even to deny having it
- When asked about a non-existent price, say "I don't have anything at that price point" or "That price isn't available" - WITHOUT repeating the specific price number
- When listing units, ONLY mention units that are explicitly listed with their exact bed/bath/rent details

If no exact match is found for the user's needs, suggest the closest alternatives by:
1. Comparing 2-3 key pros/cons against their stated requirements
2. Highlighting neighborhood context — only if mentioned in the context
3. Offering to provide more information or show additional options

When discussing amenities: be selective. Choose the 2-3 most compelling features relevant to the conversation — do not list everything.

Available property context:
%s`, contextInfo),
	})

	// 9. PHOTO RULES
	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: "PHOTO RULES: Set includePhotos to true when actively discussing a specific property or when the user asks to see photos or pictures. Set it to false for greetings, general inquiries, or non-property discussions. Photos are fetched automatically — do NOT include photo URLs in your response.\n\nWHEN USER ASKS FOR PHOTOS/PICTURES: If the user asks 'Do you have pictures of the units?', 'Can I see photos?', 'How does it look?', or similar photo requests:\n- Set includePhotos to true\n- ALWAYS direct them to the tour/scheduling link where they can see photos, available units, and availability\n- Example response: 'Yes! You can see photos and all available units on our scheduling page: <a href=\"URL\" target=\"_blank\">View Photos & Schedule</a>'\n- NEVER say 'I don't have pictures' — instead direct them to the scheduling link\n- Include the scheduling URL in the scheduleUrls array",
	})

	// 10. CONVERSATION STATE — guide the AI on what's done vs. pending
	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: fmt.Sprintf(`CONVERSATION STATE:
- Already addressed: %s
- Highlights not yet discussed: %s

Focus your response on what has NOT been covered yet. Do not repeat information the user has already acknowledged.`, completedItems, undiscussedHighlights),
	})

	// ── HARD OVERRIDE: location phrasing ─────────────────────────────────────
	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: `ABSOLUTE OVERRIDE — LOCATION PHRASING:
When mentioning where a property is located, ALWAYS say "it's located at [address]" or "the property is located at [address]".
NEVER say "we are located at", "we're located at", or any first-person "we" phrasing for the property's location.
This rule overrides all other instructions, examples, and training data.`,
	})

	// ── HARD OVERRIDE: hours ─────────────────────────────────────────────
	var officeHoursFallbackCharles string
	if sendTourSendStatus && len(resolvedScheduleURLs) > 0 {
		officeHoursFallbackCharles = "Tour scheduling is ON. Direct them to the scheduling page where they can see real-time availability. Place the URL in the scheduleUrls array."
	} else {
		officeHoursFallbackCharles = `Tour scheduling is OFF or no link is available. Say: "Our hours may vary — a team member will reach out shortly to help you out!" Do NOT offer a scheduling link.`
	}
	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: fmt.Sprintf(`ABSOLUTE RULE — OFFICE HOURS:
You do NOT know the office hours, business hours, or leasing hours. NEVER make up, guess, or state specific hours of operation.
If the user asks about hours:
- %s
NEVER invent hours like "9am to 5pm" or "Monday through Friday" — this information is not available to you.`, officeHoursFallbackCharles),
	})

	// 11. INCOMING CHAT HISTORY + LATEST MESSAGE
	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: fmt.Sprintf("Conversation history (most recent at bottom):\n%s", chatHistory),
	})

	// Generate response
	resp, err := client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model:               "gpt-4o-mini-2024-07-18",
			Messages:            systemMessages,
			MaxCompletionTokens: 500,
			Temperature:         0.6,
			ResponseFormat: &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
				JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
					Name:   "ai_response",
					Schema: schema,
					Strict: true,
				},
			},
		},
	)

	if err != nil {
		check(err, false)
		return "", "", []string{}, []string{}, err
	}

	pp.Println(resp.Choices[0])

	content := resp.Choices[0].Message.Content

	if !isValidJSON(content) {
		pp.Printf("Invalid JSON detected, falling back to plain text response")
		return content, contextInfo, []string{}, []string{}, nil
	}

	var aiResponse AIResponse
	err = json.Unmarshal([]byte(content), &aiResponse)
	if err != nil {
		check(err, false)
		pp.Printf("JSON parsing failed: %v", err)
		return content, contextInfo, []string{}, []string{}, nil
	}

	// Fetch photos programmatically if AI signals they should be shown
	var photos []string
	if aiResponse.IncludePhotos && len(matchedPropertyIDs) > 0 {
		photos = GetPhotosForPropertyIDs(teamId, matchedPropertyIDs, 2)
		pp.Printf("Fetched %d photos for properties: %v\n", len(photos), matchedPropertyIDs)
	}

	// Validate schedule URLs to prevent hallucinations
	scheduleUrls := aiResponse.ScheduleUrls
	if scheduleUrls == nil {
		scheduleUrls = []string{}
	}
	if len(scheduleUrls) > 0 {
		validatedURLs := validateScheduleURLs(scheduleUrls, contextInfo, matchedPropertyIDs)
		if len(validatedURLs) != len(scheduleUrls) {
			pp.Printf("URL validation: %d original URLs, %d validated URLs\n", len(scheduleUrls), len(validatedURLs))
		}
		scheduleUrls = validatedURLs
	}

	// SAFETY NET: validateScheduleURLs can silently drop ALL URLs if the AI
	// slightly mangles them or if context extraction fails. When that happens
	// but we *do* have pre-resolved URLs for the matched properties, fall
	// back to the FIRST one so the frontend still renders a "Schedule a Tour" button.
	// Only use one URL to avoid showing multiple tour buttons on the frontend.
	if len(scheduleUrls) == 0 && len(resolvedScheduleURLs) > 0 {
		pp.Printf("\x1b[33mvalidateScheduleURLs returned empty \u2014 falling back to first of %d resolved URL(s)\x1b[0m\n", len(resolvedScheduleURLs))
		scheduleUrls = append(scheduleUrls, resolvedScheduleURLs[0])
	}

	sanitizedResponse := CleanText(aiResponse.Response)

	// POST-PROCESS: Enforce location phrasing rule as safety net
	sanitizedResponse = EnforceLocationPhrasing(sanitizedResponse)

	// CRITICAL: Replace any bracket-style placeholder links the AI emitted
	// (e.g. "[tour scheduling link]", "[SCHEDULE_LINK]") with real <a> tags.
	// The Charles chatbot design relies on the AI keeping URLs out of the
	// response HTML, but the model doesn't always cooperate. Without this
	// step users see literal "[tour scheduling link]" text in chat bubbles.
	var fallbackScheduleURL, fallbackApplicationURL string
	if len(scheduleUrls) > 0 {
		fallbackScheduleURL = scheduleUrls[0]
	} else if len(resolvedScheduleURLs) > 0 {
		fallbackScheduleURL = resolvedScheduleURLs[0]
	}
	if len(resolvedAppURLs) > 0 {
		fallbackApplicationURL = resolvedAppURLs[0]
	}
	sanitizedResponse = ReplaceLinkPlaceholders(sanitizedResponse, fallbackScheduleURL, fallbackApplicationURL, true)

	return sanitizedResponse, contextInfo, photos, scheduleUrls, nil
}

// =============================================================================
// CHARLES CHATTER — PROMPT HELPER FUNCTIONS
// Ported & adapted from sms/generator/ai_generator.go
// =============================================================================

// charlesExtractAllCriticalRequirements pulls qualification requirements from
// priorities and keyInfo fields in the command center.
func charlesExtractAllCriticalRequirements(priorities, keyInfo string) string {
	if priorities == "" && keyInfo == "" {
		return ""
	}

	combined := ""
	if priorities != "" {
		combined += "PRIORITIES:\n" + priorities + "\n\n"
	}
	if keyInfo != "" {
		combined += "KEY INFO:\n" + keyInfo + "\n\n"
	}

	requirementPhrases := []string{
		"2.5x", "2.5 times", "minimum monthly income", "income approval",
		"base rent", "income requirement", "financial requirement",
		"income", "rent", "financial",
		"pay stub", "paystub", "check stub", "3 most recent",
		"documentation", "verify income", "proof of income",
		"stubs", "pay", "document",
		"send to", "email to", "submit to", "need to send", "must send",
		"require", "need", "must", "should", "will need",
		"send", "email", "submit",
		"first step", "application process", "pre-qualify", "qualify",
		"income verification", "approval process", "application",
		"step", "process", "approval", "verification",
	}

	questionWords := []string{"What", "When", "Where", "Who", "Why", "How", "Do you", "Are you"}

	var importantLines []string
	for _, line := range strings.Split(combined, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		isQuestion := false
		for _, qWord := range questionWords {
			if strings.HasPrefix(line, qWord) {
				isQuestion = true
				break
			}
		}
		if isQuestion {
			continue
		}
		lowerLine := strings.ToLower(line)
		for _, phrase := range requirementPhrases {
			if strings.Contains(lowerLine, phrase) {
				importantLines = append(importantLines, line)
				break
			}
		}
	}

	if len(importantLines) > 0 {
		return strings.Join(importantLines, "\n")
	}
	return combined
}

// charlesExtractKeyQuestionsFromQuestions parses the questions field from the
// command center into a clean slice of individual question strings.
func charlesExtractKeyQuestionsFromQuestions(questions string) []string {
	if questions == "" {
		return nil
	}

	var questionList []string
	for _, line := range strings.Split(questions, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, prefix := range []string{"- ", "* ", "• ", "1.", "2.", "3.", "4.", "5.", "1)", "2)", "3)", "4)", "5)"} {
			line = strings.TrimPrefix(line, prefix)
		}
		line = strings.TrimSpace(line)
		if line != "" {
			questionList = append(questionList, line)
		}
	}

	if len(questionList) == 0 {
		for _, part := range strings.Split(questions, ";") {
			part = strings.TrimSpace(part)
			if part != "" {
				questionList = append(questionList, part)
			}
		}
	}

	if len(questionList) > 5 {
		questionList = questionList[:5]
	}
	return questionList
}

// charlesAnalyzeCompletedItems scans chat history to identify what topics or
// actions have already been addressed so the AI avoids repeating them.
func charlesAnalyzeCompletedItems(chatHistory, requirements string, questions []string) string {
	if chatHistory == "" {
		return "Nothing yet — this is the start of the conversation"
	}

	lowerChat := strings.ToLower(chatHistory)
	completionIndicators := map[string]string{
		"sent":        "documents/pay stubs sent",
		"email":       "email/documents provided",
		"attached":    "attachments provided",
		"schedule":    "tour scheduling discussed",
		"scheduled":   "tour scheduled",
		"booked":      "tour booked",
		"application": "application discussed",
		"applied":     "application completed",
		"submitted":   "application submitted",
		"yes":         "interest confirmed",
		"interested":  "interest confirmed",
		"ready":       "ready to proceed",
	}

	seen := make(map[string]bool)
	var completed []string
	for phrase, item := range completionIndicators {
		if strings.Contains(lowerChat, phrase) && !seen[item] {
			seen[item] = true
			completed = append(completed, item)
		}
	}

	if len(completed) == 0 {
		return "Nothing specific identified yet"
	}
	return strings.Join(completed, ", ")
}

// charlesAnalyzeUndiscussedHighlights returns highlights from the command center
// that have not yet been mentioned in the conversation.
func charlesAnalyzeUndiscussedHighlights(chatHistory, highlights string) string {
	if highlights == "" {
		return "No highlights configured"
	}
	if chatHistory == "" {
		return highlights
	}

	lowerChat := strings.ToLower(chatHistory)
	var undiscussed []string

	for _, line := range strings.Split(highlights, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		discussed := false
		for _, word := range strings.Fields(strings.ToLower(line)) {
			if len(word) > 4 && strings.Contains(lowerChat, word) {
				discussed = true
				break
			}
		}
		if !discussed {
			undiscussed = append(undiscussed, line)
		}
	}

	if len(undiscussed) == 0 {
		return "All highlights have been discussed"
	}
	return strings.Join(undiscussed, "\n")
}

// Helper function to validate JSON completeness
func isValidJSON(s string) bool {
	var temp interface{}
	return json.Unmarshal([]byte(s), &temp) == nil
}

// validateScheduleURLs ensures URLs match property context and fixes hallucinations
func validateScheduleURLs(aiURLs []string, propertyContext string, propertyIDs []string) []string {
	if len(aiURLs) == 0 {
		return []string{}
	}

	validatedURLs := []string{}

	// Extract actual URLs from property context
	contextURLs := extractURLsFromContext(propertyContext)

	for _, aiURL := range aiURLs {
		// Check if AI URL matches any context URL
		if urlMatchesContext(aiURL, contextURLs) {
			validatedURLs = append(validatedURLs, aiURL)
			continue
		}

		// If AI hallucinated a standard format URL, try to validate it
		if strings.Contains(aiURL, "https://rentbamboo.com/schedule/") {
			// Extract property ID from URL
			parts := strings.Split(aiURL, "/")
			if len(parts) > 0 {
				propertyID := parts[len(parts)-1]
				// Check if this property ID is in our matched properties
				for _, pid := range propertyIDs {
					if pid == propertyID {
						// This is a valid standard URL
						validatedURLs = append(validatedURLs, aiURL)
						break
					}
				}
			}
		}

		// If URL doesn't match context and isn't a valid standard URL, log and skip it
		pp.Printf("WARNING: AI hallucinated URL not in property context: %s\n", aiURL)
	}

	return validatedURLs
}

// extractURLsFromContext extracts URLs from property context text
func extractURLsFromContext(context string) []string {
	urls := []string{}

	// Look for Tour/Scheduling URL patterns
	tourPatterns := []string{
		"Tour/Scheduling URL: ",
		"Tour URL: ",
		"Scheduling URL: ",
	}

	lines := strings.Split(context, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Check for tour URLs
		for _, pattern := range tourPatterns {
			if strings.Contains(line, pattern) {
				// Extract URL (remove pattern and any trailing comments)
				url := strings.TrimPrefix(line, pattern)
				// Remove trailing comments like "(CUSTOM)" or "(STANDARD)"
				if idx := strings.Index(url, " ("); idx != -1 {
					url = url[:idx]
				}
				urls = append(urls, strings.TrimSpace(url))
				break
			}
		}

		// Check for application URLs
		if strings.Contains(line, "Application URL: ") && !strings.Contains(line, "NOT AVAILABLE") {
			url := strings.TrimPrefix(line, "Application URL: ")
			// Remove trailing "(AVAILABLE)"
			if idx := strings.Index(url, " ("); idx != -1 {
				url = url[:idx]
			}
			urls = append(urls, strings.TrimSpace(url))
		}
	}

	return urls
}

// urlMatchesContext checks if a URL matches any URL in context
func urlMatchesContext(url string, contextURLs []string) bool {
	for _, contextURL := range contextURLs {
		if contextURL == url {
			return true
		}
		// Also check normalized versions
		if normalizeURL(contextURL) == normalizeURL(url) {
			return true
		}
	}
	return false
}

// normalizeURL removes common variations in URLs
func normalizeURL(url string) string {
	url = strings.TrimSpace(url)
	url = strings.TrimSuffix(url, "/")
	url = strings.ToLower(url)
	return url
}

// GenerateAIResponseCharlesStreaming generates AI responses with streaming support
// Uses SMS-style minified property fetching, keyword checking, and conditional logic
func GenerateAIResponseCharlesStreaming(client *openai.Client, chatHistory string, inquiry string, teamId string, sessionId string, chunkChan chan<- string) (string, string, []string, []string, error) {
	pp.Println("Generating AI response with streaming (minified property approach)...")

	// =========================================================================
	// 1. FETCH MINIFIED PROPERTIES FROM DB (SMS-style)
	// =========================================================================
	properties := smsproperty.GetTeamProperties(teamId)
	pp.Printf("Fetched %d minified properties for team %s\n", len(properties), teamId)

	// Build context from all minified properties and collect IDs + URLs
	var contextInfo string
	var propertyIDs []string
	var resolvedScheduleURLs []string
	var resolvedAppURLs []string

	for i := range properties {
		prop := &properties[i]
		propertyIDs = append(propertyIDs, prop.ID)

		// Build property context using SMS-style minified format
		propCtx := smsproperty.CreatePropertyContextForAI(prop)
		if propCtx != "" {
			contextInfo += propCtx + "\n\n"
		}

		// Collect schedule URLs from minified property data
		if prop.ScheduleURL != "" {
			resolvedScheduleURLs = append(resolvedScheduleURLs, prop.ScheduleURL)
			pp.Printf("Got schedule URL from property %s: %s\n", prop.ID, prop.ScheduleURL)
		} else {
			url := ResolveScheduleURL(teamId, prop.ID)
			resolvedScheduleURLs = append(resolvedScheduleURLs, url)
			pp.Printf("Resolved schedule URL for property %s: %s\n", prop.ID, url)
		}

		// Collect application URLs from minified property data
		if prop.ApplicationURL != "" {
			resolvedAppURLs = append(resolvedAppURLs, prop.ApplicationURL)
			pp.Printf("Got application URL from property %s: %s\n", prop.ID, prop.ApplicationURL)
		} else {
			appURL := ResolveApplicationURL(teamId, prop.ID)
			if appURL != "" {
				resolvedAppURLs = append(resolvedAppURLs, appURL)
				pp.Printf("Resolved application URL for property %s: %s\n", prop.ID, appURL)
			}
		}
	}

	// =========================================================================
	// 2. FETCH COMMAND CENTER (with all fields like SMS)
	// =========================================================================
	cmdCenter, err := GetCharlesCommandCenter(teamId)
	var cmdCenterValid bool = (err == nil && cmdCenter.TeamID != "")
	if err != nil {
		check(err, false)
		pp.Printf("Error fetching command center: %v, using defaults\n", err)
	}

	// =========================================================================
	// 3. FETCH TEAM INFO
	// =========================================================================
	teamInfo, err := GetTeamInfoByTeamId(teamId)
	var teamContactPhone string
	var teamName, teamDescription, teamCity, teamState string
	if err == nil {
		teamContactPhone = teamInfo.PhoneNumber
		teamName = teamInfo.Name
		teamDescription = teamInfo.Description
		teamCity = teamInfo.City
		teamState = teamInfo.State
	}

	// =========================================================================
	// 4. FETCH JAKE TRAINING (SMS-style)
	// =========================================================================
	jakeTraining, _ := FetchJakeTraining(teamId)
	trainingContext := buildCharlesTrainingContext(jakeTraining)

	// =========================================================================
	// 5. EXTRACT COMMAND CENTER FIELDS (all fields like SMS)
	// =========================================================================
	var signingName string = "Charles"
	var sendTourSendStatus bool = true
	var sendApplicationStatus bool = true
	var highlights, questions, personality, name string
	var priorities, keyInfo, applicationNeeds string

	if cmdCenterValid {
		if cmdCenter.Name != "" {
			signingName = cmdCenter.Name
			name = cmdCenter.Name
		}

		sendTourSendStatus = cmdCenter.TourScheduling
		sendApplicationStatus = cmdCenter.ApplicationSending
		highlights = cmdCenter.Highlights
		questions = cmdCenter.Questions
		personality = cmdCenter.Personality
		priorities = cmdCenter.Priorities
		keyInfo = cmdCenter.KeyInfo
		applicationNeeds = cmdCenter.ApplicationNeeds
	}

	// =========================================================================
	// 6. EXTRACT CRITICAL REQUIREMENTS & KEY QUESTIONS (SMS-style)
	// =========================================================================
	allCriticalRequirements := extractAllCriticalRequirementsCharles(priorities, keyInfo)
	keyQuestions := extractKeyQuestionsCharles(questions)

	// =========================================================================
	// 7. ANALYZE COMPLETED ITEMS & UNDISCUSSED HIGHLIGHTS (SMS-style)
	// =========================================================================
	completedItems := analyzeCompletedItemsCharles(chatHistory, allCriticalRequirements, keyQuestions)
	undiscussedHighlights := analyzeUndiscussedHighlightsCharles(chatHistory, highlights)

	// =========================================================================
	// 8. DEFINE STRUCTURED RESPONSE FORMAT
	// =========================================================================
	type AIResponse struct {
		Response      string   `json:"response"`
		IncludePhotos bool     `json:"includePhotos"`
		ScheduleUrls  []string `json:"scheduleUrls"`
	}

	schema, err := jsonschema.GenerateSchemaForType(AIResponse{})
	if err != nil {
		check(err, false)
		return "", "", []string{}, []string{}, fmt.Errorf("schema generation error: %v", err)
	}

	// =========================================================================
	// 9. BUILD SYSTEM MESSAGES (SMS-style ordering with Charles adaptations)
	// =========================================================================
	systemMessages := []openai.ChatCompletionMessage{}

	// --- 9a. BASE SYSTEM PROMPT ---
	phoneNumberInstructions := ""
	if teamContactPhone != "" {
		phoneNumberInstructions = fmt.Sprintf(`

Phone Number Guidelines:
- Primary contact number: %s
- For single family properties: Use contact.phone from the property if available
- For multi-family properties: Each unit may have a contact.phone - use the unit's specific contact if they ask about a specific unit
- Always prioritize the team contact number (%s) as the main point of contact unless they specifically ask about a property's contact information`, teamContactPhone, teamContactPhone)
	}

	baseSystemContent := fmt.Sprintf(`You are %s, an AI real estate leasing agent on a website chat widget. Generate a structured JSON response with:
1. response: Your natural and professional reply using HTML tags. Allowed tags: <p>, <span>, <br>, <strong>, <em>, <ul>, <li>, <a>. URLs may ONLY appear inside <a href="..."> tags — never as raw text.
2. includePhotos: Boolean - set to true ONLY when discussing a specific property and photos would be helpful
3. scheduleUrls: Array of scheduling URLs (empty array if not offering tours)

Rules:
- ONLY discuss properties in the context below - do NOT discuss any other properties
- Only list units that are explicitly in the property context. If a unit type isn't listed, say "that's not available" - do NOT make up units
- CRITICAL: You MUST NOT make up or estimate prices. ONLY use the EXACT prices shown in the property context. If a price isn't listed, say "I don't have that pricing information" - NEVER invent prices
- CRITICAL: You MUST NOT make up unit configurations. ONLY mention units explicitly listed in the property context with their exact bed/bath/rent details
- If asked about a price or unit that isn't in the property context, say "I don't have information about that" - do NOT guess or estimate
- Ask clarifying questions when needed to help them find what they need
- If asked if you're AI, say yes
- Natural and professional in tone, written in HTML format
- Concise and focused - avoid listing too many amenities at once
- Formatted like a chatbot conversation (no email signatures or formal closings)

When providing property details:
1. Focus on accurate information from the database
2. Highlight 2-3 key features and amenities that are most relevant
3. Be transparent about any limitations
4. Include numbers for square footage, pricing, etc. without special formatting
5. Avoid overwhelming with exhaustive amenity lists - be selective and strategic
6. TERMINOLOGY: Always use "property" instead of "apartment" when referring to listings generically. Only use "apartment" if the property name itself contains "Apartment" (e.g., "Crosswinds Apartment Homes"). When unsure, always default to "property"

SCHEDULING LINK RULES:
- Always populate scheduleUrls array with the correct URL when offering or asked about a tour.
- When the user EXPLICITLY asks for a link, tour link, or scheduling link (e.g. "send me the link", "can I get the tour link", "what's the link"), you MUST embed the URL as an <a> tag directly in the response field AND include it in scheduleUrls. Example: <p>Here is the scheduling link: <a href="EXACT_URL" target="_blank">Schedule a Tour</a></p>
- For general tour offers where the user has NOT explicitly asked for the link, you may omit the <a> tag from response text and rely on the scheduleUrls array to display a button below your message.
- NEVER invent or guess URLs — only use the exact URLs provided in the scheduling instructions below.

PHOTO RULES:
- Set includePhotos to true when discussing specific properties or when user asks about photos/pictures
- Set includePhotos to false for greetings, general inquiries, or non-property discussions
- Photos will be fetched automatically from the database - do NOT provide photo URLs yourself
- WHEN USER ASKS FOR PHOTOS/PICTURES: If the user asks 'Do you have pictures of the units?', 'Can I see photos?', 'How does it look?', 'How many units are in the building?', or similar photo/unit requests:
  - Set includePhotos to true
  - ALWAYS direct them to the tour/scheduling link where they can see photos, available units, and availability
  - Example response: 'Yes! You can see photos and all available units on our scheduling page: <a href="URL" target="_blank">View Photos & Schedule</a>'
  - NEVER say 'I don't have pictures' or 'I don't have that information' — instead direct them to the scheduling link
  - Include the scheduling URL in the scheduleUrls array

CRITICAL: You are an AI assistant. NEVER claim to be a real person, human agent, or imply you are a human. If asked, acknowledge you are an automated assistant.

LANGUAGE RULES:
- ALWAYS respond in English by default
- ONLY switch to Spanish if the lead has clearly written multiple messages in Spanish
- When in doubt, respond in English
- Use proper Unicode characters including accented characters when needed for correct spelling
- Support UTF-8 encoding for all languages

SPANISH LANGUAGE GUIDANCE: If you need to use Spanish, use Latin American dialect (specifically Mexican). Use proper Spanish orthography including accented characters (á, é, í, ó, ú, ü, ñ) when required.
%s`, signingName, phoneNumberInstructions)

	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleSystem,
		Content: baseSystemContent,
	})

	// --- 9b. CRITICAL REQUIREMENTS (from priorities + keyInfo) - MOST IMPORTANT ---
	if allCriticalRequirements != "" {
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleSystem,
			Content: fmt.Sprintf(`🔥 CRITICAL REQUIREMENTS - MUST INCLUDE THESE:

%s

IMPORTANT: These are qualification steps. Ask the lead to complete these BEFORE sending any links.

ABSOLUTE MUST-ASK REQUIREMENT FROM COMMAND CENTER PRIORITIES:
You MUST include the specific requirements listed above in your SMS. Only ask for what is explicitly stated in the requirements above.

YOU MUST INCLUDE THIS REQUIREMENT IN YOUR SMS. This is non-negotiable.`, allCriticalRequirements),
		})
	}

	// --- 9c. KEY QUESTIONS FOR QUALIFICATION ---
	if len(keyQuestions) > 0 {
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleSystem,
			Content: fmt.Sprintf(`QUALIFY WITH THESE QUESTIONS (ask naturally during conversation):

%s`, strings.Join(keyQuestions, "\n")),
		})
	}

	// --- 9d. PERSONALITY ---
	if personality != "" {
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleSystem,
			Content: fmt.Sprintf(`YOUR PERSONALITY:

%s`, personality),
		})
	}

	// --- 9e. PROPERTY HIGHLIGHTS & SPECIALS ---
	var highlightsAndSpecials string
	if highlights != "" {
		highlightsAndSpecials = "HIGHLIGHTS:\n" + highlights + "\n"
	}
	if contextInfo != "" && strings.Contains(contextInfo, "Specials:") {
		specialsIdx := strings.Index(contextInfo, "Specials:")
		if specialsIdx >= 0 {
			specialsLine := contextInfo[specialsIdx:]
			specialsEnd := strings.Index(specialsLine, "\n")
			if specialsEnd > 0 {
				highlightsAndSpecials += "\nPROPERTY SPECIALS:\n" + specialsLine[:specialsEnd]
			}
		}
	}

	if highlightsAndSpecials != "" {
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleSystem,
			Content: fmt.Sprintf(`HIGHLIGHTS & SPECIALS - MENTION THESE NATURALLY:

%s

IMPORTANT: Weave these highlights and specials into your responses naturally throughout the conversation. Don't just list them once - mention them when relevant.`, highlightsAndSpecials),
		})
	}

	// --- 9f. APPLICATION REQUIREMENTS ---
	if sendApplicationStatus && applicationNeeds != "" {
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleSystem,
			Content: fmt.Sprintf(`APPLICATION REQUIREMENTS - MUST TELL LEAD ALL OF THESE:

%s

CRITICAL: When the lead asks about applying or is ready to apply, you MUST list EVERY item above. Do not skip any items.`, applicationNeeds),
		})
	}

	// --- 9g. CONDITIONAL SCHEDULING/APPLICATION LOGIC (SMS-style) ---
	var conditionalLogic string
	hasScheduleURLs := len(resolvedScheduleURLs) > 0
	hasAppURLs := len(resolvedAppURLs) > 0

	if sendApplicationStatus && hasAppURLs && sendTourSendStatus && hasScheduleURLs {
		conditionalLogic = "You CAN offer BOTH tour scheduling AND application links when lead is qualified."
	} else if sendApplicationStatus && hasAppURLs && (!sendTourSendStatus || !hasScheduleURLs) {
		conditionalLogic = "Application links CAN be offered when qualified."
		if !sendTourSendStatus {
			conditionalLogic += " Tour scheduling is DISABLED - do NOT offer tours."
		} else if !hasScheduleURLs {
			conditionalLogic += " Tour scheduling is enabled but no tour URLs are available."
		}
	} else if !sendApplicationStatus && sendTourSendStatus && hasScheduleURLs {
		conditionalLogic = "Tour scheduling CAN be offered when qualified. Application sending is DISABLED - do NOT offer application links."
	} else if !sendApplicationStatus && sendTourSendStatus && !hasScheduleURLs {
		conditionalLogic = "Tour scheduling is enabled but NO tour URLs are available. Application sending is DISABLED - do NOT offer application links."
	} else if !sendApplicationStatus && !sendTourSendStatus {
		conditionalLogic = "Application sending is DISABLED. Tour scheduling is DISABLED. Simply help qualify the lead and gather their contact info."
	} else {
		conditionalLogic = "Neither application nor tour scheduling is currently enabled. Simply help qualify the lead."
	}

	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: fmt.Sprintf(`CONDITIONAL RULES:

%s

SCHEDULING URL RULES:
- NEVER include URLs in your response text - they go in the scheduleUrls array only
- When offering to schedule a tour, put the URL in scheduleUrls array, not in response text
- In your response text, just say something like "You can schedule a tour to see it in person" without the actual URL`, conditionalLogic),
	})

	// --- 9h. TRAINING EXAMPLES (SMS-style) ---
	if trainingContext != "" {
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleSystem,
			Content: fmt.Sprintf(`STYLE GUIDE (use for inspiration only - create original responses):

%s`, trainingContext),
		})
	}

	// --- 9i. TEAM & COMMAND CENTER CONTEXT ---
	var teamContextBuilder strings.Builder
	if teamName != "" || teamDescription != "" || teamCity != "" || teamState != "" || teamContactPhone != "" {
		teamContextBuilder.WriteString("TEAM INFORMATION:\n")
		if teamName != "" {
			teamContextBuilder.WriteString(fmt.Sprintf("- Company/Team Name: %s\n", teamName))
		}
		if teamDescription != "" {
			teamContextBuilder.WriteString(fmt.Sprintf("- Description: %s\n", teamDescription))
		}
		if teamCity != "" || teamState != "" {
			teamContextBuilder.WriteString(fmt.Sprintf("- Location: %s, %s\n", teamCity, teamState))
		}
		if teamContactPhone != "" {
			teamContextBuilder.WriteString(fmt.Sprintf("- Phone: %s\n", teamContactPhone))
		}
		teamContextBuilder.WriteString("\n")
	}

	var cmdCenterContextBuilder strings.Builder
	if cmdCenterValid {
		cmdCenterContextBuilder.WriteString("COMMAND CENTER CONTEXT:\n")
		if name != "" {
			cmdCenterContextBuilder.WriteString(fmt.Sprintf("- Name: %s\n", name))
		}
		if personality != "" {
			cmdCenterContextBuilder.WriteString(fmt.Sprintf("- Personality: %s\n", personality))
		}
		if highlights != "" {
			cmdCenterContextBuilder.WriteString(fmt.Sprintf("- Key Highlights: %s\n", highlights))
		}
		if questions != "" {
			cmdCenterContextBuilder.WriteString(fmt.Sprintf("- Qualifying Questions: %s\n", questions))
		}
		if cmdCenter.DefaultMessage != "" {
			cmdCenterContextBuilder.WriteString(fmt.Sprintf("- Default Message: %s\n", cmdCenter.DefaultMessage))
		}
		cmdCenterContextBuilder.WriteString("\n")
	}

	combinedContext := teamContextBuilder.String() + cmdCenterContextBuilder.String()
	if combinedContext != "" {
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role: openai.ChatMessageRoleSystem,
			Content: fmt.Sprintf(`CONTEXT FOR RESPONSES:

%s

Use this information to provide accurate company details, contact information, and context in your responses.`, combinedContext),
		})
	}

	// --- 9j. PROPERTY CONTEXT ---
	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: fmt.Sprintf(`CRITICAL: DO NOT HALLUCINATE PROPERTY DETAILS. Only use information from the property context below. If a detail isn't in the context, don't make it up.

	🚨 CRITICAL ANTI-HALLUCINATION RULES:
	- You MUST NOT make up, estimate, or guess ANY prices
	- You MUST NOT make up unit configurations (bed/bath combinations)
	- ONLY use the EXACT prices and units listed in the context below
	- If a price or unit type isn't explicitly shown below, say "I don't have that information available"
	- NEVER invent pricing - if it's not in the context above, you don't know it
	- NEVER repeat or mention a price that isn't listed above, even to deny having it
	- When asked about a non-existent price, say "I don't have anything at that price point" or "That price isn't available" - WITHOUT repeating the specific price number
	- When listing units, ONLY mention units that are explicitly listed with their exact bed/bath/rent details

	If no exact property match is found, suggest alternatives by:
	1. Comparing pros/cons to their stated requirements
	2. Emphasizing 2-3 unique selling points (avoid long amenity lists) - only those mentioned in context
	3. Highlighting neighborhood benefits - only those mentioned in context
	4. Offering to show additional properties

	When discussing amenities: Be selective and strategic. Only mention the most relevant features based on the conversation context.

	Available property context:
	%s`, contextInfo),
	})

	// --- 9k. SCHEDULING URL INSTRUCTIONS ---
	if sendTourSendStatus {
		var schedulingInstructions string
		if len(resolvedScheduleURLs) > 0 {
			schedulingInstructions = "IMPORTANT: Use these EXACT scheduling URLs (do NOT make up your own URLs):\n"
			for i, url := range resolvedScheduleURLs {
				if i < len(propertyIDs) {
					schedulingInstructions += fmt.Sprintf("- Property %s: %s\n", propertyIDs[i], url)
				} else {
					schedulingInstructions += fmt.Sprintf("- Property %d: %s\n", i+1, url)
				}
			}
			schedulingInstructions += "\nSCHEDULING LINK RULES - FOLLOW EXACTLY:\n"
			schedulingInstructions += "1. Always populate the scheduleUrls array with the correct URL when offering or asked about a tour.\n"
			schedulingInstructions += "2. When the user explicitly asks for a link, a tour link, or a scheduling link, you MUST ALSO embed the URL as an HTML anchor tag directly inside your response field like this: <a href=\"URL_HERE\" target=\"_blank\">Schedule a Tour</a>\n"
			schedulingInstructions += "3. For general tour offers (not explicitly asking for a link), you may omit the <a> tag from the response text and rely on the scheduleUrls array to display a button.\n"
			schedulingInstructions += "4. DO NOT make up or guess URLs — only use the exact URLs listed above.\n"
			schedulingInstructions += "5. Example when user says 'send me the link' or 'can I get the tour link': respond with something like: <p>Here is your scheduling link: <a href=\"EXACT_URL\" target=\"_blank\">Schedule a Tour</a></p> AND include the URL in scheduleUrls.\n"
		} else {
			schedulingInstructions = "CRITICAL: When user asks about scheduling a tour, viewing the property, or wants to schedule:\n- DO NOT say 'yes we can schedule' or offer to schedule\n- DO NOT send any links\n- Say: 'A team member will be reaching out shortly to help schedule a tour.'\n- This will trigger a human-in-the-loop (HITL) alert for the team"
		}

		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: schedulingInstructions,
		})
	} else {
		systemMessages = append(systemMessages, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: "CRITICAL: Tour scheduling is DISABLED. scheduleUrls array MUST always be empty []. You must NEVER offer tours, viewings, or scheduling links. You must NEVER provide URLs. If someone asks about viewing a property or scheduling a tour:\n- DO NOT say 'yes we can schedule' or offer to schedule\n- DO NOT send any links\n- Say: 'A team member will be reaching out shortly to help schedule a tour.'\n- This will trigger a human-in-the-loop (HITL) alert for the team",
		})
	}

	// --- 9k-bis. HARD RULE - NEVER confirm or propose a specific tour day/time in chat ---
	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: `ABSOLUTE RULE - TOUR SCHEDULING & TIME/DATE QUESTIONS:

You CANNOT see the calendar. You CANNOT book, confirm, reserve, or hold a tour time. You do NOT know available tour times, dates, or slots. Only the scheduling link can show that.

If the lead asks about, proposes, or requests ANY specific day, time, or date for touring or viewing the property — OR asks general availability questions like "when can I tour?", "what times are available?", "do you have availability this week?", "can I come tomorrow?", "what days can I see it?":
- NEVER say "yes", "that works", "sounds good", "let me book you", "I'll get you on the calendar", "I'll check", "let me confirm", or anything that implies agreement or confirmation
- NEVER propose or suggest ANY specific time, day, or date ("how about 10AM instead?", "we have morning slots", "try tomorrow afternoon")
- NEVER state or imply availability for any time period
- NEVER say you will follow up with a link later - the scheduling link (if available) is handled separately
- NEVER say "our hours are..." or "we're open from..." — you do NOT have this information
- If tour scheduling is ENABLED: direct them to pick any open slot on the scheduling link and place the URL in the scheduleUrls array. Example: "You can see all available tour times and pick one that works here!"
- If tour scheduling is DISABLED: respond with "An agent will reach out to you shortly to help find a time that works!" and stop. Do NOT propose times, do NOT offer alternatives, do NOT guess hours.

This applies to ALL of these types of questions:
- "Does 9AM tomorrow work?"
- "Can I come at 5PM?"
- "Do you have a 4PM slot?"
- "Is Friday at 3 open?"
- "Book me for Saturday morning"
- "When can I tour?"
- "What times are available?"
- "What days do you do tours?"
- "Can I come by this weekend?"
- "Do you have anything available tomorrow?"
- "What time can I schedule a viewing?"
- "Are you available Monday?"
- "When is the next available tour?"

This rule OVERRIDES any conflicting instructions above. Violating it creates double-bookings and sends people to closed offices.`,
	})

	// ── HARD OVERRIDE: location phrasing ─────────────────────────────────────
	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: `ABSOLUTE OVERRIDE — LOCATION PHRASING:
When mentioning where a property is located, ALWAYS say "it's located at [address]" or "the property is located at [address]".
NEVER say "we are located at", "we're located at", or any first-person "we" phrasing for the property's location.
This rule overrides all other instructions, examples, and training data.`,
	})

	// ── HARD OVERRIDE: hours ─────────────────────────────────────────────
	var officeHoursFallbackStreaming string
	if sendTourSendStatus && len(resolvedScheduleURLs) > 0 {
		officeHoursFallbackStreaming = "Tour scheduling is ON. Direct them to the scheduling page where they can see real-time availability. Place the URL in the scheduleUrls array."
	} else {
		officeHoursFallbackStreaming = `Tour scheduling is OFF or no link is available. Say: "Our hours may vary — a team member will reach out shortly to help you out!" Do NOT offer a scheduling link.`
	}
	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleSystem,
		Content: fmt.Sprintf(`ABSOLUTE RULE — OFFICE HOURS:
You do NOT know the office hours, business hours, or leasing hours. NEVER make up, guess, or state specific hours of operation.
If the user asks about hours:
- %s
NEVER invent hours like "9am to 5pm" or "Monday through Friday" — this information is not available to you.`, officeHoursFallbackStreaming),
	})

	// --- 9l. USER MESSAGE with completed items and undiscussed highlights ---
	userMessage := fmt.Sprintf(`Recent conversation (most recent at bottom):
%s

Latest message: %s

IMPORTANT: Focus on the LATEST message. Previous messages are context.
Completed/Answered: %s
Highlights NOT yet discussed: %s

Generate a direct response.`, chatHistory, inquiry, completedItems, undiscussedHighlights)

	systemMessages = append(systemMessages, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: userMessage,
	})

	// =========================================================================
	// 10. CREATE STREAMING REQUEST
	// =========================================================================
	stream, err := client.CreateChatCompletionStream(
		context.Background(),
		openai.ChatCompletionRequest{
			Model:               "gpt-4o-mini-2024-07-18",
			Messages:            systemMessages,
			MaxCompletionTokens: 350,
			Temperature:         0.5,
			Stream:              true,
			ResponseFormat: &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
				JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
					Name:   "ai_response",
					Schema: schema,
					Strict: true,
				},
			},
		},
	)

	if err != nil {
		check(err, false)
		return "", "", []string{}, []string{}, err
	}
	defer stream.Close()

	// =========================================================================
	// 11. COLLECT STREAMED JSON CHUNKS
	// =========================================================================
	var fullContent strings.Builder

	for {
		response, err := stream.Recv()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			check(err, false)
			return "", "", []string{}, []string{}, fmt.Errorf("stream error: %v", err)
		}

		if len(response.Choices) > 0 {
			content := response.Choices[0].Delta.Content
			if content != "" {
				fullContent.WriteString(content)
			}
		}
	}

	content := fullContent.String()

	// =========================================================================
	// 12. PARSE AND STREAM RESPONSE
	// =========================================================================

	// Validate that we have complete JSON
	if !isValidJSON(content) {
		pp.Printf("Invalid JSON detected in streaming response, falling back to plain text\n")
		// Convert to runes to properly handle UTF-8 characters
		runes := []rune(content)
		for i := 0; i < len(runes); i++ {
			chunkChan <- string(runes[i])
			time.Sleep(10 * time.Millisecond)
		}
		return content, contextInfo, []string{}, []string{}, nil
	}

	// Parse the structured response
	var aiResponse AIResponse
	err = json.Unmarshal([]byte(content), &aiResponse)
	if err != nil {
		check(err, false)
		pp.Printf("JSON parsing failed in streaming: %v\n", err)
		// Convert to runes to properly handle UTF-8 characters
		runes := []rune(content)
		for i := 0; i < len(runes); i++ {
			chunkChan <- string(runes[i])
			time.Sleep(10 * time.Millisecond)
		}
		return content, contextInfo, []string{}, []string{}, nil
	}

	// Stream the actual response content in chunks for smooth typing effect
	responseText := aiResponse.Response

	// CRITICAL: Replace bracket-style placeholder links in the response BEFORE
	// streaming chunks to the client. Otherwise the user sees literal
	// "[tour scheduling link]" text appear in the chat bubble as the stream
	// progresses — this was the bug reported for the Charles chatbot path.
	// We rely on the `resolvedScheduleURLs` / `resolvedAppURLs` arrays that
	// were computed earlier from the matched properties (DB lookup, so no
	// hallucination risk). Picking the first resolved URL is acceptable:
	// `findMatchingScheduleURL`-style multi-property matching is best-effort
	// and the common failure mode is a single property anyway.
	var preStreamScheduleURL, preStreamApplicationURL string
	if len(aiResponse.ScheduleUrls) > 0 {
		preStreamScheduleURL = aiResponse.ScheduleUrls[0]
	} else if len(resolvedScheduleURLs) > 0 {
		preStreamScheduleURL = resolvedScheduleURLs[0]
	}
	if len(resolvedAppURLs) > 0 {
		preStreamApplicationURL = resolvedAppURLs[0]
	}
	responseText = ReplaceLinkPlaceholders(responseText, preStreamScheduleURL, preStreamApplicationURL, true)

	chunkSize := 5

	// Convert to runes to properly handle UTF-8 characters
	runes := []rune(responseText)
	for i := 0; i < len(runes); i += chunkSize {
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunk := string(runes[i:end])
		chunkChan <- chunk
		time.Sleep(10 * time.Millisecond)
	}

	// =========================================================================
	// 13. POST-PROCESS: PHOTOS, URLS, SANITIZATION
	// =========================================================================

	// Fetch photos programmatically from properties if AI indicates photos should be included
	var photos []string
	if aiResponse.IncludePhotos && len(propertyIDs) > 0 {
		photos = GetPhotosForPropertyIDs(teamId, propertyIDs, 2)
		pp.Printf("Fetched %d photos for properties (streaming): %v\n", len(photos), propertyIDs)
	}

	// Get schedule URLs and validate them against property context
	scheduleUrls := aiResponse.ScheduleUrls
	if scheduleUrls == nil {
		scheduleUrls = []string{}
	}

	// Validate URLs to prevent hallucinations
	if len(scheduleUrls) > 0 {
		validatedURLs := validateScheduleURLs(scheduleUrls, contextInfo, propertyIDs)
		if len(validatedURLs) != len(scheduleUrls) {
			pp.Printf("URL validation (streaming): %d original URLs, %d validated URLs\n", len(scheduleUrls), len(validatedURLs))
		}
		scheduleUrls = validatedURLs
	}

	// SAFETY NET: fall back to the FIRST pre-resolved URL if validation dropped them all.
	// Only use one URL to avoid showing multiple tour buttons on the frontend.
	if len(scheduleUrls) == 0 && len(resolvedScheduleURLs) > 0 {
		pp.Printf("\x1b[33mvalidateScheduleURLs (streaming) returned empty \u2014 falling back to first of %d resolved URL(s)\x1b[0m\n", len(resolvedScheduleURLs))
		scheduleUrls = append(scheduleUrls, resolvedScheduleURLs[0])
	}

	// Sanitize the response to ensure valid UTF-8 and remove problematic characters.
	// Placeholder replacement already happened pre-streaming above, so we just
	// need to match what was streamed by applying the same transformations in
	// the same order to the final sanitized copy returned to the caller.
	sanitizedResponse := CleanText(responseText)

	// POST-PROCESS: Enforce location phrasing rule as safety net
	sanitizedResponse = EnforceLocationPhrasing(sanitizedResponse)

	pp.Printf("Streaming generation complete: response=%d chars, photos=%d, scheduleUrls=%d\n",
		len(sanitizedResponse), len(photos), len(scheduleUrls))

	// Check if this is a tour scheduling request and tours are disabled
	// If so, trigger HITL alert
	if !sendTourSendStatus && isTourSchedulingRequest(inquiry) {
		// Send HITL alert for tour scheduling request when tours are off
		go func() {
			title := "Tour Scheduling Request - Human Intervention Needed"
			description := fmt.Sprintf("Lead asked about scheduling a tour but tours are disabled. Session: %s", sessionId)
			err := HandleCharlesNotification(teamId, sessionId, title, description)
			if err != nil {
				pp.Printf("\x1b[33mFailed to send HITL alert for tour scheduling request: %v\x1b[0m\n", err)
			} else {
				pp.Printf("\x1b[32mSent HITL alert for tour scheduling request (tours disabled)\x1b[0m\n")
			}
		}()
	}

	// Return response, context, photos, and schedule URLs separately
	return sanitizedResponse, contextInfo, photos, scheduleUrls, nil
}

// extractAllCriticalRequirementsCharles extracts critical requirements from priorities and keyInfo
func extractAllCriticalRequirementsCharles(priorities, keyInfo string) string {
	if priorities == "" && keyInfo == "" {
		return ""
	}

	combined := ""
	if priorities != "" {
		combined += "PRIORITIES:\n" + priorities + "\n\n"
	}
	if keyInfo != "" {
		combined += "KEY INFO:\n" + keyInfo + "\n\n"
	}

	requirementPhrases := []string{
		"2.5x", "2.5 times", "minimum monthly income", "income approval",
		"base rent", "income requirement", "financial requirement",
		"income", "rent", "financial",
		"pay stub", "paystub", "check stub", "3 most recent",
		"documentation", "verify income", "proof of income",
		"stubs", "pay", "document",
		"send to", "email to", "submit to", "need to send", "must send",
		"require", "need", "must", "should", "will need",
		"send", "email", "submit",
		"first step", "application process", "pre-qualify", "qualify",
		"income verification", "approval process", "application",
		"step", "process", "approval", "verification",
	}

	questionWords := []string{"What", "When", "Where", "Who", "Why", "How", "Do you", "Are you"}

	var importantLines []string
	lines := strings.Split(combined, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		isQuestion := false
		for _, qWord := range questionWords {
			if strings.HasPrefix(line, qWord) {
				isQuestion = true
				break
			}
		}
		if isQuestion {
			continue
		}

		lowerLine := strings.ToLower(line)
		for _, phrase := range requirementPhrases {
			if strings.Contains(lowerLine, phrase) {
				importantLines = append(importantLines, line)
				break
			}
		}
	}

	if len(importantLines) > 0 {
		return strings.Join(importantLines, "\n")
	}

	return combined
}

// extractKeyQuestionsCharles extracts key questions from the questions field
func extractKeyQuestionsCharles(questions string) []string {
	if questions == "" {
		return nil
	}

	var questionList []string

	lines := strings.Split(questions, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "• ")
		for i := 1; i <= 9; i++ {
			line = strings.TrimPrefix(line, fmt.Sprintf("%d.", i))
			line = strings.TrimPrefix(line, fmt.Sprintf("%d)", i))
		}
		line = strings.TrimSpace(line)

		if line != "" {
			questionList = append(questionList, line)
		}
	}

	if len(questionList) == 0 {
		parts := strings.Split(questions, ";")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				questionList = append(questionList, part)
			}
		}
	}

	if len(questionList) > 5 {
		questionList = questionList[:5]
	}

	return questionList
}

// analyzeCompletedItemsCharles analyzes chat history to find what has been completed/answered
func analyzeCompletedItemsCharles(chatHistory, requirements string, questions []string) string {
	if chatHistory == "" {
		return "Nothing yet - this is the start of conversation"
	}

	lowerChat := strings.ToLower(chatHistory)
	var completed []string

	completionIndicators := map[string]string{
		"sent":        "documents/pay stubs",
		"email":       "documents/pay stubs",
		"attached":    "documents/pay stubs",
		"schedule":    "tour scheduled",
		"scheduled":   "tour scheduled",
		"booked":      "tour scheduled",
		"application": "application started",
		"applied":     "application completed",
		"submitted":   "application completed",
		"yes":         "confirmed interest",
		"interested":  "confirmed interest",
		"great":       "confirmed interest",
		"perfect":     "confirmed interest",
		"ready":       "ready to proceed",
	}

	for phrase, item := range completionIndicators {
		if strings.Contains(lowerChat, phrase) {
			completed = append(completed, item)
		}
	}

	if len(completed) == 0 {
		return "Nothing specific identified yet"
	}

	seen := make(map[string]bool)
	var unique []string
	for _, c := range completed {
		if !seen[c] {
			seen[c] = true
			unique = append(unique, c)
		}
	}

	return strings.Join(unique, ", ")
}

// analyzeUndiscussedHighlightsCharles finds highlights that haven't been mentioned yet
func analyzeUndiscussedHighlightsCharles(chatHistory, highlights string) string {
	if highlights == "" {
		return "No highlights configured"
	}

	if chatHistory == "" {
		return highlights
	}

	lowerChat := strings.ToLower(chatHistory)
	var undiscussed []string

	highlightLines := strings.Split(highlights, "\n")
	for _, line := range highlightLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lowerLine := strings.ToLower(line)
		words := strings.Fields(lowerLine)
		discussed := false
		for _, word := range words {
			if len(word) > 4 && strings.Contains(lowerChat, word) {
				discussed = true
				break
			}
		}
		if !discussed {
			undiscussed = append(undiscussed, line)
		}
	}

	if len(undiscussed) == 0 {
		return "All highlights have been discussed"
	}

	return strings.Join(undiscussed, "\n")
}

// buildCharlesTrainingContext builds a training context string from Jake training data for Charles
func buildCharlesTrainingContext(training *types.JakeTraining) string {
	if training == nil || len(training.JakeSMS.Files) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Response Style Examples - Match this tone and style:\n\n")

	maxExamples := 5
	if len(training.JakeSMS.Files) < 5 {
		maxExamples = len(training.JakeSMS.Files)
	}

	for i := 0; i < maxExamples; i++ {
		file := training.JakeSMS.Files[i]
		if file.Content != "" {
			content := file.Content
			if len(content) > 500 {
				content = content[:500] + "..."
			}
			fmt.Fprintf(&sb, "Example %d (%s):\n%s\n\n", i+1, file.Name, content)
		}
	}

	if training.CommonInquiries != "" {
		commonInq := training.CommonInquiries
		if len(commonInq) > 300 {
			commonInq = commonInq[:300] + "..."
		}
		fmt.Fprintf(&sb, "Common Inquiries to handle:\n%s\n", commonInq)
	}

	return sb.String()
}

// GetClientIP extracts the client IP from the request
func GetClientIP(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip == "" {
		ip = r.Header.Get("X-Real-IP")
	}
	if ip == "" {
		ip = r.RemoteAddr
	}
	return ip
}

// Removes non-printable ASCII characters from a string
func cleanString(s string) string {
	// First trim regular whitespace
	s = strings.TrimSpace(s)

	// Remove all non-printable ASCII characters (codes < 32 or > 126)
	var result []rune
	for _, r := range s {
		if r >= 32 && r <= 126 {
			result = append(result, r)
		}
	}

	return string(result)
}

func GetUserEmailConfigurations() ([]types.EmailConfiguration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	check(err, false)

	defer client.Disconnect(ctx)

	dbName := os.Getenv("USERS_COLLECTION")
	collName := os.Getenv("EMAIL_CONFIGURATION_COLLECTION")

	if dbName == "" || collName == "" {
		return nil, fmt.Errorf("database or collection name not set in environment")
	}

	collection := client.Database(dbName).Collection(collName)

	filter := bson.M{
		"hasAutoRespond": bson.M{"$exists": true},
		"scan":           bson.M{"$exists": true},
	}

	cursor, err := collection.Find(ctx, filter)
	check(err, false)

	defer cursor.Close(ctx)

	var results []bson.M
	err = cursor.All(ctx, &results)
	check(err, false)

	if len(results) == 0 {
		return nil, fmt.Errorf("no records found for user")
	}

	configs := make([]types.EmailConfiguration, 0)

	for _, result := range results {
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
			if key == "_id" || key == "userId" || key == "hasAutoRespond" || key == "teamId" || key == "tourTimeInterval" || key == "companyGiven" || key == "createdAt" || key == "updatedAt" || key == "scan" || key == "configId" || key == "name" || key == "isDefault" {
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
								var err error
								decryptedValue, err = security.Decrypt(encryptedValue)
								check(err, false)
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
							// Clean password of control characters and trailing whitespace
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
								var err error
								decryptedValue, err = security.Decrypt(encryptedValue)
								check(err, false)
							}
						}
						switch settingKey {
						case "host":
							imapConfig.Host = cleanString(decryptedValue)
						case "port":
							imapConfig.Port = strings.TrimSpace(decryptedValue)
						case "username":
							// Clean username of control characters and trailing whitespace
							imapConfig.Username = cleanString(decryptedValue)
						case "password":
							// Clean password of control characters and trailing whitespace
							imapConfig.Password = cleanString(decryptedValue)
						}
					}
					config.IMAP = imapConfig
				}
			}
		}

		configs = append(configs, config)
	}

	return configs, nil
}

// formatValue recursively formats any JSON value type
func formatValue(value interface{}, indent int) string {
	indentStr := strings.Repeat("  ", indent)

	switch v := value.(type) {
	case nil:
		return "null"
	case bool:
		return fmt.Sprintf("%t", v)
	case float64:
		// Check if it's actually an integer
		if v == float64(int64(v)) {
			return fmt.Sprintf("%.0f", v)
		}
		return fmt.Sprintf("%g", v)
	case string:
		return fmt.Sprintf("%q", v)
	case []interface{}:
		if len(v) == 0 {
			return "[]"
		}

		var result strings.Builder
		result.WriteString("[\n")
		for i, item := range v {
			result.WriteString(indentStr + "  ")
			result.WriteString(formatValue(item, indent+1))
			if i < len(v)-1 {
				result.WriteString(",")
			}
			result.WriteString("\n")
		}
		result.WriteString(indentStr + "]")
		return result.String()
	case map[string]interface{}:
		if len(v) == 0 {
			return "{}"
		}

		var result strings.Builder
		result.WriteString("{\n")
		i := 0
		for key, val := range v {
			result.WriteString(indentStr + "  ")
			result.WriteString(fmt.Sprintf("%q: ", key))
			result.WriteString(formatValue(val, indent+1))
			if i < len(v)-1 {
				result.WriteString(",")
			}
			result.WriteString("\n")
			i++
		}
		result.WriteString(indentStr + "}")
		return result.String()
	default:
		return fmt.Sprintf("%v (type: %s)", v, reflect.TypeOf(v))
	}
}

// JSONToText converts JSON data to a human-readable text format
func JSONToText(jsonData []byte, indent int) (string, error) {
	var data interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return "", fmt.Errorf("invalid JSON: %w", err)
	}

	return formatValue(data, indent), nil
}

func GetMessagesBetweenPhoneNumbers(from, to string) ([]bson.M, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		check(err, false)
		return nil, fmt.Errorf("failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	// Validate numbers and their formats
	// Use standardized E.164 format for phone numbers
	fromNumber := from
	if !strings.HasPrefix(from, "+") {
		fromNumber = "+" + strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, from)
	}

	toNumber := to
	if !strings.HasPrefix(to, "+") {
		toNumber = "+1" + strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, to)
	}

	// Query the database for messages between these numbers
	collection := client.Database("sms").Collection("messages")

	filter := bson.M{
		"$or": []bson.M{
			{"from": fromNumber, "to": toNumber},
			{"from": toNumber, "to": fromNumber},
		},
	}

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		check(err, false)
		return nil, fmt.Errorf("error querying messages: %v", err)
	}
	defer cursor.Close(ctx)

	var messages []bson.M
	err = cursor.All(ctx, &messages)
	if err != nil {
		check(err, false)
		return nil, fmt.Errorf("error retrieving messages: %v", err)
	}

	// Sort by timestamp from oldest to youngest
	sort.Slice(messages, func(i, j int) bool {
		var timeI, timeJ time.Time

		if timestamp, ok := messages[i]["timestamp"].(primitive.DateTime); ok {
			timeI = timestamp.Time()
		} else if timestampStr, ok := messages[i]["timestamp"].(string); ok {
			parsedTime, err := time.Parse(time.RFC3339, timestampStr)
			if err == nil {
				timeI = parsedTime
			} else {
				check(err, false)
			}
		}

		if timestamp, ok := messages[j]["timestamp"].(primitive.DateTime); ok {
			timeJ = timestamp.Time()
		} else if timestampStr, ok := messages[j]["timestamp"].(string); ok {
			parsedTime, err := time.Parse(time.RFC3339, timestampStr)
			if err == nil {
				timeJ = parsedTime
			} else {
				check(err, false)
			}
		}

		return timeI.Before(timeJ)
	})

	return messages, nil
}

func CheckEmailInDatabase(email string, teamId string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		check(err, false)
		return false, err
	}
	defer client.Disconnect(ctx)

	collection := client.Database("teams").Collection("leads")

	filter := bson.M{
		"teamId": teamId,
		"email":  email,
	}

	var result bson.M
	err = collection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return false, nil
		}
		check(err, false)
		return false, err
	}

	return true, nil
}

func CreateLead(lead *types.Lead, teamId string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		check(err, false)
		return fmt.Errorf("failed to connect to MongoDB: %v", err)
	}

	defer client.Disconnect(ctx)

	// Ensure the lead has the correct team ID
	lead.TeamID = teamId

	collection := client.Database("teams").Collection("leads")
	_, err = collection.InsertOne(ctx, lead)
	if err != nil {
		check(err, false)
		return fmt.Errorf("failed to insert lead: %v", err)
	}

	return nil
}

func GenerateStructuredLead(email, emailThread string, teamId string) (*types.Lead, error) {
	ctx := context.Background()
	client := openai.NewClient(os.Getenv("OPENAI_API_KEY"))

	// Define the expected lead structure for schema generation
	type LeadInfo struct {
		Name     string   `json:"name"`
		Email    string   `json:"email"`
		Phone    string   `json:"phone"`
		Comments string   `json:"comments"`
		Tags     []string `json:"tags"`
	}

	// Generate the JSON schema for Lead struct
	schema, err := jsonschema.GenerateSchemaForType(LeadInfo{})
	if err != nil {
		check(err, false)
		return nil, fmt.Errorf("schema generation error: %v", err)
	}

	resp, err := client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: "gpt-4o-mini-2024-07-18",
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: `Extract structured lead information from the email thread. Return JSON with complete information about the lead.`,
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: emailThread,
				},
			},
			Temperature:         0.2,
			MaxCompletionTokens: 250,
			ResponseFormat: &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
				JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
					Name:   "lead_extraction",
					Schema: schema,
					Strict: true,
				},
			},
		},
	)

	if err != nil {
		check(err, false)
		return nil, fmt.Errorf("error calling OpenAI API: %v", err)
	}

	// Parse the structured response
	var leadInfo LeadInfo
	err = json.Unmarshal([]byte(resp.Choices[0].Message.Content), &leadInfo)
	if err != nil {
		// If JSON parsing fails, create with default values
		check(err, false)
		return &types.Lead{
			ID:         uuid.New().String(),
			TeamID:     teamId,
			Email:      email, // Use the email parameter as fallback
			LeadSource: "Charles",
			Status:     "Interested",
			Comments:   []string{},
			Tags:       []string{"Auto-Generated"},
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
			Industry:   "Other",
			LeadOwner: struct {
				ID    string `bson:"id"`
				Email string `bson:"email"`
				Name  string `bson:"name"`
			}{
				ID:    "",
				Email: "",
				Name:  "",
			},
		}, nil
	}

	// Extract email from leadInfo or use default
	extractedEmail := leadInfo.Email
	if extractedEmail == "" {
		// Use the email parameter as default
		extractedEmail = email
	}

	// Extract tags
	tags := []string{"Auto-Generated"}
	if len(leadInfo.Tags) > 0 {
		for _, tag := range leadInfo.Tags {
			if tag != "" {
				tags = append(tags, tag)
			}
		}
	}

	// Extract comments
	comments := []string{}
	if leadInfo.Comments != "" {
		comments = append(comments, leadInfo.Comments)
	}

	// Get team members and assign lead owner
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	mongoClient, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		check(err, false)
	}

	var leadOwner struct {
		ID    string `bson:"id"`
		Email string `bson:"email"`
		Name  string `bson:"name"`
	}

	if err == nil {
		defer mongoClient.Disconnect(ctx)
		teamsCollection := mongoClient.Database("teams").Collection("teams")

		var team bson.M
		err = teamsCollection.FindOne(ctx, bson.M{"teamId": teamId}).Decode(&team)
		if err != nil {
			check(err, false)
		}

		if err == nil && team["members"] != nil {
			if members, ok := team["members"].(primitive.A); ok {
				var assignedMember primitive.M

				// Try to assign to regular member first
				for _, m := range members {
					if member, ok := m.(primitive.M); ok {
						if role, ok := member["role"].(string); ok && role == "member" {
							assignedMember = member
							break
						}
					}
				}

				// If no member found, try to assign to admin
				if assignedMember == nil {
					for _, m := range members {
						if member, ok := m.(primitive.M); ok {
							if role, ok := member["role"].(string); ok && role == "admin" {
								assignedMember = member
								break
							}
						}
					}
				}

				// If no admin found, assign to owner
				if assignedMember == nil {
					for _, m := range members {
						if member, ok := m.(primitive.M); ok {
							if role, ok := member["role"].(string); ok && role == "owner" {
								assignedMember = member
								break
							}
						}
					}
				}

				// Extract member info
				if assignedMember != nil {
					if id, ok := assignedMember["userId"].(string); ok {
						leadOwner.ID = id
					}
					if email, ok := assignedMember["email"].(string); ok {
						leadOwner.Email = email
					}
					if name, ok := assignedMember["name"].(string); ok {
						leadOwner.Name = name
					}
				}
			}
		}
	}

	// Create the lead with extracted information and assigned owner
	lead := &types.Lead{
		ID:         uuid.New().String(),
		TeamID:     teamId,
		Email:      extractedEmail,
		FirstName:  leadInfo.Name,
		LastName:   "",
		Phone:      leadInfo.Phone,
		LeadSource: "Charles",
		Status:     "Interested",
		Comments:   comments,
		Tags:       tags,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Industry:   "Other",
		LeadOwner:  leadOwner,
	}

	return lead, nil
}

func getStringValue(data bson.M, key string) string {
	if value, ok := data[key].(string); ok {
		return value
	}
	return ""
}

func getBoolValue(data bson.M, key string) bool {
	if value, ok := data[key].(bool); ok {
		return value
	}
	return false
}

func getTimeValue(data bson.M, key string) time.Time {
	if value, ok := data[key].(primitive.DateTime); ok {
		return value.Time()
	}
	if value, ok := data[key].(time.Time); ok {
		return value
	}
	return time.Time{}
}

func GetTeamInfoByClientId(clientId string) (TeamInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		check(err, false)
		return TeamInfo{}, fmt.Errorf("failed to connect to MongoDB: %v", err)
	}

	defer client.Disconnect(ctx)

	dbName := os.Getenv("MONGODB_DATABASE")
	collName := os.Getenv("MONGODB_DATABASE")
	if dbName == "" || collName == "" {
		return TeamInfo{}, fmt.Errorf("database or collection name not set in environment")
	}

	collection := client.Database(dbName).Collection(collName)

	filter := bson.M{
		"clientId": clientId,
	}

	// Only return public fields
	projection := bson.M{
		"name":        1,
		"description": 1,
		"city":        1,
		"state":       1,
		"logoUrl":     1,
		"teamId":      1,
		"phoneNumber": 1,
	}

	var team bson.M
	err = collection.FindOne(ctx, filter, options.FindOne().SetProjection(projection)).Decode(&team)
	if err != nil {
		check(err, false)
		if err == mongo.ErrNoDocuments {
			return TeamInfo{}, fmt.Errorf("no team found for client ID %s", clientId)
		}
		return TeamInfo{}, err
	}

	teamID, ok := team["teamId"].(string)
	if !ok {
		return TeamInfo{}, fmt.Errorf("invalid team ID format")
	}

	// phoneNumber may be empty, ensure we return a string to prevent errors
	phoneNumber := ""
	if pn := getStringValue(team, "phoneNumber"); pn != "" {
		phoneNumber = pn
	}

	teamInfo := TeamInfo{
		Name:        getStringValue(team, "name"),
		Description: getStringValue(team, "description"),
		City:        getStringValue(team, "city"),
		State:       getStringValue(team, "state"),
		LogoURL:     getStringValue(team, "logoUrl"),
		TeamID:      teamID,
		PhoneNumber: phoneNumber,
	}

	return teamInfo, nil
}

func GetTeamInfoByTeamId(teamId string) (TeamInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		check(err, false)
		return TeamInfo{}, fmt.Errorf("failed to connect to MongoDB: %v", err)
	}

	defer client.Disconnect(ctx)

	dbName := os.Getenv("MONGODB_DATABASE")
	collName := os.Getenv("MONGODB_DATABASE")
	if dbName == "" || collName == "" {
		return TeamInfo{}, fmt.Errorf("database or collection name not set in environment")
	}

	collection := client.Database(dbName).Collection(collName)

	filter := bson.M{
		"teamId": teamId,
	}

	// Only return public fields
	projection := bson.M{
		"name":        1,
		"description": 1,
		"city":        1,
		"state":       1,
		"logoUrl":     1,
		"teamId":      1,
		"phoneNumber": 1,
		"clientId":    1,
	}

	var team bson.M
	err = collection.FindOne(ctx, filter, options.FindOne().SetProjection(projection)).Decode(&team)
	if err != nil {
		check(err, false)
		if err == mongo.ErrNoDocuments {
			return TeamInfo{}, fmt.Errorf("no team found for team ID %s", teamId)
		}
		return TeamInfo{}, err
	}

	teamID, ok := team["teamId"].(string)
	if !ok {
		return TeamInfo{}, fmt.Errorf("invalid team ID format")
	}

	// phoneNumber may be empty, ensure we return a string to prevent errors
	phoneNumber := ""
	if pn := getStringValue(team, "phoneNumber"); pn != "" {
		phoneNumber = pn
	}

	teamInfo := TeamInfo{
		Name:        getStringValue(team, "name"),
		Description: getStringValue(team, "description"),
		City:        getStringValue(team, "city"),
		State:       getStringValue(team, "state"),
		LogoURL:     getStringValue(team, "logoUrl"),
		TeamID:      teamID,
		PhoneNumber: phoneNumber,
	}

	return teamInfo, nil
}

func GetCharlesCommandCenter(teamId string) (CharlesCommandCenter, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		check(err, false)
		return CharlesCommandCenter{}, fmt.Errorf("failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	collection := client.Database("teams").Collection("charles-cmd-centers")
	filter := bson.M{"teamId": teamId}
	projection := bson.M{"_id": 0}

	var team bson.M
	err = collection.FindOne(ctx, filter, options.FindOne().SetProjection(projection)).Decode(&team)
	if err != nil {
		check(err, false)
		if err == mongo.ErrNoDocuments {
			return CharlesCommandCenter{}, fmt.Errorf("no team found for team ID %s", teamId)
		}
		return CharlesCommandCenter{}, err
	}

	teamID, ok := team["teamId"].(string)
	if !ok {
		return CharlesCommandCenter{}, fmt.Errorf("invalid team ID format")
	}

	// Extract showcase properties
	var showCaseProperties []ShowCaseProperty
	if showCaseData, ok := team["showCaseProperty"].(primitive.A); ok {
		for _, item := range showCaseData {
			if property, ok := item.(primitive.M); ok {
				// Extract photos array
				var photos []string
				if photosData, ok := property["photos"].(primitive.A); ok {
					for _, photo := range photosData {
						if photoStr, ok := photo.(string); ok {
							photos = append(photos, photoStr)
						}
					}
				}

				showCaseProperty := ShowCaseProperty{
					ID:                getStringValue(property, "id"),
					Location:          getStringValue(property, "location"),
					Title:             getStringValue(property, "title"),
					Description:       getStringValue(property, "description"),
					Prompt:            getStringValue(property, "prompt"),
					Photos:            photos,
					CustomScheduleUrl: getStringValue(property, "customScheduleUrl"),
				}
				showCaseProperties = append(showCaseProperties, showCaseProperty)
			}
		}
	}

	teamInfo := CharlesCommandCenter{
		Questions:          getStringValue(team, "questions"),
		Priorities:         getStringValue(team, "priorities"),
		Personality:        getStringValue(team, "personality"),
		Name:               getStringValue(team, "name"),
		KeyInfo:            getStringValue(team, "keyInfo"),
		Highlights:         getStringValue(team, "highlights"),
		ApplicationNeeds:   getStringValue(team, "applicationNeeds"),
		TeamID:             teamID,
		ApplicationSending: getBoolValue(team, "applicationSending"),
		TourScheduling:     getBoolValue(team, "tourScheduling"),
		Domain:             getStringValue(team, "domain"),
		UserID:             getStringValue(team, "userId"),
		CreatedAt:          getTimeValue(team, "createdAt"),
		UpdatedAt:          getTimeValue(team, "updatedAt"),
		Color:              getStringValue(team, "color"),
		DefaultMessage:     getStringValue(team, "defaultMessage"),
		Align:              getStringValue(team, "align"),
		CustomLink:         getStringValue(team, "customLink"),
		ShowCaseProperty:   showCaseProperties,
	}

	return teamInfo, nil
}

func SaveMessage(message string, clientId string, timestamp time.Time, page string, sessionId string, teamId string, ip string, direction string, propertyContext string, photos []string, scheduleUrls []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		check(err, false)
		return fmt.Errorf("failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	collection := client.Database("teams").Collection("charles-chats")

	// Check if this is first message for session
	filter := bson.M{"sessionId": sessionId}
	count, err := collection.CountDocuments(ctx, filter)
	if err != nil {
		check(err, false)
		return fmt.Errorf("failed to check message count: %v", err)
	}

	if count == 0 {
		// Insert welcome message first
		welcomeId := "bamboo-" + GenerateRandomString(8)
		welcomeDoc := bson.M{
			"id":              welcomeId,
			"clientId":        clientId,
			"message":         "Welcome! How can I help you today?",
			"timestamp":       timestamp.Add(-1 * time.Second), // 1 second before user message
			"page":            page,
			"sessionId":       sessionId,
			"teamId":          teamId,
			"ip":              ip,
			"direction":       "outgoing",
			"propertyContext": propertyContext,
			"images":          []string{},
		}
		_, err = collection.InsertOne(ctx, welcomeDoc)
		if err != nil {
			check(err, false)
			return fmt.Errorf("failed to insert welcome message: %v", err)
		}

	}

	// Generate unique ID with "bamboo-" prefix + 8 random chars
	randomChars := GenerateRandomString(8)
	id := "bamboo-" + randomChars

	doc := bson.M{
		"id":              id,
		"clientId":        clientId,
		"message":         message,
		"timestamp":       timestamp,
		"page":            page,
		"sessionId":       sessionId,
		"teamId":          teamId,
		"ip":              ip,
		"direction":       direction,
		"propertyContext": propertyContext,
		"photos":          photos,
		"scheduleUrls":    scheduleUrls,
	}

	_, err = collection.InsertOne(ctx, doc)
	if err != nil {
		check(err, false)
		return fmt.Errorf("failed to insert message: %v", err)
	}

	return nil
}

// truncateMessage ensures a message doesn't exceed maxMessageLength characters
func truncateMessage(msg string) string {
	if utf8.RuneCountInString(msg) <= maxMessageLength {
		return msg
	}
	runes := []rune(msg)
	return string(runes[:maxMessageLength]) + "..."
}

// optimizeChatHistory takes full chat history and returns an optimized version
func optimizeChatHistory(messages []bson.M) []bson.M {
	if len(messages) <= maxChatHistory {
		// If we have fewer messages than the limit, just optimize message length
		for i := range messages {
			if msg, ok := messages[i]["message"].(string); ok {
				messages[i]["message"] = truncateMessage(msg)
			}
		}
		return messages
	}

	// Keep first message (system message) and last (maxChatHistory-1) messages
	optimizedMessages := make([]bson.M, maxChatHistory)
	optimizedMessages[0] = messages[0] // Keep system/welcome message

	// Copy last (maxChatHistory-1) messages
	start := len(messages) - (maxChatHistory - 1)
	copy(optimizedMessages[1:], messages[start:])

	// Truncate long messages
	for i := range optimizedMessages {
		if msg, ok := optimizedMessages[i]["message"].(string); ok {
			optimizedMessages[i]["message"] = truncateMessage(msg)
		}
	}

	return optimizedMessages
}

func GetChatBySessionId(sessionId string) ([]bson.M, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		check(err, false)
		return nil, fmt.Errorf("failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	collection := client.Database("teams").Collection("charles-chats")

	filter := bson.M{"sessionId": sessionId}
	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: 1}})

	cursor, err := collection.Find(ctx, filter, opts)
	if err != nil {
		check(err, false)
		return nil, fmt.Errorf("error querying messages: %v", err)
	}
	defer cursor.Close(ctx)

	var messages []bson.M
	err = cursor.All(ctx, &messages)
	if err != nil {
		check(err, false)
		return nil, fmt.Errorf("error retrieving messages: %v", err)
	}

	// Optimize chat history before returning
	return optimizeChatHistory(messages), nil
}

// Take Chat info and get a structure response ai summary for title and description
func GetChatSummary(messageThread string, sessionId string) (string, string, string, error) {
	ctx := context.Background()
	client := openai.NewClient(os.Getenv("OPENAI_API_KEY"))
	// Generate the JSON schema for ChatSummary struct
	schema, err := jsonschema.GenerateSchemaForType(ChatSummary{})
	if err != nil {
		check(err, false)
		return "Chat Summary", "Unable to generate summary", "", err
	}

	resp, err := client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: "gpt-4o-mini-2024-07-18",
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: `Generate a short and brief title and description for this chat conversation. The title should be 3-5 words maximum. The description should be a single sentence summarizing the main topic or request. Only include lead information (name, email, phone, etc.) if there is clear and sufficient context to extract it from the conversation, otherwise omit it.`,
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: messageThread,
				},
			},
			Temperature:         0.3,
			MaxCompletionTokens: 200,
			ResponseFormat: &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
				JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
					Name:   "chat_summary",
					Schema: schema,
					Strict: false,
				},
			},
		},
	)

	if err != nil {
		check(err, false)
		return "Chat Summary", "Unable to generate summary", "", err
	}

	// Parse the structured response
	var summary ChatSummary
	err = json.Unmarshal([]byte(resp.Choices[0].Message.Content), &summary)
	if err != nil {
		check(err, false)
		return "Chat Summary", "Unable to generate summary", "", err
	}

	// Save chat metadata to database
	err = saveChatMetadata(summary, sessionId)
	if err != nil {
		check(err, false)
		pp.Printf("Error saving chat metadata: %v\n", err)
		return "Chat Summary", "Unable to generate summary", "", err
	}

	// Return title and description, with fallbacks if empty
	title := summary.Title
	if title == "" {
		title = "Chat Summary"
	}
	description := summary.Description
	if description == "" {
		description = "Chat conversation summary"
	}

	// Extract lead information from summary if it exists
	leadJSON := ""
	if summary.Lead != nil {
		jsonBytes, err := json.Marshal(summary.Lead)
		if err == nil {
			leadJSON = string(jsonBytes)
		} else {
			check(err, false)
		}
	}

	return title, description, leadJSON, nil
}

func saveChatMetadata(summary ChatSummary, sessionId string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		check(err, false)
		return fmt.Errorf("failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	collection := client.Database("teams").Collection("charles-message-metadata")

	doc := bson.M{
		"id":          uuid.New().String(),
		"title":       summary.Title,
		"description": summary.Description,
		"lead":        summary.Lead,
		"sessionId":   sessionId,
		"createdAt":   time.Now(),
	}

	_, err = collection.InsertOne(ctx, doc)
	if err != nil {
		check(err, false)
		return fmt.Errorf("failed to insert chat metadata: %v", err)
	}

	return nil
}

func HandleCharlesNotification(teamId string, sessionId string, title string, description string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		check(err, false)
		return fmt.Errorf("failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	collection := client.Database("Users").Collection("inbox")

	notification := bson.M{
		"id":          uuid.New().String(),
		"type":        "CHARLES_NEW_CONVERSATION",
		"title":       title,
		"description": description,
		"createdAt":   time.Now(),
		"readAt":      nil,
		"teamId":      teamId,
		"sessionId":   sessionId,
	}
	_, err = collection.InsertOne(ctx, notification)
	if err != nil {
		check(err, false)
		return fmt.Errorf("failed to insert notification: %v", err)
	}

	return nil
}

// HandleLeadFormSubmission processes a lead form submission from the widget
func HandleLeadFormSubmission(submission *types.LeadFormSubmission) (*types.Lead, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		check(err, false)
		return nil, fmt.Errorf("failed to connect to MongoDB: %v", err)
	}
	defer client.Disconnect(ctx)

	// Get team info from client ID
	teamInfo, err := GetTeamInfoByClientId(submission.ClientID)
	if err != nil {
		check(err, false)
		return nil, fmt.Errorf("failed to get team info: %v", err)
	}

	// Create tags based on outreach preference
	tags := []string{"Charles Lead Form"}
	switch submission.OutreachPreference {
	case "sms":
		tags = append(tags, "Prefers SMS")
	case "email":
		tags = append(tags, "Prefers Email")
	case "both":
		tags = append(tags, "SMS + Email OK")
	}

	// Create the lead
	lead := &types.Lead{
		ID:                 uuid.New().String(),
		TeamID:             teamInfo.TeamID,
		FirstName:          submission.FirstName,
		LastName:           submission.LastName,
		Email:              submission.Email,
		Phone:              submission.Phone,
		PropertyID:         submission.PropertyID,
		LeadSource:         "Charles Widget",
		Status:             "Interested",
		Comments:           []string{},
		Tags:               tags,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
		Industry:           "Other",
		OutreachPreference: submission.OutreachPreference,
		LeadOwner: struct {
			ID    string `bson:"id"`
			Email string `bson:"email"`
			Name  string `bson:"name"`
		}{
			ID:    "",
			Email: "",
			Name:  "",
		},
	}

	if submission.PropertyName != "" {
		lead.Property = types.LeadProperty{
			ID:           submission.PropertyID,
			PropertyName: submission.PropertyName,
		}
	}

	// Add message as comment if provided
	if submission.Message != "" {
		lead.Comments = append(lead.Comments, submission.Message)
	}

	// Insert the lead into the database
	collection := client.Database("teams").Collection("leads")
	_, err = collection.InsertOne(ctx, lead)
	if err != nil {
		check(err, false)
		return nil, fmt.Errorf("failed to insert lead: %v", err)
	}

	// Send Discord notification
	go ReportLeadFormSubmission(submission, teamInfo.Name)

	// Create inbox notification
	notificationTitle := fmt.Sprintf("New Lead: %s %s", submission.FirstName, submission.LastName)
	notificationDesc := fmt.Sprintf("Contact preference: %s", submission.OutreachPreference)
	if submission.PropertyName != "" {
		notificationDesc += fmt.Sprintf(" | Property: %s", submission.PropertyName)
	}
	if submission.Email != "" {
		notificationDesc += fmt.Sprintf(" | Email: %s", submission.Email)
	}
	if submission.Phone != "" {
		notificationDesc += fmt.Sprintf(" | Phone: %s", submission.Phone)
	}
	go HandleCharlesNotification(teamInfo.TeamID, submission.SessionID, notificationTitle, notificationDesc)

	return lead, nil
}

// ReportLeadFormSubmission sends a Discord notification for a new lead form submission
func ReportLeadFormSubmission(submission *types.LeadFormSubmission, teamName string) error {
	webhookURL := os.Getenv("OUTREACH_WEBHOOK_URL")
	if webhookURL == "" {
		return nil
	}

	webhookURL = strings.TrimSpace(webhookURL)

	// Use team name or fallback to client ID
	if teamName == "" {
		teamName = submission.ClientID
	}

	// Format outreach preference for display
	outreachDisplay := "Not specified"
	switch submission.OutreachPreference {
	case "sms":
		outreachDisplay = "📱 SMS Only"
	case "email":
		outreachDisplay = "📧 Email Only"
	case "both":
		outreachDisplay = "📱📧 SMS & Email"
	}

	// Create embed with lead information
	embed := map[string]any{
		"title": "🎯 New Lead Form Submission",
		"color": 3066993, // Green color
		"fields": []map[string]any{
			{
				"name":   "👤 Name",
				"value":  fmt.Sprintf("%s %s", submission.FirstName, submission.LastName),
				"inline": true,
			},
			{
				"name":   "🏢 Team",
				"value":  teamName,
				"inline": true,
			},
			{
				"name":   "📞 Contact Preference",
				"value":  outreachDisplay,
				"inline": true,
			},
			{
				"name":   "📧 Email",
				"value":  submission.Email,
				"inline": true,
			},
			{
				"name":   "📱 Phone",
				"value":  submission.Phone,
				"inline": true,
			},
			{
				"name":   "🌐 Source Page",
				"value":  submission.Page,
				"inline": false,
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
		"footer": map[string]any{
			"text": "🤖 Charles Lead Form Tracker",
		},
	}

	// Add message field if provided
	if submission.Message != "" {
		embed["fields"] = append(embed["fields"].([]map[string]any), map[string]any{
			"name":   "💬 Message",
			"value":  submission.Message,
			"inline": false,
		})
	}

	payload := map[string]any{
		"embeds": []map[string]any{embed},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		pp.Printf("\x1b[31mFailed to marshal lead form webhook payload: %v\x1b[0m\n", err)
		report.InsertError(fmt.Sprintf("Lead form webhook marshal error: %v", err))
		return err
	}

	// Create HTTP client with timeout
	httpClient := &http.Client{
		Timeout: 10 * time.Second,
	}

	// Retry logic for webhook call
	maxRetries := 3
	for attempt := 1; attempt <= maxRetries; attempt++ {
		resp, err := httpClient.Post(webhookURL, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			pp.Printf("\x1b[33mLead form webhook attempt %d/%d failed: %v\x1b[0m\n", attempt, maxRetries, err)
			if attempt == maxRetries {
				pp.Printf("\x1b[31mAll lead form webhook attempts failed, giving up\x1b[0m\n")
				report.InsertError(fmt.Sprintf("Lead form webhook error after %d attempts: %v", maxRetries, err))
				return err
			}
			time.Sleep(2 * time.Second)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
			pp.Printf("\x1b[33mLead form webhook attempt %d/%d returned status: %d\x1b[0m\n", attempt, maxRetries, resp.StatusCode)
			if attempt == maxRetries {
				pp.Printf("\x1b[31mLead form webhook returned non-success status after all attempts: %d\x1b[0m\n", resp.StatusCode)
				report.InsertError(fmt.Sprintf("Lead form webhook returned status %d after %d attempts", resp.StatusCode, maxRetries))
				return err
			}
			time.Sleep(2 * time.Second)
			continue
		}

		pp.Printf("\x1b[32mLead form webhook completed successfully on attempt %d\n\x1b[0m", attempt)
		return nil
	}

	return nil
}

// ================== EMBEDDING STORAGE FUNCTIONS ==================

// TeamHasEmbeddings checks if a team has stored embeddings
func TeamHasEmbeddings(teamId string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return false, err
	}
	defer client.Disconnect(ctx)

	collection := client.Database("embeddings").Collection("property_embeddings")

	count, err := collection.CountDocuments(ctx, bson.M{"teamId": teamId})
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// GetTeamEmbeddings retrieves stored embeddings for a team
func GetTeamEmbeddings(teamId string) ([]ai.EmbeddingStorage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, err
	}
	defer client.Disconnect(ctx)

	collection := client.Database("embeddings").Collection("property_embeddings")

	cursor, err := collection.Find(ctx, bson.M{"teamId": teamId})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var embeddings []ai.EmbeddingStorage
	err = cursor.All(ctx, &embeddings)
	if err != nil {
		return nil, err
	}

	return embeddings, nil
}

// StoreTeamEmbeddings stores embeddings for a team
func StoreTeamEmbeddings(teamId string, embeddings []ai.EmbeddingStorage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	collection := client.Database("embeddings").Collection("property_embeddings")

	// Delete existing embeddings for this team
	_, err = collection.DeleteMany(ctx, bson.M{"teamId": teamId})
	if err != nil {
		return err
	}

	// Insert new embeddings
	if len(embeddings) > 0 {
		var docs []interface{}
		for _, emb := range embeddings {
			docs = append(docs, emb)
		}
		_, err = collection.InsertMany(ctx, docs)
		if err != nil {
			return err
		}
	}

	pp.Printf("\x1b[32mStored %d embeddings for team %s\x1b[0m\n", len(embeddings), teamId)
	return nil
}

// ClearTeamEmbeddings removes all embeddings for a team
func ClearTeamEmbeddings(teamId string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return err
	}
	defer client.Disconnect(ctx)

	collection := client.Database("embeddings").Collection("property_embeddings")

	_, err = collection.DeleteMany(ctx, bson.M{"teamId": teamId})
	if err != nil {
		return err
	}

	pp.Printf("\x1b[33mCleared embeddings for team %s\x1b[0m\n", teamId)
	return nil
}

// EmbeddingsAreStale checks if embeddings are older than 7 days
func EmbeddingsAreStale(teamId string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return false, err
	}
	defer client.Disconnect(ctx)

	collection := client.Database("embeddings").Collection("property_embeddings")

	// Find the most recent embedding for this team
	var latestEmbedding ai.EmbeddingStorage
	err = collection.FindOne(ctx, bson.M{"teamId": teamId}, options.FindOne().SetSort(bson.M{"embeddedAt": -1})).Decode(&latestEmbedding)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return true, nil // No embeddings = considered stale
		}
		return false, err
	}

	// Check if older than 7 days
	staleThreshold := time.Now().Add(-7 * 24 * time.Hour)
	return latestEmbedding.EmbeddedAt.Before(staleThreshold), nil
}

// ================== OPTIMIZED PROPERTY PROCESSING ==================

// ProcessPropertiesWithCaching processes properties with caching and batch optimization
func ProcessPropertiesWithCaching(teamId string, properties []bson.M, openaiClient *openai.Client) (*ai.QASystem, error) {
	// Check if we have a cached QA system
	if cachedQA, found := GetCachedQASystem(teamId); found {
		pp.Printf("\x1b[32mUsing cached QA system for team %s (%d documents)\x1b[0m\n", teamId, cachedQA.GetDocumentCount())
		return cachedQA, nil
	}

	// Check if we have stored embeddings
	hasEmbeddings, err := TeamHasEmbeddings(teamId)
	if err != nil {
		pp.Printf("\x1b[33mError checking embeddings: %v\x1b[0m\n", err)
	}

	if hasEmbeddings {
		// Check if embeddings are stale
		stale, err := EmbeddingsAreStale(teamId)
		if err != nil {
			pp.Printf("\x1b[33mError checking staleness: %v\x1b[0m\n", err)
		}

		if !stale {
			// Load from storage
			embeddings, err := GetTeamEmbeddings(teamId)
			if err == nil && len(embeddings) > 0 {
				qa := ai.NewQASystemWithTeam(teamId)
				qa.LoadFromStorage(embeddings)
				SetCachedQASystem(teamId, qa)
				pp.Printf("\x1b[32mLoaded %d embeddings from storage for team %s\x1b[0m\n", len(embeddings), teamId)
				return qa, nil
			}
		} else {
			pp.Printf("\x1b[33mEmbeddings are stale for team %s, regenerating...\x1b[0m\n", teamId)
		}
	}

	// Generate new embeddings
	qa, err := ProcessPropertiesInBatches(teamId, properties, openaiClient)
	if err != nil {
		return nil, err
	}

	// Cache the QA system
	SetCachedQASystem(teamId, qa)

	// Store embeddings in background
	go func() {
		embeddings := qa.GetEmbeddingsForStorage()
		if err := StoreTeamEmbeddings(teamId, embeddings); err != nil {
			pp.Printf("\x1b[31mFailed to store embeddings: %v\x1b[0m\n", err)
		}
	}()

	return qa, nil
}

// getCachedSessionPreferences returns cached preferences if available and not expired
func getCachedSessionPreferences(sessionId string) (*preference.Preferences, bool) {
	sessionPreferencesCacheLock.RLock()
	defer sessionPreferencesCacheLock.RUnlock()

	entry, exists := sessionPreferencesCache[sessionId]
	if !exists {
		return nil, false
	}

	if time.Now().After(entry.expiresAt) {
		return nil, false
	}

	return entry.preferences, true
}

// setCachedSessionPreferences caches session preferences for 5 minutes
func setCachedSessionPreferences(sessionId string, prefs *preference.Preferences) {
	sessionPreferencesCacheLock.Lock()
	defer sessionPreferencesCacheLock.Unlock()

	sessionPreferencesCache[sessionId] = sessionPreferenceEntry{
		preferences: prefs,
		expiresAt:   time.Now().Add(5 * time.Minute),
	}
}

// ClearSessionPreferencesCache clears the cache for a specific session
func ClearSessionPreferencesCache(sessionId string) {
	sessionPreferencesCacheLock.Lock()
	defer sessionPreferencesCacheLock.Unlock()
	delete(sessionPreferencesCache, sessionId)
}

// GetSessionPreferences retrieves preferences for a given session
// Uses in-memory cache first, falls back to MongoDB if not cached
func GetSessionPreferences(teamId, sessionId string) (*preference.Preferences, error) {
	// Check in-memory cache first
	if cachedPrefs, found := getCachedSessionPreferences(sessionId); found {
		pp.Printf("Using cached session preferences for session %s\n", sessionId)
		return cachedPrefs, nil
	}

	// Fall back to MongoDB if not in cache
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	store, err := preference.NewStore()
	if err != nil {
		pp.Printf("Failed to create preference store: %v\n", err)
		return nil, err
	}
	defer store.Close(ctx)

	leadPref, err := store.GetBySessionID(ctx, sessionId)
	if err != nil {
		pp.Printf("Failed to get preferences: %v\n", err)
		return nil, err
	}

	if leadPref == nil {
		return nil, nil
	}

	// Cache the preferences for future calls
	setCachedSessionPreferences(sessionId, &leadPref.Preferences)

	return &leadPref.Preferences, nil
}

// FilterPropertiesByPreferences filters properties based on user preferences
// This combines semantic search results with programmatic preference filtering
func FilterPropertiesByPreferences(teamId string, propertyIDs []string, prefs *preference.Preferences, properties []bson.M) []string {
	if prefs == nil || !prefs.HasPreferences() {
		return propertyIDs
	}

	pp.Printf("Filtering %d properties by preferences for team %s\n", len(propertyIDs), teamId)

	// Create a map of propertyID to property for quick lookup
	propertyMap := make(map[string]bson.M)
	for _, prop := range properties {
		if id, ok := prop["id"].(string); ok {
			propertyMap[id] = prop
		}
	}

	// Create filter engine
	filterEngine, err := preference.NewFilterEngine()
	if err != nil {
		pp.Printf("Failed to create filter engine: %v\n", err)
		return propertyIDs // Return original if filter fails
	}

	// Convert bson.M to preference.Property
	var prefProps []preference.Property
	for _, prop := range properties {
		// Only include properties that are in our semantic search results
		if id, ok := prop["id"].(string); ok {
			// Check if this property ID matches any of our search results
			found := false
			for _, pid := range propertyIDs {
				// Handle chunked property IDs
				basePid := pid
				if idx := strings.LastIndex(pid, "-chunk"); idx != -1 {
					basePid = pid[:idx]
				}
				if id == basePid || id == pid {
					found = true
					break
				}
			}
			if !found {
				continue
			}

			prefProp := convertToPreferenceProperty(prop)
			if prefProp != nil {
				prefProps = append(prefProps, *prefProp)
			}
		}
	}

	// Apply filter
	filtered := filterEngine.FilterPropertiesByPreferences(prefProps, prefs)

	// Extract IDs from filtered properties
	filteredIDs := make([]string, 0, len(filtered))
	for _, prop := range filtered {
		filteredIDs = append(filteredIDs, prop.ID)
	}

	pp.Printf("Filtered from %d to %d properties based on preferences\n", len(propertyIDs), len(filteredIDs))

	return filteredIDs
}

// convertToPreferenceProperty converts a bson.M property to preference.Property
func convertToPreferenceProperty(prop bson.M) *preference.Property {
	result := &preference.Property{}

	// Basic fields
	if id, ok := prop["id"].(string); ok {
		result.ID = id
	}
	if name, ok := prop["propertyName"].(string); ok {
		result.PropertyName = name
	}
	if teamID, ok := prop["teamId"].(string); ok {
		result.TeamID = teamID
	}
	if propType, ok := prop["type"].(string); ok {
		result.Type = propType
	}

	// Location
	if loc, ok := prop["location"].(primitive.M); ok {
		result.Location = preference.Location{}
		if addr, ok := loc["fullAddress"].(string); ok {
			result.Location.FullAddress = addr
		}
		if state, ok := loc["state"].(string); ok {
			result.Location.State = state
		}
		if city, ok := loc["city"].(string); ok {
			result.Location.City = city
		}
		if postal, ok := loc["postalCode"].(string); ok {
			result.Location.PostalCode = postal
		}
		if street, ok := loc["streetAddress"].(string); ok {
			result.Location.StreetAddress = street
		}
	}

	// Sqft
	if sqft, ok := prop["sqft"].(int32); ok {
		sqftInt := int(sqft)
		result.Sqft = &sqftInt
	} else if sqft, ok := prop["sqft"].(int); ok {
		result.Sqft = &sqft
	}

	// Bedrooms
	if beds, ok := prop["bedrooms"].(int32); ok {
		bedsInt := int(beds)
		result.Bedrooms = &bedsInt
	} else if beds, ok := prop["bedrooms"].(int); ok {
		result.Bedrooms = &beds
	}

	// Bathrooms
	if baths, ok := prop["bathrooms"].(float64); ok {
		bathsInt := int(baths)
		result.Bathrooms = &bathsInt
	} else if baths, ok := prop["bathrooms"].(int32); ok {
		bathsInt := int(baths)
		result.Bathrooms = &bathsInt
	} else if baths, ok := prop["bathrooms"].(int); ok {
		result.Bathrooms = &baths
	}

	// Rent
	if rent, ok := prop["rent"].(float64); ok {
		result.Rent = &rent
	}

	// Deposit
	if deposit, ok := prop["deposit"].(float64); ok {
		result.Deposit = &deposit
	}

	// Amenities
	if amenities, ok := prop["amenities"].(primitive.A); ok {
		for _, a := range amenities {
			if amenity, ok := a.(string); ok {
				result.Amenities = append(result.Amenities, amenity)
			}
		}
	}

	// Pet Fees
	if petFees, ok := prop["petFees"].(primitive.A); ok {
		for _, pf := range petFees {
			if fee, ok := pf.(string); ok {
				result.PetFees = append(result.PetFees, fee)
			}
		}
	}

	// Units
	if units, ok := prop["units"].(primitive.A); ok {
		for _, u := range units {
			if unitMap, ok := u.(primitive.M); ok {
				unit := preference.Unit{}

				if uid, ok := unitMap["id"].(string); ok {
					unit.ID = uid
				}
				if unitName, ok := unitMap["unitName"].(string); ok {
					unit.UnitName = unitName
				}
				if unitType, ok := unitMap["unitType"].(string); ok {
					unit.UnitType = unitType
				}
				if beds, ok := unitMap["bedrooms"].(float64); ok {
					unit.Bedrooms = int(beds)
				}
				if baths, ok := unitMap["bathrooms"].(float64); ok {
					unit.Bathrooms = int(baths)
				}
				if sqft, ok := unitMap["squareFootage"].(float64); ok {
					unit.SquareFootage = sqft
				}
				if rent, ok := unitMap["rent"].(float64); ok {
					unit.Rent = rent
				}
				if deposit, ok := unitMap["deposit"].(float64); ok {
					unit.Deposit = deposit
				}
				if isVacant, ok := unitMap["isVacant"].(bool); ok {
					unit.IsVacant = isVacant
				}

				if amenities, ok := unitMap["amenities"].(primitive.A); ok {
					for _, a := range amenities {
						if amenity, ok := a.(string); ok {
							unit.Amenities = append(unit.Amenities, amenity)
						}
					}
				}

				result.Units = append(result.Units, unit)
			}
		}
	}

	return result
}

// ProcessPropertiesInBatches processes properties in batches for embedding generation
func ProcessPropertiesInBatches(teamId string, properties []bson.M, openaiClient *openai.Client) (*ai.QASystem, error) {
	pp.Printf("Processing %d properties in batches for team %s\n", len(properties), teamId)

	// Initialize property optimizer
	optimizer := ai.NewPropertyOptimizer()

	// Create QA system
	qa := ai.NewQASystemWithTeam(teamId)

	// Prepare property texts and IDs
	propertyTexts := make([]string, 0, len(properties))
	propertyIds := make([]string, 0, len(properties))

	for _, property := range properties {
		propertyId, ok := property["id"].(string)
		if !ok {
			propertyId = fmt.Sprintf("property-%d", len(propertyIds))
		}

		// Use property optimizer to create minified JSON
		propertyText, tokenEstimate, err := optimizer.OptimizePropertyForEmbedding(property)
		if err != nil {
			pp.Printf("\x1b[33mError optimizing property: %v\x1b[0m\n", err)
			continue
		}

		// Check if property is still too large after optimization
		if tokenEstimate > 8000 {
			// If property is extremely large, split it into chunks
			if tokenEstimate > 15000 {
				chunks, chunkTokens, err := optimizer.SplitPropertyIntoChunks(property, 7000)
				if err != nil {
					pp.Printf("\x1b[31mFailed to split property %s: %v\x1b[0m\n", propertyId, err)
					continue
				}

				for chunkIdx, chunk := range chunks {
					chunkId := fmt.Sprintf("%s-chunk%d", propertyId, chunkIdx+1)
					propertyTexts = append(propertyTexts, chunk)
					propertyIds = append(propertyIds, chunkId)
					pp.Printf("Property %s: chunk %d ~%d tokens\n", propertyId, chunkIdx+1, chunkTokens[chunkIdx])
				}
				continue
			}

			// Try conservative estimation for moderately large properties
			propertyText, tokenEstimate, err = optimizer.OptimizePropertyForEmbeddingConservative(property)
			if err != nil {
				continue
			}
		}

		propertyTexts = append(propertyTexts, propertyText)
		propertyIds = append(propertyIds, propertyId)
	}

	if len(propertyTexts) == 0 {
		return qa, fmt.Errorf("no valid property texts to process")
	}

	// Batch processing constants
	const (
		maxTokensPerBatch   = 7000
		maxBatchSize        = 5
		sleepBetweenBatches = 500 * time.Millisecond
	)

	// Process in batches
	successCount := 0

	for i := 0; i < len(propertyTexts); {
		batchTexts := make([]string, 0)
		batchIds := make([]string, 0)
		currentTokens := 0

		for j := i; j < len(propertyTexts) && len(batchTexts) < maxBatchSize; j++ {
			tokenEstimate := len(propertyTexts[j]) / 2

			if tokenEstimate > maxTokensPerBatch {
				if len(batchTexts) > 0 {
					break
				}
				batchTexts = append(batchTexts, propertyTexts[j])
				batchIds = append(batchIds, propertyIds[j])
				i = j + 1
				break
			}

			if currentTokens+tokenEstimate > maxTokensPerBatch && len(batchTexts) > 0 {
				break
			}

			batchTexts = append(batchTexts, propertyTexts[j])
			batchIds = append(batchIds, propertyIds[j])
			currentTokens += tokenEstimate
			i = j + 1
		}

		if len(batchTexts) == 0 {
			if i >= len(propertyTexts) {
				break
			}
			i = i + 1
			continue
		}

		// Get embeddings for this batch
		embeddings, err := ai.GetEmbeddingsBatch(openaiClient, batchTexts)
		if err != nil {
			// Fallback: try processing individually
			for k, text := range batchTexts {
				propertyId := batchIds[k]
				embedding, err := ai.GetEmbeddingWithClient(openaiClient, text)
				if err != nil {
					continue
				}
				qa.AddDocument(text, embedding, propertyId)
				successCount++
				time.Sleep(100 * time.Millisecond)
			}
		} else {
			for k, embedding := range embeddings {
				if k < len(batchIds) {
					qa.AddDocument(batchTexts[k], embedding, batchIds[k])
					successCount++
				}
			}
		}

		// Sleep between batches to avoid rate limiting
		if i < len(propertyTexts) {
			time.Sleep(sleepBetweenBatches)
		}
	}

	pp.Printf("\x1b[32mBatch processing complete: %d properties embedded\x1b[0m\n", successCount)

	return qa, nil
}

// GetPropertyURLs retrieves scheduling and application URLs directly from database
// This is called AFTER AI response to ensure accurate URLs (no hallucinations)
func GetPropertyURLs(teamID, propertyID string) (*PropertyURLs, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer client.Disconnect(ctx)

	dbName := os.Getenv("MONGODB_DATABASE")
	collName := os.Getenv("MONGODB_COLLECTION_PROPERTIES")

	if dbName == "" || collName == "" {
		// Fallback to default if env vars not set
		pp.Printf("\x1b[33mGetPropertyURLs: Environment variables not set, using default URL for %s\x1b[0m\n", propertyID)
		return &PropertyURLs{
			PropertyID:         propertyID,
			DefaultScheduleURL: fmt.Sprintf("https://rentbamboo.com/schedule/%s", propertyID),
		}, nil
	}

	collection := client.Database(dbName).Collection(collName)

	// Strip chunk suffix if present
	baseID := propertyID
	if idx := strings.LastIndex(propertyID, "-chunk"); idx != -1 {
		baseID = propertyID[:idx]
	}

	// Only fetch the fields we need: customScheduleUrl and applicationUrl
	projection := bson.M{
		"id":                1,
		"customScheduleUrl": 1,
		"applicationUrl":    1,
	}

	pp.Printf("\x1b[36mGetPropertyURLs: Querying database for property %s (baseID: %s) in team %s\x1b[0m\n", propertyID, baseID, teamID)

	var property bson.M
	err = collection.FindOne(ctx, bson.M{
		"id":     baseID,
		"teamId": teamID,
	}, options.FindOne().SetProjection(projection)).Decode(&property)

	if err != nil {
		if err == mongo.ErrNoDocuments {
			pp.Printf("\x1b[33mGetPropertyURLs: Property %s not found in database, using default URL\x1b[0m\n", baseID)
			// Property not found, return default
			return &PropertyURLs{
				PropertyID:         propertyID,
				DefaultScheduleURL: fmt.Sprintf("https://rentbamboo.com/schedule/%s", baseID),
			}, nil
		}
		pp.Printf("\x1b[31mGetPropertyURLs: Error fetching property %s: %v\x1b[0m\n", baseID, err)
		return nil, fmt.Errorf("failed to fetch property: %w", err)
	}

	// Debug: Print what we found
	pp.Printf("\x1b[36mGetPropertyURLs: Found property %s in database\x1b[0m\n", baseID)
	if customURL, ok := property["customScheduleUrl"].(string); ok {
		pp.Printf("\x1b[36mGetPropertyURLs: customScheduleUrl field value: %s\x1b[0m\n", customURL)
	} else {
		pp.Printf("\x1b[33mGetPropertyURLs: customScheduleUrl field not found or not a string\x1b[0m\n")
	}
	if appURL, ok := property["applicationUrl"].(string); ok {
		pp.Printf("\x1b[36mGetPropertyURLs: applicationUrl field value: %s\x1b[0m\n", appURL)
	}

	urls := &PropertyURLs{
		PropertyID:         propertyID,
		DefaultScheduleURL: fmt.Sprintf("https://rentbamboo.com/schedule/%s", baseID),
	}

	// Get customScheduleUrl if exists
	if customURL, ok := property["customScheduleUrl"].(string); ok && customURL != "" {
		urls.CustomScheduleURL = customURL
		pp.Printf("\x1b[32mGetPropertyURLs: Using customScheduleUrl: %s\x1b[0m\n", customURL)
	} else {
		pp.Printf("\x1b[33mGetPropertyURLs: No customScheduleUrl found, using default: %s\x1b[0m\n", urls.DefaultScheduleURL)
	}

	// Get applicationUrl if exists
	if appURL, ok := property["applicationUrl"].(string); ok && appURL != "" {
		urls.ApplicationURL = appURL
	}

	return urls, nil
}

// ResolveScheduleURL resolves the correct scheduling URL for a property
// Priority: 1. customScheduleUrl from DB, 2. default RentBamboo URL
func ResolveScheduleURL(teamID, propertyID string) string {
	urls, err := GetPropertyURLs(teamID, propertyID)
	if err != nil {
		// On error, return default URL
		baseID := propertyID
		if idx := strings.LastIndex(propertyID, "-chunk"); idx != -1 {
			baseID = propertyID[:idx]
		}
		return fmt.Sprintf("https://rentbamboo.com/schedule/%s", baseID)
	}

	if urls.CustomScheduleURL != "" {
		return urls.CustomScheduleURL
	}

	return urls.DefaultScheduleURL
}

// ResolveApplicationURL resolves the correct application URL for a property
// Returns empty string if no application URL exists (don't hallucinate!)
func ResolveApplicationURL(teamID, propertyID string) string {
	urls, err := GetPropertyURLs(teamID, propertyID)
	if err != nil || urls.ApplicationURL == "" {
		// Return empty string if no URL exists - don't hallucinate!
		return ""
	}

	return urls.ApplicationURL
}

// =============================================================================
// LINK PLACEHOLDER REPLACEMENT (safety-net for empty/placeholder links)
// =============================================================================

// schedulePlaceholderRegex matches bracket-enclosed natural-language references
// to a scheduling/tour link that the AI sometimes emits instead of an actual
// URL. We match on word *roots* (e.g. `schedul` instead of `schedule`) so
// variants like "scheduling" / "scheduled" are all caught.
//
// Examples that should match:
//
//	[SCHEDULE_LINK]
//	[tour scheduling link]
//	[scheduling link]
//	[tour link]
//	[schedule link]
//	[schedule a tour link]
//	[tour url]
//	[link to schedule a tour]
//	[viewing link]
var schedulePlaceholderRegex = regexp.MustCompile(`(?i)\[[^\]]*(?:schedul|tour|view|visit)[^\]]*(?:link|url)[^\]]*\]|\[[^\]]*(?:link|url)[^\]]*(?:schedul|tour|view|visit)[^\]]*\]|\[SCHEDULE_LINK\]`)

// applicationPlaceholderRegex matches bracket-enclosed natural-language references
// to an application link. Examples:
//
//	[APPLICATION_LINK]
//	[application link]
//	[application url]
//	[apply link]
//	[link to apply]
var applicationPlaceholderRegex = regexp.MustCompile(`(?i)\[[^\]]*(?:applicat|apply|applying)[^\]]*(?:link|url)[^\]]*\]|\[[^\]]*(?:link|url)[^\]]*(?:applicat|apply|applying)[^\]]*\]|\[APPLICATION_LINK\]`)

// stripStalePlaceholderRegex matches generic URL placeholders the AI might
// have copied from chat history (where URLs were redacted with [URL] or
// [stale-link]). These should be stripped, NOT replaced with the current
// property's URL — the AI was quoting old/stale info, so injecting a fresh
// URL would be misleading. Canonical placeholders like [SCHEDULE_LINK] are
// handled by schedulePlaceholderRegex/applicationPlaceholderRegex and DO
// get replaced with the current URL.
var stripStalePlaceholderRegex = regexp.MustCompile(`(?i)\[URL\]|\[stale-link\]|\[stale_url\]|\[old-link\]|\[old_url\]`)

// ReplaceLinkPlaceholders scans AI-generated text for placeholder link references
// (both explicit `[SCHEDULE_LINK]` / `[APPLICATION_LINK]` and natural-language
// variants like `[tour scheduling link]`) and replaces them with the actual
// URLs. If `htmlMode` is true, URLs are rendered as HTML `<a>` tags (suitable
// for the Charles chatbot widget). Otherwise URLs are inserted as plain text
// (suitable for SMS).
//
// If a corresponding URL is empty, the placeholder is stripped entirely and
// surrounding whitespace/punctuation is tidied up so the resulting text is
// still grammatical.
func ReplaceLinkPlaceholders(text, scheduleURL, applicationURL string, htmlMode bool) string {
	if text == "" {
		return text
	}

	renderURL := func(url string, label string) string {
		if url == "" {
			return ""
		}
		if htmlMode {
			return fmt.Sprintf(`<a href="%s" target="_blank">%s</a>`, url, label)
		}
		return url
	}

	// Strip generic placeholders the AI might have copied from chat history
	// (where URLs were redacted to [URL] or [stale-link]). These are stale
	// info; we don't want to inject the current property's URL into a
	// sentence the AI was quoting from old history.
	if stripStalePlaceholderRegex.MatchString(text) {
		pp.Printf("\x1b[33mReplaceLinkPlaceholders: stripping stale placeholder(s) from text\x1b[0m\n")
		text = stripStalePlaceholderRegex.ReplaceAllString(text, "")
		text = tidyPlaceholderArtifacts(text)
	}

	// Replace scheduling placeholders
	text = schedulePlaceholderRegex.ReplaceAllStringFunc(text, func(match string) string {
		pp.Printf("\x1b[33mReplaceLinkPlaceholders: found schedule placeholder %q\x1b[0m\n", match)
		return renderURL(scheduleURL, "Schedule a Tour")
	})

	// Replace application placeholders
	text = applicationPlaceholderRegex.ReplaceAllStringFunc(text, func(match string) string {
		pp.Printf("\x1b[33mReplaceLinkPlaceholders: found application placeholder %q\x1b[0m\n", match)
		return renderURL(applicationURL, "Apply Now")
	})

	// Tidy up artifacts from removed placeholders (e.g. doubled spaces,
	// dangling punctuation sequences that come from stripping a placeholder).
	text = tidyPlaceholderArtifacts(text)

	return text
}

// tidyPlaceholderArtifacts removes awkward whitespace/punctuation patterns
// produced by stripping an empty placeholder from a sentence.
func tidyPlaceholderArtifacts(text string) string {
	// Collapse runs of spaces (but preserve newlines).
	spaceRe := regexp.MustCompile(`[ \t]{2,}`)
	text = spaceRe.ReplaceAllString(text, " ")

	// Fix dangling punctuation like ": .", " : ", "here: ." etc.
	text = strings.ReplaceAll(text, ": .", ".")
	text = strings.ReplaceAll(text, ": !", "!")
	text = strings.ReplaceAll(text, ": ?", "?")
	text = strings.ReplaceAll(text, " .", ".")
	text = strings.ReplaceAll(text, " ,", ",")
	text = strings.ReplaceAll(text, " !", "!")
	text = strings.ReplaceAll(text, " ?", "?")
	text = strings.ReplaceAll(text, "( )", "")
	text = strings.ReplaceAll(text, "()", "")

	// Trim spaces at end of each line.
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	text = strings.Join(lines, "\n")

	return text
}

// findMatchingScheduleURL tries to match property names mentioned in the AI response
// to the correct scheduling URL based on property names in contextInfo
// lastDiscussedPropertyID is prioritized - if provided, use that property's URL as fallback
func findMatchingScheduleURL(responseText, contextInfo string, resolvedScheduleURLs, propertyIDs []string, lastDiscussedPropertyID string) string {
	if len(resolvedScheduleURLs) == 0 {
		return ""
	}

	// If only one property, use its URL
	if len(resolvedScheduleURLs) == 1 {
		return resolvedScheduleURLs[0]
	}

	// Extract property names from contextInfo
	// ContextInfo format: "Property: Crosswinds Apartment Homes\nLocation: ...\n\nProperty: The Rocklyn\n..."
	propertyNames := extractPropertyNamesFromContext(contextInfo)

	// Try to match property names mentioned in response
	for i, name := range propertyNames {
		if i < len(resolvedScheduleURLs) {
			// Check if this property name is mentioned in the response
			if strings.Contains(strings.ToLower(responseText), strings.ToLower(name)) {
				pp.Printf("Matched property name '%s' in response, using URL index %d\n", name, i)
				return resolvedScheduleURLs[i]
			}
		}
	}

	// Check for "other one", "second one", "first one" patterns
	responseLower := strings.ToLower(responseText)
	if strings.Contains(responseLower, "other one") || strings.Contains(responseLower, "second one") {
		// Use last property URL
		pp.Printf("Detected 'other one' or 'second one' in response, using last property URL\n")
		return resolvedScheduleURLs[len(resolvedScheduleURLs)-1]
	}

	if strings.Contains(responseLower, "first one") {
		// Use first property URL
		pp.Printf("Detected 'first one' in response, using first property URL\n")
		return resolvedScheduleURLs[0]
	}

	// PRIORITY FALLBACK: Use lastDiscussedPropertyID if provided and in the propertyIDs list
	// This is the key fix - the tracking already knows which property was last discussed!
	if lastDiscussedPropertyID != "" && len(propertyIDs) > 0 {
		for i, pid := range propertyIDs {
			// Handle chunked property IDs
			basePid := pid
			if idx := strings.LastIndex(pid, "-chunk"); idx != -1 {
				basePid = pid[:idx]
			}

			lastBaseID := lastDiscussedPropertyID
			if idx := strings.LastIndex(lastDiscussedPropertyID, "-chunk"); idx != -1 {
				lastBaseID = lastDiscussedPropertyID[:idx]
			}

			if basePid == lastBaseID && i < len(resolvedScheduleURLs) {
				pp.Printf("Using lastDiscussedPropertyID %s match, using URL index %d\n", lastDiscussedPropertyID, i)
				return resolvedScheduleURLs[i]
			}
		}
	}

	// Default to first URL if no match found
	pp.Printf("No property name match found, using first URL\n")
	return resolvedScheduleURLs[0]
}

// extractPropertyNamesFromContext extracts property names from contextInfo string
func extractPropertyNamesFromContext(contextInfo string) []string {
	var names []string

	// Split by double newlines to get property sections
	sections := strings.Split(contextInfo, "\n\n")

	for _, section := range sections {
		// Trim whitespace
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}

		// Property name is everything before the first colon
		// Format: "PropertyName: BedBathConfig, City, State. Availability..."
		if idx := strings.Index(section, ":"); idx != -1 {
			name := strings.TrimSpace(section[:idx])
			if name != "" {
				names = append(names, name)
			}
		}
	}

	return names
}

// GeneratePropertySummary generates a summary for a property using the property summarizer
// This is used for SMS responses instead of embedding-based context
func GeneratePropertySummary(teamId, propertyId string, inquiry string) string {
	// Fetch the property data
	property, err := GetPropertyByID(teamId, propertyId)
	if err != nil {
		pp.Printf("Error fetching property %s for summary: %v\n", propertyId, err)
		return ""
	}

	// Create context from inquiry
	context := make(map[string]bool)
	inquiryLower := strings.ToLower(inquiry)

	// Check for common context keywords
	contextKeywords := map[string][]string{
		"pet":       {"pet", "dog", "cat", "pets"},
		"parking":   {"parking", "garage", "car"},
		"deposit":   {"deposit", "security", "fee"},
		"utilities": {"utilities", "utility", "water", "electric", "gas"},
		"income":    {"income", "restriction", "qualify"},
		"contact":   {"contact", "phone", "call", "email"},
	}

	for contextKey, keywords := range contextKeywords {
		for _, keyword := range keywords {
			if strings.Contains(inquiryLower, keyword) {
				context[contextKey] = true
				break
			}
		}
	}

	// Generate summary
	config := DefaultPropertySummaryConfig()
	config.Context = context
	config.MaxLength = 150 // Shorter for SMS

	return GetOptimalSummary(property, config)
}

// getContactInfoFromProperty extracts contact info from an already-fetched property (no DB query)
// Takes a bson.M property and returns formatted contact string
func getContactInfoFromProperty(property bson.M) string {
	// Extract contact info from property.contact
	if contact, ok := property["contact"].(primitive.M); ok {
		var contactParts []string

		// Get name
		if name, ok := contact["name"].(string); ok && name != "" {
			contactParts = append(contactParts, fmt.Sprintf("Name: %s", name))
		}

		// Get email
		if email, ok := contact["email"].(string); ok && email != "" {
			contactParts = append(contactParts, fmt.Sprintf("Email: %s", email))
		}

		// Get phone
		if phone, ok := contact["phone"].(string); ok && phone != "" {
			contactParts = append(contactParts, fmt.Sprintf("Phone: %s", phone))
		}

		if len(contactParts) > 0 {
			return strings.Join(contactParts, ", ")
		}
	}

	// If no contact object, check for assignedMember as fallback
	if assignedMember, ok := property["assignedMember"].(primitive.M); ok {
		var contactParts []string

		if name, ok := assignedMember["name"].(string); ok && name != "" {
			contactParts = append(contactParts, fmt.Sprintf("Agent: %s", name))
		}

		if email, ok := assignedMember["email"].(string); ok && email != "" {
			contactParts = append(contactParts, fmt.Sprintf("Email: %s", email))
		}

		if len(contactParts) > 0 {
			return strings.Join(contactParts, ", ")
		}
	}

	return ""
}

// getPropertyContactInfo retrieves contact info (name, email, phone) from a property's contact object
// Returns a formatted string with contact information for AI context
func getPropertyContactInfo(teamId string, propertyIDs []string) string {
	if len(propertyIDs) == 0 {
		return ""
	}

	// Get the first property ID (most relevant)
	propertyID := propertyIDs[0]

	// Strip chunk suffix if present
	baseID := propertyID
	if idx := strings.LastIndex(propertyID, "-chunk"); idx != -1 {
		baseID = propertyID[:idx]
	}

	// Fetch the property data
	property, err := GetPropertyByID(teamId, baseID)
	if err != nil {
		pp.Printf("Error fetching property %s for contact info: %v\n", baseID, err)
		return ""
	}

	// Use the helper function to extract contact info
	return getContactInfoFromProperty(property)
}

// getMatchingPropertyContactInfo retrieves contact info for the property mentioned in the response
// Uses the same matching logic as findMatchingScheduleURL - matches property names in response to property IDs
// lastDiscussedPropertyID is prioritized as the fallback when no match is found in the response
func getMatchingPropertyContactInfo(teamId string, responseText string, contextInfo string, propertyIDs []string, lastDiscussedPropertyID string) string {
	if len(propertyIDs) == 0 {
		return ""
	}

	// If only one property, use its contact info
	if len(propertyIDs) == 1 {
		return getPropertyContactInfo(teamId, propertyIDs)
	}

	// Extract property names from contextInfo (same logic as findMatchingScheduleURL)
	propertyNames := extractPropertyNamesFromContext(contextInfo)

	// Try to match property names mentioned in response
	for i, name := range propertyNames {
		if i < len(propertyIDs) {
			// Check if this property name is mentioned in the response
			if strings.Contains(strings.ToLower(responseText), strings.ToLower(name)) {
				pp.Printf("Matched property name '%s' in response for contact info, using property index %d\n", name, i)
				// Get contact info for this specific property
				return getPropertyContactInfo(teamId, []string{propertyIDs[i]})
			}
		}
	}

	// Check for "other one", "second one", "first one" patterns
	responseLower := strings.ToLower(responseText)
	if strings.Contains(responseLower, "other one") || strings.Contains(responseLower, "second one") {
		pp.Printf("Detected 'other one' or 'second one' in response, using last property for contact info\n")
		return getPropertyContactInfo(teamId, []string{propertyIDs[len(propertyIDs)-1]})
	}

	if strings.Contains(responseLower, "first one") {
		pp.Printf("Detected 'first one' in response, using first property for contact info\n")
		return getPropertyContactInfo(teamId, []string{propertyIDs[0]})
	}

	// PRIORITY FALLBACK: Use lastDiscussedPropertyID if provided and in the propertyIDs list
	if lastDiscussedPropertyID != "" {
		for _, pid := range propertyIDs {
			if pid == lastDiscussedPropertyID {
				pp.Printf("Using lastDiscussedPropertyID %s for contact info (no property name match in response)\n", lastDiscussedPropertyID)
				return getPropertyContactInfo(teamId, []string{lastDiscussedPropertyID})
			}
		}
	}

	// Default to first property's contact info as fallback
	pp.Printf("No property name match found for contact info, using first property\n")
	return getPropertyContactInfo(teamId, []string{propertyIDs[0]})
}

// getFirstPropertyID retrieves the first property ID from a team's properties
// Used as fallback when no specific properties are matched but we need a default URL
// GetFirstPropertyIDForTeam returns the first property ID for a team, or an
// empty string if the team has no properties. This is the exported counterpart
// of getFirstPropertyID and is used by callers outside this package (e.g. the
// tour-intent deflector in the chat handler).
func GetFirstPropertyIDForTeam(teamId string) string {
	return getFirstPropertyID(teamId)
}

func getFirstPropertyID(teamId string) string {
	// Use the existing GetTeamProperties function to get properties
	properties, err := GetTeamProperties(teamId)
	if err != nil {
		pp.Printf("Error getting team properties for fallback URL: %v\n", err)
		return ""
	}

	if len(properties) == 0 {
		pp.Printf("No properties found for team %s\n", teamId)
		return ""
	}

	// Return the first property's ID
	for _, prop := range properties {
		if id, ok := prop["id"].(string); ok && id != "" {
			pp.Printf("Using first property ID %s as fallback URL\n", id)
			return id
		}
	}

	return ""
}
