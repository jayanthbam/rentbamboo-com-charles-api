package property

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/k0kubun/pp/v3"
)

// SelectPropertyForSMS selects the best property for SMS context using the priority order:
// 1. Single Property Team: If team has only 1 property, use that
// 2. Property Context First: Extract UUID from email property context if available
// 3. Rule-Based Matching: Use budget matching for multiple properties
// 4. Team Info: Use team information as last resort
func SelectPropertyForSMS(
	teamID string,
	propertyContext string, // From email processing (optional)
	leadEmail string, // For database fallback lookup
	budget string,
) (*PropertySelectionResult, error) {

	// 1. SINGLE PROPERTY TEAM CHECK (FIRST)
	properties := GetTeamProperties(teamID)
	if len(properties) == 0 {
		pp.Printf("DEBUG: Team %s has no available properties\n", teamID)
		return &PropertySelectionResult{
			SelectionType: "none",
			Confidence:    0.0,
			Reason:        "Team has no available properties",
		}, nil
	}

	if len(properties) == 1 {
		pp.Printf("DEBUG: Team %s has only one property, using it directly\n", teamID)
		return &PropertySelectionResult{
			PropertyID:    properties[0].ID,
			Property:      &properties[0],
			SelectionType: "single",
			Confidence:    1.0,
			Reason:        "Team has only one property",
		}, nil
	}

	// 2. PROPERTY CONTEXT EXTRACTION
	// If property context is provided, try to extract UUID from it
	if propertyContext == "" && leadEmail != "" {
		// Fallback: Try to get from database
		propertyContext = GetPropertyContextFromDB(teamID, leadEmail)
	}

	if propertyID := ExtractUUIDFromContext(propertyContext); propertyID != "" {
		pp.Printf("DEBUG: Found property ID %s in property context\n", propertyID)
		if property := GetPropertyByID(teamID, propertyID); property != nil {
			pp.Printf("DEBUG: Successfully retrieved property %s from context\n", property.Name)
			return &PropertySelectionResult{
				PropertyID:    propertyID,
				Property:      property,
				SelectionType: "context",
				Confidence:    1.0,
				Reason:        "Extracted from email property context",
			}, nil
		}
		pp.Printf("DEBUG: Property ID %s not found in team %s\n", propertyID, teamID)
	}

	// 3. RULE-BASED MATCHING
	// Try simple budget matching first
	if matched := ruleBasedMatch(properties, budget); matched != nil {
		pp.Printf("DEBUG: Rule-based match found: %s (budget: %s)\n", matched.Name, budget)
		return &PropertySelectionResult{
			PropertyID:    matched.ID,
			Property:      matched,
			SelectionType: "rule",
			Confidence:    0.8,
			Reason:        "Matched based on budget",
		}, nil
	}

	// 4. TEAM INFO FALLBACK
	pp.Printf("DEBUG: No property matched, falling back to team info\n")
	return &PropertySelectionResult{
		SelectionType: "team",
		Confidence:    0.0,
		Reason:        "No specific property matched, using team info",
	}, nil
}

// ruleBasedMatch matches properties based on budget
func ruleBasedMatch(properties []MinifiedProperty, budget string) *MinifiedProperty {
	if budget == "" {
		return nil
	}

	// Parse budget (handle formats like "$1000", "1000", "$1000-$1500")
	budget = strings.TrimPrefix(budget, "$")
	budget = strings.TrimSpace(budget)

	var minBudget, maxBudget float64

	if strings.Contains(budget, "-") {
		// Range format: "1000-1500"
		parts := strings.Split(budget, "-")
		if len(parts) == 2 {
			fmt.Sscanf(parts[0], "%f", &minBudget)
			fmt.Sscanf(parts[1], "%f", &maxBudget)
		}
	} else {
		// Single value: "1000"
		fmt.Sscanf(budget, "%f", &minBudget)
		maxBudget = minBudget + 200 // Allow $200 flexibility
	}

	if minBudget == 0 {
		return nil
	}

	// Find property within budget
	for _, prop := range properties {
		var propRent float64

		if prop.Type == "single" {
			// Single-family: status lives at root level
			if prop.Status == "off-market" {
				continue
			}
			propRent = prop.Rent
		} else if len(prop.Units) > 0 {
			// Use average rent for multi-family, excluding off-market units
			var total float64
			var count int
			for _, unit := range prop.Units {
				if unit.Status == "off-market" {
					continue
				}
				if unit.Vacant {
					total += unit.Rent
					count++
				}
			}
			if count > 0 {
				propRent = total / float64(count)
			}
		}

		if propRent > 0 && propRent >= minBudget && propRent <= maxBudget+200 {
			return &prop
		}
	}

	return nil
}

// GetPropertyContextForSMS returns the formatted property context for SMS generation
func GetPropertyContextForSMS(result *PropertySelectionResult) string {
	if result == nil || result.Property == nil {
		return ""
	}

	return CreatePropertyContextForAI(result.Property)
}

// CreatePropertyContextForAI creates property context for AI prompts
func CreatePropertyContextForAI(property *MinifiedProperty) string {
	if property == nil {
		return ""
	}

	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Property: %s\n", property.Name))
	sb.WriteString(fmt.Sprintf("Address: %s\n", property.Address))

	if property.Type == "single" {
		// Single-family: status lives at root level
		if property.Rent > 0 {
			sb.WriteString(fmt.Sprintf("Rent: $%.0f\n", property.Rent))
		}
		bedText := fmt.Sprintf("%d Bedroom", property.Beds)
		if property.Beds == 0 {
			bedText = "Studio"
		} else if property.Beds > 1 {
			bedText = fmt.Sprintf("%d Bedrooms", property.Beds)
		}
		sb.WriteString(fmt.Sprintf("Details: %s, %s, %d sq ft\n", bedText, formatBathText(property.Baths), property.Sqft))
	} else if len(property.Units) > 0 {
		// Count vacant units first for a clear header
		vacantCount := 0
		for _, unit := range property.Units {
			if unit.Status != "off-market" && unit.Vacant && unit.Rent > 0 {
				vacantCount++
			}
		}
		fmt.Fprintf(&sb, "Available Units (%d vacant):\n", vacantCount)
		for _, unit := range property.Units {
			if unit.Status == "off-market" {
				continue
			}
			if unit.Vacant && unit.Rent > 0 {
				bedText := fmt.Sprintf("%d Bedroom", unit.Beds)
				if unit.Beds == 0 {
					bedText = "Studio"
				} else if unit.Beds > 1 {
					bedText = fmt.Sprintf("%d Bedrooms", unit.Beds)
				}
				bathText := formatBathText(unit.Baths)
				if unit.UnitName != "" {
					fmt.Fprintf(&sb, "- %s — %s, %s for $%.0f per month\n", unit.UnitName, bedText, bathText, unit.Rent)
				} else {
					fmt.Fprintf(&sb, "- %s, %s for $%.0f per month\n", bedText, bathText, unit.Rent)
				}
			}
		}
	}

	if len(property.Amenities) > 0 {
		// Take first 5 amenities
		maxAmenities := 5
		if len(property.Amenities) < maxAmenities {
			maxAmenities = len(property.Amenities)
		}
		sb.WriteString(fmt.Sprintf("Amenities: %s\n", strings.Join(property.Amenities[:maxAmenities], ", ")))
	}

	if len(property.Specials) > 0 {
		sb.WriteString(fmt.Sprintf("Specials: %s\n", strings.Join(property.Specials, ", ")))
	}

	if property.Contact.Phone != "" {
		sb.WriteString(fmt.Sprintf("Contact Phone: %s\n", property.Contact.Phone))
	}

	if property.ScheduleURL != "" {
		sb.WriteString(fmt.Sprintf("TOUR_LINK: %s\n", property.ScheduleURL))
	}

	if property.ApplicationURL != "" {
		sb.WriteString(fmt.Sprintf("APPLICATION_LINK: %s\n", property.ApplicationURL))
	}

	return sb.String()
}

// FormatPropertyForSMS creates a human-readable property summary for SMS
func FormatPropertyForSMS(result *PropertySelectionResult) string {
	if result == nil || result.Property == nil {
		return ""
	}

	prop := result.Property
	var sb strings.Builder

	fmt.Fprintf(&sb, "Property: %s\n", prop.Name)
	fmt.Fprintf(&sb, "Location: %s\n", prop.Address)

	if prop.Type == "single" {
		// Single-family: status lives at root level
		if prop.Status == "off-market" {
			return ""
		}
		bedText := fmt.Sprintf("%d Bedroom", prop.Beds)
		if prop.Beds == 0 {
			bedText = "Studio"
		} else if prop.Beds > 1 {
			bedText = fmt.Sprintf("%d Bedrooms", prop.Beds)
		}
		bathText := formatBathText(prop.Baths)
		fmt.Fprintf(&sb, "Unit: %s, %s for %.0f per month\n", bedText, bathText, prop.Rent)
	} else if len(prop.Units) > 0 {
		// Multi-family: getUnitRange already excludes off-market units
		unitRange := getUnitRange(*prop)
		if unitRange == "" {
			return ""
		}
		fmt.Fprintf(&sb, "Units: %s\n", unitRange)
	}

	if len(prop.Amenities) > 0 {
		sb.WriteString("Amenities: ")
		maxAmenities := min(3, len(prop.Amenities))
		for i := 0; i < maxAmenities; i++ {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(prop.Amenities[i])
		}
		sb.WriteString("\n")
	}

	if len(prop.Specials) > 0 {
		sb.WriteString(fmt.Sprintf("Special: %s\n", prop.Specials[0]))
	}

	if prop.Contact.Phone != "" {
		sb.WriteString(fmt.Sprintf("Contact: %s\n", prop.Contact.Phone))
	}

	return sb.String()
}

// getUnitRange returns a string describing the unit range
func getUnitRange(property MinifiedProperty) string {
	if len(property.Units) == 0 {
		return ""
	}

	// Collect only on-market units
	var availableUnits []MinifiedUnit
	for _, unit := range property.Units {
		if unit.Status != "off-market" {
			availableUnits = append(availableUnits, unit)
		}
	}
	if len(availableUnits) == 0 {
		return ""
	}

	minBeds := availableUnits[0].Beds
	maxBeds := availableUnits[0].Beds
	minRent := availableUnits[0].Rent
	maxRent := availableUnits[0].Rent

	for _, unit := range availableUnits {
		if unit.Beds < minBeds {
			minBeds = unit.Beds
		}
		if unit.Beds > maxBeds {
			maxBeds = unit.Beds
		}
		if unit.Rent < minRent {
			minRent = unit.Rent
		}
		if unit.Rent > maxRent {
			maxRent = unit.Rent
		}
	}

	var sb strings.Builder

	// Bedroom range
	if minBeds == maxBeds {
		sb.WriteString(fmt.Sprintf("%d bed", minBeds))
	} else {
		sb.WriteString(fmt.Sprintf("%d-%d bed", minBeds, maxBeds))
	}

	// Rent range
	sb.WriteString(fmt.Sprintf(", $%.0f", minRent))
	if minRent != maxRent {
		sb.WriteString(fmt.Sprintf("-$%.0f", maxRent))
	}

	return sb.String()
}

// formatBathText returns formatted bath text like "1 Bath", "2 Baths", "1.5 Baths"
func formatBathText(baths float64) string {
	if baths == float64(int(baths)) {
		count := int(baths)
		if count == 1 {
			return "1 Bath"
		}
		return fmt.Sprintf("%d Baths", count)
	}
	return fmt.Sprintf("%.1f Baths", baths)
}

// formatBathCount returns just the number portion: "1", "2", "1.5"
func formatBathCount(baths float64) string {
	if baths == float64(int(baths)) {
		return fmt.Sprintf("%d", int(baths))
	}
	return fmt.Sprintf("%.1f", baths)
}

// ExtractPropertyNameFromContext extracts property name from context string
func ExtractPropertyNameFromContext(context string) string {
	if context == "" {
		return ""
	}

	// Try multiple formats:
	// 1. Format: "Property: Name - Address"
	if strings.HasPrefix(context, "Property: ") {
		parts := strings.SplitN(strings.TrimPrefix(context, "Property: "), " - ", 2)
		if len(parts) > 0 {
			return strings.TrimSpace(parts[0])
		}
	}

	// 2. Format from embeddings: "Property: [Name]"
	if matches := regexp.MustCompile(`Property:\s*([^\n]+)`).FindStringSubmatch(context); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	// 3. Format: "Property 1 (relevance: X.X):\nProperty: Name\nAddress: ..."
	if matches := regexp.MustCompile(`Property \d+ \(relevance: [\d.]+\):\nProperty:\s*([^\n]+)`).FindStringSubmatch(context); len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}

	return ""
}
