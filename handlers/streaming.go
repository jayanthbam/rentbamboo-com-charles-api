package handlers

import (
	"bamboo/helpers"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/k0kubun/pp/v3"
	"github.com/sashabaranov/go-openai"
)

type StreamingMessageRequest struct {
	ClientID  string `json:"clientId"`
	TeamID    string `json:"teamId"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
	Page      string `json:"page"`
	SessionID string `json:"sessionId"`
}

type StreamChunk struct {
	Type         string   `json:"type"` // "chunk", "done", "error"
	Content      string   `json:"content"`
	Photos       []string `json:"photos,omitempty"`
	ScheduleUrls []string `json:"scheduleUrls,omitempty"`
	Context      string   `json:"context,omitempty"`
}

// HandleStreamingMessage handles streaming AI responses using Server-Sent Events
func HandleStreamingMessage(w http.ResponseWriter, r *http.Request) {
	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request
	var req StreamingMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		pp.Printf("Error decoding request: %v\n", err)
		sendSSEError(w, "Invalid request format")
		return
	}

	pp.Printf("Streaming request: ClientID=%s, TeamID=%s, SessionID=%s, Message=%s\n",
		req.ClientID, req.TeamID, req.SessionID, req.Message)

	// Get team info - try clientId first, then teamId
	var teamInfo helpers.TeamInfo
	var err error

	if req.ClientID != "" {
		teamInfo, err = helpers.GetTeamInfoByClientId(req.ClientID)
		if err != nil {
			pp.Printf("Failed to get team info by clientId: %v, trying teamId\n", err)
			// Try teamId as fallback
			if req.TeamID != "" {
				teamInfo, err = helpers.GetTeamInfoByTeamId(req.TeamID)
				if err != nil {
					sendSSEError(w, "Failed to get team info")
					return
				}
			} else {
				sendSSEError(w, "Failed to get team info")
				return
			}
		}
	} else if req.TeamID != "" {
		teamInfo, err = helpers.GetTeamInfoByTeamId(req.TeamID)
		if err != nil {
			sendSSEError(w, "Failed to get team info")
			return
		}
	} else {
		sendSSEError(w, "Either clientId or teamId must be provided")
		return
	}

	teamCommandCenter, err := helpers.GetCharlesCommandCenter(teamInfo.TeamID)
	if err != nil {
		pp.Printf("Error getting command center: %v\n", err)
	}

	// Check CORS against domain and origin header
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
			sendSSEError(w, "Origin not allowed")
			return
		}
	}

	// Get client IP for rate limiting
	ip := helpers.GetClientIP(r)

	// Save the incoming message
	timestamp, _ := time.Parse(time.RFC3339, req.Timestamp)
	err = helpers.SaveMessage(req.Message, req.ClientID, timestamp, req.Page, req.SessionID, teamInfo.TeamID, ip, "incoming", "", []string{}, []string{})
	if err != nil {
		pp.Printf("Error saving message: %v\n", err)
	}

	// Get chat history
	chatHistory, err := helpers.GetChatBySessionId(req.SessionID)
	if err != nil {
		sendSSEError(w, "Failed to get chat history")
		return
	}

	// Get OpenAI API key from environment
	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey == "" {
		pp.Printf("Error: OpenAI API key not found in environment variables\n")
		sendSSEError(w, "Configuration error")
		return
	}

	// Initialize OpenAI client
	openaiClient := openai.NewClient(openaiKey)

	teamId := teamInfo.TeamID

	// Build chat thread from messages
	var chatThread string
	messageCount := len(chatHistory)
	for _, message := range chatHistory {
		if body, ok := message["message"].(string); ok {
			propCtx := ""
			if pctx, ok := message["propertyContext"].(string); ok {
				propCtx = pctx
			}
			chatThread += body + " " + propCtx + "\n"
		}
	}

	// Check if we should run lead extraction (2nd, 4th, 6th message)
	shouldRunAI := messageCount == 2 || messageCount == 4 || messageCount == 6

	// ===== Hours-intent deflector =====
	// If the lead is asking about office hours, business hours, etc., we redirect
	// them to the scheduling page (which shows real availability) rather than
	// letting the AI hallucinate incorrect hours.
	//   - tourScheduling ON  -> direct them to the scheduling link
	//   - tourScheduling OFF -> tell them a team member will reach out
	if helpers.DetectHoursIntent(req.Message) {
		pp.Printf("\x1b[35mHandleStreamingMessage: Hours-intent deflector triggered\x1b[0m\n")

		tourScheduling := teamCommandCenter.TourScheduling

		var scheduleURL string
		var deflectionScheduleUrls []string
		if tourScheduling {
			if firstPropID := helpers.GetFirstPropertyIDForTeam(teamId); firstPropID != "" {
				scheduleURL = helpers.ResolveScheduleURL(teamId, firstPropID)
				if scheduleURL != "" {
					deflectionScheduleUrls = append(deflectionScheduleUrls, scheduleURL)
				}
			}
		}

		deflectionPlain := helpers.HoursDeflectionResponse(tourScheduling, scheduleURL)
		deflectionHTML := "<p>" + deflectionPlain + "</p>"

		// Create a flusher so we can emit SSE events.
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		// Emit the deflection as a single "chunk" event then close with "done".
		chunkEvent := StreamChunk{
			Type:    "chunk",
			Content: deflectionHTML,
		}
		if data, err := json.Marshal(chunkEvent); err == nil {
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			flusher.Flush()
		}

		doneEvent := StreamChunk{
			Type:         "done",
			Content:      deflectionHTML,
			Photos:       []string{},
			ScheduleUrls: deflectionScheduleUrls,
			Context:      "",
		}
		if data, err := json.Marshal(doneEvent); err == nil {
			fmt.Fprintf(w, "data: %s\n\n", string(data))
			flusher.Flush()
		}

		// Persist the deflection reply.
		go func() {
			if err := helpers.SaveMessage(deflectionHTML, req.ClientID, time.Now(), req.Page, req.SessionID, teamInfo.TeamID, "bot", "outgoing", "", []string{}, deflectionScheduleUrls); err != nil {
				pp.Printf("Failed to save hours-intent deflection reply: %v\n", err)
			}
		}()

		pp.Println("Streaming completed via hours-intent deflection")
		return
	}

	// Create channel for receiving chunks
	chunkChan := make(chan string, 100)

	// Variable to store complete response and metadata
	var fullResponse string
	var contextInfo string
	var photos []string
	var scheduleUrls []string
	var genError error

	// Start streaming generation in goroutine
	go func() {
		defer close(chunkChan)
		fullResponse, contextInfo, photos, scheduleUrls, genError = helpers.GenerateAIResponseCharlesStreaming(
			openaiClient,
			chatThread,
			req.Message,
			teamId,
			req.SessionID,
			chunkChan,
		)
	}()

	// Create a flusher to ensure data is sent immediately
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Stream chunks to client
	for chunk := range chunkChan {
		streamChunk := StreamChunk{
			Type:    "chunk",
			Content: chunk,
		}

		data, err := json.Marshal(streamChunk)
		if err != nil {
			pp.Printf("Error marshaling chunk: %v\n", err)
			continue
		}

		// Send as SSE format: data: {json}\n\n
		fmt.Fprintf(w, "data: %s\n\n", string(data))
		flusher.Flush()
	}

	// Check for errors from generation
	if genError != nil {
		sendSSEError(w, fmt.Sprintf("Error generating response: %v", genError))
		flusher.Flush()
		return
	}

	// Send final "done" event with metadata
	doneChunk := StreamChunk{
		Type:         "done",
		Content:      fullResponse,
		Photos:       photos,
		ScheduleUrls: scheduleUrls,
		Context:      contextInfo,
	}

	data, err := json.Marshal(doneChunk)
	if err != nil {
		pp.Printf("Error marshaling done chunk: %v\n", err)
		sendSSEError(w, "Error completing response")
		flusher.Flush()
		return
	}

	fmt.Fprintf(w, "data: %s\n\n", string(data))
	flusher.Flush()

	// Save the AI response to database
	go func() {
		err = helpers.SaveMessage(fullResponse, req.ClientID, time.Now(), req.Page, req.SessionID, teamInfo.TeamID, "bot", "outgoing", contextInfo, photos, scheduleUrls)
		if err != nil {
			pp.Printf("Failed to save AI response: %v\n", err)
		}

		// Extract property ID from schedule URLs if available
		propertyId := ""
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
		err = helpers.ReportAIReplyTelemetry(fullResponse, req.Message, teamInfo.TeamID, req.SessionID, propertyId)
		if err != nil {
			pp.Printf("\x1b[33mFailed to send AI reply telemetry: %v\x1b[0m\n", err)
		}
	}()

	// Perform lead extraction in background if we're at specific message counts
	if shouldRunAI {
		go func() {
			pp.Println("Ran Lead Extraction")

			title, description, leadJSON, err := helpers.GetChatSummary(chatThread, req.SessionID)
			if err != nil {
				pp.Printf("Error extracting chat summary: %v\n", err)
			} else {
				pp.Printf("Chat Summary - Title: %s, Description: %s, Lead: %s\n", title, description, leadJSON)

				// If this is the 2nd message, save alert to db with title and description
				if messageCount == 2 {
					// New Chat Notification
					err = helpers.HandleCharlesNotification(teamInfo.TeamID, req.SessionID, title, description)
					if err != nil {
						pp.Printf("Failed to send new chat notification: %v\n", err)
					}

					// Report telemetry for new chat
					go func() {
						helpers.ReportInternalAnonTelemetry("Charles New Chat", title, description, teamInfo.TeamID)
					}()
				}
			}
		}()
	}

	pp.Println("Streaming completed successfully")
}

// sendSSEError sends an error event in SSE format
func sendSSEError(w http.ResponseWriter, message string) {
	errorChunk := StreamChunk{
		Type:    "error",
		Content: message,
	}

	data, _ := json.Marshal(errorChunk)
	fmt.Fprintf(w, "data: %s\n\n", string(data))

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
