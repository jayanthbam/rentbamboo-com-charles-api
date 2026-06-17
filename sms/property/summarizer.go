package property

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CreateSMSPropertySummary creates your minified JSON for SMS context
func CreateSMSPropertySummary(property *MinifiedProperty) string {
	if property == nil {
		return ""
	}

	// Create the minified JSON exactly as you specified
	summary := createMinifiedJSONObject(property)

	// Convert to JSON
	jsonBytes, err := json.Marshal(summary)
	if err != nil {
		// Fallback to simple string representation
		return createMinifiedJSON(property)
	}

	return string(jsonBytes)
}

// createMinifiedJSONObject creates the minified JSON object structure
func createMinifiedJSONObject(property *MinifiedProperty) interface{} {
	if property == nil {
		return nil
	}

	// Build the base object
	result := make(map[string]interface{})
	result["type"] = property.Type
	result["id"] = property.ID
	result["name"] = property.Name
	result["address"] = property.Address
	result["vacant"] = property.Vacant
	result["amenities"] = property.Amenities

	// Add contact
	if property.Contact.Phone != "" || property.Contact.Email != "" {
		contact := make(map[string]string)
		if property.Contact.Phone != "" {
			contact["phone"] = property.Contact.Phone
		}
		if property.Contact.Email != "" {
			contact["email"] = property.Contact.Email
		}
		result["contact"] = contact
	}

	// Add URLs if present
	if property.ApplicationURL != "" {
		result["applicationUrl"] = property.ApplicationURL
	}
	if property.ScheduleURL != "" {
		result["scheduleUrl"] = property.ScheduleURL
	}

	// Add fees if present
	if len(property.RequiredFees) > 0 {
		result["requiredFees"] = property.RequiredFees
	}
	if len(property.Fees) > 0 {
		result["fees"] = property.Fees
	}
	if len(property.PetFees) > 0 {
		result["petFees"] = property.PetFees
	}

	// Add specials if present
	if len(property.Specials) > 0 {
		result["specials"] = property.Specials
	}

	// Handle single vs multi properties
	if property.Type == "single" {
		result["status"] = property.Status // on-market / off-market

		// Skip single family properties that are off-market
		if property.Status == "off-market" {
			return nil
		}

		// Single family properties
		if property.Beds > 0 {
			result["beds"] = property.Beds
		}
		if property.Baths > 0 {
			result["baths"] = property.Baths
		}
		if property.Sqft > 0 {
			result["sqft"] = property.Sqft
		}
		if property.Rent > 0 {
			result["rent"] = property.Rent
		}
		if property.Deposit > 0 {
			result["deposit"] = property.Deposit
		}

	} else if property.Type == "multi" {
		// Multi-family properties with units
		if len(property.Units) > 0 {
			var units []map[string]interface{}
			for _, unit := range property.Units {
				if unit.Status == "off-market" {
					continue
				}
				unitMap := make(map[string]interface{})
				unitMap["id"] = unit.ID
				unitMap["beds"] = unit.Beds
				unitMap["baths"] = unit.Baths
				unitMap["status"] = unit.Status

				if unit.Sqft > 0 {
					unitMap["sqft"] = unit.Sqft
				}
				if unit.Rent > 0 {
					unitMap["rent"] = unit.Rent
				}
				if unit.Deposit > 0 {
					unitMap["deposit"] = unit.Deposit
				}
				unitMap["vacant"] = unit.Vacant

				units = append(units, unitMap)
			}
			result["units"] = units
		}
	}

	return result
}

// FormatPropertyForSMSText creates a human-readable property summary for SMS
func FormatPropertyForSMSText(property *MinifiedProperty) string {
	if property == nil {
		return ""
	}

	var sb strings.Builder

	// Property name and address
	sb.WriteString(fmt.Sprintf("%s in %s", property.Name, getCityFromAddress(property.Address)))

	// Unit information
	if property.Type == "single" {
		// Convert 0BR to Studio
		bedroomText := fmt.Sprintf("%dBR", property.Beds)
		if property.Beds == 0 {
			bedroomText = "Studio"
		}
		sb.WriteString(fmt.Sprintf(". %s/%gBA, $%.0f/month", bedroomText, property.Baths, property.Rent))
	} else if property.Type == "multi" && len(property.Units) > 0 {
		unitRange := getUnitRange(*property)
		if unitRange != "" {
			sb.WriteString(fmt.Sprintf(". Units: %s", unitRange))
		}
	}

	// Key amenities (first 2-3)
	if len(property.Amenities) > 0 {
		sb.WriteString(". Amenities: ")
		maxAmenities := min(len(property.Amenities), 3)
		for i := 0; i < maxAmenities; i++ {
			if i > 0 {
				sb.WriteString(", ")
			}
			// Clean up amenity names
			amenity := cleanAmenityName(property.Amenities[i])
			sb.WriteString(amenity)
		}
	}

	// Specials
	if len(property.Specials) > 0 {
		sb.WriteString(fmt.Sprintf(". Special: %s", property.Specials[0]))
	}

	// Contact phone
	if property.Contact.Phone != "" {
		sb.WriteString(fmt.Sprintf(". Contact: %s", formatPhoneNumber(property.Contact.Phone)))
	}

	return sb.String()
}

// getCityFromAddress extracts city from full address
func getCityFromAddress(address string) string {
	parts := strings.Split(address, ",")
	if len(parts) >= 2 {
		return strings.TrimSpace(parts[1])
	}
	return address
}

// cleanAmenityName cleans up amenity names for SMS
func cleanAmenityName(amenity string) string {
	// Remove common prefixes and clean up
	amenity = strings.TrimSpace(amenity)

	// Replace common patterns
	amenity = strings.ReplaceAll(amenity, "AC:", "A/C ")
	amenity = strings.ReplaceAll(amenity, "Heating:", "")
	amenity = strings.ReplaceAll(amenity, "WasherDryer", "W/D")
	amenity = strings.ReplaceAll(amenity, "Hookups", "")
	amenity = strings.ReplaceAll(amenity, "On-Site", "Onsite")

	return strings.TrimSpace(amenity)
}

// formatPhoneNumber formats phone number for SMS
func formatPhoneNumber(phone string) string {
	// Remove all non-numeric characters
	var digits strings.Builder
	for _, ch := range phone {
		if ch >= '0' && ch <= '9' {
			digits.WriteRune(ch)
		}
	}

	phoneDigits := digits.String()

	// Format as (XXX) XXX-XXXX if 10 digits
	if len(phoneDigits) == 10 {
		return fmt.Sprintf("(%s) %s-%s",
			phoneDigits[0:3],
			phoneDigits[3:6],
			phoneDigits[6:10])
	}

	// Return original if can't format
	return phone
}

// createMinifiedJSON is a fallback for JSON marshaling errors
func createMinifiedJSON(property *MinifiedProperty) string {
	if property == nil {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s - %s", property.Name, property.Address))
	if property.Type == "single" {
		sb.WriteString(fmt.Sprintf(" %dBR/%gBA $%.0f", property.Beds, property.Baths, property.Rent))
	}
	return sb.String()
}
