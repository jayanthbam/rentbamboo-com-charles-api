package preference

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/sashabaranov/go-openai"
)

// Parser handles AI-based preference extraction
type Parser struct {
	client *openai.Client
}

// NewParser creates a new preference parser
func NewParser() *Parser {
	client := openai.NewClient(os.Getenv("OPENAI_API_KEY"))
	return &Parser{
		client: client,
	}
}

// ExtractPreferencesFromText uses AI to extract preferences from user message
func (p *Parser) ExtractPreferencesFromText(ctx context.Context, message string) (*ExtractedPreferences, error) {
	systemPrompt := `You are a housing preference extraction system. Analyze the user's message and extract any housing preferences they EXPLICITLY state.

Return ONLY valid JSON (no markdown, no explanation) with these fields:
{"bedrooms": 0, "bathrooms": 0, "budgetMin": 0, "budgetMax": 0, "moveInDate": "", "locations": [], "petNeeds": [], "amenities": [], "propertyType": "", "sqftMin": 0, "sqftMax": 0, "sortBy": ""}

CRITICAL RULES - FOLLOW THESE EXACTLY:
1. NEVER assume or infer budget. Only extract budgetMin/budgetMax if user EXPLICITLY states a dollar amount (e.g., "$1000", "1000 a month", "under 1500"). If they say "i need a 2 bed" without a price, set budgetMin: 0, budgetMax: 0.
2. NEVER assume move-in date. Only set moveInDate if they explicitly say "moving next week", "need it by March 1st", etc.
3. IGNORE "any" - if user says "any bedrooms", "anytime", "any price", "whatever", etc., treat as NO preference (return 0 or empty).
4. Only set propertyType if they explicitly say "house", "apartment", "condo", "unit"
5. Only set sortBy if they explicitly mention sorting

Field details:
- bedrooms: single number (e.g., 2 for "2 bedroom", 1 for "1 bed"). If they say "1 or 2 beds", pick the higher one.
- bathrooms: single bathroom count (e.g., 1 for "1 bathroom", 2 for "2 baths")
- budgetMin/budgetMax: ONLY if they say a specific dollar amount
- moveInDate: ONLY if they say a specific date
- locations: cities/neighborhoods mentioned
- petNeeds: pet requirements ("cat", "dog", "pets")
- amenities: amenities mentioned (pool, gym, parking, laundry)
- propertyType: "single" for houses, "multi" for apartments/units
- sqftMin/sqftMax: ONLY if they mention square footage
- sortBy: ONLY for explicit sorting requests`

	resp, err := p.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: "gpt-4.1-mini-2025-04-14",
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleSystem,
					Content: systemPrompt,
				},
				{
					Role:    openai.ChatMessageRoleUser,
					Content: message,
				},
			},
			Temperature:         0.3,
			MaxCompletionTokens: 500,
		},
	)

	if err != nil {
		return nil, fmt.Errorf("failed to extract preferences: %w", err)
	}

	// Clean the response - remove any markdown formatting
	content := resp.Choices[0].Message.Content
	content = cleanJSONResponse(content)

	var extracted ExtractedPreferences
	err = json.Unmarshal([]byte(content), &extracted)
	if err != nil {
		return nil, fmt.Errorf("failed to parse extracted preferences: %w", err)
	}

	return &extracted, nil
}

// cleanJSONResponse removes markdown formatting from JSON response
func cleanJSONResponse(content string) string {
	// Remove ```json and ``` markers
	content = removeMarkdownBlock(content)
	// Trim whitespace
	content = trimWhitespace(content)
	return content
}

func removeMarkdownBlock(s string) string {
	// Remove ```json at start
	if len(s) >= 7 && s[:7] == "```json" {
		s = s[7:]
	}
	// Remove ``` at end
	if len(s) >= 4 && s[len(s)-4:] == "```" {
		s = s[:len(s)-4]
	}
	return s
}

func trimWhitespace(s string) string {
	// Remove leading/trailing whitespace
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\n' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\n' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

// MergePreferences merges extracted preferences with existing preferences
func MergePreferences(existing *Preferences, extracted *ExtractedPreferences) *Preferences {
	merged := *existing

	if extracted.Bedrooms > 0 {
		merged.Bedrooms = extracted.Bedrooms
	}

	if extracted.BudgetMin > 0 {
		merged.BudgetMin = extracted.BudgetMin
	}

	if extracted.BudgetMax > 0 {
		merged.BudgetMax = extracted.BudgetMax
	}

	if len(extracted.Locations) > 0 {
		merged.Locations = extracted.Locations
	}

	if len(extracted.PetNeeds) > 0 {
		merged.PetNeeds = extracted.PetNeeds
	}

	if len(extracted.Amenities) > 0 {
		merged.Amenities = extracted.Amenities
	}

	if extracted.PropertyType != "" {
		merged.PropertyType = extracted.PropertyType
	}

	if extracted.Bathrooms > 0 {
		merged.Bathrooms = extracted.Bathrooms
	}

	if extracted.SquareFootageMin > 0 {
		merged.SquareFootageMin = extracted.SquareFootageMin
	}

	if extracted.SquareFootageMax > 0 {
		merged.SquareFootageMax = extracted.SquareFootageMax
	}

	if extracted.MoveInDate != "" {
		if parsed, err := time.Parse("2006-01-02", extracted.MoveInDate); err == nil {
			merged.MoveInDate = &parsed
		}
	}

	// Handle sortBy - always update if explicitly extracted
	if extracted.SortBy != "" {
		merged.SortBy = SortOption(extracted.SortBy)
	}

	merged.LastUpdated = time.Now()
	return &merged
}

// ProcessMessageWithPreferences processes a user message and updates preferences
func ProcessMessageWithPreferences(
	ctx context.Context,
	store *Store,
	teamID string,
	sessionID string,
	message string,
	role string,
) (*Preferences, bool, error) {
	existing, err := store.GetBySessionID(ctx, sessionID)
	if err != nil {
		return nil, false, err
	}

	if existing == nil {
		existing = NewLeadPreference(teamID, sessionID)
		existing.TeamID = teamID
	}

	parser := NewParser()
	extracted, err := parser.ExtractPreferencesFromText(ctx, message)
	if err != nil {
		// If we can't extract preferences, just return existing preferences
		return &existing.Preferences, false, nil
	}

	oldPrefs := existing.Preferences
	newPrefs := MergePreferences(&existing.Preferences, extracted)
	changed := !preferencesEqual(oldPrefs, *newPrefs)

	existing.Preferences = *newPrefs
	err = store.Upsert(ctx, existing)
	if err != nil {
		return nil, changed, err
	}

	return newPrefs, changed, nil
}

func preferencesEqual(a, b Preferences) bool {
	if a.Bedrooms != b.Bedrooms {
		return false
	}
	if a.Bathrooms != b.Bathrooms {
		return false
	}
	if a.BudgetMin != b.BudgetMin || a.BudgetMax != b.BudgetMax {
		return false
	}
	if len(a.Locations) != len(b.Locations) || len(a.PetNeeds) != len(b.PetNeeds) ||
		len(a.Amenities) != len(b.Amenities) {
		return false
	}
	if a.PropertyType != b.PropertyType {
		return false
	}
	if a.SquareFootageMin != b.SquareFootageMin || a.SquareFootageMax != b.SquareFootageMax {
		return false
	}
	if a.SortBy != b.SortBy {
		return false
	}
	return true
}
