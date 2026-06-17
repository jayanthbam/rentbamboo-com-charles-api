package utils

import (
	"fmt"
	"regexp"
	"strings"
)

// ValidatePhoneNumber validates and formats a phone number
func ValidatePhoneNumber(phone string) (string, error) {
	// Remove all non-digit characters
	re := regexp.MustCompile(`\D`)
	digits := re.ReplaceAllString(phone, "")

	// Check length
	if len(digits) < 10 || len(digits) > 15 {
		return "", fmt.Errorf("invalid phone number length: %d digits", len(digits))
	}

	// If it's 10 digits, assume US/Canada and add +1
	if len(digits) == 10 {
		return "+1" + digits, nil
	}

	// If it's 11 digits and starts with 1, assume US/Canada
	if len(digits) == 11 && digits[0] == '1' {
		return "+" + digits, nil
	}

	// Otherwise, add + if not present
	if !strings.HasPrefix(digits, "+") {
		// Check if it starts with a country code
		if len(digits) >= 11 {
			return "+" + digits, nil
		}
	}

	return digits, nil
}

// FormatPhoneNumberForDisplay formats a phone number for display
func FormatPhoneNumberForDisplay(phone string) string {
	// Remove all non-digit characters
	re := regexp.MustCompile(`\D`)
	digits := re.ReplaceAllString(phone, "")

	if len(digits) == 10 {
		return fmt.Sprintf("(%s) %s-%s", digits[0:3], digits[3:6], digits[6:10])
	} else if len(digits) == 11 && digits[0] == '1' {
		return fmt.Sprintf("+1 (%s) %s-%s", digits[1:4], digits[4:7], digits[7:11])
	}

	return phone
}

// ExtractPhoneNumbers extracts phone numbers from text
func ExtractPhoneNumbers(text string) []string {
	// Regex pattern for phone numbers
	pattern := `(?:\+?\d{1,3}[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}`
	re := regexp.MustCompile(pattern)

	matches := re.FindAllString(text, -1)

	// Validate and format each phone number
	var phoneNumbers []string
	for _, match := range matches {
		if formatted, err := ValidatePhoneNumber(match); err == nil {
			phoneNumbers = append(phoneNumbers, formatted)
		}
	}

	return phoneNumbers
}
