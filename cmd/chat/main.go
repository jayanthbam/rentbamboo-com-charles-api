package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"bamboo/sms"
	"bamboo/sms/generator"
	"bamboo/types"

	"github.com/google/uuid"
	"github.com/k0kubun/pp/v3"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Configuration for the test
var (
	teamID    = "5c79b633-d439-4962-a291-fde3273ff605"
	leadPhone = "+12102743516"
	teamPhone = "+12109859970"
	sessionID = "terminal-sms-test"
	mongoURI  string
)

// Redact URLs from old conversation history — links may reference stale properties
var urlRedact = regexp.MustCompile(`https?://\S+`)

func main() {
	loadEnv()

	// Skip Discord webhooks in test mode, suppress telemetry noise
	os.Setenv("OUTREACH_WEBHOOK_URL", "")
	os.Setenv("DISCORD_WEBHOOK_URL", "")

	// Flags: set DEBUG_FULL_PROMPT=false for clean mode
	verbose := os.Getenv("DEBUG_FULL_PROMPT") != "false"

	mongoURI = os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	// ── HEADER ──────────────────────────────────────────────────────────
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  📟 SMS Live Texting Test")
	fmt.Printf("  Team: %s | Lead: %s | Phone: %s\n", teamID, leadPhone, teamPhone)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()

	gen, err := generator.NewAIGenerator()
	if err != nil {
		fmt.Printf("Error creating generator: %v\n", err)
		os.Exit(1)
	}

	lead, err := getLeadByPhone(teamID, leadPhone)
	if err != nil {
		fmt.Printf("Warning: Could not look up lead: %v\n", err)
	} else if lead != nil {
		fmt.Printf("👤 Lead: %s %s (%s)\n", lead.FirstName, lead.LastName, lead.Status)
		if lead.Property.ID != "" {
			fmt.Printf("🏠 Property: %s (%s)\n", lead.Property.PropertyName, lead.Property.ID[:8])
		} else {
			fmt.Printf("🏠 Property: none assigned\n")
		}
	} else {
		fmt.Printf("⚠️ No lead found\n")
	}
	fmt.Println()

	fmt.Println("💬 Chat ready! Type 'quit' to exit.")
	if verbose {
		fmt.Println("   (verbose mode: showing full prompts)")
	}
	fmt.Println()

	// Create session folder for full prompt dumps
	sessionFolder := ""
	if promptDir := os.Getenv("DEBUG_PROMPTS_DIR"); promptDir != "" {
		sessionFolder = filepath.Join(promptDir, time.Now().Format("2006-01-02_15-04-05"))
	} else {
		sessionFolder = filepath.Join("debug-prompts", time.Now().Format("2006-01-02_15-04-05"))
	}
	if err := os.MkdirAll(sessionFolder, 0755); err != nil {
		pp.Printf("Warning: could not create prompt dump folder: %v\n", err)
		sessionFolder = ""
	} else {
		fmt.Printf("📂 Saving full prompts to: %s\n", sessionFolder)
	}

	turnCounter := 0
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("You ▶  ")
		message, err := reader.ReadString('\n')
		if err != nil {
			fmt.Printf("Error reading input: %v\n", err)
			continue
		}

		message = strings.TrimSpace(message)
		if message == "" {
			continue
		}

		if message == "quit" || message == "exit" {
			fmt.Println("\n👋 Goodbye!")
			break
		}

		// HITL test affordance: prefix the message with "h " to send it
		// as a human agent (mock). The stripped body is saved with
		// sentBy="user_test_human" so the AI distinguishes it as a
		// "Team:" message in the chat history.
		humanSender := false
		if strings.HasPrefix(message, "h ") {
			message = strings.TrimSpace(message[2:])
			humanSender = true
		}
		if message == "" {
			fmt.Println("(empty message after stripping 'h ' prefix)")
			continue
		}

		if humanSender {
			// Save as outbound from the team with a mock userId.
			saveSMSToDB(mongoURI, teamPhone, leadPhone, message, "outbound", "user_test_human")
			// Continue to next turn so the lead's next reply triggers AI generation.
			fmt.Println("(saved as human-sent message from team)")
			continue
		}

		// Save user's inbound message to MongoDB so chat history persists across turns
		saveSMSToDB(mongoURI, leadPhone, teamPhone, message, "inbound", "")

		// Get chat context
		chat, err := sms.GetMessagesBetweenPhoneNumbers(leadPhone, teamPhone)
		if err != nil {
			pp.Printf("Warning: Could not get chat history: %v\n", err)
		}

		// Build turn-based thread (grouped: AI messages + Lead messages per turn).
		// Multi-line SMS from either side are grouped within a turn.
		// Human-sent outbound messages (msg.SentBy != "") are labeled "Team:"
		// so the AI can distinguish them from its own prior replies.
		var thread string
		var lastAIReply string
		if len(chat) > 0 {
			// Group into turns: each turn = consecutive AI lines + consecutive Lead lines
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

			for _, msg := range chat {
				body := urlRedact.ReplaceAllString(msg.Body, "[stale-link]")
				if strings.TrimSpace(body) == "" {
					continue
				}
				if msg.Direction == "outbound" {
					// If we already have leadLines for current turn, close it
					if len(current.leadLines) > 0 {
						flush()
					}
					if body == prevOutboundBody {
						continue // dedup consecutive duplicate
					}
					prevOutboundBody = body
					// HITL: human-sent outbound messages get the "Team:" label
					if msg.SentBy != "" {
						current.humanLines = append(current.humanLines, body)
					} else {
						current.aiLines = append(current.aiLines, body)
						// Track the most recent AI message (raw, unredacted isn't important here)
						lastAIReply = body
					}
				} else {
					prevOutboundBody = ""
					current.leadLines = append(current.leadLines, body)
				}
			}
			flush()

			// Render turns
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

		leadPropertyID := ""
		if lead != nil {
			if lead.Property.ID != "" {
				leadPropertyID = lead.Property.ID
			} else if lead.PropertyID != "" {
				leadPropertyID = lead.PropertyID
			}
		}

		// Read feature flags from the team's command center
		applicationSending, tourScheduling := fetchCmdCenterFlags(mongoURI, teamID)

	start := time.Now()
		aiResponse, _, err := gen.GenerateLiveTextResponse(
			thread, message, teamID, sessionID,
			leadPropertyID, applicationSending, tourScheduling, lead, "", lastAIReply,
		)
		elapsed := time.Since(start)

		if err != nil {
			fmt.Printf("❌ Error: %v\n", err)
			continue
		}

		// Save full prompt to file if session folder exists
		if sessionFolder != "" {
			savePromptToFile(gen.GetLastFullPrompt(), turnCounter, sessionFolder)
			// Also save the DeepSeek reasoning_content (if any) to a
			// sibling file so we can audit the model's thinking.
			if reasoning := gen.GetLastReasoningContent(); reasoning != "" {
				saveReasoningToFile(reasoning, turnCounter, sessionFolder)
			}
			turnCounter++
		}

		// ── RESPONSE (clean) ────────────────────────────────────────────
		fmt.Printf("\n%s ◀  %s  [%.1fs]\n", modelLabel(), aiResponse, elapsed.Seconds())

		// ── VERBOSE: show exactly what the AI receives, in order ─────────
		if verbose {
			fmt.Println()
			fmt.Println("╔═══════════════════════════════════════════╗")
			fmt.Println("║  ⚙️  SYSTEM PROMPT")
			fmt.Println("╚═══════════════════════════════════════════╝")
			if sysPrompt := gen.GetLastSystemPrompt(); sysPrompt != "" {
				for _, l := range strings.Split(sysPrompt, "\n") {
					fmt.Printf("  %s\n", l)
				}
			}

			// HUMAN TAKEOVER box — printed only when the most recent
			// outbound was sent by a human team member. Mirrors the
			// red section at the top of the system prompt.
			if gen.GetLastHumanTakeover() {
				fmt.Println()
				fmt.Println("╔═══════════════════════════════════════════╗")
				fmt.Println("║  🔴  HUMAN TAKEOVER — HIGH PRIORITY")
				fmt.Println("╚═══════════════════════════════════════════╝")
				fmt.Println("  🔴 A human team member has taken over this conversation.")
				fmt.Println("  Match their tone, don't contradict or repeat what they said.")
				fmt.Println("  Stay in supporting role.")
			}

			fmt.Println()
			fmt.Println("╔═══════════════════════════════════════════╗")
			fmt.Println("║  ⚙️  CONFIG")
			fmt.Println("╚═══════════════════════════════════════════╝")
		fmt.Printf("  applicationSending: %v\n", applicationSending)
		fmt.Printf("  tourScheduling: %v\n", tourScheduling)
		fmt.Printf("  qualificationMode: %s\n", gen.GetLastQualCtx().Mode)

		// CURRENT CONTEXT (date/time/timezone) — extracted from the
		// MISCELLANEOUS section of the system prompt. Shows the user
		// the team's local time + timezone the AI will use.
		fmt.Println()
		fmt.Println("╔═══════════════════════════════════════════╗")
		fmt.Println("║  🕐  CURRENT CONTEXT")
		fmt.Println("╚═══════════════════════════════════════════╝")
		if sysPrompt := gen.GetLastSystemPrompt(); sysPrompt != "" {
			if idx := strings.Index(sysPrompt, "CURRENT_DATE:"); idx != -1 {
				rest := sysPrompt[idx:]
				// Find the next blank line or section delimiter.
				endIdx := strings.Index(rest, "\n\n")
				if endIdx == -1 {
					endIdx = len(rest)
				}
				for _, l := range strings.Split(rest[:endIdx], "\n") {
					fmt.Printf("  %s\n", l)
				}
			} else {
				fmt.Println("  (no date/time set)")
			}
		} else {
			fmt.Println("  (no date/time set)")
		}

		if lead != nil {
			// Show ✅ PROFILE: parsed/known profile fields. These are
			// post-parser values, so they show what the AI actually sees.
			fmt.Println()
			fmt.Println("╔═══════════════════════════════════════════╗")
			fmt.Println("║  ✅  PROFILE (known attributes)")
			fmt.Println("╚═══════════════════════════════════════════╝")
			if lead.Budget != "" {
				fmt.Printf("  Budget: %s\n", lead.Budget)
			}
			if lead.MoveInDate != "" {
				fmt.Printf("  Move-In: %s\n", lead.MoveInDate)
			}
			if lead.Pets != "" {
				fmt.Printf("  Pets: %s\n", lead.Pets)
			}
			if lead.BedroomPreference != "" {
				fmt.Printf("  Bedroom Pref: %s\n", lead.BedroomPreference)
			}
			if lead.JobTitle != "" {
				fmt.Printf("  Job Title: %s\n", lead.JobTitle)
			}
			if lead.Industry != "" {
				fmt.Printf("  Industry: %s\n", lead.Industry)
			}
			if lead.Email != "" {
				fmt.Printf("  Email: %s\n", lead.Email)
			}
			if lead.Phone != "" {
				fmt.Printf("  Phone: %s\n", lead.Phone)
			}
			if lead.LeadSource != "" {
				fmt.Printf("  Source: %s\n", lead.LeadSource)
			}
			if len(lead.Tags) > 0 {
				fmt.Printf("  Tags: [%s]\n", strings.Join(lead.Tags, ", "))
			}
			if len(lead.Comments) > 0 {
				fmt.Printf("  Comments: %s\n", strings.Join(lead.Comments, "; "))
			}
			// Personality is read from the cmd-center field. Show the
			// friendly label so the user can confirm the agent is
			// matching the team's chosen style.
			if personality := extractPersonality(gen.GetLastSystemPrompt()); personality != "" {
				fmt.Printf("  Personality: %s\n", personalityLabel(personality))
			}
		}
		if os.Getenv("DEEPSEEK_API_KEY") != "" {
			effort := os.Getenv("REASONING_EFFORT")
			if effort == "" {
				effort = "medium"
			}
			fmt.Printf("  thinking: enabled (effort: %s)\n", effort)
		}

			fmt.Println()
			fmt.Println("╔═══════════════════════════════════════════╗")
			fmt.Println("║  💬  CHAT HISTORY")
			fmt.Println("╚═══════════════════════════════════════════╝")
			lines := strings.Split(strings.TrimSpace(thread), "\n")
			s := 0
			if len(lines) > 12 {
				s = len(lines) - 12
				fmt.Printf("  (... %d earlier messages omitted)\n", len(lines)-12)
			}
			for _, l := range lines[s:] {
				if len(l) > 100 {
					l = l[:100] + "..."
				}
				fmt.Printf("  %s\n", l)
			}

			// Show the structured Q&A state — mirrors sections 5 & 6 of the system prompt
			qualCtx := gen.GetLastQualCtx()
			if qualCtx.Mode == "structured" && len(qualCtx.Questions) > 0 {
				var answered, unanswered []string
				for _, q := range qualCtx.Questions {
					switch q.Status {
					case "answered":
						answered = append(answered, fmt.Sprintf("  ✅ %q -> %q", q.Question, q.Answer))
					case "unanswered":
						unanswered = append(unanswered, fmt.Sprintf("  ❌ %q", q.Question))
					default:
						unanswered = append(unanswered, fmt.Sprintf("  ⬜ %q", q.Question))
					}
				}
				if len(answered) > 0 {
					fmt.Println()
					fmt.Println("╔═══════════════════════════════════════════╗")
					fmt.Println("║  ✅  INFORMATION_CONTEXT (already answered)")
					fmt.Println("╚═══════════════════════════════════════════╝")
					for _, l := range answered {
						fmt.Println(l)
					}
				}
				if len(unanswered) > 0 {
					fmt.Println()
					fmt.Println("╔═══════════════════════════════════════════╗")
					fmt.Println("║  ❌  ASK_NEXT (pick 1-2 to weave in)")
					fmt.Println("╚═══════════════════════════════════════════╝")
					for _, l := range unanswered {
						fmt.Println(l)
					}
				}
			}

			// PRIVATE TEAM NOTES (extract from system prompt; show
			// fallback when no notes exist so the user can confirm
			// the system is working, not missing the lead's notes).
			fmt.Println()
			fmt.Println("╔═══════════════════════════════════════════╗")
			fmt.Println("║  🔒  PRIVATE TEAM NOTES (never expose)")
			fmt.Println("╚═══════════════════════════════════════════╝")
			if sysPrompt := gen.GetLastSystemPrompt(); sysPrompt != "" {
				if idx := strings.Index(sysPrompt, "1.25. PRIVATE TEAM NOTES"); idx != -1 {
					rest := sysPrompt[idx:]
					endIdx := strings.Index(rest, "═════════════")
					if endIdx == -1 {
						endIdx = len(rest)
					} else {
						endIdx = idx + endIdx
					}
					notesSection := sysPrompt[idx:endIdx]
					for _, l := range strings.Split(notesSection, "\n") {
						if l == "" {
							continue
						}
						fmt.Printf("  %s\n", l)
					}
				} else {
					fmt.Println("  (no private notes)")
				}
			} else {
				fmt.Println("  (no private notes)")
			}

			// TOUR HISTORY (extract from system prompt; show fallback
			// when no tours are scheduled so the user can confirm the
			// system is working, not missing the lead's tours).
			fmt.Println()
			fmt.Println("╔═══════════════════════════════════════════╗")
			fmt.Println("║  📅  TOUR HISTORY")
			fmt.Println("╚═══════════════════════════════════════════╝")
			if sysPrompt := gen.GetLastSystemPrompt(); sysPrompt != "" {
				if idx := strings.Index(sysPrompt, "1.5. TOUR HISTORY"); idx != -1 {
					rest := sysPrompt[idx:]
					endIdx := strings.Index(rest, "═════════════")
					if endIdx == -1 {
						endIdx = len(rest)
					} else {
						endIdx = idx + endIdx
					}
					tourSection := sysPrompt[idx:endIdx]
					for _, l := range strings.Split(tourSection, "\n") {
						if l == "" {
							continue
						}
						fmt.Printf("  %s\n", l)
					}
				} else {
					fmt.Println("  📅 No tours scheduled")
				}
			} else {
				fmt.Println("  📅 No tours scheduled")
			}

			fmt.Println()
			fmt.Println("╔═══════════════════════════════════════════╗")
			fmt.Println("║  🎯  YOUR TASK")
			fmt.Println("╚═══════════════════════════════════════════╝")
			fmt.Println("  Generate the next reply based on the importance order above.")
			fmt.Println()
		}

		// Save AI's outbound response to MongoDB so it's available as chat history next turn
		saveSMSToDB(mongoURI, teamPhone, leadPhone, aiResponse, "outbound", "")
	}
}

// saveSMSToDB saves an SMS message directly to MongoDB without sending via Twilio.
// Used by the test tool to persist conversation history across turns and sessions.
// If sentBy is non-empty, the message is marked as human-sent and will be
// labeled "Team:" in subsequent chat history reads.
func saveSMSToDB(uri, from, to, body, direction, sentBy string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return
	}
	defer client.Disconnect(ctx)

	doc := bson.M{
		"messageId":  uuid.New().String(),
		"body":       body,
		"from":       from,
		"to":         to,
		"direction":  direction,
		"timestamp":  time.Now(),
		"automated":  direction == "outbound" && sentBy == "",
		"segments":   1,
		"mediaCount": 0,
		"accountSid": "terminal-test",
		"status":     "delivered",
	}
	if sentBy != "" {
		doc["sentBy"] = sentBy
	}

	_, err = client.Database("sms").Collection("messages").InsertOne(ctx, doc)
	if err != nil {
		pp.Printf("Warning: failed to save message: %v\n", err)
	}
}

// savePromptToFile saves the full prompt to a file in the session's debug folder
func savePromptToFile(prompt string, turn int, sessionFolder string) {
	if sessionFolder == "" {
		return
	}

	filename := fmt.Sprintf("turn-%03d.txt", turn)
	filepath := filepath.Join(sessionFolder, filename)

	err := os.WriteFile(filepath, []byte(prompt), 0644)
	if err != nil {
		pp.Printf("Warning: failed to save prompt file: %v\n", err)
	}
}

// saveReasoningToFile saves the DeepSeek reasoning_content to a
// sibling file (turn-NNN-thinking.txt) so we can audit the model's
// internal thinking for each turn. Companion to savePromptToFile.
func saveReasoningToFile(reasoning string, turn int, sessionFolder string) {
	if sessionFolder == "" || reasoning == "" {
		return
	}

	filename := fmt.Sprintf("turn-%03d-thinking.txt", turn)
	filepath := filepath.Join(sessionFolder, filename)

	header := fmt.Sprintf("DeepSeek reasoning_content (thinking mode)\nTurn: %d\nGenerated: %s\n\n",
		turn, time.Now().Format(time.RFC3339))

	err := os.WriteFile(filepath, []byte(header+reasoning), 0644)
	if err != nil {
		pp.Printf("Warning: failed to save reasoning file: %v\n", err)
	}
}

func modelLabel() string {
	if m := os.Getenv("AI_MODEL"); m != "" {
		return "🤖 " + m
	}
	if os.Getenv("DEEPSEEK_API_KEY") != "" {
		return "🤖 deepseek"
	}
	return "🤖 openai"
}

// fetchCmdCenterFlags reads applicationSending and tourScheduling from the
// team's command center MongoDB document. Returns defaults (false, false)
// on any error or missing document.
func fetchCmdCenterFlags(uri, teamID string) (appSending, tourSched bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return false, false
	}
	defer client.Disconnect(ctx)

	var doc bson.M
	err = client.Database("teams").Collection("command-centers").
		FindOne(ctx, bson.M{"teamId": teamID}).Decode(&doc)
	if err != nil {
		return false, false
	}

	if v, ok := doc["applicationSending"].(bool); ok {
		appSending = v
	}
	if v, ok := doc["tourScheduling"].(bool); ok {
		tourSched = v
	}
	return
}

// getLeadByPhone looks up a lead by phone number
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

	normalizedPhone := phoneNumber
	if strings.HasPrefix(phoneNumber, "+1") {
		normalizedPhone = strings.TrimPrefix(phoneNumber, "+1")
	} else if strings.HasPrefix(phoneNumber, "1") && len(phoneNumber) > 10 {
		normalizedPhone = strings.TrimPrefix(phoneNumber, "1")
	}

	collection := client.Database("teams").Collection("leads")
	filter := bson.M{
		"teamId": teamId,
		"phone":  normalizedPhone,
	}

	var lead types.Lead
	err = collection.FindOne(ctx, filter).Decode(&lead)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, fmt.Errorf("error finding lead: %w", err)
	}

	return &lead, nil
}

func loadEnv() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx > 0 {
			key := strings.TrimSpace(line[:idx])
			value := strings.TrimSpace(line[idx+1:])
			value = strings.Trim(value, `"`)
			os.Setenv(key, value)
		}
	}
}

// extractPersonality looks for "You communicate in a <Label> style:" in
// the rendered system prompt and returns the underlying enum value
// (relationship/consultant/challenger/expert/closer). Returns "" if
// no PERSONALITY section is present.
func extractPersonality(sysPrompt string) string {
	if sysPrompt == "" {
		return ""
	}
	idx := strings.Index(sysPrompt, "0.5. PERSONALITY")
	if idx == -1 {
		return ""
	}
	rest := sysPrompt[idx:]
	// Find the "You communicate in a <Label> style:" line.
	marker := "You communicate in a "
	if i := strings.Index(rest, marker); i != -1 {
		after := rest[i+len(marker):]
		if j := strings.Index(after, " style:"); j != -1 {
			label := after[:j]
			return labelToPersonality(label)
		}
	}
	return ""
}

// labelToPersonality maps a friendly label back to the Zod enum value.
func labelToPersonality(label string) string {
	switch label {
	case "Relationship Builder":
		return "relationship"
	case "Consultative":
		return "consultant"
	case "Challenger":
		return "challenger"
	case "Expert":
		return "expert"
	case "Closer":
		return "closer"
	default:
		return ""
	}
}

// personalityLabel returns the friendly display name for a Zod enum
// value. Falls back to the raw value if unknown.
func personalityLabel(p string) string {
	switch p {
	case "relationship":
		return "Relationship Builder"
	case "consultant":
		return "Consultative"
	case "challenger":
		return "Challenger"
	case "expert":
		return "Expert"
	case "closer":
		return "Closer"
	default:
		return p
	}
}
