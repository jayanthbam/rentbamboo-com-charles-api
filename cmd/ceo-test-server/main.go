package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"bamboo/sms/generator"
	"bamboo/types"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ============================================================================
// CEO Test Server — small HTTP server that exposes the AI generator behind
// a simple REST API. Used by the React app hosted on Vercel. Stores chat
// messages in the real sms.messages collection (user's choice).
// ============================================================================

// Server holds the shared dependencies (MongoDB, AI generator).
type Server struct {
	mongoClient *mongo.Client
	gen         *generator.AIGenerator
	secret      string
}

func main() {
	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("❌ Failed to connect to MongoDB: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("❌ Failed to ping MongoDB: %v", err)
	}
	log.Printf("✓ Connected to MongoDB")

	gen, err := generator.NewAIGenerator()
	if err != nil {
		log.Fatalf("❌ Failed to create AI generator: %v", err)
	}
	log.Printf("✓ AI generator ready")

	secret := os.Getenv("INTERNAL_SECRET")
	if secret == "" {
		secret = "ceo-test-insecure-default"
		log.Printf("⚠️  INTERNAL_SECRET not set — using insecure default (DO NOT USE IN PRODUCTION)")
	}

	srv := &Server{
		mongoClient: client,
		gen:         gen,
		secret:      secret,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/test/start", corsMiddleware(authMiddleware(secret, srv.handleTestStart)))
	mux.HandleFunc("/v1/test/send", corsMiddleware(authMiddleware(secret, srv.handleTestSend)))
	mux.HandleFunc("/v1/test/reset", corsMiddleware(authMiddleware(secret, srv.handleTestReset)))
	mux.HandleFunc("/health", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("🚀 CEO test server listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}

// ============================================================================
// Middleware
// ============================================================================

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Allow any origin — this is a temp tool for the CEO, no auth.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Internal-Secret")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func authMiddleware(expected string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("X-Internal-Secret")
		if got != expected {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// ============================================================================
// Request/Response types
// ============================================================================

type StartRequest struct {
	TeamID string `json:"teamId"`
	LeadID string `json:"leadId"`
	Phone  string `json:"phone"`
}

type StartResponse struct {
	Lead            *types.Lead `json:"lead"`
	TeamName        string      `json:"teamName"`
	TeamPhone       string      `json:"teamPhone"`
	NextTour        string      `json:"nextTour"`
	HasToursAhead   bool        `json:"hasToursAhead"`
}

type SendRequest struct {
	TeamID      string `json:"teamId"`
	LeadID      string `json:"leadId"`
	Phone       string `json:"phone"`
	Message     string `json:"message"`
	SendAsHuman bool   `json:"sendAsHuman"`
}

type SendResponse struct {
	Reply            string `json:"reply"`
	SystemPrompt     string `json:"systemPrompt"`
	ReasoningContent string `json:"reasoningContent"`
	LatencyMs        int64  `json:"latencyMs"`
	TokensUsed       int    `json:"tokensUsed"`
}

type ResetRequest struct {
	TeamID string `json:"teamId"`
	LeadID string `json:"leadId"`
	Phone  string `json:"phone"`
}

type ResetResponse struct {
	Deleted int `json:"deleted"`
}

// ============================================================================
// Handlers
// ============================================================================

// handleTestStart validates the team/lead/phone and returns a lead summary.
func (s *Server) handleTestStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req StartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.TeamID == "" || req.Phone == "" {
		writeError(w, http.StatusBadRequest, "teamId and phone are required")
		return
	}

	// Lookup team phone
	teamPhone, teamName, err := s.getTeam(req.TeamID)
	if err != nil {
		writeError(w, http.StatusNotFound, "team not found: "+err.Error())
		return
	}

	// Lookup lead — try by ID first, fall back to phone
	var lead *types.Lead
	if req.LeadID != "" {
		lead, err = s.getLeadByID(req.TeamID, req.LeadID)
	}
	if lead == nil {
		lead, err = s.getLeadByPhone(req.TeamID, req.Phone)
	}
	if err != nil || lead == nil {
		writeError(w, http.StatusNotFound, "lead not found")
		return
	}

	// Look up upcoming tour (best effort)
	nextTour, hasTours := s.getNextTour(req.TeamID, lead.ID, lead.Phone)

	writeJSON(w, http.StatusOK, StartResponse{
		Lead:          lead,
		TeamName:      teamName,
		TeamPhone:     teamPhone,
		NextTour:      nextTour,
		HasToursAhead: hasTours,
	})
}

// handleTestSend saves a message from the lead, generates the AI reply,
// saves the AI reply, and returns the reply + system prompt.
func (s *Server) handleTestSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.TeamID == "" || req.Phone == "" || req.Message == "" {
		writeError(w, http.StatusBadRequest, "teamId, phone, message are required")
		return
	}

	teamPhone, _, err := s.getTeam(req.TeamID)
	if err != nil {
		writeError(w, http.StatusNotFound, "team not found: "+err.Error())
		return
	}

	var lead *types.Lead
	if req.LeadID != "" {
		lead, _ = s.getLeadByID(req.TeamID, req.LeadID)
	}
	if lead == nil {
		lead, _ = s.getLeadByPhone(req.TeamID, req.Phone)
	}
	if lead == nil {
		writeError(w, http.StatusNotFound, "lead not found")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Save the message to sms.messages (so it shows up in chat history).
	sentBy := ""
	if req.SendAsHuman {
		sentBy = "user_test_human"
	}
	if err := s.saveMessage(ctx, req.Phone, teamPhone, req.Message, "inbound", sentBy); err != nil {
		log.Printf("⚠️ Failed to save inbound message: %v", err)
	}

	// Get chat history
	chat, err := s.getChatHistory(ctx, req.Phone, teamPhone)
	if err != nil {
		log.Printf("⚠️ Failed to get chat history: %v", err)
		chat = nil
	}

	// Build thread
	thread := buildChatThread(chat)

	// Look up feature flags
	appSending, tourScheduling := s.getCmdCenterFlags(ctx, req.TeamID)

	// Generate AI reply
	start := time.Now()
	leadPropertyID := lead.Property.ID
	if leadPropertyID == "" {
		leadPropertyID = lead.PropertyID
	}
	aiReply, systemPrompt, err := s.gen.GenerateLiveTextResponse(
		thread, req.Message, req.TeamID, "ceo-test-"+uuid.NewString(),
		leadPropertyID, appSending, tourScheduling, lead, "", "",
	)
	latency := time.Since(start)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "AI generation failed: "+err.Error())
		return
	}

	// Save AI reply
	if err := s.saveMessage(ctx, teamPhone, req.Phone, aiReply, "outbound", ""); err != nil {
		log.Printf("⚠️ Failed to save outbound AI message: %v", err)
	}

	// Capture the model's reasoning content (DeepSeek's thinking
	// before the visible reply). May be empty for older models.
	reasoningContent := s.gen.GetLastReasoningContent()

	writeJSON(w, http.StatusOK, SendResponse{
		Reply:            aiReply,
		SystemPrompt:     systemPrompt,
		ReasoningContent: reasoningContent,
		LatencyMs:        latency.Milliseconds(),
	})
}

// handleTestReset deletes all messages in the conversation.
func (s *Server) handleTestReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req ResetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json: "+err.Error())
		return
	}
	if req.TeamID == "" || req.Phone == "" {
		writeError(w, http.StatusBadRequest, "teamId and phone are required")
		return
	}

	teamPhone, _, err := s.getTeam(req.TeamID)
	if err != nil {
		writeError(w, http.StatusNotFound, "team not found: "+err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	phoneVariants := phoneVariants(req.Phone)
	orClauses := []bson.M{
		{"from": req.Phone, "to": teamPhone},
		{"from": teamPhone, "to": req.Phone},
	}
	for _, v := range phoneVariants {
		orClauses = append(orClauses, bson.M{"from": v, "to": teamPhone})
		orClauses = append(orClauses, bson.M{"from": teamPhone, "to": v})
	}

	res, err := s.mongoClient.Database("sms").Collection("messages").DeleteMany(ctx, bson.M{"$or": orClauses})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "delete failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ResetResponse{Deleted: int(res.DeletedCount)})
}

// ============================================================================
// Helpers
// ============================================================================

func (s *Server) getTeam(teamID string) (phone, name string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var team bson.M
	err = s.mongoClient.Database("teams").Collection("teams").
		FindOne(ctx, bson.M{"teamId": teamID}).Decode(&team)
	if err != nil {
		return "", "", err
	}
	if v, ok := team["phoneNumber"].(string); ok {
		phone = v
	}
	if v, ok := team["name"].(string); ok {
		name = v
	}
	return phone, name, nil
}

func (s *Server) getLeadByID(teamID, leadID string) (*types.Lead, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var lead types.Lead
	err := s.mongoClient.Database("teams").Collection("leads").
		FindOne(ctx, bson.M{"teamId": teamID, "id": leadID}).Decode(&lead)
	if err != nil {
		return nil, err
	}
	return &lead, nil
}

func (s *Server) getLeadByPhone(teamID, phone string) (*types.Lead, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	normalized := normalizePhone(phone)
	var lead types.Lead
	err := s.mongoClient.Database("teams").Collection("leads").
		FindOne(ctx, bson.M{"teamId": teamID, "phone": normalized}).Decode(&lead)
	if err != nil {
		return nil, err
	}
	return &lead, nil
}

func (s *Server) getNextTour(teamID, leadID, leadPhone string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	orClauses := []bson.M{{"leadId": leadID}}
	for _, v := range phoneVariants(leadPhone) {
		orClauses = append(orClauses, bson.M{"phone": v})
	}

	cur, err := s.mongoClient.Database("teams").Collection("meetings").Find(ctx, bson.M{
		"$or":    orClauses,
		"teamId": teamID,
	})
	if err != nil {
		return "", false
	}
	defer cur.Close(ctx)

	var meetings []bson.M
	if err := cur.All(ctx, &meetings); err != nil {
		return "", false
	}

	now := time.Now()
	loc, _ := time.LoadLocation("America/Chicago")
	if loc == nil {
		loc = time.UTC
	}
	for _, m := range meetings {
		status, _ := m["status"].(string)
		if status != "scheduled" {
			continue
		}
		var startTime time.Time
		if dt, ok := m["start"].(primitive.DateTime); ok {
			startTime = dt.Time()
		} else if t, ok := m["start"].(time.Time); ok {
			startTime = t
		}
		if startTime.After(now) {
			local := startTime.In(loc)
			return local.Format("Mon Jan 2, 3:04 PM MST"), true
		}
	}
	return "", false
}

func (s *Server) getCmdCenterFlags(ctx context.Context, teamID string) (appSending, tourScheduling bool) {
	var doc bson.M
	err := s.mongoClient.Database("teams").Collection("command-centers").
		FindOne(ctx, bson.M{"teamId": teamID}).Decode(&doc)
	if err != nil {
		return false, false
	}
	if v, ok := doc["applicationSending"].(bool); ok {
		appSending = v
	}
	if v, ok := doc["tourScheduling"].(bool); ok {
		tourScheduling = v
	}
	return
}

func (s *Server) saveMessage(ctx context.Context, from, to, body, direction, sentBy string) error {
	now := time.Now()
	msg := bson.M{
		"messageId":  uuid.NewString(),
		"body":       body,
		"from":       from,
		"to":         to,
		"direction":  direction,
		"timestamp":  now,
		"automated":  direction == "outbound" && sentBy == "",
		"segments":   1,
		"mediaCount": 0,
		"accountSid": "ceo-test",
		"status":     "delivered",
	}
	if sentBy != "" {
		msg["sentBy"] = sentBy
	}
	_, err := s.mongoClient.Database("sms").Collection("messages").InsertOne(ctx, msg)
	return err
}

type chatMessage struct {
	MessageID string    `bson:"messageId"`
	Body       string    `bson:"body"`
	From       string    `bson:"from"`
	To         string    `bson:"to"`
	Direction  string    `bson:"direction"`
	Timestamp  time.Time `bson:"timestamp"`
	SentBy     string    `bson:"sentBy,omitempty"`
}

func (s *Server) getChatHistory(ctx context.Context, leadPhone, teamPhone string) ([]chatMessage, error) {
	phoneVariantsList := phoneVariants(leadPhone)
	orClauses := []bson.M{
		{"from": leadPhone, "to": teamPhone},
		{"from": teamPhone, "to": leadPhone},
	}
	for _, v := range phoneVariantsList {
		orClauses = append(orClauses,
			bson.M{"from": v, "to": teamPhone},
			bson.M{"from": teamPhone, "to": v},
		)
	}

	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: 1}}).SetLimit(40)
	cur, err := s.mongoClient.Database("sms").Collection("messages").Find(ctx, bson.M{"$or": orClauses}, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	var messages []chatMessage
	if err := cur.All(ctx, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

// buildChatThread renders messages in the same turn-based format as
// cmd/chat: AI: / Lead: / Team: labels, dedup consecutive, grouped by turn.
func buildChatThread(messages []chatMessage) string {
	if len(messages) == 0 {
		return ""
	}
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

	for _, msg := range messages {
		body := msg.Body
		if strings.TrimSpace(body) == "" {
			continue
		}
		if msg.Direction == "outbound" {
			if len(current.leadLines) > 0 {
				flush()
			}
			if body == prevOutboundBody {
				continue
			}
			prevOutboundBody = body
			if msg.SentBy != "" {
				current.humanLines = append(current.humanLines, body)
			} else {
				current.aiLines = append(current.aiLines, body)
			}
		} else {
			prevOutboundBody = ""
			current.leadLines = append(current.leadLines, body)
		}
	}
	flush()

	var thread string
	for i, t := range turns {
		thread += fmt.Sprintf("[Turn %d]\n", i+1)
		for _, l := range t.humanLines {
			thread += "Team: " + l + "\n"
		}
		for _, l := range t.aiLines {
			thread += "AI: " + l + "\n"
		}
		for _, l := range t.leadLines {
			thread += "Lead: " + l + "\n"
		}
		thread += "\n"
	}
	return thread
}

// ============================================================================
// Utilities
// ============================================================================

func normalizePhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if strings.HasPrefix(phone, "+1") && len(phone) > 2 {
		return phone[2:]
	}
	if strings.HasPrefix(phone, "1") && len(phone) == 11 {
		return phone[1:]
	}
	return phone
}

func phoneVariants(phone string) []string {
	normalized := normalizePhone(phone)
	variants := []string{normalized}
	if normalized != "" && normalized != phone {
		variants = append(variants, phone)
	}
	if normalized != "" && len(normalized) == 10 {
		variants = append(variants, "1"+normalized, "+1"+normalized)
	}
	return variants
}

func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
