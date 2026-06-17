package ai

import (
	"bamboo/report"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

// Document represents a text document with its embedding
type Document struct {
	Text       string    `json:"text"`
	Embedding  []float32 `json:"embedding"`
	propertyId string
}

// SearchResult represents a search result with similarity score
type SearchResult struct {
	Document   Document
	Similarity float32
}

// QASystem handles document storage and similarity search
type QASystem struct {
	documents []Document
	teamId    string
}

// EmbeddingStorage represents stored embeddings for a property
type EmbeddingStorage struct {
	PropertyID string    `bson:"propertyId" json:"propertyId"`
	Embedding  []float32 `bson:"embedding" json:"embedding"`
	Text       string    `bson:"text" json:"text"`
	TeamID     string    `bson:"teamId" json:"teamId"`
	EmbeddedAt time.Time `bson:"embeddedAt" json:"embeddedAt"`
	Version    int       `bson:"version" json:"version"`
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

// NewQASystemWithTeam creates a new QA system with team ID
func NewQASystemWithTeam(teamId string) *QASystem {
	return &QASystem{
		documents: make([]Document, 0),
		teamId:    teamId,
	}
}

// AddDocument adds a document to the system
func (qa *QASystem) AddDocument(text string, embedding []float32, id string) {
	doc := Document{
		Text:       text,
		Embedding:  embedding,
		propertyId: id,
	}
	qa.documents = append(qa.documents, doc)
}

// GetTeamID returns the team ID associated with this QA system
func (qa *QASystem) GetTeamID() string {
	return qa.teamId
}

// DocumentCount returns the number of documents in the QA system
func (qa *QASystem) DocumentCount() int {
	return len(qa.documents)
}

// GetDocumentCount returns the number of documents in the system (alias for DocumentCount)
func (qa *QASystem) GetDocumentCount() int {
	return len(qa.documents)
}

// GetEmbeddingsForStorage returns embeddings in storage format
func (qa *QASystem) GetEmbeddingsForStorage() []EmbeddingStorage {
	var embeddings []EmbeddingStorage

	for _, doc := range qa.documents {
		embeddings = append(embeddings, EmbeddingStorage{
			PropertyID: doc.propertyId,
			Embedding:  doc.Embedding,
			Text:       doc.Text,
			TeamID:     qa.teamId,
			EmbeddedAt: time.Now(),
			Version:    1,
		})
	}

	return embeddings
}

// LoadFromStorage loads embeddings from storage format
func (qa *QASystem) LoadFromStorage(embeddings []EmbeddingStorage) {
	for _, emb := range embeddings {
		qa.AddDocument(emb.Text, emb.Embedding, emb.PropertyID)
	}
}

// GetPropertyContext returns the document text for a given property ID
func (qa *QASystem) GetPropertyContext(propertyID string) (string, bool) {
	// Remove chunk suffix if present (e.g., "prop123-chunk1" -> "prop123")
	baseID := strings.Split(propertyID, "-chunk")[0]

	for _, doc := range qa.documents {
		docID := doc.propertyId
		// Check exact match or base match (for chunks)
		if docID == propertyID || docID == baseID || strings.HasPrefix(docID, baseID+"-chunk") {
			return doc.Text, true
		}
	}
	return "", false
}

// ResolveScheduleURL resolves the scheduling URL for a property
// It first checks for customScheduleUrl in property data, then falls back to default
func ResolveScheduleURL(qa *QASystem, teamID, propertyID string) string {
	// Strip chunk suffix if present
	baseID := strings.Split(propertyID, "-chunk")[0]

	// Try to get property context to check for custom URL
	context, found := qa.GetPropertyContext(baseID)
	if !found {
		// Try original propertyID
		context, found = qa.GetPropertyContext(propertyID)
	}

	if found && context != "" {
		// Try to extract customScheduleUrl from property data
		// Find the JSON in the context
		jsonStart := strings.Index(context, "{")
		jsonEnd := strings.LastIndex(context, "}")

		if jsonStart >= 0 && jsonEnd > jsonStart {
			jsonStr := context[jsonStart : jsonEnd+1]
			var propertyData map[string]interface{}
			if err := json.Unmarshal([]byte(jsonStr), &propertyData); err == nil {
				// Check for customScheduleUrl
				if customURL, ok := propertyData["customScheduleUrl"].(string); ok && customURL != "" {
					return customURL
				}
				// Check for applicationUrl as fallback
				if appURL, ok := propertyData["applicationUrl"].(string); ok && appURL != "" {
					return appURL
				}
			}
		}
	}

	// Fall back to default RentBamboo scheduling URL
	return fmt.Sprintf("https://rentbamboo.com/schedule/%s", baseID)
}

// Constants for batch processing
const (
	MAX_EMBED_TOKENS = 7000 // Safety margin from 8192 token limit
	CHUNK_SIZE       = 5    // Default properties per batch
	SLEEP_BETWEEN_MS = 500  // Milliseconds between batches
)

// EstimateTokens roughly estimates token count (4 chars ≈ 1 token for English text)
func EstimateTokens(text string) int {
	// Rough estimation: 4 characters ≈ 1 token for English text
	// This is conservative - actual tokenization may vary
	return len(text) / 4
}

// GetEmbedding gets embedding for a single text
func GetEmbedding(client *openai.Client, text string) ([]float32, error) {
	embeddings, err := GetEmbeddingsBatch(client, []string{text})
	if err != nil {
		return nil, err
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return embeddings[0], nil
}

// GetEmbeddingWithClient gets embedding for a single text with provided client
func GetEmbeddingWithClient(client *openai.Client, text string) ([]float32, error) {
	return GetEmbedding(client, text)
}

// GetEmbeddingsBatch gets embeddings for multiple texts in a single API call
func GetEmbeddingsBatch(client *openai.Client, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, fmt.Errorf("no texts provided")
	}

	resp, err := client.CreateEmbeddings(
		context.Background(),
		openai.EmbeddingRequest{
			Input: texts,
			Model: openai.LargeEmbedding3,
		},
	)

	if err != nil {
		return nil, err
	}

	// Extract embeddings from response
	embeddings := make([][]float32, len(resp.Data))
	for i, data := range resp.Data {
		embeddings[i] = data.Embedding
	}

	return embeddings, nil
}

func Sqrt(x float32) float32 {
	return float32(math.Sqrt(float64(x)))
}

func CalculateCosineSimilarity(a, b []float32) float32 {
	var dotProduct float32
	var normA float32
	var normB float32

	for i := range a {
		dotProduct += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	// Avoid division by zero
	if normA == 0 || normB == 0 {
		return 0
	}

	return dotProduct / (Sqrt(normA) * Sqrt(normB))
}

// Search finds the most similar documents to a query embedding
func (qa *QASystem) Search(queryEmbedding []float32, topK int) []SearchResult {
	results := make([]SearchResult, 0, len(qa.documents))

	// Calculate similarity scores for all documents
	for _, doc := range qa.documents {
		similarity := CalculateCosineSimilarity(queryEmbedding, doc.Embedding)
		results = append(results, SearchResult{
			Document:   doc,
			Similarity: similarity,
		})
	}

	// Sort results by similarity score in descending order
	sort.Slice(results, func(i, j int) bool {
		return results[i].Similarity > results[j].Similarity
	})

	// Return top K results
	if topK > len(results) {
		topK = len(results)
	}
	return results[:topK]
}

// AnswerQuestion generates an answer based on similar documents and returns property IDs
func (qa *QASystem) AnswerQuestion(questionEmbedding []float32) (string, []string) {
	// Find most relevant documents - top 2 docs
	results := qa.Search(questionEmbedding, 2) // 2 docs

	if len(results) == 0 {
		return "I couldn't find any property information that matches your search.", nil
	}

	// Lower threshold from 0.3 to 0.15 for better matching
	threshold := float32(0.15)

	// Check if the highest relevance score is below the threshold
	if results[0].Similarity < threshold {
		return "I couldn't find any property information that matches your search.", nil
	}

	// More conversational answer generation
	var answer strings.Builder
	var propertyIDs []string

	// Count properties above threshold
	relevantCount := 0
	for _, result := range results {
		if result.Similarity >= threshold {
			relevantCount++
		}
	}

	if relevantCount == 1 {
		answer.WriteString(fmt.Sprintf("I found %d property that might match what you're looking for:\n\n", relevantCount))
	} else if relevantCount > 1 {
		answer.WriteString(fmt.Sprintf("I found %d properties that might match what you're looking for:\n\n", relevantCount))
	} else {
		// Should not happen since we checked results[0].Similarity >= threshold
		return "I couldn't find any property information that matches your search.", nil
	}

	displayedCount := 0
	for i, result := range results {
		// Skip documents with relevance below threshold
		if result.Similarity < threshold {
			continue
		}

		// Limit to showing 3 properties max to avoid overwhelming
		if displayedCount >= 3 {
			break
		}

		propertyId := strings.TrimSpace(result.Document.propertyId)

		// Extract key information from property text instead of using full text
		summary := summarizePropertyText(result.Document.Text)

		if propertyId != "" {
			answer.WriteString(fmt.Sprintf("**Property %d** (relevance score: %.2f):\n%s\n\n",
				i+1, result.Similarity, summary))
			// Add property ID to the list
			propertyIDs = append(propertyIDs, propertyId)
		} else {
			answer.WriteString(fmt.Sprintf("**Property %d** (relevance score: %.2f):\n%s\n\n",
				i+1, result.Similarity, summary))
		}

		displayedCount++
	}

	// Add a closing note
	if relevantCount > 0 {
		answer.WriteString("Let me know if you'd like more details about any of these properties!")
	}

	return answer.String(), propertyIDs
}

// summarizePropertyText extracts key information from property text and formats it naturally
func summarizePropertyText(fullText string) string {
	// Handle malformed JSON with markers like "=== PROPERTY START ===\n{...}"
	cleanText := fullText

	// Try to find actual JSON start (after markers)
	jsonStart := strings.Index(cleanText, "{")
	if jsonStart > 0 {
		// Found JSON start after some prefix
		cleanText = cleanText[jsonStart:]
	}

	// Also try from the end (in case there are markers at both ends)
	jsonEnd := strings.LastIndex(cleanText, "}")
	if jsonEnd > 0 && jsonEnd < len(cleanText)-1 {
		// There's content after the JSON closing brace
		cleanText = cleanText[:jsonEnd+1]
	}

	cleanText = strings.TrimSpace(cleanText)

	// Try to parse as JSON
	var propertyData map[string]interface{}
	if err := json.Unmarshal([]byte(cleanText), &propertyData); err != nil {
		// If not valid JSON, try to extract JSON more aggressively
		// Look for JSON object between { and }
		start := strings.Index(fullText, "{")
		end := strings.LastIndex(fullText, "}")
		if start > 0 && end > start {
			potentialJSON := fullText[start : end+1]
			if err := json.Unmarshal([]byte(potentialJSON), &propertyData); err != nil {
				// Still not valid JSON, return truncated version
				if len(fullText) > 800 {
					return fullText[:800] + "..."
				}
				return fullText
			}
		} else {
			// No JSON found, return truncated version
			if len(fullText) > 800 {
				return fullText[:800] + "..."
			}
			return fullText
		}
	}

	// Extract key information
	var summary strings.Builder

	// Property name/address
	if propertyName, ok := propertyData["propertyName"].(string); ok && propertyName != "" {
		summary.WriteString(fmt.Sprintf("Property: %s", propertyName))
	} else if location, ok := propertyData["location"].(map[string]interface{}); ok {
		if address, ok := location["fullAddress"].(string); ok && address != "" {
			summary.WriteString(fmt.Sprintf("Address: %s", address))
		} else if street, ok := location["streetAddress"].(string); ok && street != "" {
			summary.WriteString(fmt.Sprintf("Address: %s", street))
			if city, ok := location["city"].(string); ok && city != "" {
				summary.WriteString(fmt.Sprintf(", %s", city))
			}
			if state, ok := location["state"].(string); ok && state != "" {
				summary.WriteString(fmt.Sprintf(", %s", state))
			}
		}
	}

	// Property UUID (for tour links)
	if propertyId, ok := propertyData["id"].(string); ok && propertyId != "" {
		summary.WriteString(fmt.Sprintf("\nProperty ID: %s", propertyId))

		// Include custom schedule URL if available, otherwise standard format
		if customScheduleUrl, ok := propertyData["customScheduleUrl"].(string); ok && customScheduleUrl != "" {
			summary.WriteString(fmt.Sprintf("\nTour/Scheduling URL: %s (CUSTOM)", customScheduleUrl))
		} else {
			// Standard tour link format
			tourLink := fmt.Sprintf("https://rentbamboo.com/schedule/%s", propertyId)
			summary.WriteString(fmt.Sprintf("\nTour/Scheduling URL: %s (STANDARD)", tourLink))
		}
	}

	// Application URL - explicitly mark availability
	if applicationUrl, ok := propertyData["applicationUrl"].(string); ok && applicationUrl != "" {
		summary.WriteString(fmt.Sprintf("\nApplication URL: %s (AVAILABLE)", applicationUrl))
	} else {
		summary.WriteString("\nApplication URL: NOT AVAILABLE - contact office for application form")
	}

	// Square footage
	if sqft, ok := propertyData["sqft"].(float64); ok && sqft > 0 {
		summary.WriteString(fmt.Sprintf("\nSquare footage: %.0f sqft", sqft))
	}

	// Extract unit information
	if units, ok := propertyData["units"].([]interface{}); ok && len(units) > 0 {
		summary.WriteString("\n\nAvailable units:")

		// Show first 3 units max
		maxUnits := 3
		if len(units) < maxUnits {
			maxUnits = len(units)
		}

		for i := 0; i < maxUnits; i++ {
			if unit, ok := units[i].(map[string]interface{}); ok {
				summary.WriteString("\n- ")

				// Unit type
				if unitType, ok := unit["unitType"].(string); ok && unitType != "" {
					summary.WriteString(unitType)
				} else if bedrooms, ok := unit["bedrooms"].(float64); ok {
					summary.WriteString(fmt.Sprintf("%.0f bedroom", bedrooms))
					if bedrooms != 1 {
						summary.WriteString("s")
					}
				}

				// Rent
				if rent, ok := unit["rent"].(float64); ok && rent > 0 {
					summary.WriteString(fmt.Sprintf(" for $%.0f/month", rent))
				}

				// Deposit
				if deposit, ok := unit["deposit"].(float64); ok && deposit > 0 {
					summary.WriteString(fmt.Sprintf(" (deposit: $%.0f)", deposit))
				}

				// Square footage
				if unitSqft, ok := unit["squareFootage"].(float64); ok && unitSqft > 0 {
					summary.WriteString(fmt.Sprintf(", %.0f sqft", unitSqft))
				}

				// Availability
				if availability, ok := unit["availability"].(string); ok && availability != "" && availability != "1970-01-01" {
					summary.WriteString(fmt.Sprintf(", available from %s", availability))
				}
			}
		}

		if len(units) > maxUnits {
			summary.WriteString(fmt.Sprintf("\n... and %d more units", len(units)-maxUnits))
		}
	} else {
		// No units array - single-family property
		// Check for bedrooms at property level
		if bedrooms, ok := propertyData["bedrooms"].(float64); ok && bedrooms >= 0 {
			summary.WriteString(fmt.Sprintf("\nBedrooms: %.0f", bedrooms))
		}
		if bathrooms, ok := propertyData["bathrooms"].(float64); ok && bathrooms >= 0 {
			summary.WriteString(fmt.Sprintf("\nBathrooms: %g", bathrooms))
		}
		if rent, ok := propertyData["rent"].(float64); ok && rent > 0 {
			summary.WriteString(fmt.Sprintf("\nRent: $%.0f/month", rent))
		}
		if deposit, ok := propertyData["deposit"].(float64); ok && deposit > 0 {
			summary.WriteString(fmt.Sprintf("\nDeposit: $%.0f", deposit))
		}
	}

	// Description (truncated)
	if description, ok := propertyData["description"].(string); ok && description != "" {
		// Truncate long descriptions
		desc := description
		if len(desc) > 200 {
			desc = desc[:200] + "..."
		}
		summary.WriteString(fmt.Sprintf("\n\nDescription: %s", desc))
	}

	// Key amenities (first 5)
	if amenities, ok := propertyData["amenities"].([]interface{}); ok && len(amenities) > 0 {
		summary.WriteString("\n\nKey amenities: ")
		maxAmenities := 5
		if len(amenities) < maxAmenities {
			maxAmenities = len(amenities)
		}

		for i := 0; i < maxAmenities; i++ {
			if amenity, ok := amenities[i].(string); ok {
				if i > 0 {
					summary.WriteString(", ")
				}
				summary.WriteString(amenity)
			}
		}

		if len(amenities) > maxAmenities {
			summary.WriteString(fmt.Sprintf(" and %d more", len(amenities)-maxAmenities))
		}
	}

	result := summary.String()
	if result == "" {
		// Fallback to truncated text
		if len(fullText) > 800 {
			return fullText[:800] + "..."
		}
		return fullText
	}

	return result
}

// ClassifyLeadStage classifies a lead stage based on content and available stages
// Uses predefined thread stages like "Interested", "Nurture", "Closed Lost"
func ClassifyLeadStage(client *openai.Client, content string) (string, error) {
	stages := []string{"Interested", "Nurture", "Closed Lost"}
	inputs := make([]string, len(stages)+1)
	inputs[0] = content
	copy(inputs[1:], stages)

	resp, err := client.CreateEmbeddings(
		context.Background(),
		openai.EmbeddingRequest{
			Input: inputs,
			Model: openai.LargeEmbedding3,
		},
	)

	check(err, true)

	contentEmbedding := resp.Data[0].Embedding
	var maxSimilarity float32 = -1
	var bestStage string

	// Compare content embedding with each stage embedding
	for i := 0; i < len(stages); i++ {
		similarity := CalculateCosineSimilarity(contentEmbedding, resp.Data[i+1].Embedding)
		if similarity > maxSimilarity {
			maxSimilarity = similarity
			bestStage = stages[i]
		}
	}

	return bestStage, nil
}
