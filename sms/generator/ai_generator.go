package generator

import (
	"bamboo/helpers"
	"bamboo/sms/availability"
	"bamboo/sms/property"
	"bamboo/types"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/k0kubun/pp/v3"
	"github.com/sashabaranov/go-openai"
	"github.com/sashabaranov/go-openai/jsonschema"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var debugProp = os.Getenv("DEBUG_PROPERTY") == "true"

// AIGenerator handles AI-powered SMS content generation
type AIGenerator struct {
	openaiClient          *openai.Client
	deepseekClient        *DeepSeekClient
	mongoClient           *mongo.Client
	teamConfigCache       *TeamConfigCache
	availCache            *AvailabilityCache
	modelName             string
	lastSysPrompt         string
	lastFullPrompt        string
	lastCompletedItems    string
	lastUndiscussedHL     string
	lastQualStatus        string
	lastQualCtx           QualificationContext
	lastAvailabilityBlock string
	lastTaskNote          string
	lastHumanTakeover     bool
	useJSONResponse       bool
}

// NewAIGenerator creates a new AI generator. Supports DeepSeek (if
// DEEPSEEK_API_KEY is set) or OpenAI (OPENAI_API_KEY). DeepSeek uses the
// OpenAI-compatible API endpoint.
func NewAIGenerator() (*AIGenerator, error) {
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

	var openaiClient *openai.Client
	var deepseekClient *DeepSeekClient
	modelName := "gpt-4o-mini-2024-07-18"

	if key := os.Getenv("DEEPSEEK_API_KEY"); key != "" {
		// DeepSeek gets its own client (DeepSeekClient) so we can inject
		// the `thinking` and `reasoning_effort` parameters that the
		// standard go-openai library doesn't expose. The signature is
		// compatible with openai.Client.CreateChatCompletion, so the
		// rest of the generator code is unchanged.
		deepseekClient = NewDeepSeekClient(key)
		modelName = "deepseek-v4-flash"
		pp.Printf("DEBUG: DeepSeek client initialized (model: deepseek-v4-flash, thinking: enabled, effort: %s)\n", deepseekClient.ReasoningEffort())
	} else if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		openaiClient = openai.NewClient(key)
		pp.Printf("DEBUG: OpenAI client initialized (model: gpt-4o-mini)\n")
	} else {
		pp.Printf("WARNING: No AI API key set, AI generation will use fallback\n")
	}

	// Allow model name override via env var
	if override := os.Getenv("AI_MODEL"); override != "" && (openaiClient != nil || deepseekClient != nil) {
		modelName = override
		pp.Printf("DEBUG: Model override: %s\n", modelName)
	}

	useJSONResponse := modelName != "deepseek-v4-flash"

	return &AIGenerator{
		openaiClient:    openaiClient,
		deepseekClient:  deepseekClient,
		mongoClient:     client,
		teamConfigCache: NewTeamConfigCache(),
		availCache:      NewAvailabilityCache(),
		modelName:       modelName,
		useJSONResponse: useJSONResponse,
	}, nil
}

// client returns whichever AI client is configured (DeepSeek takes
// precedence when DEEPSEEK_API_KEY is set, otherwise OpenAI). Returns
// nil if neither API key is configured.
func (g *AIGenerator) client() chatCompleter {
	if g.deepseekClient != nil {
		return g.deepseekClient
	}
	if g.openaiClient != nil {
		return g.openaiClient
	}
	return nil
}

// isFullyQualified returns true if all of the lead's qualification
// questions have been answered (AnsweredAt != nil). Used to swap the
// AI prompt into "PUSH for tour / close-out" mode.
//
// Returns false if the lead is nil or has no questions at all (a lead
// with zero questions is not "fully qualified" — there are no questions
// to qualify against).
func isFullyQualified(lead *types.Lead) bool {
	if lead == nil || len(lead.QualificationQuestions) == 0 {
		return false
	}
	for _, q := range lead.QualificationQuestions {
		if q.AnsweredAt == nil {
			return false
		}
	}
	return true
}

// fetchLeadTours queries the meetings collection for the lead's tour
// history and returns formatted text for scheduled and previous tours.
// Used by the prompt builder to render the TOUR HISTORY section.
//
// Scheduled = meetings with status="scheduled" and start > now.
// Previous  = everything else (past scheduled, cancelled, no-show,
// completed, etc.) — sorted by start descending.
//
// leadID and leadPhone are both used to find the lead's meetings:
//   - leadID matches documents with a `leadId` field (newer format)
//   - leadPhone matches documents with a `phone` field (legacy format,
//     often stored as 10-digit US number)
//
// Property and unit names are looked up in a single batched query each
// (one for properties, one for units) so the per-meeting render doesn't
// need to query MongoDB N times.
func (g *AIGenerator) fetchLeadTours(ctx context.Context, leadID, leadPhone, teamID, teamTimezone string) (scheduled, previous string, err error) {
	if g.mongoClient == nil || leadID == "" {
		return "", "", nil
	}

	orClauses := []bson.M{
		{"leadId": leadID},
	}
	if leadPhone != "" {
		normalized := normalizePhone(leadPhone)
		// Try all common phone formats to handle inconsistent storage.
		phoneVariants := []string{normalized}
		if normalized != "" && normalized != leadPhone {
			phoneVariants = append(phoneVariants, leadPhone)
		}
		if normalized != "" && len(normalized) == 10 {
			phoneVariants = append(phoneVariants, "1"+normalized, "+1"+normalized)
		}
		for _, p := range phoneVariants {
			orClauses = append(orClauses, bson.M{"phone": p})
		}
	}

	collection := g.mongoClient.Database("teams").Collection("meetings")
	cursor, err := collection.Find(ctx, bson.M{
		"$or":    orClauses,
		"teamId": teamID,
	})
	if err != nil {
		return "", "", err
	}
	defer cursor.Close(ctx)

	var meetings []bson.M
	if err := cursor.All(ctx, &meetings); err != nil {
		return "", "", err
	}

	// Batched lookups: collect unique propertyId from meetings, then
	// one query to resolve property + nested unit names. Falls back
	// gracefully if the lookup fails (just omits the names from the
	// line). Filtered by teamId for safety.
	propertyNames, unitNames := g.lookupTourNames(ctx, meetings, teamID)

	now := time.Now()
	var scheduledLines, previousLines []string
	for _, m := range meetings {
		// Parse start time (BSON DateTime)
		var startTime time.Time
		if dt, ok := m["start"].(primitive.DateTime); ok {
			startTime = dt.Time()
		} else if t, ok := m["start"].(time.Time); ok {
			startTime = t
		}

		// Look up names for this meeting
		var propertyName, unitName string
		if pid, ok := m["propertyId"].(string); ok {
			propertyName = propertyNames[pid]
		}
		if uid, ok := m["unitId"].(string); ok {
			unitName = unitNames[uid]
		}

		// Format: - "Wed Jun 17, 2:00 PM at <property> (<unit>) with <agent>"
		line := formatTourLine(m, startTime, propertyName, unitName, teamTimezone)

		// Categorize
		status, _ := m["status"].(string)
		isScheduled := status == "scheduled" && startTime.After(now)
		if isScheduled {
			scheduledLines = append(scheduledLines, line)
		} else {
			previousLines = append(previousLines, line)
		}
	}

	return strings.Join(scheduledLines, "\n"), strings.Join(previousLines, "\n"), nil
}

// lookupTourNames returns maps of propertyId -> name and unitId ->
// unitName, looked up in batch from the properties collection. Missing
// or unresolvable IDs are simply absent from the maps (caller falls
// back to omitting the name from the rendered tour line).
//
// IMPORTANT: units are NESTED inside the property document under a
// `units` array, NOT in a separate `units` collection. The property
// field for the name is `propertyName` (not `name`).
func (g *AIGenerator) lookupTourNames(ctx context.Context, meetings []bson.M, teamID string) (map[string]string, map[string]string) {
	propertyNames := make(map[string]string)
	unitNames := make(map[string]string)
	if g.mongoClient == nil || len(meetings) == 0 {
		return propertyNames, unitNames
	}

	// Collect unique propertyIds from meetings (only — units come
	// nested inside the property doc, so we don't need a separate
	// unit ID set here).
	propSet := make(map[string]struct{})
	for _, m := range meetings {
		if pid, ok := m["propertyId"].(string); ok && pid != "" {
			propSet[pid] = struct{}{}
		}
	}
	if len(propSet) == 0 {
		return propertyNames, unitNames
	}

	propIDs := make([]string, 0, len(propSet))
	for id := range propSet {
		propIDs = append(propIDs, id)
	}

	// One query: fetch all relevant properties, filtered by teamId
	// (defense in depth — teamId is already a property field, and
	// without it we could pull cross-team data on shared propertyIds).
	filter := bson.M{"id": bson.M{"$in": propIDs}}
	if teamID != "" {
		filter["teamId"] = teamID
	}

	cur, err := g.mongoClient.Database("teams").Collection("properties").Find(ctx, filter)
	if err != nil {
		return propertyNames, unitNames
	}
	defer cur.Close(ctx)

	var properties []bson.M
	if cur.All(ctx, &properties) != nil {
		return propertyNames, unitNames
	}

	// Extract property names + nested unit names from each property.
	for _, p := range properties {
		pid, _ := p["id"].(string)
		pname, _ := p["propertyName"].(string)
		if pid != "" && pname != "" {
			propertyNames[pid] = pname
		}
		// Units are nested under `units` (bson.A of bson.M).
		if units, ok := p["units"].(bson.A); ok {
			for _, u := range units {
				if unit, ok := u.(bson.M); ok {
					uid, _ := unit["id"].(string)
					uname, _ := unit["unitName"].(string)
					if uid != "" && uname != "" {
						unitNames[uid] = uname
					}
				}
			}
		}
	}
	return propertyNames, unitNames
}

// formatTourLine formats a meeting into a one-line summary like:
// "- Wed Jun 17, 2:00 PM CDT at <property> (<unit>) with <agent>"
// propertyName/unitName are optional — empty values are omitted
// gracefully so the line degrades cleanly if the lookup fails.
// teamTimezone is the IANA timezone string (e.g. "America/Chicago")
// the time should be rendered in; empty defaults to UTC.
func formatTourLine(m bson.M, startTime time.Time, propertyName, unitName, teamTimezone string) string {
	// Convert startTime to the team's local timezone so the time
	// matches what the team sees on their calendar. Falls back to
	// UTC if the timezone is unknown.
	loc, err := time.LoadLocation(teamTimezone)
	if err != nil || loc == nil {
		loc = time.UTC
	}
	localTime := startTime.In(loc)

	dateStr := localTime.Format("Mon Jan 2, 3:04 PM MST")
	if startTime.IsZero() {
		dateStr = "(unknown time)"
	}

	// Agent name
	agent := "(no agent assigned)"
	if member, ok := m["member"].(bson.M); ok {
		if name, ok := member["name"].(string); ok && name != "" {
			agent = name
		} else if email, ok := member["email"].(string); ok && email != "" {
			agent = email
		}
	}

	// Status (for previous tours, e.g. "cancelled", "no-show")
	status := ""
	if s, ok := m["status"].(string); ok && s != "" {
		if s != "scheduled" {
			status = " [" + s + "]"
		}
	}

	// Build the location suffix. Examples:
	//   at Reata at Alamo Ranch (Unit 8209)
	//   at Reata at Alamo Ranch
	//   (no location info)
	locSuffix := ""
	switch {
	case propertyName != "" && unitName != "":
		locSuffix = fmt.Sprintf(" at %s (%s)", propertyName, unitName)
	case propertyName != "":
		locSuffix = fmt.Sprintf(" at %s", propertyName)
	}

	return fmt.Sprintf("- %s%s with %s%s", dateStr, locSuffix, agent, status)
}

// normalizePhone strips a US phone number down to its 10-digit form
// (e.g. "+12102743516" -> "2102743516", "12102743516" -> "2102743516").
// Used to look up meetings stored with different phone formats in the
// same database. Returns the input unchanged if it's not a recognizable
// US phone number.
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

// fetchLeadNotes queries the leadNotes collection for private team notes
// about the lead. Returns notes sorted newest-first. Best-effort — if
// the query fails, returns nil (no notes rendered). The notes are
// strictly for AI context and must NEVER be exposed to the lead.
func (g *AIGenerator) fetchLeadNotes(ctx context.Context, leadID, teamID string) ([]types.LeadNote, error) {
	if g.mongoClient == nil || leadID == "" {
		return nil, nil
	}
	collection := g.mongoClient.Database("teams").Collection("leadNotes")
	opts := options.Find().SetSort(bson.D{{Key: "createdAt", Value: -1}})
	cursor, err := collection.Find(ctx, bson.M{
		"leadId": leadID,
		"teamId": teamID,
	}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var notes []types.LeadNote
	if err := cursor.All(ctx, &notes); err != nil {
		return nil, err
	}
	return notes, nil
}

// getTeamTimezone returns the team's IANA timezone string by reading
// the team document. Falls back through the same priority as
// availability/fetcher.go: communication_timezone → team_timezone →
// centralized_timezone → "America/Chicago". Returns "" if the team
// document can't be loaded.
func (g *AIGenerator) getTeamTimezone(ctx context.Context, teamID string) string {
	if g.mongoClient == nil || teamID == "" {
		return ""
	}
	collection := g.mongoClient.Database("teams").Collection("teams")
	var team bson.M
	err := collection.FindOne(ctx, bson.M{"teamId": teamID}).Decode(&team)
	if err != nil {
		return ""
	}
	for _, key := range []string{"communication_timezone", "team_timezone", "centralized_timezone"} {
		if tz, ok := team[key].(string); ok && tz != "" {
			return tz
		}
	}
	return "America/Chicago"
}

// chatCompleter is the minimal interface both openai.Client and
// DeepSeekClient satisfy. Lets us swap them transparently.
type chatCompleter interface {
	CreateChatCompletion(ctx context.Context, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error)
}

// =============================================================================
// LIVE TEXTING - IMPROVED VERSION WITH BETTER PROMPTING
// =============================================================================

// GenerateLiveTextResponse generates a response for ongoing conversation (live texting)
// Uses ONLY: chat history, command center, jake training, and lead's single property
func (g *AIGenerator) GenerateLiveTextResponse(
	chatHistory string,
	message string,
	teamID string,
	sessionID string,
	leadPropertyID string,
	applicationSending bool,
	tourScheduling bool,
	lead *types.Lead,
	propertySwitchNote string,
	lastAIReply string,
) (string, string, error) {
	if debugProp {
		pp.Println("Generating live text response (improved prompting)...")
	}

	// Get command center + team info (cached — these change rarely)
	var cmdCenter bson.M
	var teamInfo bson.M
	if cached, cachedTeam, ok := g.teamConfigCache.Get(teamID); ok {
		cmdCenter = cached
		teamInfo = cachedTeam
	} else {
		cmdCenter, _ = g.fetchCommandCenter(teamID)
		teamInfo, _ = g.getTeamInfo(teamID)
		g.teamConfigCache.Set(teamID, cmdCenter, teamInfo)
	}

	// Fetch available tour time slots if feature is enabled (cached — 2 min TTL)
	var availabilityContext string
	// Get property name early (for availability message formatting) — the full property
	// context fetch happens below, but this lightweight lookup is cached (5-min TTL).
	resolvedPropName := ""
	if leadPropertyID != "" {
		if p := property.GetPropertyByID(teamID, leadPropertyID); p != nil {
			resolvedPropName = p.Name
		}
	}
	if cmdCenter != nil {
		if availRec, ok := cmdCenter["availabilityRecommendations"].(bool); ok && availRec && tourScheduling {
			if leadPropertyID != "" {
				if cachedSlots, ok := g.availCache.Get(leadPropertyID, teamID); ok {
					if cachedSlots != "" {
						availabilityContext = cachedSlots
						fmt.Printf("\x1b[32m✅ AVAILABILITY: cache hit for property %s\x1b[0m\n", leadPropertyID)
					}
				} else {
					slots := availability.FetchAvailableSlots(g.mongoClient, leadPropertyID, teamID)
					if len(slots) > 0 {
						availabilityContext = availability.FormatSlotsForPrompt(slots, resolvedPropName)
						fmt.Printf("\x1b[32m✅ AVAILABILITY: %d slots fetched for property %s\n%s\x1b[0m\n",
							len(slots), leadPropertyID, availabilityContext)
					} else {
						fmt.Printf("\x1b[33m⚠️ AVAILABILITY: enabled but no slots found for property %s\x1b[0m\n", leadPropertyID)
					}
					g.availCache.Set(leadPropertyID, teamID, availabilityContext)
				}
			}
		} else {
			if availRec, ok := cmdCenter["availabilityRecommendations"].(bool); ok && availRec && !tourScheduling {
				fmt.Printf("\x1b[33m⚠️ AVAILABILITY: recommendations ON but tourScheduling is OFF\x1b[0m\n")
			}
		}
	}

	// Get team info for contact and context (already fetched above)

	// Extract team information
	var teamPhone, teamName, teamDescription, teamCity, teamState, teamDomain, teamWebsite string
	if teamInfo != nil {
		if name, ok := teamInfo["name"].(string); ok {
			teamName = name
		}
		if desc, ok := teamInfo["description"].(string); ok {
			teamDescription = desc
		}
		if city, ok := teamInfo["city"].(string); ok {
			teamCity = city
		}
		if state, ok := teamInfo["state"].(string); ok {
			teamState = state
		}
	}

	// Get lead's single property (ONLY property to discuss)
	var propertyCtx string
	var propertyName string
	var scheduleURL string
	var applicationURL string
	if leadPropertyID != "" {
		prop := property.GetPropertyByID(teamID, leadPropertyID)
		if prop != nil {
			// Use Jake's enhanced property context
			propertyCtx = property.CreatePropertyContextForAI(prop)

			propertyName = prop.Name
			scheduleURL = prop.ScheduleURL
			applicationURL = prop.ApplicationURL
			if debugProp { pp.Printf("DEBUG: Got property context for: %s\n", prop.Name) }
		} else {
			pp.Printf("WARNING: Property not found for ID: %s\n", leadPropertyID)
		}

		// FALLBACK: If the property didn't have a customScheduleUrl populated,
		// ResolveScheduleURL will fall back to the default RentBamboo URL so we
		// never end up with an empty scheduleURL when tours are supposed to work.
		if tourScheduling && scheduleURL == "" {
			resolved := helpers.ResolveScheduleURL(teamID, leadPropertyID)
			if resolved != "" {
				scheduleURL = resolved
				if debugProp { pp.Printf("DEBUG: Resolved fallback scheduleURL for property %s: %s\n", leadPropertyID, scheduleURL) }
			}
		}
		// Same fallback for application URL.
		if applicationSending && applicationURL == "" {
			resolvedApp := helpers.ResolveApplicationURL(teamID, leadPropertyID)
			if resolvedApp != "" {
				applicationURL = resolvedApp
				if debugProp { pp.Printf("DEBUG: Resolved fallback applicationURL for property %s: %s\n", leadPropertyID, applicationURL) }
			}
		}

		// Add fallback URLs to propertyCtx if the property context doesn't have them.
		// This ensures the AI always sees a link when the feature is enabled.
		// Uses the same TOUR_LINK / APPLICATION_LINK labels that selector.go emits,
		// so the strip step below can find and remove them correctly.
		if scheduleURL != "" && !strings.Contains(propertyCtx, "TOUR_LINK:") {
			propertyCtx += fmt.Sprintf("TOUR_LINK: %s\n", scheduleURL)
		}
		if applicationURL != "" && !strings.Contains(propertyCtx, "APPLICATION_LINK:") {
			propertyCtx += fmt.Sprintf("APPLICATION_LINK: %s\n", applicationURL)
		}

		// Strip APPLICATION_LINK from context when sending is disabled,
		// otherwise the AI sees a URL it's not supposed to send.
		if !applicationSending && strings.Contains(propertyCtx, "APPLICATION_LINK:") {
			lines := strings.Split(propertyCtx, "\n")
			filtered := make([]string, 0, len(lines))
			for _, l := range lines {
				if strings.Contains(l, "APPLICATION_LINK:") {
					continue
				}
				filtered = append(filtered, l)
			}
			propertyCtx = strings.Join(filtered, "\n")
		}

		// Strip TOUR_LINK from context when tour scheduling is disabled,
		// otherwise the AI sees a URL it's not supposed to send.
		if !tourScheduling && strings.Contains(propertyCtx, "TOUR_LINK:") {
			lines := strings.Split(propertyCtx, "\n")
			filtered := make([]string, 0, len(lines))
			for _, l := range lines {
				if strings.Contains(l, "TOUR_LINK:") {
					continue
				}
				filtered = append(filtered, l)
			}
			propertyCtx = strings.Join(filtered, "\n")
		}
	}

	// FALLBACK: Tours are ON but no property is assigned to this lead.
	// Resolve a generic team-level schedule URL so the AI never ends up in
	// the "no URL available" dead end when it should be pushing tours.
	if tourScheduling && scheduleURL == "" {
		scheduleURL = "https://rentbamboo.com/schedule"
		pp.Printf("DEBUG: No lead property — using generic team schedule URL: %s\n", scheduleURL)
	}

	// Fetch Jake training
	jakeTraining, _ := g.fetchJakeTraining(teamID)
	smsTrainingContext := g.buildSMSTrainingContext(jakeTraining)

	// Extract command center fields
	var priorities, keyInfo, highlights, questions string
	var personality string
	var applicationNeeds, signingName string

	if cmdCenter != nil {
		if name, ok := cmdCenter["name"].(string); ok && name != "" {
			signingName = name
		}
		// Default fallback if signingName is empty
		if signingName == "" {
			signingName = "Jake"
		}
		if p, ok := cmdCenter["priorities"].(string); ok {
			priorities = p
		}
		if ki, ok := cmdCenter["keyInfo"].(string); ok {
			keyInfo = ki
		}
		if h, ok := cmdCenter["highlights"].(string); ok {
			highlights = h
		}
		if q, ok := cmdCenter["questions"].(string); ok {
			questions = q
		}
		if per, ok := cmdCenter["personality"].(string); ok {
			personality = per
		}
		if appNeeds, ok := cmdCenter["applicationNeeds"].(string); ok {
			applicationNeeds = appNeeds
		}

	}

	// ── STRUCTURED QUALIFICATION (Phase 1) ────────────────────────────
	// When structured mode is on: seed questions from command center onto lead,
	// detect if the lead's current message answers a pending question, and build
	// the structured context for the prompt builder.
	qualMode, _ := cmdCenter["qualificationMode"].(string)
	qualCtx := QualificationContext{Mode: "free-text"}

	if qualMode == "structured" && lead != nil {
		// Seed/sync questions from command center to lead
		if seedQualificationQuestions(lead, cmdCenter) {
			// Save immediately — if the AI call fails, seeded questions persist
			if err := saveQualificationState(g.mongoClient, teamID, lead); err != nil {
				pp.Printf("\x1b[33m⚠️ Failed to save seeded questions: %v\x1b[0m\n", err)
			}
		}

		// Build structured context for the prompt
		qualCtx = buildQualificationContext(lead, cmdCenter)
	}

	// Extract ALL critical requirements from both priorities and keyInfo (like Jake)
	allCriticalRequirements := g.extractAllCriticalRequirements(priorities, keyInfo)

	// Extract key questions for qualification
	keyQuestions := g.extractKeyQuestionsFromQuestions(questions)

	// Build three lists of qualification questions by their state:
	//   1. answeredQuestions  — already answered (for reference context)
	//   2. askedNotAnswered  — AI asked but lead hasn't answered yet
	//   3. notAskedYet       — seeded from cmd-center but never formally asked
	// The parser is allowed to match the lead's answers to groups 2 and 3.
	// Group 1 is context only — the AI won't re-output answers for these.
	//
	// Why include unasked questions: the AI's paraphrase may not have matched
	// detectAskedQuestions' regex, leaving askedAt=nil. The parser has the
	// lead's message and the question text directly — it's more reliable at
	// matching answers to questions, even if the AI hasn't formally asked.
	var (
		answeredQuestions []types.QualificationQuestion
		askedNotAnswered  []types.QualificationQuestion
		notAskedYet       []types.QualificationQuestion
	)
	if lead != nil {
		for _, q := range lead.QualificationQuestions {
			switch {
			case q.AnsweredAt != nil:
				answeredQuestions = append(answeredQuestions, q)
			case q.AskedAt != nil:
				askedNotAnswered = append(askedNotAnswered, q)
			default:
				notAskedYet = append(notAskedYet, q)
			}
		}
	}

	// Run AI parser to extract preferences AND qualification answers
	parserOutput := g.parseLeadResponse(message, answeredQuestions, askedNotAnswered, notAskedYet)

	// Whether the parser successfully answered any qualification question.
	// Used to decide if the regex-based fallback should run.
	parserAnsweredAny := false

	// Mutate in-memory lead + queue a single DB write
	//
	// Profile fields are OVERWRITTEN when the parser returns a non-empty
	// value. The parser only extracts values when the lead explicitly
	// states them (per PART 1 in the parser prompt), so overwriting is
	// safe — the lead's latest statement is the source of truth.
	//
	// Example: if lead.Pets was "2 pets" and the lead says "I have 3 pets",
	// the parser returns pets="3 pets" and we overwrite to "3 pets".
	if lead != nil {
		fieldsToSet := bson.M{}
		if parserOutput.BedroomPreference != "" {
			lead.BedroomPreference = parserOutput.BedroomPreference
			fieldsToSet["bedroomPreference"] = parserOutput.BedroomPreference
		}
		if parserOutput.Budget != "" {
			lead.Budget = parserOutput.Budget
			fieldsToSet["budget"] = parserOutput.Budget
		}
		if parserOutput.MoveInDate != "" {
			lead.MoveInDate = parserOutput.MoveInDate
			fieldsToSet["moveInDate"] = parserOutput.MoveInDate
		}
		if parserOutput.JobTitle != "" {
			lead.JobTitle = parserOutput.JobTitle
			fieldsToSet["jobTitle"] = parserOutput.JobTitle
		}
		if parserOutput.Industry != "" {
			lead.Industry = parserOutput.Industry
			fieldsToSet["industry"] = parserOutput.Industry
		}
		if parserOutput.Pets != "" {
			lead.Pets = parserOutput.Pets
			fieldsToSet["pets"] = parserOutput.Pets
		}

		// Apply qualification question answers from the AI parser
		// Build a map of question ID -> lead question for quick lookup
		now := time.Now()
		for _, ans := range parserOutput.QualificationAnswers {
			if ans.QuestionID == "" || ans.Answer == "" {
				continue
			}
			if ans.Confidence < 0.5 {
				log.Printf("\x1b[33m⚠️ Parser low-confidence answer for %s (%v) — skipping\x1b[0m\n", ans.QuestionID, ans.Confidence)
				continue
			}
			for i := range lead.QualificationQuestions {
				q := &lead.QualificationQuestions[i]
				if q.ID == ans.QuestionID && q.AnsweredAt == nil {
					q.Answer = ans.Answer
					q.AnsweredAt = &now
					// If the question was never formally asked (AskedAt == nil),
					// the parser found a match anyway. Set AskedAt to now so the
					// question shows as "answered" (not "not-asked") in subsequent
					// prompt builds. Common case: AI's paraphrase didn't match
					// detectAskedQuestions' regex, leaving askedAt=nil.
					if q.AskedAt == nil {
						q.AskedAt = &now
					}
					conf := ans.Confidence
					q.Confidence = &conf
					parserAnsweredAny = true
					log.Printf("\x1b[32m✓ Parser answered question %s: %q (conf=%v)\x1b[0m\n", ans.QuestionID, ans.Answer, ans.Confidence)
					// Persist the answer atomically so it survives a page reload.
					// Without this, the in-memory update is lost when the next
					// request reads the lead fresh from MongoDB. Mirrors what
					// detectAnsweredQuestion's fallback does below.
					if err := saveAnsweredQuestion(g.mongoClient, teamID, lead.ID, q.ID, q.Answer, q.Confidence); err != nil {
						log.Printf("\x1b[33m⚠️ Failed to save parser's answered question: %v\x1b[0m\n", err)
					}
					break
				}
			}
		}

		// ── Rebuild lastQualCtx so the chat display shows fresh status ──
		// The original buildQualificationContext() call happened BEFORE
		// the parser ran, so any question the parser just answered would
		// still show as "unanswered" in the verbose display. Rebuild
		// now that lead.QualificationQuestions has the parser's updates.
		g.lastQualCtx = buildQualificationContext(lead, cmdCenter)

		if len(fieldsToSet) > 0 {
			go g.saveFields(teamID, lead.ID, fieldsToSet)
		}
	}

	// Fallback: if the AI parser didn't find any qualification answers,
	// run the regex-based detectAnsweredQuestion as a safety net.
	if !parserAnsweredAny {
		if detectAnsweredQuestion(lead, message, lastAIReply) {
			// Save the answer atomically — no race condition with concurrent SMS
			for _, q := range lead.QualificationQuestions {
				if q.AnsweredAt != nil && q.Answer == message {
					if err := saveAnsweredQuestion(g.mongoClient, teamID, lead.ID, q.ID, q.Answer, q.Confidence); err != nil {
						pp.Printf("\x1b[33m⚠️ Failed to save answered question: %v\x1b[0m\n", err)
					}
					break
				}
			}
		}
	}

	// Extract lead fields into local variables for the prompt config.
	// Done AFTER the parser loop so the prompt shows the parser-updated
	// values (e.g., lead.Pets="3 pets" not the stale "2 pets").
	var leadFirstName, leadLastName, leadEmail, leadPhone string
	var leadBudget, leadMoveIn, leadJobTitle, leadIndustry string
	var leadSource, leadStatus string
	var leadComments, leadTags []string
	var leadBedroomPreference, leadPets string
	if lead != nil {
		leadFirstName = lead.FirstName
		leadLastName = lead.LastName
		leadEmail = lead.Email
		leadPhone = lead.Phone
		leadBudget = lead.Budget
		leadMoveIn = lead.MoveInDate
		leadJobTitle = lead.JobTitle
		leadIndustry = lead.Industry
		leadSource = lead.LeadSource
		leadStatus = lead.Status
		leadComments = lead.Comments
		leadTags = lead.Tags
		leadBedroomPreference = lead.BedroomPreference
		leadPets = lead.Pets
	}

	// Fetch the lead's tour history (scheduled + previous). Used by
	// the TOUR HISTORY section in the prompt. Best-effort — if the
	// query fails, we just skip the section.
	var toursScheduled, toursPrevious string
	var leadNotes []types.LeadNote
	var teamTimezone string
	if lead != nil {
		fetchCtx, fetchCancel := context.WithTimeout(context.Background(), 3*time.Second)
		// Fetch timezone FIRST so fetchLeadTours can use it to render
		// tour times in the team's local timezone.
		teamTimezone = g.getTeamTimezone(fetchCtx, teamID)
		toursScheduled, toursPrevious, _ = g.fetchLeadTours(fetchCtx, lead.ID, lead.Phone, teamID, teamTimezone)
		leadNotes, _ = g.fetchLeadNotes(fetchCtx, lead.ID, teamID)
		fetchCancel()
	}

	// Build system prompt using the prompt builder (single message, all sections)
	promptCfg := PromptConfig{
		SigningName:        signingName,
		Requirements:       allCriticalRequirements,
		KeyQuestions:       keyQuestions,
		Personality:        personality,
		Highlights:         highlights,
		AppSending:         applicationSending,
		AppURL:             applicationURL,
		TourScheduling:     tourScheduling,
		ScheduleURL:        scheduleURL,
		AvailabilityCtx:    availabilityContext,
		AppNeeds:           applicationNeeds,
		PropertyCtx:        propertyCtx,
		PropertyName:       propertyName,
		TeamName:           teamName,
		TeamDescription:    teamDescription,
		TeamCity:           teamCity,
		TeamState:          teamState,
		TeamPhone:          teamPhone,
		TeamWebsite:        teamWebsite,
		TeamDomain:         teamDomain,
		LeadFirstName:      leadFirstName,
		LeadLastName:       leadLastName,
		LeadEmail:          leadEmail,
		LeadPhone:          leadPhone,
		LeadBudget:         leadBudget,
		LeadMoveIn:         leadMoveIn,
		LeadJobTitle:       leadJobTitle,
		LeadIndustry:       leadIndustry,
		LeadSource:         leadSource,
		LeadStatus:         leadStatus,
		LeadComments:       leadComments,
		LeadTags:              leadTags,
		LeadBedroomPreference: leadBedroomPreference,
		LeadPets:              leadPets,
		KeyInfo:               keyInfo,
		Priorities:            priorities,
		LatestMessage:         message,
		PropertySwitchNote:    propertySwitchNote,
		TrainingCtx:        smsTrainingContext,
		QualificationCtx:   qualCtx,
		// FULLY QUALIFIED: swap to PUSH-for-tour / close-out mode.
		IsFullyQualified: isFullyQualified(lead),
		// TOUR HISTORY: show scheduled + previous tours from meetings.
		ToursScheduled: toursScheduled,
		ToursPrevious:   toursPrevious,
		// PRIVATE team-internal notes about the lead. Rendered in
		// the PRIVATE TEAM NOTES section with strict never-expose rules.
		LeadNotes: leadNotes,
		// Current date/time + team timezone (for CURRENT_DATE /
		// CURRENT_TIME / TEAM_TIMEZONE in MISCELLANEOUS).
		CurrentDateTime: time.Now(),
		TeamTimezone:    teamTimezone,
		// HITL: detect whether the most recent outbound message in the
		// chat thread was sent by a human agent. When true, the prompt
		// builder injects a "HUMAN TAKEOVER — HIGH PRIORITY" section
		// at the top of the system prompt.
		HumanTakeover: detectHumanTakeoverFromThread(chatHistory),
	}

	// Read qualifyWithoutProperty from command center
	if cmdCenter != nil {
		if qwp, ok := cmdCenter["qualifyWithoutProperty"].(bool); ok && qwp {
			promptCfg.QualifyWithoutProperty = true
		}
	}

	systemPrompt := BuildSystemPrompt(promptCfg)
	g.lastSysPrompt = systemPrompt

	// ── USER MESSAGE ──────────────────────────────────────────────────────
	// Analyze what has been asked/completed in chat history
	completedItems := g.analyzeCompletedItems(chatHistory, allCriticalRequirements, keyQuestions)
	undiscussedHighlights := g.analyzeUndiscussedHighlights(chatHistory, highlights)

	// Determine qualification status — are all key questions answered?
	qualificationStatus := g.determineQualificationStatus(chatHistory, keyQuestions, allCriticalRequirements)

	g.lastCompletedItems = completedItems
	g.lastUndiscussedHL = undiscussedHighlights
	g.lastQualStatus = qualificationStatus
	g.lastQualCtx = qualCtx
	g.lastHumanTakeover = promptCfg.HumanTakeover

	// Build availability-aware task instructions
	var availabilityTaskNote string
	if availabilityContext != "" {
		availabilityTaskNote = `
- When the lead shows interest in touring, viewing, scheduling, or availability:
  1. Recommend 2-3 specific times from the AVAILABLE TOUR TIMES listed below
  2. Include the scheduling link: [SCHEDULE_LINK]
  3. Example: "We have tours Monday at 9 AM, 10 AM, and 11 AM — book here: [SCHEDULE_LINK]"
- NEVER just say "here's the link" when you have specific times to offer — USE THEM`
	} else {
		availabilityTaskNote = `
- If the lead mentions anything about touring, viewing, or seeing the property, SKIP remaining qualification questions and send the tour link immediately`
	}

	var availabilityBlock string
	if availabilityContext != "" {
		availabilityBlock = fmt.Sprintf(`

📅 AVAILABLE TOUR TIMES (you MUST use these when recommending a tour):
%s`, availabilityContext)
	}
	g.lastAvailabilityBlock = availabilityBlock
	g.lastTaskNote = availabilityTaskNote

	userMessage := fmt.Sprintf(`━━━━ PAST CONVERSATION (turns, most recent at bottom — for continuity only) ━━━━
%s

━━━━ LATEST LEAD MESSAGE ━━━━
%s

YOUR TASK: Generate the next reply based on the importance order above.`,
		chatHistory, message)

	systemMessages := []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
		{Role: openai.ChatMessageRoleUser, Content: userMessage},
	}

	// Store the full prompt for debugging
	fullPromptStr := fmt.Sprintf("=== SYSTEM PROMPT ===\n%s\n\n=== USER MESSAGE ===\n%s", systemPrompt, userMessage)
	g.lastFullPrompt = fullPromptStr

	// Debug: print full prompt in test mode
	if os.Getenv("DEBUG_FULL_PROMPT") == "true" {
		fmt.Printf("\n\x1b[36m========== FULL PROMPT (%d system messages) ==========\x1b[0m\n", len(systemMessages)-1)
		for i, msg := range systemMessages {
			if msg.Role == openai.ChatMessageRoleUser {
				fmt.Printf("\x1b[33m[USER MSG %d] (%d chars):\x1b[0m\n%s\n\n", i, len(msg.Content), msg.Content)
			} else {
				fmt.Printf("\x1b[32m[SYS %d] (%d chars):\x1b[0m\n%s\n\n", i, len(msg.Content), msg.Content)
			}
		}
		fmt.Printf("\x1b[36m========== END FULL PROMPT ==========\x1b[0m\n\n")
	}

	// Generate
	if g.client() == nil {
		fallbackResponse := g.fallbackLiveTextResponse(message, signingName, teamPhone)
		// Send telemetry for fallback AI reply
		go func() {
			err := helpers.ReportAIReplyTelemetry(fallbackResponse, message, teamID, sessionID, leadPropertyID)
			if err != nil {
				pp.Printf("\x1b[33mFailed to send AI reply telemetry: %v\x1b[0m\n", err)
			}
		}()
		return fallbackResponse, propertyCtx, nil
	}

	type AIResponse struct {
		Response string `json:"response"`
	}

	schema, err := jsonschema.GenerateSchemaForType(AIResponse{})
	if err != nil {
		fallbackResponse := g.fallbackLiveTextResponse(message, signingName, teamPhone)
		// Send telemetry for fallback AI reply
		go func() {
			err := helpers.ReportAIReplyTelemetry(fallbackResponse, message, teamID, sessionID, leadPropertyID)
			if err != nil {
				pp.Printf("\x1b[33mFailed to send AI reply telemetry: %v\x1b[0m\n", err)
			}
		}()
		return fallbackResponse, propertyCtx, nil
	}

	req := openai.ChatCompletionRequest{
		Model:               g.modelName,
		Messages:            systemMessages,
		Temperature:         0.5,
		MaxCompletionTokens: 350,
	}
	if g.useJSONResponse {
		req.ResponseFormat = &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONSchema,
			JSONSchema: &openai.ChatCompletionResponseFormatJSONSchema{
				Name:   "ai_response",
				Schema: schema,
			},
		}
	}

	resp, err := g.client().CreateChatCompletion(context.Background(), req)

	if err != nil || len(resp.Choices) == 0 {
		pp.Printf("ERROR: OpenAI call failed: %v\n", err)
		fallbackResponse := g.fallbackLiveTextResponse(message, signingName, teamPhone)
		// Send telemetry for fallback AI reply
		go func() {
			err := helpers.ReportAIReplyTelemetry(fallbackResponse, message, teamID, sessionID, leadPropertyID)
			if err != nil {
				pp.Printf("\x1b[33mFailed to send AI reply telemetry: %v\x1b[0m\n", err)
			}
		}()
		return fallbackResponse, propertyCtx, nil
	}

	// Parse response
	var responseText string
	if g.useJSONResponse {
		var aiResponse AIResponse
		err = json.Unmarshal([]byte(resp.Choices[0].Message.Content), &aiResponse)
		if err != nil {
			pp.Printf("ERROR: Failed to parse AI response: %v\n", err)
			fallbackResponse := g.fallbackLiveTextResponse(message, signingName, teamPhone)
			go func() {
				err := helpers.ReportAIReplyTelemetry(fallbackResponse, message, teamID, sessionID, leadPropertyID)
				if err != nil {
					pp.Printf("\x1b[33mFailed to send AI reply telemetry: %v\x1b[0m\n", err)
				}
			}()
			return fallbackResponse, propertyCtx, nil
		}
		responseText = aiResponse.Response
	} else {
		responseText = resp.Choices[0].Message.Content
	}

	// Sanitize the response to ensure valid UTF-8 and remove problematic characters
	responseText = helpers.CleanText(responseText)

	// POST-PROCESS: Enforce location phrasing rule as safety net
	responseText = helpers.EnforceLocationPhrasing(responseText)

	// CRITICAL: Replace any link placeholders the AI emitted with the actual
	// URLs. This handles both the canonical `[SCHEDULE_LINK]` / `[APPLICATION_LINK]`
	// placeholders *and* natural-language variants the model tends to produce
	// (e.g. `[tour scheduling link]`, `[scheduling link]`, `[application link]`).
	// Without this step the raw placeholders leak to the user — see the bug
	// report where SMS recipients received messages containing literal
	// "[tour scheduling link]" text.
	responseText = helpers.ReplaceLinkPlaceholders(responseText, scheduleURL, applicationURL, false)

	// Safety net: if the AI emitted a raw rentbamboo schedule URL from chat
	// history (e.g. when the lead was previously assigned to a different
	// property), forcibly rewrite all rentbamboo schedule URLs to the current
	// property's correct URL.
	if scheduleURL != "" && strings.Contains(responseText, "rentbamboo.com/schedule/") {
		re := regexp.MustCompile(`https?://rentbamboo\.com/schedule/[a-f0-9-]+`)
		responseText = re.ReplaceAllString(responseText, scheduleURL)
	}

	// ── STRUCTURED QUALIFICATION: Post-AI tracking ────────────────────────
	// If structured mode is on, detect which questions the AI just asked in its
	// response and save each one atomically to MongoDB.
	if qualMode == "structured" && lead != nil {
		askedIDs := detectAskedQuestions(responseText, lead, cmdCenter)
		for _, qid := range askedIDs {
			if err := saveAskedQuestion(g.mongoClient, teamID, lead.ID, qid); err != nil {
				pp.Printf("\x1b[33m⚠️ Failed to save asked question %s: %v\x1b[0m\n", qid, err)
			}
		}
	}

	// Send telemetry for AI reply
	go func() {
		err := helpers.ReportAIReplyTelemetry(responseText, message, teamID, sessionID, leadPropertyID)
		if err != nil {
			pp.Printf("\x1b[33mFailed to send AI reply telemetry: %v\x1b[0m\n", err)
		}
	}()

	return responseText, propertyCtx, nil
}

// GetLastSystemPrompt returns the last built system prompt (for test tool display).
func (g *AIGenerator) GetLastSystemPrompt() string {
	return g.lastSysPrompt
}

// GetLastFullPrompt returns the full prompt sent to AI (system + user message combined).
func (g *AIGenerator) GetLastFullPrompt() string {
	return g.lastFullPrompt
}

func (g *AIGenerator) GetLastCompletedItems() string        { return g.lastCompletedItems }
func (g *AIGenerator) GetLastUndiscussedHL() string         { return g.lastUndiscussedHL }
func (g *AIGenerator) GetLastQualStatus() string             { return g.lastQualStatus }
func (g *AIGenerator) GetLastQualCtx() QualificationContext  { return g.lastQualCtx }
func (g *AIGenerator) GetLastAvailabilityBlock() string      { return g.lastAvailabilityBlock }
func (g *AIGenerator) GetLastTaskNote() string               { return g.lastTaskNote }

// GetLastHumanTakeover returns whether the most recent outbound
// message in the chat history was sent by a human agent. Used by
// the cmd/chat verbose display to surface HITL state.
func (g *AIGenerator) GetLastHumanTakeover() bool { return g.lastHumanTakeover }

// GetLastReasoningContent returns the reasoning_content (model's
// internal thinking) from the most recent DeepSeek call. Returns
// empty string if thinking wasn't enabled, the model returned no
// reasoning, or the model isn't DeepSeek.
func (g *AIGenerator) GetLastReasoningContent() string {
	if g.deepseekClient == nil {
		return ""
	}
	return g.deepseekClient.ReasoningContent()
}

// =============================================================================
// HELPER METHODS
// =============================================================================

func (g *AIGenerator) fetchCommandCenter(teamID string) (bson.M, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := g.mongoClient.Database("teams").Collection("command-centers")
	filter := bson.M{"teamId": teamID}

	var result bson.M
	err := collection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}

	return result, nil
}

func (g *AIGenerator) getTeamInfo(teamID string) (bson.M, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := g.mongoClient.Database("teams").Collection("teams")
	filter := bson.M{"teamId": teamID}

	var result bson.M
	err := collection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}

	return result, nil
}

func (g *AIGenerator) fetchJakeTraining(teamID string) (*types.JakeTraining, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collection := g.mongoClient.Database("teams").Collection("jake-training")
	filter := bson.M{"teamId": teamID}

	var result types.JakeTraining
	err := collection.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}

	return &result, nil
}

func (g *AIGenerator) buildSMSTrainingContext(training *types.JakeTraining) string {
	if training == nil || len(training.JakeSMS.Files) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("SMS Response Style Examples:\n\n")

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

	return sb.String()
}

func (g *AIGenerator) extractAllCriticalRequirements(priorities, keyInfo string) string {
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

	// Key phrases that indicate important requirements
	requirementPhrases := []string{
		// Income/Financial requirements
		"2.5x", "2.5 times", "minimum monthly income", "income approval",
		"base rent", "income requirement", "financial requirement",
		"income", "rent", "financial",

		// Documentation requirements
		"pay stub", "paystub", "check stub", "3 most recent",
		"documentation", "verify income", "proof of income",
		"stubs", "pay", "document",

		// Action requirements
		"send to", "email to", "submit to", "need to send", "must send",
		"require", "need", "must", "should", "will need",
		"send", "email", "submit",

		// Process requirements
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

		// Skip questions
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

		// Check for requirement phrases
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

// extractKeyQuestionsFromQuestions extracts key questions from the questions field
func (g *AIGenerator) extractKeyQuestionsFromQuestions(questions string) []string {
	if questions == "" {
		return nil
	}

	// Split by common delimiters
	var questionList []string

	// Try splitting by newline first
	lines := strings.Split(questions, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Clean up the line - remove bullet points, numbers, etc.
		line = strings.TrimPrefix(line, "- ")
		line = strings.TrimPrefix(line, "* ")
		line = strings.TrimPrefix(line, "• ")
		// Remove leading numbers like "1." or "1)"
		line = strings.TrimPrefix(line, "1.")
		line = strings.TrimPrefix(line, "2.")
		line = strings.TrimPrefix(line, "3.")
		line = strings.TrimPrefix(line, "4.")
		line = strings.TrimPrefix(line, "5.")
		line = strings.TrimPrefix(line, "1)")
		line = strings.TrimPrefix(line, "2)")
		line = strings.TrimPrefix(line, "3)")
		line = strings.TrimPrefix(line, "4)")
		line = strings.TrimPrefix(line, "5)")
		line = strings.TrimSpace(line)

		if line != "" {
			questionList = append(questionList, line)
		}
	}

	// If no lines, try splitting by semicolon
	if len(questionList) == 0 {
		parts := strings.Split(questions, ";")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				questionList = append(questionList, part)
			}
		}
	}

	// Limit to top 5 questions
	if len(questionList) > 5 {
		questionList = questionList[:5]
	}

	return questionList
}

// fallbackLiveTextResponse returns a fallback response when AI generation fails
func (g *AIGenerator) fallbackLiveTextResponse(message, signingName, teamPhone string) string {
	// Use default if signingName is empty
	if signingName == "" {
		signingName = "Jake"
	}
	response := fmt.Sprintf("Thanks for your message! I'd love to help you find the perfect place. Text me back with any questions! -%s", signingName)
	// Sanitize the response to ensure valid UTF-8 and remove problematic characters
	return helpers.CleanText(response)
}

// analyzeCompletedItems analyzes chat history to find what has been completed/answered
func (g *AIGenerator) analyzeCompletedItems(chatHistory, requirements string, questions []string) string {
	if chatHistory == "" {
		return "Nothing yet - this is the start of conversation"
	}

	lowerChat := strings.ToLower(chatHistory)
	var completed []string

	// Track info already shared by the agent
	infoShared := map[string][]string{
		// Location/address indicators
		"address shared": {"located at", "address is", "we are at", "find us at", "our address"},
		// Tour format shared
		"tour format explained": {"open house", "15 minutes", "15 minute", "arrive on time"},
		// Schedule link sent
		"schedule link sent": {"schedule your tour here", "schedule here", "rentbamboo.com/schedule", "scheduling link"},
		// Application requirements mentioned
		"application requirements listed": {"government id", "paystubs", "pay stubs", "bank statements", "application process"},
		// Application link sent
		"application link sent": {"application link", "apply here", "application url"},
	}

	for info, phrases := range infoShared {
		for _, phrase := range phrases {
			if strings.Contains(lowerChat, phrase) {
				completed = append(completed, info)
				break
			}
		}
	}

	// Track questions already answered by lead
	questionAnswered := map[string][]string{
		// Eviction question answered
		"eviction status confirmed": {"no eviction", "not had any eviction", "haven't had eviction", "no we have not had"},
		// Lease question answered
		"lease info provided": {"lease ends", "lease is up", "current lease", "my lease", "our lease", "ends may", "ends june", "ends july", "month to month", "no lease"},
		// Move-in date provided
		"move-in date provided": {"move in", "moving in", "move-in", "looking to move"},
		// Budget mentioned
		"budget discussed": {"budget", "afford", "price range", "looking for something around"},
	}

	for info, phrases := range questionAnswered {
		for _, phrase := range phrases {
			if strings.Contains(lowerChat, phrase) {
				completed = append(completed, info)
				break
			}
		}
	}

	// Track conversation progress
	progressIndicators := map[string]string{
		"schedule":    "tour scheduling discussed",
		"scheduled":   "tour may be scheduled",
		"booked":      "tour booked",
		"application": "application mentioned",
		"applied":     "application started",
		"submitted":   "application submitted",
	}

	for phrase, item := range progressIndicators {
		if strings.Contains(lowerChat, phrase) {
			completed = append(completed, item)
		}
	}

	if len(completed) == 0 {
		return "Nothing specific identified yet"
	}

	// Remove duplicates
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

// analyzeUndiscussedHighlights finds highlights that haven't been mentioned yet
func (g *AIGenerator) analyzeUndiscussedHighlights(chatHistory, highlights string) string {
	if highlights == "" {
		return "No highlights configured"
	}

	if chatHistory == "" {
		return highlights // All highlights are new
	}

	lowerChat := strings.ToLower(chatHistory)
	var undiscussed []string

	// Split highlights into lines and check if discussed
	highlightLines := strings.Split(highlights, "\n")
	for _, line := range highlightLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Extract key words from highlight (first few words)
		lowerLine := strings.ToLower(line)
		// Check if any significant word from this highlight appears in chat
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

// determineQualificationStatus checks if key qualification questions have been answered
// and returns a clear status message for the AI to act on
func (g *AIGenerator) determineQualificationStatus(chatHistory string, keyQuestions []string, requirements string) string {
	if chatHistory == "" {
		return "NOT STARTED - Ask the first qualification question."
	}

	lowerChat := strings.ToLower(chatHistory)

	// Count how many key questions appear to have been answered
	totalQuestions := len(keyQuestions)
	answeredCount := 0
	var unanswered []string

	for _, q := range keyQuestions {
		lowerQ := strings.ToLower(q)
		// Extract key words from the question to check if topic was discussed
		words := strings.Fields(lowerQ)
		discussed := false
		for _, word := range words {
			// Only check meaningful words (longer than 4 chars)
			if len(word) > 4 && strings.Contains(lowerChat, word) {
				discussed = true
				break
			}
		}
		if discussed {
			answeredCount++
		} else {
			unanswered = append(unanswered, q)
		}
	}

	// Also check requirements keywords in chat
	requirementsMentioned := false
	if requirements != "" {
		reqLines := strings.Split(requirements, "\n")
		for _, line := range reqLines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			lowerLine := strings.ToLower(line)
			words := strings.Fields(lowerLine)
			for _, word := range words {
				if len(word) > 4 && strings.Contains(lowerChat, word) {
					requirementsMentioned = true
					break
				}
			}
			if requirementsMentioned {
				break
			}
		}
	}

	// Determine status
	if totalQuestions == 0 {
		// No specific questions configured - consider qualified after any engagement
		if len(lowerChat) > 200 {
			return "\u2705 QUALIFIED - No specific questions required. PUSH FOR TOUR NOW. Send the scheduling link and encourage them to book a time."
		}
		return "IN PROGRESS - Engage briefly then push for tour."
	}

	if answeredCount >= totalQuestions || (totalQuestions > 0 && answeredCount >= totalQuestions-1 && requirementsMentioned) {
		return "\u2705 QUALIFIED - All key questions have been addressed. STOP ASKING QUESTIONS. Your ONLY job now is to get them to schedule a tour. Send the tour link: [SCHEDULE_LINK]"
	}

	if answeredCount > 0 {
		return fmt.Sprintf("IN PROGRESS - %d of %d questions answered. Still need to ask: %s. After this, push for tour.", answeredCount, totalQuestions, strings.Join(unanswered, "; "))
	}

	return fmt.Sprintf("NOT STARTED - %d questions remaining. Ask ONE at a time, then push for tour.", totalQuestions)
}

// isTourSchedulingRequest checks if a message is asking about tour scheduling
func (g *AIGenerator) isTourSchedulingRequest(message string) bool {
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
		"when can i tour",
		"when can i come",
		"what times are available",
		"what time can i",
		"what days do you",
		"available tomorrow",
		"available today",
		"available this week",
		"available monday",
		"available tuesday",
		"available wednesday",
		"available thursday",
		"available friday",
		"available saturday",
		"available sunday",
		"can i come tomorrow",
		"can i come today",
		"this weekend",
		"next available",
		"tour time",
		"tour date",
		"viewing time",
		"showing time",
		"showing available",
		"schedule a showing",
		"schedule showing",
	}

	for _, keyword := range tourKeywords {
		if strings.Contains(lowerMsg, keyword) {
			return true
		}
	}

	return false
}



