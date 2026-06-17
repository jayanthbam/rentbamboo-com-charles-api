package preference

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// PropertyCacheEntry represents cached property results for a preference set
type PropertyCacheEntry struct {
	Properties []Property
	ExpiresAt  time.Time
}

// preferenceCache stores property results by preference hash
var (
	preferenceCache     = make(map[string]PropertyCacheEntry)
	preferenceCacheLock sync.RWMutex
)

// PreferenceHash generates a unique hash key for a preference set
func PreferenceHash(teamID string, prefs *Preferences) string {
	if prefs == nil {
		return teamID + ":none"
	}
	return fmt.Sprintf("%s:%d:%d:%.0f:%.0f:%d:%d:%s",
		teamID,
		prefs.Bedrooms,
		prefs.Bathrooms,
		prefs.BudgetMin,
		prefs.BudgetMax,
		int(prefs.SquareFootageMin),
		int(prefs.SquareFootageMax),
		prefs.PropertyType,
	)
}

// GetCachedProperties returns cached properties if available and not expired
func GetCachedProperties(teamID string, prefs *Preferences) ([]Property, bool) {
	preferenceCacheLock.RLock()
	defer preferenceCacheLock.RUnlock()

	hash := PreferenceHash(teamID, prefs)
	entry, exists := preferenceCache[hash]
	if !exists {
		return nil, false
	}

	if time.Now().After(entry.ExpiresAt) {
		return nil, false
	}

	return entry.Properties, true
}

// SetCachedProperties caches properties for 5 minutes
func SetCachedProperties(teamID string, prefs *Preferences, properties []Property) {
	preferenceCacheLock.Lock()
	defer preferenceCacheLock.Unlock()

	hash := PreferenceHash(teamID, prefs)
	preferenceCache[hash] = PropertyCacheEntry{
		Properties: properties,
		ExpiresAt:  time.Now().Add(5 * time.Minute),
	}
}

// ClearPreferenceCache clears the cache for a specific team
func ClearPreferenceCache(teamID string) {
	preferenceCacheLock.Lock()
	defer preferenceCacheLock.Unlock()

	// Clear all entries for this team
	for key := range preferenceCache {
		if strings.HasPrefix(key, teamID+":") {
			delete(preferenceCache, key)
		}
	}
}

// Property represents a property from the database
type Property struct {
	ID           string   `bson:"id" json:"id"`
	PropertyName string   `bson:"propertyName" json:"propertyName"`
	TeamID       string   `bson:"teamId" json:"teamId"`
	Type         string   `bson:"type" json:"type"` // "single" or "multi"
	Location     Location `bson:"location" json:"location"`
	Sqft         *int     `bson:"sqft" json:"sqft"`
	Bedrooms     *int     `bson:"bedrooms" json:"bedrooms"`
	Bathrooms    *int     `bson:"bathrooms" json:"bathrooms"`
	Rent         *float64 `bson:"rent" json:"rent"`
	Deposit      *float64 `bson:"deposit" json:"deposit"`
	Amenities    []string `bson:"amenities" json:"amenities"`
	PetFees      []string `bson:"petFees" json:"petFees"`
	Units        []Unit   `bson:"units" json:"units"`
}

// Location represents property location
type Location struct {
	FullAddress   string `bson:"fullAddress" json:"fullAddress"`
	State         string `bson:"state" json:"state"`
	City          string `bson:"city" json:"city"`
	PostalCode    string `bson:"postalCode" json:"postalCode"`
	StreetAddress string `bson:"streetAddress" json:"streetAddress"`
}

// Unit represents a unit in a property
type Unit struct {
	ID            string   `bson:"id" json:"id"`
	UnitName      string   `bson:"unitName" json:"unitName"`
	UnitType      string   `bson:"unitType" json:"unitType"`
	Bedrooms      int      `bson:"bedrooms" json:"bedrooms"`
	Bathrooms     int      `bson:"bathrooms" json:"bathrooms"`
	SquareFootage float64  `bson:"squareFootage" json:"squareFootage"`
	Rent          float64  `bson:"rent" json:"rent"`
	Deposit       float64  `bson:"deposit" json:"deposit"`
	Amenities     []string `bson:"amenities" json:"amenities"`

	IsVacant bool `bson:"isVacant" json:"isVacant"`
}

// FilterResult contains filtered properties with match reasons
type FilterResult struct {
	Properties   []Property `json:"properties"`
	MatchReasons []string   `json:"matchReasons"`
	TotalMatches int        `json:"totalMatches"`
}

// PropertyWithRent is a helper for sorting by rent
type PropertyWithRent struct {
	Property Property
	MinRent  float64
	MaxRent  float64
	MinSqft  float64
	MaxSqft  float64
}

// FilterEngine handles programmatic property filtering
type FilterEngine struct {
	collection *mongo.Collection
}

// NewFilterEngine creates a new filter engine
func NewFilterEngine() (*FilterEngine, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Get database name at runtime (after env is loaded)
	dbName := os.Getenv("MONGODB_DATABASE")
	if dbName == "" {
		return nil, fmt.Errorf("database name cannot be empty: MONGODB_DATABASE not set")
	}

	collName := os.Getenv("MONGODB_COLLECTION_PROPERTIES")
	if collName == "" {
		collName = "properties"
	}

	collection := client.Database(dbName).Collection(collName)

	return &FilterEngine{
		collection: collection,
	}, nil
}

// FilterByPreferences filters properties based on user preferences
// This does a STRICT MongoDB query with proper range filters
func (fe *FilterEngine) FilterByPreferences(ctx context.Context, teamID string, prefs *Preferences, limit int) (*FilterResult, error) {
	if prefs == nil || !prefs.HasPreferences() {
		// No preferences set, return all properties
		return fe.GetAllProperties(ctx, teamID, limit)
	}

	// Build MongoDB query with STRICT range filters
	filter := bson.M{
		"teamId":   teamID,
		"isPublic": true,
	}

	// Property type - exact match
	if prefs.PropertyType != "" {
		filter["type"] = prefs.PropertyType
	}

	// Location - case-insensitive regex match on city, fullAddress, AND propertyName
	if len(prefs.Locations) > 0 {
		locationRegex := fmt.Sprintf("(?i)(%s)", joinOr(prefs.Locations))
		filter["$or"] = []bson.M{
			{"location.city": bson.M{"$regex": locationRegex}},
			{"location.fullAddress": bson.M{"$regex": locationRegex}},
			{"propertyName": bson.M{"$regex": locationRegex}},
		}
	}

	// Execute query
	cursor, err := fe.collection.Find(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to query properties: %w", err)
	}
	defer cursor.Close(ctx)

	var properties []Property
	if err := cursor.All(ctx, &properties); err != nil {
		return nil, fmt.Errorf("failed to decode properties: %w", err)
	}

	// Apply in-memory filters for unit-level details
	filtered := fe.filterPropertiesInternal(properties, prefs)

	// Limit results
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}

	// Generate match reasons
	matchReasons := make([]string, len(filtered))
	for i := range filtered {
		matchReasons[i] = generateMatchReason(&filtered[i], prefs)
	}

	return &FilterResult{
		Properties:   filtered,
		MatchReasons: matchReasons,
		TotalMatches: len(filtered),
	}, nil
}

// FilterPropertiesByPreferences is a public method that applies preference filters to properties
// This can be called from external packages like helpers
func (fe *FilterEngine) FilterPropertiesByPreferences(properties []Property, prefs *Preferences) []Property {
	if prefs == nil || !prefs.HasPreferences() {
		return properties
	}

	var result []Property

	for _, prop := range properties {
		if prop.Type == "single" {
			// Single family home - filter at property level
			if fe.propertyMatchesPreferences(&prop, prefs) {
				result = append(result, prop)
			}
		} else {
			// Multi-unit property - check if any unit matches
			matchingUnits := fe.filterUnitsByPreferences(prop.Units, prefs)
			if len(matchingUnits) > 0 {
				// Create a copy with matching units only
				propCopy := prop
				propCopy.Units = matchingUnits
				result = append(result, propCopy)
			}
		}
	}

	return result
}

// filterPropertiesInternal is the internal implementation used by FilterByPreferences
func (fe *FilterEngine) filterPropertiesInternal(properties []Property, prefs *Preferences) []Property {
	var result []Property

	for _, prop := range properties {
		if prop.Type == "single" {
			// Single family home - filter at property level
			if fe.propertyMatchesPreferences(&prop, prefs) {
				result = append(result, prop)
			}
		} else {
			// Multi-unit property - check if any unit matches
			matchingUnits := fe.filterUnitsByPreferences(prop.Units, prefs)
			if len(matchingUnits) > 0 {
				// Create a copy with matching units only
				propCopy := prop
				propCopy.Units = matchingUnits
				result = append(result, propCopy)
			}
		}
	}

	return result
}

// propertyMatchesPreferences checks if a single property matches preferences
func (fe *FilterEngine) propertyMatchesPreferences(prop *Property, prefs *Preferences) bool {
	// Check bedrooms
	if prefs.Bedrooms > 0 && prop.Bedrooms != nil {
		if *prop.Bedrooms != prefs.Bedrooms {
			return false
		}
	}

	// Check bathrooms
	if prefs.Bathrooms > 0 && prop.Bathrooms != nil {
		if *prop.Bathrooms < prefs.Bathrooms {
			return false
		}
	}

	// Check rent (budget)
	if prefs.BudgetMax > 0 && prop.Rent != nil {
		if *prop.Rent > prefs.BudgetMax {
			return false
		}
	}
	if prefs.BudgetMin > 0 && prop.Rent != nil {
		if *prop.Rent < prefs.BudgetMin {
			return false
		}
	}

	// Check sqft
	if prefs.SquareFootageMax > 0 && prop.Sqft != nil {
		if *prop.Sqft > int(prefs.SquareFootageMax) {
			return false
		}
	}
	if prefs.SquareFootageMin > 0 && prop.Sqft != nil {
		if *prop.Sqft < int(prefs.SquareFootageMin) {
			return false
		}
	}

	// Check pet needs
	if len(prefs.PetNeeds) > 0 {
		hasPetFees := len(prop.PetFees) > 0
		if !hasPetFees {
			return false
		}
	}

	// Check amenities
	if len(prefs.Amenities) > 0 {
		propAmenities := toLowerSlice(prop.Amenities)
		prefAmenities := toLowerSlice(prefs.Amenities)
		matched := false
		for _, prefAmenity := range prefAmenities {
			for _, propAmenity := range propAmenities {
				if containsWord(propAmenity, prefAmenity) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		// If any amenity requested, at least one should match
		if !matched && len(prefs.Amenities) > 0 {
			// Optional - don't filter out if amenities don't match
		}
	}

	return true
}

// filterUnitsByPreferences filters units within a property
func (fe *FilterEngine) filterUnitsByPreferences(units []Unit, prefs *Preferences) []Unit {
	var result []Unit

	for _, unit := range units {
		// Only include vacant units
		if !unit.IsVacant {
			continue
		}

		// Check bedrooms
		if prefs.Bedrooms > 0 {
			if unit.Bedrooms != prefs.Bedrooms {
				continue
			}
		}

		// Check bathrooms
		if prefs.Bathrooms > 0 && unit.Bathrooms < prefs.Bathrooms {
			continue
		}

		// Check rent
		if prefs.BudgetMax > 0 && unit.Rent > prefs.BudgetMax {
			continue
		}
		if prefs.BudgetMin > 0 && unit.Rent < prefs.BudgetMin {
			continue
		}

		// Check sqft
		if prefs.SquareFootageMax > 0 && unit.SquareFootage > prefs.SquareFootageMax {
			continue
		}
		if prefs.SquareFootageMin > 0 && unit.SquareFootage < prefs.SquareFootageMin {
			continue
		}

		result = append(result, unit)
	}

	return result
}

// GetAllProperties returns all properties for a team
func (fe *FilterEngine) GetAllProperties(ctx context.Context, teamID string, limit int) (*FilterResult, error) {
	filter := bson.M{
		"teamId":   teamID,
		"isPublic": true,
	}

	opts := options.Find()
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}

	cursor, err := fe.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to query properties: %w", err)
	}
	defer cursor.Close(ctx)

	var properties []Property
	if err := cursor.All(ctx, &properties); err != nil {
		return nil, fmt.Errorf("failed to decode properties: %w", err)
	}

	matchReasons := make([]string, len(properties))
	for i := range properties {
		matchReasons[i] = "Matches general criteria"
	}

	return &FilterResult{
		Properties:   properties,
		MatchReasons: matchReasons,
		TotalMatches: len(properties),
	}, nil
}

// SortProperties sorts properties based on the SortOption
func SortProperties(properties []Property, sortBy SortOption) []Property {
	if len(properties) == 0 || sortBy == "" || sortBy == SortNone {
		return properties
	}

	// Create a copy to avoid modifying original
	result := make([]Property, len(properties))
	copy(result, properties)

	switch sortBy {
	case SortPriceLow:
		// Cheapest first - sort by minimum rent
		sort.Slice(result, func(i, j int) bool {
			rentI := getPropertyMinRent(&result[i])
			rentJ := getPropertyMinRent(&result[j])
			return rentI < rentJ
		})
	case SortPriceHigh:
		// Most expensive first - sort by maximum rent
		sort.Slice(result, func(i, j int) bool {
			rentI := getPropertyMaxRent(&result[i])
			rentJ := getPropertyMaxRent(&result[j])
			return rentI > rentJ
		})
	case SortNewest:
		// Newest/soonest available first (for now, just return as-is since we don't have availability date in this struct)
		// Could be extended to check unit availability dates
		return result
	case SortOldest:
		// Oldest/furthest availability
		return result
	case SortLargest:
		// Largest first - sort by max sqft
		sort.Slice(result, func(i, j int) bool {
			sqftI := getPropertyMaxSqft(&result[i])
			sqftJ := getPropertyMaxSqft(&result[j])
			return sqftI > sqftJ
		})
	case SortSmallest:
		// Smallest first - sort by min sqft
		sort.Slice(result, func(i, j int) bool {
			sqftI := getPropertyMinSqft(&result[i])
			sqftJ := getPropertyMinSqft(&result[j])
			return sqftI < sqftJ
		})
	}

	return result
}

// getPropertyMinRent returns the minimum rent across property and its units
func getPropertyMinRent(prop *Property) float64 {
	minRent := float64(0)

	if prop.Type == "single" && prop.Rent != nil {
		minRent = *prop.Rent
	}

	// Check units
	for _, unit := range prop.Units {
		if unit.IsVacant {
			if minRent == 0 || unit.Rent < minRent {
				minRent = unit.Rent
			}
		}
	}

	return minRent
}

// getPropertyMaxRent returns the maximum rent across property and its units
func getPropertyMaxRent(prop *Property) float64 {
	maxRent := float64(0)

	if prop.Type == "single" && prop.Rent != nil {
		maxRent = *prop.Rent
	}

	// Check units
	for _, unit := range prop.Units {
		if unit.IsVacant {
			if unit.Rent > maxRent {
				maxRent = unit.Rent
			}
		}
	}

	return maxRent
}

// getPropertyMinSqft returns the minimum sqft across property and its units
func getPropertyMinSqft(prop *Property) float64 {
	minSqft := float64(0)

	if prop.Type == "single" && prop.Sqft != nil {
		minSqft = float64(*prop.Sqft)
	}

	// Check units
	for _, unit := range prop.Units {
		if unit.IsVacant {
			if minSqft == 0 || unit.SquareFootage < minSqft {
				minSqft = unit.SquareFootage
			}
		}
	}

	return minSqft
}

// getPropertyMaxSqft returns the maximum sqft across property and its units
func getPropertyMaxSqft(prop *Property) float64 {
	maxSqft := float64(0)

	if prop.Type == "single" && prop.Sqft != nil {
		maxSqft = float64(*prop.Sqft)
	}

	// Check units
	for _, unit := range prop.Units {
		if unit.IsVacant {
			if unit.SquareFootage > maxSqft {
				maxSqft = unit.SquareFootage
			}
		}
	}

	return maxSqft
}

// Helper functions
func joinOr(items []string) string {
	result := ""
	for i, item := range items {
		if i > 0 {
			result += "|"
		}
		result += item
	}
	return result
}

func toLowerSlice(items []string) []string {
	result := make([]string, len(items))
	for i, item := range items {
		result[i] = toLower(item)
	}
	return result
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

func containsWord(text, word string) bool {
	text = toLower(text)
	word = toLower(word)
	// Simple contains check
	return len(text) >= len(word) && (text == word || containsSubstring(text, word))
}

func containsSubstring(text, substr string) bool {
	for i := 0; i <= len(text)-len(substr); i++ {
		if text[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func generateMatchReason(prop *Property, prefs *Preferences) string {
	reasons := []string{}

	if prop.Bedrooms != nil {
		reasons = append(reasons, fmt.Sprintf("%d bed", *prop.Bedrooms))
	}

	if prop.Rent != nil {
		reasons = append(reasons, fmt.Sprintf("$%.0f/mo", *prop.Rent))
	}

	if len(prop.Location.City) > 0 {
		reasons = append(reasons, prop.Location.City)
	}

	if len(reasons) == 0 {
		return "Available property"
	}

	return joinSlice(reasons, ", ")
}

func joinSlice(items []string, sep string) string {
	result := ""
	for i, item := range items {
		if i > 0 {
			result += sep
		}
		result += item
	}
	return result
}
