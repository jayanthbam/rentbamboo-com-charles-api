package utils

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// CountSMSSegments calculates how many SMS segments a message will use
// Simplified version for fault tolerance - uses Twilio's general guidelines
func CountSMSSegments(message string) int {
	if message == "" {
		return 1
	}

	// Check if message contains Unicode characters (non-ASCII)
	hasUnicode := false
	for _, r := range message {
		if r > 127 {
			// Check if it's a GSM extended character
			if !isGSM7Extended(r) {
				hasUnicode = true
				break
			}
		}
	}

	if hasUnicode {
		// UCS-2 encoding for Unicode messages
		// First segment: 70 characters, subsequent: 67 characters
		messageLen := len(message)
		if messageLen <= 70 {
			return 1
		}
		// Simple calculation: 1 for first segment, plus segments for remaining
		return 1 + (messageLen-70+66)/67
	}

	// GSM-7 encoding for ASCII messages
	// First segment: 160 characters, subsequent: 153 characters
	messageLen := len(message)
	if messageLen <= 160 {
		return 1
	}
	// Simple calculation: 1 for first segment, plus segments for remaining
	return 1 + (messageLen-160+152)/153
}

// isGSM7Extended checks if a rune is in the GSM 7-bit default alphabet extended character set
func isGSM7Extended(r rune) bool {
	switch r {
	case '\\', '^', '{', '}', '[', ']', '~', '|', '€':
		return true
	default:
		return false
	}
}

// TruncateSMSMessage truncates a message to fit within SMS character limits
func TruncateSMSMessage(message string, maxLength int) string {
	if maxLength <= 0 {
		return message
	}

	if len(message) <= maxLength {
		return message
	}

	// Try to truncate at a word boundary
	truncated := message[:maxLength]
	lastSpace := strings.LastIndex(truncated, " ")
	if lastSpace > maxLength*2/3 && lastSpace > 0 { // Only truncate at space if it's in the latter third
		truncated = truncated[:lastSpace]
	}

	// Add ellipsis if we truncated
	if len(truncated) < len(message) {
		return truncated + "..."
	}
	return truncated
}

// ShouldTruncateSMSMessage checks if a message needs truncation for SMS limits
func ShouldTruncateSMSMessage(message string, maxSegments int) bool {
	segments := CountSMSSegments(message)
	return segments > maxSegments
}

// GetMaxMessageLength returns the maximum message length for a given number of segments
func GetMaxMessageLength(segments int, hasUnicode bool) int {
	if segments <= 0 {
		return 0
	}

	if hasUnicode {
		// UCS-2 encoding: first segment 70, subsequent 67
		if segments == 1 {
			return 70
		}
		return 70 + (segments-1)*67
	}

	// GSM-7 encoding: first segment 160, subsequent 153
	if segments == 1 {
		return 160
	}
	return 160 + (segments-1)*153
}

// OptimizeSMSMessage optimizes a message for SMS delivery
func OptimizeSMSMessage(message string, maxSegments int) string {
	if maxSegments <= 0 {
		maxSegments = 10 // Default to reasonable limit
	}

	if !ShouldTruncateSMSMessage(message, maxSegments) {
		return message
	}

	// Check if message has Unicode
	hasUnicode := false
	for _, r := range message {
		if r > 127 && !isGSM7Extended(r) {
			hasUnicode = true
			break
		}
	}

	maxLength := GetMaxMessageLength(maxSegments, hasUnicode)
	return TruncateSMSMessage(message, maxLength)
}

// SanitizeSMSMessage removes or replaces characters that might cause issues in SMS
func SanitizeSMSMessage(message string) string {
	// Replace common problematic characters
	replacements := map[string]string{
		"&":  "and",
		"<":  "less than",
		">":  "greater than",
		"\"": "'",
		"`":  "'",
		"~":  "-",
		"|":  "-",
		"\\": "/",
	}

	sanitized := message
	for old, new := range replacements {
		sanitized = strings.ReplaceAll(sanitized, old, new)
	}

	// Remove control characters
	sanitized = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' {
			return -1
		}
		return r
	}, sanitized)

	return sanitized
}

// IsValidSMSTemplate checks if a message template is valid
func IsValidSMSTemplate(template string, variables []string) bool {
	// Check for unmatched braces
	braceCount := 0
	for _, char := range template {
		if char == '{' {
			braceCount++
		} else if char == '}' {
			braceCount--
			if braceCount < 0 {
				return false // Closing brace without opening
			}
		}
	}

	if braceCount != 0 {
		return false // Unmatched braces
	}

	// Extract variable names from template
	re := regexp.MustCompile(`\{(\w+)\}`)
	templateVars := re.FindAllStringSubmatch(template, -1)

	// Check if all template variables are in the provided variables list
	for _, match := range templateVars {
		if len(match) > 1 {
			found := false
			for _, v := range variables {
				if v == match[1] {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}

	return true
}

// RenderSMSTemplate renders an SMS template with variables
func RenderSMSTemplate(template string, variables map[string]string) (string, error) {
	if !IsValidSMSTemplate(template, getMapKeys(variables)) {
		return "", fmt.Errorf("invalid template or missing variables")
	}

	// Replace variables in template
	result := template
	for key, value := range variables {
		placeholder := "{" + key + "}"
		result = strings.ReplaceAll(result, placeholder, value)
	}

	return result, nil
}

func getMapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// CalculateSMSCost estimates the cost of sending an SMS
func CalculateSMSCost(message string, segments int, costPerSegment float64) float64 {
	// If segments is 0 or invalid, calculate it from the message
	if segments <= 0 {
		segments = CountSMSSegments(message)
	}
	// Ensure at least 1 segment
	if segments < 1 {
		segments = 1
	}
	return float64(segments) * costPerSegment
}
