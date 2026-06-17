package property

import (
	"time"
)

// MinifiedProperty - JSON format for SMS context
type MinifiedProperty struct {
	Type           string          `json:"type"` // "single" or "multi"
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Address        string          `json:"address"`
	Beds           int             `json:"beds,omitempty"`
	Baths          float64         `json:"baths,omitempty"`
	Sqft           int             `json:"sqft,omitempty"`
	Rent           float64         `json:"rent,omitempty"`
	Deposit        float64         `json:"deposit,omitempty"`
	Vacant         bool            `json:"vacant"`
	Amenities      []string        `json:"amenities"`
	RequiredFees   []string        `json:"requiredFees,omitempty"`
	Fees           []string        `json:"fees,omitempty"`
	PetFees        []string        `json:"petFees,omitempty"`
	Specials       []string        `json:"specials,omitempty"`
	Contact        MinifiedContact `json:"contact"`
	ApplicationURL string          `json:"applicationUrl,omitempty"`
	ScheduleURL    string          `json:"scheduleUrl,omitempty"`
	Units          []MinifiedUnit  `json:"units,omitempty"`
	Status         string          `json:"status"`
}

// MinifiedUnit represents a unit in multi-family property
type MinifiedUnit struct {
	ID       string  `json:"id"`
	UnitName string  `json:"unitName,omitempty"` // e.g. "Unit 8209", "A101"
	Beds     int     `json:"beds"`
	Baths    float64 `json:"baths"`
	Sqft     int     `json:"sqft"`
	Rent     float64 `json:"rent"`
	Deposit  float64 `json:"deposit"`
	Vacant   bool    `json:"vacant"`
	Status   string  `json:"status"`
}

// MinifiedContact represents contact info
type MinifiedContact struct {
	Phone string `json:"phone"`
	Email string `json:"email"`
}

// PropertySelectionResult represents the result of property selection
type PropertySelectionResult struct {
	PropertyID    string            `json:"propertyId"`
	Property      *MinifiedProperty `json:"property,omitempty"`
	SelectionType string            `json:"selectionType"` // "single", "context", "rule", "ai", "team", "none"
	Confidence    float64           `json:"confidence"`
	Reason        string            `json:"reason"`
}

// PropertySelectionConfig holds configuration for property selection
type PropertySelectionConfig struct {
	UseAI           bool
	MinAIConfidence float64
}

// DefaultPropertySelectionConfig returns the default configuration
func DefaultPropertySelectionConfig() PropertySelectionConfig {
	return PropertySelectionConfig{
		UseAI:           true,
		MinAIConfidence: 0.7,
	}
}

// cacheEntry for 5-minute caching
type cacheEntry struct {
	properties []MinifiedProperty
	expiresAt  time.Time
}

type propertyCacheEntry struct {
	property  *MinifiedProperty
	expiresAt time.Time
}
