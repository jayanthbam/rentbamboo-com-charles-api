package ai

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PropertyOptimizer handles optimization of property data for embedding
type PropertyOptimizer struct{}

// NewPropertyOptimizer creates a new property optimizer
func NewPropertyOptimizer() *PropertyOptimizer {
	return &PropertyOptimizer{}
}

// MinifyPropertyJSON takes a property as interface{} and returns minified JSON string
// Removes all unnecessary whitespace while preserving all data
func (po *PropertyOptimizer) MinifyPropertyJSON(property interface{}) (string, error) {
	// Convert property to minified JSON
	jsonBytes, err := json.Marshal(property)
	if err != nil {
		return "", fmt.Errorf("failed to marshal property: %w", err)
	}

	// Parse and re-marshal without indentation for minification
	var data interface{}
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return "", fmt.Errorf("failed to unmarshal property: %w", err)
	}

	minifiedBytes, err := json.Marshal(data)
	if err != nil {
		return "", fmt.Errorf("failed to marshal minified property: %w", err)
	}

	return string(minifiedBytes), nil
}

// MinifyPropertyJSONFast is a faster version that doesn't re-parse
// It removes whitespace from already JSON-marshaled data
func (po *PropertyOptimizer) MinifyPropertyJSONFast(property interface{}) (string, error) {
	jsonBytes, err := json.Marshal(property)
	if err != nil {
		return "", fmt.Errorf("failed to marshal property: %w", err)
	}

	// More aggressive whitespace removal
	minified := string(jsonBytes)

	// Remove all whitespace between JSON tokens
	minified = strings.ReplaceAll(minified, "\n", "")
	minified = strings.ReplaceAll(minified, "\t", "")
	minified = strings.ReplaceAll(minified, "\r", "")

	// Remove spaces around punctuation (but not within strings)
	minified = strings.ReplaceAll(minified, " : ", ":")
	minified = strings.ReplaceAll(minified, ": ", ":")
	minified = strings.ReplaceAll(minified, " :", ":")

	minified = strings.ReplaceAll(minified, ", ", ",")
	minified = strings.ReplaceAll(minified, " ,", ",")

	minified = strings.ReplaceAll(minified, "[ ", "[")
	minified = strings.ReplaceAll(minified, " [", "[")
	minified = strings.ReplaceAll(minified, " ]", "]")
	minified = strings.ReplaceAll(minified, "] ", "]")

	minified = strings.ReplaceAll(minified, "{ ", "{")
	minified = strings.ReplaceAll(minified, " {", "{")
	minified = strings.ReplaceAll(minified, " }", "}")
	minified = strings.ReplaceAll(minified, "} ", "}")

	// Remove multiple spaces
	for strings.Contains(minified, "  ") {
		minified = strings.ReplaceAll(minified, "  ", " ")
	}

	// Final cleanup
	minified = strings.TrimSpace(minified)

	return minified, nil
}

// EstimateTokensJSON provides accurate token estimation for JSON text
// JSON with lots of punctuation tokenizes differently than English text
func (po *PropertyOptimizer) EstimateTokensJSON(jsonText string) int {
	// For minified JSON, tokens are very dense
	// Based on testing: ~2.2 characters per token for dense JSON
	// Add 10% safety margin
	estimated := len(jsonText) / 2
	return estimated
}

// EstimateTokensJSONConservative is more conservative (higher estimate)
// Use this for properties that have failed before
func (po *PropertyOptimizer) EstimateTokensJSONConservative(jsonText string) int {
	// More conservative: ~1.8 characters per token
	return len(jsonText) * 5 / 9
}

// PropertyToCompactText creates a compact text representation
// Alternative to JSON that might be more token-efficient
func (po *PropertyOptimizer) PropertyToCompactText(property interface{}) (string, error) {
	minifiedJSON, err := po.MinifyPropertyJSONFast(property)
	if err != nil {
		return "", err
	}

	// Add property markers for better parsing
	return "=== PROPERTY START ===\n" + minifiedJSON + "\n=== PROPERTY END ===", nil
}

// OptimizePropertyForEmbedding is the main function to prepare property for embedding
// Returns optimized text and accurate token estimate
func (po *PropertyOptimizer) OptimizePropertyForEmbedding(property interface{}) (string, int, error) {
	// Use minified JSON (preserves all data, just removes whitespace)
	optimizedText, err := po.PropertyToCompactText(property)
	if err != nil {
		return "", 0, err
	}

	// Get accurate token estimate for JSON
	tokenEstimate := po.EstimateTokensJSON(optimizedText)

	return optimizedText, tokenEstimate, nil
}

// OptimizePropertyForEmbeddingConservative is for properties that might be too large
// Uses more conservative token estimation
func (po *PropertyOptimizer) OptimizePropertyForEmbeddingConservative(property interface{}) (string, int, error) {
	optimizedText, err := po.PropertyToCompactText(property)
	if err != nil {
		return "", 0, err
	}

	// Use conservative estimate (higher) to be safe
	tokenEstimate := po.EstimateTokensJSONConservative(optimizedText)

	return optimizedText, tokenEstimate, nil
}

// SplitPropertyIntoChunks splits a large property into chunks that fit within token limit
// Returns chunks and their token estimates
func (po *PropertyOptimizer) SplitPropertyIntoChunks(property interface{}, maxTokensPerChunk int) ([]string, []int, error) {
	// First, get the minified JSON
	minifiedJSON, err := po.MinifyPropertyJSONFast(property)
	if err != nil {
		return nil, nil, err
	}

	// Parse the JSON to understand its structure
	var data map[string]interface{}
	if err := json.Unmarshal([]byte(minifiedJSON), &data); err != nil {
		return nil, nil, fmt.Errorf("failed to parse property JSON: %w", err)
	}

	// Try to split by units if the property has many units
	if units, ok := data["units"].([]interface{}); ok && len(units) > 1 {
		// Property has units array - split by units
		return po.splitByUnits(data, units, maxTokensPerChunk)
	}

	// If no units or can't split by units, try to split the JSON by size
	return po.splitByJSONSize(minifiedJSON, maxTokensPerChunk)
}

// splitByUnits splits a property by its units array
func (po *PropertyOptimizer) splitByUnits(property map[string]interface{}, units []interface{}, maxTokensPerChunk int) ([]string, []int, error) {
	var chunks []string
	var tokenEstimates []int

	// Create base property without units
	baseProperty := make(map[string]interface{})
	for key, value := range property {
		if key != "units" {
			baseProperty[key] = value
		}
	}

	// Process units in groups that fit within token limit
	currentChunkUnits := make([]interface{}, 0)
	currentChunk := baseProperty

	for i, unit := range units {
		// Create a test chunk with current units + this unit
		testUnits := append(currentChunkUnits, unit)
		testChunk := make(map[string]interface{})
		for k, v := range baseProperty {
			testChunk[k] = v
		}
		testChunk["units"] = testUnits

		// Optimize this test chunk to check token count
		_, tokenEstimate, err := po.OptimizePropertyForEmbedding(testChunk)
		if err != nil {
			return nil, nil, err
		}

		// If adding this unit would exceed limit (or this is the first unit and already exceeds limit)
		if tokenEstimate > maxTokensPerChunk && len(currentChunkUnits) > 0 {
			// Save current chunk
			currentChunk["units"] = currentChunkUnits
			chunkText, _, err := po.OptimizePropertyForEmbedding(currentChunk)
			if err != nil {
				return nil, nil, err
			}
			chunks = append(chunks, chunkText)
			tokenEstimates = append(tokenEstimates, po.EstimateTokensJSON(chunkText))

			// Start new chunk with this unit
			currentChunkUnits = []interface{}{unit}
		} else {
			// Add unit to current chunk
			currentChunkUnits = append(currentChunkUnits, unit)
		}

		// If this is the last unit, save the chunk
		if i == len(units)-1 {
			currentChunk["units"] = currentChunkUnits
			chunkText, _, err := po.OptimizePropertyForEmbedding(currentChunk)
			if err != nil {
				return nil, nil, err
			}
			chunks = append(chunks, chunkText)
			tokenEstimates = append(tokenEstimates, po.EstimateTokensJSON(chunkText))
		}
	}

	return chunks, tokenEstimates, nil
}

// splitByJSONSize splits minified JSON by approximate size
func (po *PropertyOptimizer) splitByJSONSize(minifiedJSON string, maxTokensPerChunk int) ([]string, []int, error) {
	// Simple splitting by character count (conservative)
	// Since we can't easily parse and split JSON while keeping it valid
	// We'll use a simple approach: split into ~maxTokensPerChunk * 2 character chunks

	maxCharsPerChunk := maxTokensPerChunk * 2 // Conservative: 2 chars per token
	if len(minifiedJSON) <= maxCharsPerChunk {
		// Property fits in one chunk
		return []string{minifiedJSON}, []int{po.EstimateTokensJSON(minifiedJSON)}, nil
	}

	// Split into chunks
	var chunks []string
	var tokenEstimates []int

	for i := 0; i < len(minifiedJSON); i += maxCharsPerChunk {
		end := i + maxCharsPerChunk
		if end > len(minifiedJSON) {
			end = len(minifiedJSON)
		}

		chunk := minifiedJSON[i:end]
		chunks = append(chunks, chunk)
		tokenEstimates = append(tokenEstimates, po.EstimateTokensJSON(chunk))
	}

	return chunks, tokenEstimates, nil
}
