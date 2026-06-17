package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"
	"time"

	"bamboo/ai"
	"bamboo/email"
	"bamboo/handlers"
	"bamboo/helpers"
	"bamboo/preference"
	"bamboo/report"
	"bamboo/sms"
	"bamboo/sms/generator"
	"bamboo/types"

	"github.com/k0kubun/pp/v3"
	"github.com/redis/go-redis/v9"
	resend "github.com/resend/resend-go/v2"
	"github.com/sashabaranov/go-openai"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// Redact URLs from old conversation history — links may reference stale properties
var urlRedact = regexp.MustCompile(`https?://\S+`)

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

func check(err error, tweakOut bool) {
	if err != nil {
		fmt.Printf("\x1b[31mError: %v\x1b[0m\n", err)
		report.InsertError(string(err.Error()))
		if tweakOut {
			panic(err)
		}
	}
}

// Helper function to get client IP
func getClientIP(r *http.Request) string {
	return helpers.GetClientIP(r)
}

// default per ip ..20 per minute
func checkRateLimit(ip string, limit int, window time.Duration) (bool, error) {
	// Skip rate limiting in local development (FLY_APP_NAME is set by Fly.io in production)
	if os.Getenv("FLY_APP_NAME") == "" {
		fmt.Println("Rate limiting: using local (skipped)")
		return true, nil
	}
	fmt.Println("Rate limiting: using prod (Redis)")

	var ctx = context.Background()

	opt, err := redis.ParseURL(os.Getenv("REDIS_URL"))
	if err != nil {
		return false, err
	}
	client := redis.NewClient(opt)
	defer client.Close()

	key := fmt.Sprintf("rate_limit:%s", ip)

	// Get current count
	count, err := client.Get(ctx, key).Int()
	if err != nil && err != redis.Nil {
		return false, err
	}

	// If count exceeds limit, reject request
	if count >= limit {
		return false, nil
	}

	// Increment counter
	pipe := client.Pipeline()
	pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, window)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return false, err
	}

	return true, nil
}

// Function to block user from api.
func blockUser(w *http.ResponseWriter) {
	(*w).WriteHeader(http.StatusForbidden)
	(*w).Header().Set("Content-Type", "application/json")
	json.NewEncoder(*w).Encode(map[string]string{
		"status":  "error",
		"message": "User blocked",
	})
}

func allowCors(w *http.ResponseWriter) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	(*w).Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
}

// Function to handle rate limit exceeded
func rateLimitExceeded(w http.ResponseWriter) {
	w.WriteHeader(http.StatusTooManyRequests)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "error",
		"message": "Rate limit exceeded",
	})
}

// Public - Health handler returns a simple health status
func healthHandler(w http.ResponseWriter, r *http.Request) {
	// Rate limiting
	ip := getClientIP(r)
	allowed, err := checkRateLimit(ip, 5, time.Second)
	check(err, false)
	if !allowed {
		rateLimitExceeded(w)
		return
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"time":   time.Now().String(),
	})
}

func eventsHandler(w http.ResponseWriter, r *http.Request) {
	allowCors(&w)

	// Rate limiting
	ip := getClientIP(r)
	allowed, err := checkRateLimit(ip, 5, time.Second)
	check(err, false)
	if !allowed {
		rateLimitExceeded(w)
		return
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var eventData struct {
		Type          string `json:"type"`
		SessionId     string `json:"sessionId"`
		Timestamp     string `json:"timestamp"`
		PropertyId    string `json:"propertyId,omitempty"`
		PropertyTitle string `json:"propertyTitle,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&eventData); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Invalid JSON payload",
		})
		return
	}

	// Parse timestamp
	timestamp, err := time.Parse(time.RFC3339, eventData.Timestamp)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Invalid timestamp format",
		})
		return
	}
	propertyId := ""
	if eventData.PropertyId != "" {
		propertyId = eventData.PropertyId
	}

	propertyTitle := ""
	if eventData.PropertyTitle != "" {
		propertyTitle = eventData.PropertyTitle
	}

	err = helpers.StoreEvent(eventData.Type, eventData.SessionId, timestamp, propertyId, propertyTitle)
	check(err, false)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Failed to save event",
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"time":   time.Now().String(),
	})
}

// Public
func hiHandler(w http.ResponseWriter, r *http.Request) {
	allowCors(&w)

	// Rate limiting
	// ip := getClientIP(r)
	// allowed, err := checkRateLimit(ip, 5, time.Second)
	// if err != nil || !allowed {
	// 	rateLimitExceeded(w)
	// 	return
	// }

	http.Redirect(w, r, "https://rentbamboo.com", http.StatusMovedPermanently)
}

// Public
func handleSMS(w http.ResponseWriter, r *http.Request) {
	allowCors(&w)

	// Rate limiting
	ip := getClientIP(r)
	allowed, err := checkRateLimit(ip, 20, time.Second)
	check(err, false)
	if !allowed {
		rateLimitExceeded(w)
		return
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var messageData struct {
		From    string `json:"from"`
		To      string `json:"to"`
		Message string `json:"message"`
		TeamId  string `json:"teamId"`
	}

	if err := json.NewDecoder(r.Body).Decode(&messageData); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Invalid JSON payload",
		})
		return
	}

	// If teamId is not provided, look it up from the "to" phone number
	if messageData.TeamId == "" {
		if messageData.To == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "error",
				"message": "Missing required field: teamId or to (phone number)",
			})
			return
		}

		// Look up teamId from phone number
		pp.Printf("\x1b[36mhandleSMS: No teamId provided, looking up from phone number %s\x1b[0m\n", messageData.To)
		teamId, err := sms.GetTeamIdByPhoneNumber(messageData.To)
		if err != nil {
			pp.Printf("\x1b[31mhandleSMS: Failed to get teamId from phone number: %v\x1b[0m\n", err)
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{
				"status":  "error",
				"message": "Could not determine team from phone number. Please provide teamId.",
			})
			return
		}
		messageData.TeamId = teamId
		pp.Printf("\x1b[32mhandleSMS: Resolved teamId %s from phone number\x1b[0m\n", teamId)
	}

	// Get OpenAI API key from environment
	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey == "" {
		fmt.Printf("\x1b[31mError: OpenAI API key not found in environment variables\x1b[0m\n")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "OpenAI API key not configured",
		})
		return
	}

	// OpenAI client - no longer needed for SMS (using generator package)
	_ = openai.NewClient(openaiKey)

	// Get team properties
	properties, err := helpers.GetTeamProperties(messageData.TeamId)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Failed to get team properties",
		})
		return
	}

	// SKIP EMBEDDINGS FOR SMS - Use strict MongoDB filtering only (much faster)
	// Embeddings add ~2-4 seconds of latency but are not used for SMS filtering
	// We use strict preference filtering + property summarizer instead
	/*
		qa, err := helpers.ProcessPropertiesWithCaching(messageData.TeamId, properties, openaiClient)
		if err != nil {
			pp.Printf("\x1b[33mWarning: Error processing properties: %v\x1b[0m\n", err)
			// Create empty QA system as fallback
			qa = ai.NewQASystemWithTeam(messageData.TeamId)
		}
		pp.Printf("\x1b[32mSMS Handler: Using QA system with %d documents for team %s\x1b[0m\n", qa.GetDocumentCount(), messageData.TeamId)
	*/

	// QA system - no longer needed for SMS (using generator package)
	_ = ai.NewQASystemWithTeam(messageData.TeamId)

	// Get chat context using SMS package
	chat, err := sms.GetMessagesBetweenPhoneNumbers(messageData.From, messageData.To)
	check(err, false)

	if err != nil {
		fmt.Printf("\x1b[31mError: %v\x1b[0m\n", err)
		return
	}

	// Build chat thread from messages — skip empty messages and
	// deduplicate consecutive identical outbound messages (template blasts).
	// URLs in old messages are redacted as they may reference stale properties.
	// Threads are grouped by turn (consecutive AI/Lead messages).
	// Human-sent outbound messages (SentBy != "") are labeled "Team:" so the
	// AI can distinguish them from its own prior replies (HITL awareness).
	var thread string
	var lastAIReply string
	if len(chat) > 0 {
		type turn struct {
			humanLines []string
			aiLines    []string
			leadLines  []string
		}
		var turns []turn
		current := &turn{}
		prevOutboundBody := ""

		flush := func() {
			if len(current.humanLines) > 0 || len(current.aiLines) > 0 || len(current.leadLines) > 0 {
				turns = append(turns, *current)
			}
			current = &turn{}
		}

		for _, message := range chat {
			body := urlRedact.ReplaceAllString(message.Body, "[stale-link]")
			if strings.TrimSpace(body) == "" {
				continue
			}
			if message.Direction == "outbound" {
				if len(current.leadLines) > 0 {
					flush()
				}
				if body == prevOutboundBody {
					continue
				}
				prevOutboundBody = body
				// HITL: human-sent outbound messages get the "Team:" label
				// (they have a non-empty SentBy field) so the AI knows they
				// were sent by a human agent, not by the AI itself.
				if message.SentBy != "" {
					current.humanLines = append(current.humanLines, body)
				} else {
					current.aiLines = append(current.aiLines, body)
					lastAIReply = body
				}
			} else {
				prevOutboundBody = ""
				current.leadLines = append(current.leadLines, body)
			}
		}
		flush()

		for i, t := range turns {
			thread += fmt.Sprintf("[Turn %d]\n", i+1)
			for _, line := range t.humanLines {
				thread += "Team: " + line + "\n"
			}
			for _, line := range t.aiLines {
				thread += "AI: " + line + "\n"
			}
			for _, line := range t.leadLines {
				thread += "Lead: " + line + "\n"
			}
			thread += "\n"
		}
	}

	// Generate session ID from phone numbers for preference tracking
	sessionId := messageData.From + "-" + messageData.To

	// ===== STEP 0: Look up lead by phone number =====
	var currentLeadID string
	var currentLeadStatus string
	lead, err := getLeadByPhone(messageData.TeamId, messageData.From)
	if err != nil {
		pp.Printf("\x1b[33mhandleSMS: Failed to look up lead by phone: %v\x1b[0m\n", err)
	} else if lead != nil {
		currentLeadID = lead.ID
		currentLeadStatus = lead.Status
		pp.Printf("\x1b[32mhandleSMS: Found lead %s for phone %s (status: %s)\x1b[0m\n", lead.ID, messageData.From, lead.Status)
		if lead.FirstName != "" {
			pp.Printf("\x1b[32mhandleSMS: Lead name: %s %s\x1b[0m\n", lead.FirstName, lead.LastName)
		}

		// ===== STEP 0.5: Check if lead status is active =====
		// Only respond if status is one of: interested, nurture, tour scheduled, application
		// If status is anything else (closed won, closed lost, custom stages, UUIDs), skip entirely
		if !isActiveLeadStatus(currentLeadStatus) {
			pp.Printf("\x1b[33mhandleSMS: Lead status '%s' is not active. Skipping reply entirely.\x1b[0m\n", currentLeadStatus)
			response := map[string]any{
				"status":    "ok",
				"data":      "", // No response - lead is not active
				"context":   "",
				"timestamp": time.Now().String(),
				"skipped":   true,
				"reason":    "lead status not active",
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)
			return
		}
	}

	// ===== STEP 1: Load existing preferences from MongoDB =====
	var currentPreferences preference.Preferences
	prefStore, err := preference.NewStore()
	if err != nil {
		pp.Printf("\x1b[33mhandleSMS: Failed to create preference store: %v\x1b[0m\n", err)
	} else {
		defer prefStore.Close(context.Background())

		// Load existing preferences for this session
		existingPref, err := prefStore.GetBySessionID(context.Background(), sessionId)
		if err != nil {
			pp.Printf("\x1b[33mhandleSMS: Failed to get existing preferences: %v\x1b[0m\n", err)
		} else if existingPref != nil {
			currentPreferences = existingPref.Preferences
			pp.Printf("\x1b[32mhandleSMS: Loaded existing preferences for session %s\x1b[0m\n", sessionId)
			printCurrentPreferences(&currentPreferences)
		}
	}

	// ===== STEP 2: Extract preferences from current SMS message =====
	prefParser := preference.NewParser()
	extracted, err := prefParser.ExtractPreferencesFromText(context.Background(), messageData.Message)
	if err != nil {
		pp.Printf("\x1b[33mhandleSMS: Could not extract preferences: %v\x1b[0m\n", err)
		extracted = &preference.ExtractedPreferences{}
	}

	// Merge with existing in-memory preferences
	newPrefs := preference.MergePreferences(&currentPreferences, extracted)
	currentPreferences = *newPrefs

	// Print extracted preferences
	printExtractedPreferences(extracted)
	printCurrentPreferences(&currentPreferences)

	// ===== STEP 3: Filter properties based on preferences =====
	var filteredPropertyIDs []string
	var resolvedScheduleURLs []string
	var resolvedApplicationURLs []string

	if currentPreferences.HasPreferences() {
		// Use strict preference filtering
		pp.Printf("\x1b[36mhandleSMS: Using STRICT query with preferences: bedrooms=%d, bathrooms=%d, type=%s, budget=%.0f-%.0f\x1b[0m\n",
			currentPreferences.Bedrooms, currentPreferences.Bathrooms, currentPreferences.PropertyType,
			currentPreferences.BudgetMin, currentPreferences.BudgetMax)

		// Check cache first
		cachedProps, found := preference.GetCachedProperties(messageData.TeamId, &currentPreferences)
		if found && len(cachedProps) > 0 {
			pp.Printf("\x1b[32mhandleSMS: Using %d cached properties for preferences\x1b[0m\n", len(cachedProps))
			for _, prop := range cachedProps {
				filteredPropertyIDs = append(filteredPropertyIDs, prop.ID)
				url := helpers.ResolveScheduleURL(messageData.TeamId, prop.ID)
				resolvedScheduleURLs = append(resolvedScheduleURLs, url)
				appURL := helpers.ResolveApplicationURL(messageData.TeamId, prop.ID)
				resolvedApplicationURLs = append(resolvedApplicationURLs, appURL)
			}
		} else {
			// Query MongoDB with strict filters
			filterEngine, err := preference.NewFilterEngine()
			if err == nil {
				ctx := context.Background()
				result, err := filterEngine.FilterByPreferences(ctx, messageData.TeamId, &currentPreferences, 10)
				if err == nil && len(result.Properties) > 0 {
					pp.Printf("\x1b[32mhandleSMS: Found %d properties matching strict preferences\x1b[0m\n", len(result.Properties))

					// Cache the results
					preference.SetCachedProperties(messageData.TeamId, &currentPreferences, result.Properties)

					for _, prop := range result.Properties {
						filteredPropertyIDs = append(filteredPropertyIDs, prop.ID)
						url := helpers.ResolveScheduleURL(messageData.TeamId, prop.ID)
						resolvedScheduleURLs = append(resolvedScheduleURLs, url)
						appURL := helpers.ResolveApplicationURL(messageData.TeamId, prop.ID)
						resolvedApplicationURLs = append(resolvedApplicationURLs, appURL)
					}
				} else {
					pp.Printf("\x1b[33mhandleSMS: No properties found with strict query\x1b[0m\n")
				}
			} else {
				pp.Printf("\x1b[33mhandleSMS: Failed to create filter engine: %v\x1b[0m\n", err)
			}
		}
	} else {
		// No preferences - use first 5 properties (like chat does)
		pp.Printf("\x1b[33mhandleSMS: No preferences specified - using all available properties\x1b[0m\n")
		count := 0
		for _, prop := range properties {
			if count >= 5 {
				break
			}
			if propID, ok := prop["id"].(string); ok {
				filteredPropertyIDs = append(filteredPropertyIDs, propID)
				url := helpers.ResolveScheduleURL(messageData.TeamId, propID)
				resolvedScheduleURLs = append(resolvedScheduleURLs, url)
				appURL := helpers.ResolveApplicationURL(messageData.TeamId, propID)
				resolvedApplicationURLs = append(resolvedApplicationURLs, appURL)
				count++
			}
		}
		pp.Printf("\x1b[32mhandleSMS: Using %d properties (no preference filtering)\x1b[0m\n", len(filteredPropertyIDs))
	}

	// ===== STEP 4: Save preferences to MongoDB =====
	// Save preferences if we have preferences OR if we have a lead ID
	shouldSave := currentPreferences.HasPreferences() || currentLeadID != ""
	if prefStore != nil && shouldSave {
		pp.Printf("\x1b[32mhandleSMS: Saving preferences to MongoDB (leadId: %s)...\x1b[0m\n", currentLeadID)
		leadPref := &preference.LeadPreference{
			SessionID:         sessionId,
			TeamID:            messageData.TeamId,
			LeadID:            currentLeadID, // Now we set the leadId!
			Preferences:       currentPreferences,
			MatchedProperties: filteredPropertyIDs,
		}
		err := prefStore.Upsert(context.Background(), leadPref)
		if err != nil {
			pp.Printf("\x1b[33mhandleSMS: Could not save preferences: %v\x1b[0m\n", err)
		} else {
			pp.Printf("\x1b[32mhandleSMS: Preferences saved to MongoDB with leadId: %s\x1b[0m\n", currentLeadID)
		}
	}

	// ===== SIMPLIFIED: Get lead's property (single source of truth) =====
	var leadPropertyID string
	var originalLeadPropertyName string
	var applicationSending bool
	var tourScheduling bool

	// Get property ID from lead - check nested property object first, then fall back to legacy field
	if lead != nil {
		if lead.Property.ID != "" {
			// New format: nested property object
			leadPropertyID = lead.Property.ID
			originalLeadPropertyName = lead.Property.PropertyName
			pp.Printf("\x1b[32mhandleSMS: Using lead property ID (nested): %s (%s)\x1b[0m\n", leadPropertyID, lead.Property.PropertyName)
		} else if lead.PropertyID != "" {
			// Legacy format: top-level propertyId field
			leadPropertyID = lead.PropertyID
			pp.Printf("\x1b[32mhandleSMS: Using lead property ID (legacy): %s\x1b[0m\n", leadPropertyID)
		}
	}

	// ===== EDGE CASE: Lead asks about a DIFFERENT property by name =====
	// When a lead has a property assigned to them but texts asking about another
	// property the team manages, dynamically switch context to the mentioned property.
	//
	// This handles scenarios like:
	//   Lead assigned to "711 W 22nd St" but texts "Tell me about 309 S. High St."
	var propertySwitchNote string
	if leadPropertyID != "" && len(properties) > 0 {
		mentionedProp := detectPropertyNameInMessage(messageData.Message, properties)
		if mentionedProp != nil {
			mentionedID, ok := (*mentionedProp)["id"].(string)
			mentionedName, nameOk := (*mentionedProp)["propertyName"].(string)
			if ok && nameOk && mentionedID != "" && mentionedID != leadPropertyID {
				pp.Printf("\x1b[33mhandleSMS: Lead asked about DIFFERENT property \"%s\" (id=%s) while assigned to \"%s\" — switching context\x1b[0m\n",
					mentionedName, mentionedID, originalLeadPropertyName)
				leadPropertyID = mentionedID
				propertySwitchNote = fmt.Sprintf(
					"The lead was previously assigned to \"%s\" but is now asking about \"%s\". Use the property context below for \"%s\" — do NOT redirect them back to the old property.",
					originalLeadPropertyName, mentionedName, mentionedName,
				)
			}
		}
	}

	// Filter chat history when property changes — strip old property name
	// references so the AI doesn't conflate old and new property context.
	if propertySwitchNote != "" && originalLeadPropertyName != "" {
		thread = strings.ReplaceAll(thread, originalLeadPropertyName, "[previous property]")
		thread = fmt.Sprintf("[PROPERTY CHANGED: %s]\n%s", propertySwitchNote, thread)
	}

	// Get command center to check applicationSending and tourScheduling flags
	cmdCenter, err := helpers.FetchCommandCenter(messageData.TeamId)
	if err == nil {
		// Extract values from bson.M map
		if appSending, ok := cmdCenter["applicationSending"].(bool); ok {
			applicationSending = appSending
		}
		if tourSched, ok := cmdCenter["tourScheduling"].(bool); ok {
			tourScheduling = tourSched
		}
		pp.Printf("\x1b[32mhandleSMS: Command center - applicationSending: %v, tourScheduling: %v\x1b[0m\n", applicationSending, tourScheduling)
	}

	// ===== Generate AI response using simplified approach =====
	var aiResponse string
	var propertyContext string
	var genErr error

	aiResponse, propertyContext, genErr = sms.GenerateLiveTextResponse(
		thread,
		messageData.Message,
		messageData.TeamId,
		sessionId,
		leadPropertyID,
		applicationSending,
		tourScheduling,
		lead,
		propertySwitchNote,
		lastAIReply,
	)

	if genErr != nil {
		pp.Printf("\x1b[33mhandleSMS: Generator failed: %v\x1b[0m\n", genErr)
		// Fallback response
		aiResponse = "Thanks for your message! I'm here to help. Could you tell me more about what you're looking for?"
	}

	response := map[string]any{
		"status":    "ok",
		"data":      aiResponse,
		"context":   propertyContext,
		"timestamp": time.Now().String(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func teamHandler(w http.ResponseWriter, r *http.Request) {
	allowCors(&w)

	// Rate limiting
	ip := getClientIP(r)
	allowed, err := checkRateLimit(ip, 5, time.Second)
	check(err, false)
	if !allowed {
		rateLimitExceeded(w)
		return
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	// Get clientId from URL query parameters
	clientId := r.URL.Query().Get("clientId")
	if clientId == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Missing clientId parameter",
		})
		return
	}

	// Get team info from the database using clientId
	teamInfo, err := helpers.GetTeamInfoByClientId(clientId)
	check(err, false)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Failed to retrieve team info",
		})
		return
	}

	teamCommandCenter, err := helpers.GetCharlesCommandCenter(teamInfo.TeamID)
	check(err, false)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Failed to retrieve team command center",
		})
		return
	}

	// check cors against domain and origin header
	origin := r.Header.Get("Origin")
	if origin != "" && teamCommandCenter.Domain != "" {
		// Extract domain from origin (remove protocol and path)
		originDomain := strings.TrimPrefix(origin, "https://")
		originDomain = strings.TrimPrefix(originDomain, "http://")

		// Remove port number if present
		if idx := strings.Index(originDomain, ":"); idx != -1 {
			originDomain = originDomain[:idx]
		}

		// Remove path if present
		if idx := strings.Index(originDomain, "/"); idx != -1 {
			originDomain = originDomain[:idx]
		}

		// Remove www. prefix if present for comparison
		originDomain = strings.TrimPrefix(originDomain, "www.")

		// Get team domain and remove www. prefix and port if present
		teamDomain := teamCommandCenter.Domain
		teamDomain = strings.TrimPrefix(teamDomain, "www.")
		if idx := strings.Index(teamDomain, ":"); idx != -1 {
			teamDomain = teamDomain[:idx]
		}

		if originDomain != teamDomain {
			pp.Printf("Blocked Origin: %s (expected domain: %s)\n", origin, teamDomain)
			blockUser(&w)
			return
		}
	}

	response := map[string]any{
		"status": "ok",
		"data": map[string]any{
			"name":               teamInfo.Name,
			"description":        teamInfo.Description,
			"city":               teamInfo.City,
			"state":              teamInfo.State,
			"logoUrl":            teamInfo.LogoURL,
			"domain":             teamCommandCenter.Domain,
			"bot-name":           teamCommandCenter.Name,
			"color":              teamCommandCenter.Color,
			"defaultMessage":     teamCommandCenter.DefaultMessage,
			"align":              teamCommandCenter.Align,
			"customLink":         teamCommandCenter.CustomLink,
			"showCaseProperties": teamCommandCenter.ShowCaseProperty,
			"phoneNumber":        teamInfo.PhoneNumber,
		},
		"time": time.Now().String(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func handleCharlesMessageHandler(w http.ResponseWriter, r *http.Request) {
	allowCors(&w)

	// Rate limiting
	ip := helpers.GetClientIP(r)
	allowed, err := checkRateLimit(ip, 5, time.Second)
	check(err, false)
	if !allowed {
		rateLimitExceeded(w)
		return
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var messageData struct {
		ClientId  string `json:"clientId"`
		Message   string `json:"message"`
		Timestamp string `json:"timestamp"`
		Page      string `json:"page"`
		SessionId string `json:"sessionId"`
	}

	if err := json.NewDecoder(r.Body).Decode(&messageData); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Invalid JSON payload",
		})
		return
	}

	// Get team info from the database using clientId
	teamInfo, err := helpers.GetTeamInfoByClientId(messageData.ClientId)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Failed to get team info",
		})
		return
	}

	teamCommandCenter, err := helpers.GetCharlesCommandCenter(teamInfo.TeamID)
	check(err, false)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Failed to retrieve team command center",
		})
		return
	}

	// check cors against domain and origin header
	origin := r.Header.Get("Origin")
	if origin != "" && teamCommandCenter.Domain != "" {
		// Extract domain from origin (remove protocol and path)
		originDomain := strings.TrimPrefix(origin, "https://")
		originDomain = strings.TrimPrefix(originDomain, "http://")

		// Remove port number if present
		if idx := strings.Index(originDomain, ":"); idx != -1 {
			originDomain = originDomain[:idx]
		}

		// Remove path if present
		if idx := strings.Index(originDomain, "/"); idx != -1 {
			originDomain = originDomain[:idx]
		}

		// Remove www. prefix if present for comparison
		originDomain = strings.TrimPrefix(originDomain, "www.")

		// Get team domain and remove www. prefix and port if present
		teamDomain := teamCommandCenter.Domain
		teamDomain = strings.TrimPrefix(teamDomain, "www.")
		if idx := strings.Index(teamDomain, ":"); idx != -1 {
			teamDomain = teamDomain[:idx]
		}

		if originDomain != teamDomain {
			pp.Printf("Blocked Origin: %s (expected domain: %s)\n", origin, teamDomain)
			blockUser(&w)
			return
		}
	}

	// Save the incoming message
	timestamp, _ := time.Parse(time.RFC3339, messageData.Timestamp)
	err = helpers.SaveMessage(messageData.Message, messageData.ClientId, timestamp, messageData.Page, messageData.SessionId, teamInfo.TeamID, ip, "incoming", "", []string{}, []string{})
	check(err, false)

	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Failed to save message",
		})
		return
	}

	// Get chat history
	chatHistory, err := helpers.GetChatBySessionId(messageData.SessionId)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Failed to get chat history",
		})
		return
	}

	// Get OpenAI API key from environment
	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey == "" {
		fmt.Printf("\x1b[31mError: OpenAI API key not found in environment variables\x1b[0m\n")
		return
	}

	// Initialize OpenAI client
	openaiClient := openai.NewClient(openaiKey)

	// We need to do some ai generation / lead extraction on the 2nd 4th 6th message for the session id.
	messageCount := len(chatHistory)
	shouldRunAI := messageCount == 2 || messageCount == 4 || messageCount == 6

	// Build chat thread from messages
	var chatThread string
	for _, message := range chatHistory {
		if body, ok := message["message"].(string); ok {
			propCtx := ""
			if pctx, ok := message["propertyContext"].(string); ok {
				propCtx = pctx
			}
			chatThread += body + " " + propCtx + "\n"
		}
	}

	// Perform lead extraction in background if we're at specific message counts
	if shouldRunAI {
		go func() {
			pp.Println("Ran Lead Extraction")

			title, description, leadJSON, err := helpers.GetChatSummary(chatThread, messageData.SessionId)
			if err != nil {
				pp.Printf("Error extracting chat summary: %v\n", err)
				check(err, false)
			} else {
				pp.Printf("Chat Summary - Title: %s, Description: %s, Lead: %s\n", title, description, leadJSON)

				// If this is the 2nd message, save alert to db with title and description
				if messageCount == 2 {
					// New Chat Notification.
					// Send notification to team members about new chat session
					err = helpers.HandleCharlesNotification(teamInfo.TeamID, messageData.SessionId, title, description)
					if err != nil {
						pp.Printf("Failed to send new chat notification: %v\n", err)
						check(err, false)
					}

					// Report telemetry for new chat
					go func() {
						helpers.ReportInternalAnonTelemetry("Charles New Chat", title, description, teamInfo.TeamID)
					}()
				}
			}
		}()
	}

	// ===== Hours-intent deflector =====
	// If the lead is asking about office hours, business hours, etc., we redirect
	// them to the scheduling page (which shows real availability) rather than
	// letting the AI hallucinate incorrect hours.
	//   - tourScheduling ON  -> direct them to the scheduling link
	//   - tourScheduling OFF -> tell them a team member will reach out
	if helpers.DetectHoursIntent(messageData.Message) {
		pp.Printf("\x1b[35mhandleCharlesMessageHandler: Hours-intent deflector triggered\x1b[0m\n")

		tourScheduling := teamCommandCenter.TourScheduling

		var scheduleURL string
		var deflectionScheduleUrls []string
		if tourScheduling {
			if firstPropID := helpers.GetFirstPropertyIDForTeam(teamInfo.TeamID); firstPropID != "" {
				scheduleURL = helpers.ResolveScheduleURL(teamInfo.TeamID, firstPropID)
				if scheduleURL != "" {
					deflectionScheduleUrls = append(deflectionScheduleUrls, scheduleURL)
				}
			}
		}

		deflectionPlain := helpers.HoursDeflectionResponse(tourScheduling, scheduleURL)
		deflectionHTML := "<p>" + deflectionPlain + "</p>"

		response := map[string]any{
			"status":       "ok",
			"data":         deflectionHTML,
			"photos":       []string{},
			"scheduleUrls": deflectionScheduleUrls,
			"timestamp":    time.Now().String(),
			"deflected":    "hours-intent",
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)

		// Persist the deflection reply.
		go func() {
			if err := helpers.SaveMessage(deflectionHTML, messageData.ClientId, time.Now(), messageData.Page, messageData.SessionId, teamInfo.TeamID, "bot", "outgoing", "", []string{}, deflectionScheduleUrls); err != nil {
				pp.Printf("Failed to save hours-intent deflection reply: %v\n", err)
			}
		}()

		return
	}

	// Generate AI response
	aiResponse, propertyCtx, photos, scheduleUrls, err := helpers.GenerateAIResponseCharles(openaiClient, chatThread, messageData.Message, teamInfo.TeamID)

	if err != nil {
		pp.Println(err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Failed to generate AI response",
		})
		return
	}

	// Send reply immediately
	response := map[string]any{
		"status":       "ok",
		"data":         aiResponse,
		"photos":       photos,
		"scheduleUrls": scheduleUrls,
		"timestamp":    time.Now().String(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)

	// Do everything else after sending the reply
	go func() {
		// Save the AI response
		err = helpers.SaveMessage(aiResponse, messageData.ClientId, time.Now(), messageData.Page, messageData.SessionId, teamInfo.TeamID, "bot", "outgoing", propertyCtx, photos, scheduleUrls)
		if err != nil {
			pp.Printf("Failed to save AI response: %v\n", err)
			check(err, false)
		}

		// Extract property ID from context if available
		propertyId := ""
		// Try to extract property ID from matched property IDs (first one)
		// The GenerateAIResponseCharles function returns matchedPropertyIDs internally
		// We'll need to parse propertyCtx or extract from photos/scheduleUrls
		if len(scheduleUrls) > 0 {
			// Try to extract property ID from first schedule URL
			// Schedule URLs format: /schedule/{teamId}/{propertyId}
			for _, url := range scheduleUrls {
				if strings.Contains(url, "/schedule/") {
					parts := strings.Split(url, "/")
					if len(parts) >= 4 {
						propertyId = parts[3]
						break
					}
				}
			}
		}

		// Send telemetry for AI reply
		err = helpers.ReportAIReplyTelemetry(aiResponse, messageData.Message, teamInfo.TeamID, messageData.SessionId, propertyId)
		if err != nil {
			pp.Printf("\x1b[33mFailed to send AI reply telemetry: %v\x1b[0m\n", err)
		}
	}()
}

// handleLeadFormSubmission handles incoming lead form submissions from the widget
func handleLeadFormSubmission(w http.ResponseWriter, r *http.Request) {
	allowCors(&w)

	// Rate limiting
	ip := getClientIP(r)
	allowed, err := checkRateLimit(ip, 10, time.Minute)
	check(err, false)
	if !allowed {
		rateLimitExceeded(w)
		return
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var submission types.LeadFormSubmission
	if err := json.NewDecoder(r.Body).Decode(&submission); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Invalid JSON payload",
		})
		return
	}

	// Validate required fields
	if submission.ClientID == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Client ID is required",
		})
		return
	}

	if submission.Email == "" && submission.Phone == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Email or phone is required",
		})
		return
	}

	if submission.OutreachPreference == "" {
		submission.OutreachPreference = "both" // Default to both
	}

	// Validate outreach preference
	validPreferences := map[string]bool{"sms": true, "email": true, "both": true}
	if !validPreferences[submission.OutreachPreference] {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Invalid outreach preference. Must be 'sms', 'email', or 'both'",
		})
		return
	}

	// Process the lead form submission
	lead, err := helpers.HandleLeadFormSubmission(&submission)
	if err != nil {
		pp.Printf("\x1b[31mError processing lead form: %v\x1b[0m\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Failed to process lead form submission",
		})
		return
	}

	pp.Printf("\x1b[32mLead form submission processed successfully: %s\x1b[0m\n", lead.ID)

	// Send automated outreach (email + SMS) asynchronously
	go sendLeadFollowUpOutreach(&submission, lead.TeamID)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"status":  "ok",
		"message": "Lead form submitted successfully",
		"leadId":  lead.ID,
	})
}

// sendLeadFollowUpOutreach sends automated email and SMS follow-up to the lead
func sendLeadFollowUpOutreach(submission *types.LeadFormSubmission, teamID string) {
	teamInfo, err := helpers.GetTeamInfoByTeamId(teamID)
	if err != nil {
		pp.Printf("\x1b[33mhandleLeadFormSubmission: failed to get team info for outreach: %v\x1b[0m\n", err)
		return
	}

	teamName := teamInfo.Name
	if teamName == "" {
		teamName = "Our Team"
	}

	propertyName := submission.PropertyName
	if propertyName == "" {
		propertyName = "our property"
	}

	if submission.Email != "" {
		sendLeadFollowUpEmail(submission, teamName, propertyName)
	}

	if submission.Phone != "" && (submission.OutreachPreference == "both" || submission.OutreachPreference == "sms") {
		sendLeadFollowUpSMS(submission, teamID, teamName, propertyName)
	}
}

func sendLeadFollowUpEmail(submission *types.LeadFormSubmission, teamName string, propertyName string) {
	apiKey := os.Getenv("RESEND_API_KEY")
	if apiKey == "" {
		pp.Printf("\x1b[33msendLeadFollowUpEmail: RESEND_API_KEY not set, skipping\x1b[0m\n")
		return
	}

	firstName := submission.FirstName
	if firstName == "" {
		firstName = "there"
	}

	plainBody := fmt.Sprintf(
		"Hi %s,\n\nThanks for reaching out about %s!\n\nAn agent from %s will be in touch shortly to help you with any questions.\n\nBest,\n%s",
		firstName, propertyName, teamName, teamName,
	)

	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head><meta charset="UTF-8"></head>
<body style="font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;background-color:#f4f4f8;margin:0;padding:20px;">
<div style="max-width:500px;margin:0 auto;background-color:#ffffff;border-radius:8px;overflow:hidden;box-shadow:0 2px 10px rgba(0,0,0,0.1);">
<div style="background-color:#16a34a;padding:24px;text-align:center;">
<h1 style="color:white;margin:0;font-size:20px;font-weight:600;">Thanks for reaching out!</h1>
</div>
<div style="padding:24px;">
<p style="font-size:16px;color:#374151;margin:0 0 12px 0;">Hi %s,</p>
<p style="font-size:16px;color:#374151;margin:0 0 12px 0;">Thanks for your interest in <strong>%s</strong>!</p>
<p style="font-size:16px;color:#374151;margin:0 0 24px 0;">An agent from %s will be in touch shortly to answer your questions and help you schedule a tour.</p>
<p style="font-size:14px;color:#6b7280;margin:0;">Best,<br/>%s</p>
</div>
</div>
</body>
</html>`,
		firstName, propertyName, teamName, teamName,
	)

	client := resend.NewClient(apiKey)
	params := &resend.SendEmailRequest{
		From:    "RentBamboo <notifications@rentbamboo.com>",
		To:      []string{submission.Email},
		Subject: fmt.Sprintf("Thanks for your interest, %s!", firstName),
		Text:    plainBody,
		Html:    htmlBody,
	}

	if _, err := client.Emails.Send(params); err != nil {
		pp.Printf("\x1b[31msendLeadFollowUpEmail: failed to send to %s: %v\x1b[0m\n", submission.Email, err)
	} else {
		pp.Printf("\x1b[32msendLeadFollowUpEmail: sent to %s\x1b[0m\n", submission.Email)
	}
}

func sendLeadFollowUpSMS(submission *types.LeadFormSubmission, teamID string, teamName string, propertyName string) {
	smsConfig, err := sms.GetTeamSMSConfig(teamID)
	if err != nil {
		pp.Printf("\x1b[33msendLeadFollowUpSMS: no SMS config for team %s: %v\x1b[0m\n", teamID, err)
		return
	}

	firstName := submission.FirstName
	if firstName == "" {
		firstName = "there"
	}

	message := fmt.Sprintf("Hi %s! Thanks for your interest in %s. An agent from %s will be in touch soon.", firstName, propertyName, teamName)

	_, err = sms.SendSMS(submission.Phone, smsConfig.PhoneNumber, message, true, teamID)
	if err != nil {
		pp.Printf("\x1b[31msendLeadFollowUpSMS: failed to send to %s: %v\x1b[0m\n", submission.Phone, err)
	} else {
		pp.Printf("\x1b[32msendLeadFollowUpSMS: sent to %s\x1b[0m\n", submission.Phone)
	}
}

func getScript(w http.ResponseWriter, r *http.Request) {
	// Rate limiting
	ip := getClientIP(r)
	allowed, err := checkRateLimit(ip, 5, time.Second)
	check(err, false)
	if !allowed {
		rateLimitExceeded(w)
		return
	}

	// Default version if none specified
	// version := "7-20-25" // 001
	// version := "7-21-25" // 002
	// version := "8-1-25" // 003
	// version := "8-19-25" // 004
	// version := "9-17-25"  // 005
	// version := "10-13-25" // 006
	// version := "10-27-25" // 007
	// version := "11-26-25" // 008
	// version := "12-8-25" // 009
	// version := "12-9-25"  // 0010
	// version := "12-10-25" // 0011
	// version := "12-10-25-v2" // 0012
	// version := "12-11-25" // 0013
	// version := "12-11-25-v2" // 0014
	// version := "12-28-25" // 0015
	// version := "12-28-25-v2" // 0016
	// version := "2-16-26" // 0017
	// version := "3-19-26" // 0018
	// version := "3-23-26" // 0019
	// version := "3-29-26" // 0020
	// version := "5-7-26" // 0021
	version := "5-31-26" // 0021

	// Construct filename dynamically based on version
	filename := fmt.Sprintf("./scripts/charles-%s.js", version)

	// Read the JavaScript file from the scripts directory
	jsContent, err := os.ReadFile(filename)
	check(err, false)
	if err != nil {
		fmt.Printf("\x1b[31mgetScript: Failed to read script file %s: %v\x1b[0m\n", filename, err)
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Failed to read script file",
		})
		return
	}

	fmt.Printf("\x1b[32mgetScript: Successfully served script %s\x1b[0m\n", filename)

	// Set content type to JavaScript
	w.Header().Set("Content-Type", "application/javascript")
	w.WriteHeader(http.StatusOK)
	w.Write(jsContent)
}

// chatHandler serves the chat frontend
func chatHandler(w http.ResponseWriter, r *http.Request) {
	// Read the HTML file
	htmlContent, err := os.ReadFile("./frontend/index.html")
	if err != nil {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	// Set content type and cache headers
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)
	w.Write(htmlContent)
}

// loadEnv loads environment variables from .env file
func loadEnv() {
	// Read .env file manually
	data, err := os.ReadFile(".env")
	if err != nil {
		return // No .env file, that's fine
	}

	// Parse each line as KEY=VALUE
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Find the first = sign
		if idx := strings.Index(line, "="); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			// Remove quotes if present
			value = strings.Trim(value, `"`)
			os.Setenv(key, value)
		}
	}
}

// getLeadByPhone looks up a lead by phone number (normalized for +1 prefix)
func getLeadByPhone(teamId, phoneNumber string) (*types.Lead, error) {
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

	// Normalize phone number: remove +1 prefix for comparison
	// Lead phones in DB don't have +1 (e.g., "5957673317")
	// SMS From has +1 (e.g., "+12102743516")
	normalizedPhone := phoneNumber
	if strings.HasPrefix(phoneNumber, "+1") {
		normalizedPhone = strings.TrimPrefix(phoneNumber, "+1")
	} else if strings.HasPrefix(phoneNumber, "1") && len(phoneNumber) > 10 {
		normalizedPhone = strings.TrimPrefix(phoneNumber, "1")
	}

	pp.Printf("\x1b[36mgetLeadByPhone: Looking for lead with phone %s (normalized from %s)\x1b[0m\n", normalizedPhone, phoneNumber)

	collection := client.Database("teams").Collection("leads")

	// Query by teamId AND phone (normalized)
	filter := bson.M{
		"teamId": teamId,
		"phone":  normalizedPhone,
	}

	var lead types.Lead
	var rawLead bson.M
	// Decode into bson.M first (to get the raw document for defensive
	// date normalization), then into the typed struct. Some old leads
	// have askedAt/answeredAt stored as ISO strings instead of BSON
	// dates, so we need access to the raw values to normalize them.
	err = collection.FindOne(ctx, filter).Decode(&rawLead)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			pp.Printf("\x1b[33mgetLeadByPhone: No lead found for phone %s in team %s\x1b[0m\n", normalizedPhone, teamId)
			return nil, nil
		}
		pp.Printf("\x1b[31mgetLeadByPhone: Error finding lead: %v\x1b[0m\n", err)
		return nil, fmt.Errorf("error finding lead: %w", err)
	}
	// Now decode the raw bson.M into the struct.
	if err := decodeLeadFromBson(rawLead, &lead); err != nil {
		pp.Printf("\x1b[31mgetLeadByPhone: Error decoding lead struct: %v\x1b[0m\n", err)
		return nil, fmt.Errorf("error decoding lead: %w", err)
	}

	// Defensive: normalize any string-format dates in the lead's
	// qualification questions to *time.Time. Some old data was saved
	// as ISO strings via JSON round-trips; this makes the in-memory
	// representation consistent so subsequent writes persist as
	// BSON dates. Over time, old string data gets rewritten.
	generator.NormalizeQualificationDates(&lead, rawLead)

	pp.Printf("\x1b[32mgetLeadByPhone: Found lead %s for phone %s\x1b[0m\n", lead.ID, normalizedPhone)
	return &lead, nil
}

// decodeLeadFromBson converts a bson.M to a types.Lead struct by
// re-marshaling to JSON and unmarshaling. This is more tolerant of
// mixed-type fields (e.g., dates as strings vs BSON dates) than
// direct bson.Unmarshal.
func decodeLeadFromBson(raw bson.M, lead *types.Lead) error {
	data, err := bson.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, lead)
}

// printExtractedPreferences prints newly extracted preferences
func printExtractedPreferences(extracted *preference.ExtractedPreferences) {
	fmt.Println("   🎯 Newly Extracted Preferences:")
	hasAny := false

	if extracted.Bedrooms > 0 {
		fmt.Printf("   - Bedrooms: %d\n", extracted.Bedrooms)
		hasAny = true
	}
	if extracted.Bathrooms > 0 {
		fmt.Printf("   - Bathrooms: %d\n", extracted.Bathrooms)
		hasAny = true
	}
	if extracted.BudgetMin > 0 || extracted.BudgetMax > 0 {
		fmt.Printf("   - Budget: $%.0f-$%.0f\n", extracted.BudgetMin, extracted.BudgetMax)
		hasAny = true
	}
	if len(extracted.Locations) > 0 {
		fmt.Printf("   - Locations: %v\n", extracted.Locations)
		hasAny = true
	}
	if len(extracted.PetNeeds) > 0 {
		fmt.Printf("   - Pet Needs: %v\n", extracted.PetNeeds)
		hasAny = true
	}
	if len(extracted.Amenities) > 0 {
		fmt.Printf("   - Amenities: %v\n", extracted.Amenities)
		hasAny = true
	}
	if extracted.PropertyType != "" {
		fmt.Printf("   - Property Type: %s\n", extracted.PropertyType)
		hasAny = true
	}
	if extracted.SquareFootageMin > 0 || extracted.SquareFootageMax > 0 {
		fmt.Printf("   - Square Footage: %.0f-%.0f sqft\n", extracted.SquareFootageMin, extracted.SquareFootageMax)
		hasAny = true
	}
	if extracted.MoveInDate != "" {
		fmt.Printf("   - Move-in Date: %s\n", extracted.MoveInDate)
		hasAny = true
	}
	if extracted.SortBy != "" {
		fmt.Printf("   - Sort By: %s\n", extracted.SortBy)
		hasAny = true
	}

	if !hasAny {
		fmt.Println("   - No specific preferences found in this message")
	}
}

// printCurrentPreferences prints current accumulated preferences
func printCurrentPreferences(prefs *preference.Preferences) {
	if !prefs.HasPreferences() {
		fmt.Println("   📊 Current Preferences: None yet")
		return
	}

	fmt.Println("   📊 Current Conversation Preferences:")
	if prefs.Bedrooms > 0 {
		fmt.Printf("   - Bedrooms: %d\n", prefs.Bedrooms)
	}
	if prefs.Bathrooms > 0 {
		fmt.Printf("   - Bathrooms: %d\n", prefs.Bathrooms)
	}
	if prefs.BudgetMin > 0 || prefs.BudgetMax > 0 {
		fmt.Printf("   - Budget: $%.0f-$%.0f\n", prefs.BudgetMin, prefs.BudgetMax)
	}
	if len(prefs.Locations) > 0 {
		fmt.Printf("   - Locations: %v\n", prefs.Locations)
	}
	if len(prefs.PetNeeds) > 0 {
		fmt.Printf("   - Pet Needs: %v\n", prefs.PetNeeds)
	}
	if len(prefs.Amenities) > 0 {
		fmt.Printf("   - Amenities: %v\n", prefs.Amenities)
	}
	if prefs.PropertyType != "" {
		fmt.Printf("   - Property Type: %s\n", prefs.PropertyType)
	}
	if prefs.SquareFootageMin > 0 || prefs.SquareFootageMax > 0 {
		fmt.Printf("   - Square Footage: %.0f-%.0f sqft\n", prefs.SquareFootageMin, prefs.SquareFootageMax)
	}
	if prefs.MoveInDate != nil {
		fmt.Printf("   - Move-in Date: %s\n", prefs.MoveInDate.Format("2006-01-02"))
	}
	if prefs.SortBy != "" {
		fmt.Printf("   - Sort By: %s\n", prefs.SortBy)
	}
}

// isActiveLeadStatus checks if the lead status is active (should receive replies)
// Only these statuses are active: interested, nurture, tour scheduled, application
// All other statuses (closed won, closed lost, custom stages, UUIDs, etc.) are inactive
func isActiveLeadStatus(status string) bool {
	if status == "" {
		// No status = treat as active (allow reply)
		return true
	}

	// Normalize: lowercase and trim whitespace
	normalizedStatus := strings.ToLower(strings.TrimSpace(status))

	// Define active statuses (lowercase for comparison)
	activeStatuses := map[string]bool{
		"interested":     true,
		"new":            true,
		"nurture":        true,
		"tour scheduled": true,
		"application":    false,
	}

	// Check if it's a UUID (custom status) - UUIDs are 36 characters with hyphens
	// or 32 characters without hyphens, containing only hex digits and hyphens
	if len(normalizedStatus) == 36 || len(normalizedStatus) == 32 {
		// Check if it looks like a UUID (hex digits and optional hyphens)
		isUUID := true
		for _, ch := range normalizedStatus {
			if !((ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || ch == '-') {
				isUUID = false
				break
			}
		}
		if isUUID {
			return false // UUID statuses are not active
		}
	}

	return activeStatuses[normalizedStatus]
}

// handleGetProperties handles GET /properties?clientId=xxx
// Returns public, vacant properties for the team identified by clientId.
func handleGetProperties(w http.ResponseWriter, r *http.Request) {
	allowCors(&w)

	// Rate limiting: 10 req/second
	ip := getClientIP(r)
	allowed, err := checkRateLimit(ip, 10, time.Second)
	check(err, false)
	if !allowed {
		rateLimitExceeded(w)
		return
	}

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	clientId := r.URL.Query().Get("clientId")
	if clientId == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "clientId query parameter is required",
		})
		return
	}

	teamInfo, err := helpers.GetTeamInfoByClientId(clientId)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": fmt.Sprintf("Failed to get team info: %v", err),
		})
		return
	}
	if teamInfo.TeamID == "" {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": "Team not found for the given clientId",
		})
		return
	}

	properties, err := helpers.GetTeamProperties(teamInfo.TeamID)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status":  "error",
			"message": fmt.Sprintf("Failed to get properties: %v", err),
		})
		return
	}

	// Sanitize each property: remove sensitive fields before returning
	sensitiveFields := []string{"teamId", "leadOwner", "_id"}
	for _, prop := range properties {
		for _, field := range sensitiveFields {
			delete(prop, field)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]any{
		"status": "ok",
		"data":   properties,
	})
}

// detectPropertyNameInMessage checks if the incoming message text contains
// the name of any team property (other than the lead's assigned one).
// Performs case-insensitive matching against property names and address parts.
// Returns the matching property bson.M or nil if none found.
func detectPropertyNameInMessage(message string, properties []bson.M) *bson.M {
	lowerMsg := strings.ToLower(strings.TrimSpace(message))
	if lowerMsg == "" {
		return nil
	}

	for i := range properties {
		prop := properties[i]

		// Check propertyName field
		if nameRaw, ok := prop["propertyName"].(string); ok && nameRaw != "" {
			lowerName := strings.ToLower(strings.TrimSpace(nameRaw))
			if lowerName != "" && strings.Contains(lowerMsg, lowerName) {
				return &prop
			}
		}

		// Also check address fields for partial matches
		if loc, ok := prop["location"].(bson.M); ok {
			// Check full address
			if addr, ok := loc["fullAddress"].(string); ok && addr != "" {
				lowerAddr := strings.ToLower(strings.TrimSpace(addr))
				if lowerAddr != "" && strings.Contains(lowerMsg, lowerAddr) {
					return &prop
				}
			}
			// Check street address
			if street, ok := loc["streetAddress"].(string); ok && street != "" {
				lowerStreet := strings.ToLower(strings.TrimSpace(street))
				if lowerStreet != "" && strings.Contains(lowerMsg, lowerStreet) {
					return &prop
				}
			}
		}
	}

	return nil
}

// handlePropertyUpdated clears caches when a property is created or updated.
// Called by the frontend after property save. Auth via X-Internal-Secret header.
func handlePropertyUpdated(w http.ResponseWriter, r *http.Request) {
	allowCors(&w)
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	secret := r.Header.Get("X-Internal-Secret")
	expected := os.Getenv("INTERNAL_API_SECRET")
	if expected == "" || secret != expected {
		pp.Printf("\x1b[31mhandlePropertyUpdated: invalid X-Internal-Secret\x1b[0m\n")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "unauthorized"})
		return
	}

	var body struct {
		TeamId     string `json:"teamId"`
		PropertyId string `json:"propertyId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TeamId == "" || body.PropertyId == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "message": "missing teamId or propertyId"})
		return
	}

	sms.InvalidateCaches(body.TeamId, body.PropertyId)
	pp.Printf("\x1b[32mhandlePropertyUpdated: caches invalidated for team %s, property %s\x1b[0m\n", body.TeamId[:8], body.PropertyId[:8])

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func main() {
	// Load environment variables from .env file
	loadEnv()

	// Wait for 1 seconds before starting the server
	pp.Printf("Initializing server... (1 second delay)\n")
	// time.Sleep(1 * time.Second)

	// Initialize email handler (it will manage its own MongoDB connections)
	emailHandler := email.NewHandler(nil)

	// Set up HTTP server
	http.HandleFunc("/", hiHandler)
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/events", eventsHandler)

	// Chat frontend - hardcoded route /chat/5c7
	http.HandleFunc("/chat/5c7", chatHandler)

	// GET team info
	http.HandleFunc("/t", teamHandler)

	// POST
	http.HandleFunc("/message", handleCharlesMessageHandler)

	// POST - Streaming endpoint
	http.HandleFunc("/message/stream", handlers.HandleStreamingMessage)

	// POST
	http.HandleFunc("/sms", handleSMS)

	// GET
	http.HandleFunc("/w", getScript)

	// POST - Lead form submission
	http.HandleFunc("/lead", handleLeadFormSubmission)

	// Email API Routes - All prefixed with /e/
	http.HandleFunc("/e/emails", emailHandler.HandleGetEmails)               // POST - Get all emails and threads
	http.HandleFunc("/e/emails/get", emailHandler.HandleGetEmailsCompatible) // GET/POST
	http.HandleFunc("/e/thread", emailHandler.HandleGetThread)               // POST

	http.HandleFunc("/e/email", emailHandler.HandleDeleteEmail)            // DELETE/POST - Delete an email
	http.HandleFunc("/e/email/read", emailHandler.HandleMarkAsRead)        // POST - Mark email as read
	http.HandleFunc("/e/email/trash", emailHandler.HandleMoveToTrash)      // POST - Move email to trash
	http.HandleFunc("/e/email/reply", emailHandler.HandleReplyEmail)       // POST - Reply to an email
	http.HandleFunc("/e/emails/search", emailHandler.HandleSearchEmails)   // POST - Search emails
	http.HandleFunc("/e/config/preview", emailHandler.HandlePreviewConfig) // POST - Preview/test email configuration
	http.HandleFunc("/e/mailboxes", emailHandler.HandleGetMailboxes)       // POST - Get available mailboxes
	http.HandleFunc("/e/stats", emailHandler.HandleGetStats)               // GET/POST - Get email statistics/analytics

	// SMS API Routes - All prefixed with /s/
	smsHandler, err := sms.NewHandler()
	if err != nil {
		fmt.Printf("\x1b[31mError creating SMS handler: %v\x1b[0m\n", err)
	}
	http.HandleFunc("/s/send", smsHandler.HandleSendSMS)                 // POST - Send SMS
	http.HandleFunc("/s/conversation", smsHandler.HandleGetConversation) // POST - Get conversation between numbers
	http.HandleFunc("/s/config", smsHandler.HandleGetConfig)             // POST - Get SMS configuration
	http.HandleFunc("/s/stats", smsHandler.HandleGetStats)               // POST - Get SMS statistics
	http.HandleFunc("/s/webhook", smsHandler.HandleWebhook)              // POST - Twilio webhook handler
	http.HandleFunc("/s/status", smsHandler.HandleStatusCallback)        // POST - Twilio status callback
	http.HandleFunc("/s/bulk", smsHandler.HandleBulkSend)                // POST - Bulk SMS sending

	// Internal — cache invalidation on property update
	http.HandleFunc("/internal/property-updated", handlePropertyUpdated)

	// Start the property change watcher as a background goroutine.
	// This is defense-in-depth: cache invalidation via change stream catches
	// property updates from ANY source (direct DB edits, imports, other services).
	go func() {
		mongoURI := os.Getenv("MONGODB_URI")
		if mongoURI == "" {
			mongoURI = "mongodb://localhost:27017"
		}
		sms.StartPropertyChangeWatcher(mongoURI)
	}()

	// Property & scheduling routes (public widget API)
	http.HandleFunc("/properties", handleGetProperties)

	// Start the HTTP server
	port := ":8080"
	fmt.Printf("\x1b[32mStarting API server on port %s\x1b[0m\n", port)

	// Start the server
	if err := http.ListenAndServe(port, nil); err != nil {
		fmt.Printf("\x1b[31mError starting server: %v\x1b[0m\n", err)
		check(err, true)
	}
}
