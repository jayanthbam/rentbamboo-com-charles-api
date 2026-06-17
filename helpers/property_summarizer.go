package helpers

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// PropertySummaryConfig holds configuration for summary generation
type PropertySummaryConfig struct {
	// Context from conversation (what the user asked about)
	Context map[string]bool
	// Max length in characters
	MaxLength int
	// Include all tiers
	IncludeAllTiers bool
}

// DefaultPropertySummaryConfig returns the default configuration
func DefaultPropertySummaryConfig() PropertySummaryConfig {
	return PropertySummaryConfig{
		Context:         make(map[string]bool),
		MaxLength:       200,
		IncludeAllTiers: true,
	}
}

// GetOptimalSummary generates an optimal property summary for SMS
// This function does NOT use embeddings - it creates text summaries directly from property data
func GetOptimalSummary(property bson.M, config PropertySummaryConfig) string {
	if len(property) == 0 {
		return ""
	}

	// Extract all fields
	tier1 := extractTier1Fields(property)
	tier2 := extractTier2Fields(property)
	tier3 := extractContextualFields(property, config.Context)

	// Build the summary
	var summary strings.Builder

	// Tier 1: MUST HAVE (always included)
	summary.WriteString(tier1)

	// Tier 2: SHOULD HAVE (if exists and fits)
	if tier2 != "" && summary.Len()+len(tier2) < config.MaxLength {
		if summary.Len() > 0 {
			summary.WriteString(". ")
		}
		summary.WriteString(tier2)
	}

	// Tier 3: CONTEXTUAL (only if relevant to conversation)
	if tier3 != "" && summary.Len()+len(tier3) < config.MaxLength {
		if summary.Len() > 0 {
			summary.WriteString(". ")
		}
		summary.WriteString(tier3)
	}

	result := summary.String()

	// Truncate if still too long (should be rare)
	if len(result) > config.MaxLength {
		result = result[:config.MaxLength-3] + "..."
	}

	return result
}

// extractTier1Fields extracts MUST HAVE fields
func extractTier1Fields(property bson.M) string {
	var result strings.Builder

	// 1. Property Name
	propertyName := getString(property, "propertyName")
	if propertyName == "" {
		// Try location as fallback
		if location, ok := property["location"].(primitive.M); ok {
			if street, ok := location["streetAddress"].(string); ok && street != "" {
				propertyName = street
			}
		}
	}

	if propertyName != "" {
		result.WriteString(propertyName)
	}

	// 2. Bed/Bath configurations with prices
	bedBathConfigs := extractBedBath(property)
	if bedBathConfigs != "" {
		if result.Len() > 0 {
			result.WriteString(": ")
		}
		result.WriteString(bedBathConfigs)
	}

	// 3. Price (for single properties only - multi-unit prices are in bedBathConfigs)
	propType := getString(property, "type")
	if propType == "single" {
		price := extractPrice(property)
		if price != "" {
			if result.Len() > 0 {
				result.WriteString(", ")
			}
			result.WriteString(price)
		}
	}

	// 4. Location (City, State)
	city, state := extractLocation(property)
	if city != "" && state != "" {
		if result.Len() > 0 {
			result.WriteString(", ")
		}
		result.WriteString(fmt.Sprintf("%s, %s", city, state))
	}

	// 5. Availability
	availability := extractAvailability(property)
	if availability != "" {
		if result.Len() > 0 {
			result.WriteString(". ")
		}
		result.WriteString(availability)
	}

	return result.String()
}

// extractTier2Fields extracts SHOULD HAVE fields
func extractTier2Fields(property bson.M) string {
	var result strings.Builder

	// 6. Key Amenities (2-3 most appealing)
	amenities := extractKeyAmenities(property, 3)
	if len(amenities) > 0 {
		if result.Len() > 0 {
			result.WriteString(". ")
		}
		result.WriteString("Key amenities: ")
		result.WriteString(strings.Join(amenities, ", "))
	}

	// 7. Square Footage
	sqft := extractSquareFootage(property)
	if sqft > 0 {
		if result.Len() > 0 {
			result.WriteString(". ")
		}
		result.WriteString(fmt.Sprintf("%d sqft", sqft))
	}

	// 8. Property Type
	propType := getString(property, "type")
	if propType != "" {
		// Map to friendly names
		friendlyType := map[string]string{
			"multi":    "Apartment",
			"single":   "Single Family Home",
			"condo":    "Condo",
			"townhome": "Townhome",
		}
		if friendly, ok := friendlyType[propType]; ok {
			propType = friendly
		}

		if result.Len() > 0 {
			result.WriteString(". ")
		}
		result.WriteString(propType)
	}

	return result.String()
}

// extractContextualFields extracts NICE TO HAVE fields based on conversation context
func extractContextualFields(property bson.M, context map[string]bool) string {
	var result strings.Builder

	// Check context and add relevant fields
	if context["pet"] || context["pets"] || context["dog"] || context["cat"] {
		petInfo := extractPetPolicy(property)
		if petInfo != "" {
			if result.Len() > 0 {
				result.WriteString(". ")
			}
			result.WriteString(petInfo)
		}
	}

	if context["parking"] || context["garage"] || context["car"] {
		parkingInfo := extractParkingInfo(property)
		if parkingInfo != "" {
			if result.Len() > 0 {
				result.WriteString(". ")
			}
			result.WriteString(parkingInfo)
		}
	}

	if context["deposit"] || context["security"] || context["fee"] {
		depositInfo := extractDepositInfo(property)
		if depositInfo != "" {
			if result.Len() > 0 {
				result.WriteString(". ")
			}
			result.WriteString(depositInfo)
		}
	}

	if context["utilities"] || context["utility"] || context["water"] || context["electric"] {
		utilitiesInfo := extractUtilitiesInfo(property)
		if utilitiesInfo != "" {
			if result.Len() > 0 {
				result.WriteString(". ")
			}
			result.WriteString(utilitiesInfo)
		}
	}

	if context["income"] || context["restriction"] || context["qualify"] {
		incomeInfo := extractIncomeRestrictions(property)
		if incomeInfo != "" {
			if result.Len() > 0 {
				result.WriteString(". ")
			}
			result.WriteString(incomeInfo)
		}
	}

	// Contact info if explicitly asked
	if context["contact"] || context["phone"] || context["call"] {
		contactInfo := extractContactInfo(property)
		if contactInfo != "" {
			if result.Len() > 0 {
				result.WriteString(". ")
			}
			result.WriteString(contactInfo)
		}
	}

	return result.String()
}

// Helper functions for field extraction

func getString(data bson.M, key string) string {
	if value, ok := data[key].(string); ok {
		return value
	}
	return ""
}

func getInt(data bson.M, key string) int {
	if value, ok := data[key].(int32); ok {
		return int(value)
	}
	if value, ok := data[key].(int64); ok {
		return int(value)
	}
	if value, ok := data[key].(int); ok {
		return value
	}
	if value, ok := data[key].(float64); ok {
		return int(value)
	}
	return -1
}

func getIntOrFloat(data bson.M, key string) float64 {
	if value, ok := data[key].(int32); ok {
		return float64(value)
	}
	if value, ok := data[key].(int64); ok {
		return float64(value)
	}
	if value, ok := data[key].(int); ok {
		return float64(value)
	}
	if value, ok := data[key].(float64); ok {
		return value
	}
	return -1
}

func formatBaths(baths float64) string {
	if baths == float64(int(baths)) {
		// Whole number: "1 Bath", "2 Baths"
		count := int(baths)
		if count == 1 {
			return "1 Bath"
		}
		return fmt.Sprintf("%d Baths", count)
	}
	// Half bath: "1.5 Baths"
	return fmt.Sprintf("%.1f Baths", baths)
}

func getFloat(data bson.M, key string) float64 {
	if value, ok := data[key].(float64); ok {
		return value
	}
	if value, ok := data[key].(int32); ok {
		return float64(value)
	}
	if value, ok := data[key].(int64); ok {
		return float64(value)
	}
	if value, ok := data[key].(int); ok {
		return float64(value)
	}
	return 0
}

func getBool(data bson.M, key string) bool {
	if value, ok := data[key].(bool); ok {
		return value
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func extractBedBath(property bson.M) string {
	propType := getString(property, "type")

	// For single properties, use property-level fields
	if propType == "single" {
		bedrooms := getInt(property, "bedrooms")
		bathrooms := getIntOrFloat(property, "bathrooms")
		if bedrooms >= 0 && bathrooms >= 0 {
			return fmt.Sprintf("%d Bedroom, %s", bedrooms, formatBaths(bathrooms))
		}
		return ""
	}

	// For multi-unit properties, collect all unique configurations
	if units, ok := property["units"].(primitive.A); ok && len(units) > 0 {
		// Map to track configurations and their prices
		configMap := make(map[string][]float64)

		for _, unit := range units {
			if unitMap, ok := unit.(primitive.M); ok {
				bedrooms := getInt(unitMap, "bedrooms")
				bathrooms := getIntOrFloat(unitMap, "bathrooms")
				rent := getFloat(unitMap, "rent")
				isVacant := getBool(unitMap, "isVacant")

				// Only include vacant units
				if isVacant && bedrooms >= 0 && bathrooms >= 0 && rent > 0 {
					config := fmt.Sprintf("%d Bedroom, %s", bedrooms, formatBaths(bathrooms))
					configMap[config] = append(configMap[config], rent)
				}
			}
		}

		// Build configuration string with price ranges
		var configs []string
		for config, prices := range configMap {
			if len(prices) == 0 {
				continue
			}

			// Sort prices to get min and max
			sort.Float64s(prices)
			minPrice := prices[0]
			maxPrice := prices[len(prices)-1]

			if len(prices) == 1 {
				// Single unit with this configuration
				configs = append(configs, fmt.Sprintf("%s for $%.0f per month", config, minPrice))
			} else if minPrice == maxPrice {
				// Multiple units, same price
				configs = append(configs, fmt.Sprintf("%s for $%.0f per month", config, minPrice))
			} else {
				// Multiple units, different prices
				configs = append(configs, fmt.Sprintf("%s for $%.0f-%.0f per month", config, minPrice, maxPrice))
			}
		}

		// Sort configurations by bedroom count
		sort.Slice(configs, func(i, j int) bool {
			// Extract bedroom count for sorting
			re := regexp.MustCompile(`(\d+) Bedroom`)
			iMatch := re.FindStringSubmatch(configs[i])
			jMatch := re.FindStringSubmatch(configs[j])

			if len(iMatch) > 1 && len(jMatch) > 1 {
				iBed, _ := strconv.Atoi(iMatch[1])
				jBed, _ := strconv.Atoi(jMatch[1])
				return iBed < jBed
			}
			return configs[i] < configs[j]
		})

		if len(configs) > 0 {
			return strings.Join(configs, ", ")
		}
	}

	return ""
}

func extractPrice(property bson.M) string {
	propType := getString(property, "type")

	// For single properties, use property-level rent
	if propType == "single" {
		price := getFloat(property, "rent")
		if price > 0 {
			return fmt.Sprintf("$%.0f", price)
		}
		return ""
	}

	// For multi-unit properties, the price is now included in the configuration string
	// Return empty string since price is handled in extractBedBath
	return ""
}

func extractLocation(property bson.M) (string, string) {
	if location, ok := property["location"].(primitive.M); ok {
		city := getString(location, "city")
		state := getString(location, "state")
		return city, state
	}
	return "", ""
}

func extractAvailability(property bson.M) string {
	propType := getString(property, "type")

	if propType == "single" {
		// Single family property
		if isVacant := getBool(property, "isVacant"); isVacant {
			return "Available now"
		}
		return "Not currently available"
	} else {
		// Multi-unit property
		if units, ok := property["units"].(primitive.A); ok {
			vacantCount := 0
			for _, unit := range units {
				if unitMap, ok := unit.(primitive.M); ok {
					if isVacant := getBool(unitMap, "isVacant"); isVacant {
						vacantCount++
					}
				}
			}

			if vacantCount > 0 {
				return fmt.Sprintf("%d units available", vacantCount)
			}
			return "No units currently available"
		}
	}

	return "Check availability"
}

func extractKeyAmenities(property bson.M, maxCount int) []string {
	var amenities []string

	// Get amenities from property level
	if amenityList, ok := property["amenities"].(primitive.A); ok {
		for _, amenity := range amenityList {
			if amenityStr, ok := amenity.(string); ok && amenityStr != "" {
				amenities = append(amenities, amenityStr)
				if len(amenities) >= maxCount {
					break
				}
			}
		}
	}

	// If no amenities at property level, check units
	if len(amenities) == 0 {
		if units, ok := property["units"].(primitive.A); ok && len(units) > 0 {
			if unit, ok := units[0].(primitive.M); ok {
				if unitAmenities, ok := unit["amenities"].(primitive.A); ok {
					for _, amenity := range unitAmenities {
						if amenityStr, ok := amenity.(string); ok && amenityStr != "" {
							amenities = append(amenities, amenityStr)
							if len(amenities) >= maxCount {
								break
							}
						}
					}
				}
			}
		}
	}

	// Filter to most appealing amenities
	return filterToMostAppealing(amenities, maxCount)
}

func filterToMostAppealing(amenities []string, maxCount int) []string {
	if len(amenities) <= maxCount {
		return amenities
	}

	// Priority order for amenities
	priorityOrder := []string{
		"Pool", "Gym", "FitnessCenter", "Swimming Pool",
		"Pet-friendly", "Pet Friendly", "Dog Park",
		"Parking", "Garage", "CoveredParking",
		"In-Unit Washer/Dryer", "WasherDryerHookups",
		"Dishwasher", "Hardwood Floors", "Balcony",
		"AC", "Heating", "Central Air",
	}

	// Score amenities based on priority
	scored := make(map[string]int)
	for _, amenity := range amenities {
		score := 0
		for i, priority := range priorityOrder {
			if strings.Contains(strings.ToLower(amenity), strings.ToLower(priority)) {
				score = len(priorityOrder) - i
				break
			}
		}
		scored[amenity] = score
	}

	// Sort by score
	var sorted []string
	for amenity := range scored {
		sorted = append(sorted, amenity)
	}

	// Sort by score descending
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if scored[sorted[j]] > scored[sorted[i]] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}

	// Return top N
	if len(sorted) > maxCount {
		return sorted[:maxCount]
	}
	return sorted
}

func extractSquareFootage(property bson.M) int {
	// Check property level first
	sqft := getInt(property, "sqft")

	// For multi-unit properties, check units
	if sqft <= 0 {
		if units, ok := property["units"].(primitive.A); ok && len(units) > 0 {
			// Get first unit's square footage
			if unit, ok := units[0].(primitive.M); ok {
				sqft = getInt(unit, "squareFootage")
			}
		}
	}

	return sqft
}

func extractPetPolicy(property bson.M) string {
	// Check for pet fees in property
	if petFees, ok := property["petFees"].(primitive.A); ok && len(petFees) > 0 {
		for _, fee := range petFees {
			if feeStr, ok := fee.(string); ok && feeStr != "" {
				return fmt.Sprintf("Pet-friendly (%s)", feeStr)
			}
		}
	}

	// Check description for pet info
	if description, ok := property["description"].(string); ok {
		descLower := strings.ToLower(description)
		if strings.Contains(descLower, "pet-friendly") || strings.Contains(descLower, "pets allowed") {
			return "Pet-friendly"
		}
		if strings.Contains(descLower, "no pets") || strings.Contains(descLower, "pets not allowed") {
			return "No pets allowed"
		}
	}

	// Check amenities for pet-friendly
	if amenities, ok := property["amenities"].(primitive.A); ok {
		for _, amenity := range amenities {
			if amenityStr, ok := amenity.(string); ok {
				if strings.Contains(strings.ToLower(amenityStr), "pet") {
					return "Pet-friendly"
				}
			}
		}
	}

	return ""
}

func extractParkingInfo(property bson.M) string {
	// Check amenities for parking
	if amenities, ok := property["amenities"].(primitive.A); ok {
		for _, amenity := range amenities {
			if amenityStr, ok := amenity.(string); ok {
				amenityLower := strings.ToLower(amenityStr)
				if strings.Contains(amenityLower, "parking") || strings.Contains(amenityLower, "garage") {
					return fmt.Sprintf("Parking: %s", amenityStr)
				}
			}
		}
	}

	// Check description
	if description, ok := property["description"].(string); ok {
		descLower := strings.ToLower(description)
		if strings.Contains(descLower, "parking") {
			// Extract parking info
			start := strings.Index(descLower, "parking")
			if start > 0 && start+50 < len(description) {
				end := min(start+50, len(description))
				info := description[start:end]
				return fmt.Sprintf("Parking: %s...", strings.TrimSpace(info))
			}
			return "Parking available"
		}
	}

	return ""
}

func extractDepositInfo(property bson.M) string {
	// Check property level
	deposit := getFloat(property, "deposit")
	if deposit > 0 {
		return fmt.Sprintf("Deposit: $%.0f", deposit)
	}

	// Check units for deposit
	if units, ok := property["units"].(primitive.A); ok && len(units) > 0 {
		// Get first unit's deposit
		if unit, ok := units[0].(primitive.M); ok {
			unitDeposit := getFloat(unit, "deposit")
			if unitDeposit > 0 {
				return fmt.Sprintf("Deposit: $%.0f", unitDeposit)
			}
		}
	}

	return ""
}

func extractUtilitiesInfo(property bson.M) string {
	// Check description for utilities info
	if description, ok := property["description"].(string); ok {
		descLower := strings.ToLower(description)

		// Look for utilities information
		utilityKeywords := []string{"utilities included", "water included", "electric included", "gas included", "sewer included", "garbage included"}
		for _, keyword := range utilityKeywords {
			if strings.Contains(descLower, keyword) {
				// Extract the relevant section
				start := strings.Index(descLower, keyword)
				if start > 0 && start+100 < len(description) {
					end := min(start+100, len(description))
					info := description[start:end]
					return fmt.Sprintf("Utilities: %s...", strings.TrimSpace(info))
				}
				return "Utilities included"
			}
		}

		// Check for tenant responsible utilities
		tenantKeywords := []string{"tenant responsible", "tenant pays", "electric not included", "water not included"}
		for _, keyword := range tenantKeywords {
			if strings.Contains(descLower, keyword) {
				return "Tenant pays utilities"
			}
		}
	}

	return ""
}

func extractIncomeRestrictions(property bson.M) string {
	// Check amenities for income restrictions
	if amenities, ok := property["amenities"].(primitive.A); ok {
		for _, amenity := range amenities {
			if amenityStr, ok := amenity.(string); ok {
				if strings.Contains(strings.ToLower(amenityStr), "income restriction") {
					return "Income restrictions apply"
				}
			}
		}
	}

	// Check description
	if description, ok := property["description"].(string); ok {
		descLower := strings.ToLower(description)
		if strings.Contains(descLower, "income restriction") || strings.Contains(descLower, "income qualify") {
			return "Income restrictions apply"
		}
	}

	return ""
}

func extractContactInfo(property bson.M) string {
	// Check contact information
	if contact, ok := property["contact"].(primitive.M); ok {
		phone := getString(contact, "phone")
		name := getString(contact, "name")

		if phone != "" && name != "" {
			return fmt.Sprintf("Contact: %s at %s", name, phone)
		} else if phone != "" {
			return fmt.Sprintf("Contact: %s", phone)
		} else if name != "" {
			return fmt.Sprintf("Contact: %s", name)
		}
	}

	// Check assigned member
	if assignedMember, ok := property["assignedMember"].(primitive.M); ok {
		name := getString(assignedMember, "name")
		phone := getString(assignedMember, "phone")

		if name != "" && phone != "" {
			return fmt.Sprintf("Agent: %s at %s", name, phone)
		} else if name != "" {
			return fmt.Sprintf("Agent: %s", name)
		}
	}

	return ""
}
