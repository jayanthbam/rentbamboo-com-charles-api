package preference

import (
	"time"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// LeadPreference represents a lead's housing preferences
type LeadPreference struct {
	ID                primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	LeadID            string             `bson:"leadId" json:"leadId"`
	SessionID         string             `bson:"sessionId" json:"sessionId"`
	TeamID            string             `bson:"teamId" json:"teamId"`
	Preferences       Preferences        `bson:"preferences" json:"preferences"`
	MatchedProperties []string           `bson:"matchedProperties" json:"matchedProperties"`
	CreatedAt         time.Time          `bson:"createdAt" json:"createdAt"`
	UpdatedAt         time.Time          `bson:"updatedAt" json:"updatedAt"`
}

// SortOption defines how to sort properties
type SortOption string

const (
	SortNone      SortOption = ""           // No sorting
	SortPriceLow  SortOption = "price_asc"  // Cheapest first
	SortPriceHigh SortOption = "price_desc" // Most expensive first
	SortNewest    SortOption = "date_desc"  // Soonest availability
	SortOldest    SortOption = "date_asc"   // Furthest availability
	SortLargest   SortOption = "sqft_desc"  // Largest first
	SortSmallest  SortOption = "sqft_asc"   // Smallest first
)

// Preferences holds the actual preference data
type Preferences struct {
	Bedrooms         int        `bson:"bedrooms" json:"bedrooms"`
	Bathrooms        int        `bson:"bathrooms" json:"bathrooms"`
	BudgetMin        float64    `bson:"budgetMin" json:"budgetMin"`
	BudgetMax        float64    `bson:"budgetMax" json:"budgetMax"`
	MoveInDate       *time.Time `bson:"moveInDate,omitempty" json:"moveInDate,omitempty"`
	Locations        []string   `bson:"locations" json:"locations"`
	PetNeeds         []string   `bson:"petNeeds" json:"petNeeds"`
	Amenities        []string   `bson:"amenities" json:"amenities"`
	PropertyType     string     `bson:"propertyType" json:"propertyType"`
	SquareFootageMin float64    `bson:"sqftMin" json:"sqftMin"`
	SquareFootageMax float64    `bson:"sqftMax" json:"sqftMax"`
	SortBy           SortOption `bson:"sortBy" json:"sortBy"`
	LastUpdated      time.Time  `bson:"lastUpdated" json:"lastUpdated"`
}

// ExtractedPreferences is the structure the AI will return
type ExtractedPreferences struct {
	Bedrooms         int      `json:"bedrooms"`
	Bathrooms        int      `json:"bathrooms"`
	BudgetMin        float64  `json:"budgetMin"`
	BudgetMax        float64  `json:"budgetMax"`
	MoveInDate       string   `json:"moveInDate"`
	Locations        []string `json:"locations"`
	PetNeeds         []string `json:"petNeeds"`
	Amenities        []string `json:"amenities"`
	PropertyType     string   `json:"propertyType"`
	SquareFootageMin float64  `json:"sqftMin"`
	SquareFootageMax float64  `json:"sqftMax"`
	SortBy           string   `json:"sortBy"`
}

// NewLeadPreference creates a new lead preference record
func NewLeadPreference(teamID, sessionID string) *LeadPreference {
	now := time.Now()
	return &LeadPreference{
		ID:                primitive.ObjectID{},
		LeadID:            uuid.New().String(),
		SessionID:         sessionID,
		TeamID:            teamID,
		Preferences:       Preferences{},
		MatchedProperties: []string{},
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

// HasPreferences checks if any preferences have been set
func (p *Preferences) HasPreferences() bool {
	return p.Bedrooms > 0 ||
		p.Bathrooms > 0 ||
		p.BudgetMin > 0 ||
		p.BudgetMax > 0 ||
		p.MoveInDate != nil ||
		len(p.Locations) > 0 ||
		len(p.PetNeeds) > 0 ||
		len(p.Amenities) > 0 ||
		p.PropertyType != "" ||
		p.SquareFootageMin > 0 ||
		p.SquareFootageMax > 0
}

// AddBedroom adds a bedroom preference
func (p *Preferences) AddBedroom(bedroom int) {
	p.Bedrooms = bedroom
	p.LastUpdated = time.Now()
}

// SetBudget sets budget range
func (p *Preferences) SetBudget(min, max float64) {
	p.BudgetMin = min
	p.BudgetMax = max
	p.LastUpdated = time.Now()
}

// AddLocation adds a location preference
func (p *Preferences) AddLocation(location string) {
	for _, l := range p.Locations {
		if l == location {
			return
		}
	}
	p.Locations = append(p.Locations, location)
	p.LastUpdated = time.Now()
}

// AddPetNeed adds a pet requirement
func (p *Preferences) AddPetNeed(pet string) {
	for _, petItem := range p.PetNeeds {
		if petItem == pet {
			return
		}
	}
	p.PetNeeds = append(p.PetNeeds, pet)
	p.LastUpdated = time.Now()
}

// AddAmenity adds an amenity preference
func (p *Preferences) AddAmenity(amenity string) {
	for _, a := range p.Amenities {
		if a == amenity {
			return
		}
	}
	p.Amenities = append(p.Amenities, amenity)
	p.LastUpdated = time.Now()
}

// SetPropertyType sets property type preference
func (p *Preferences) SetPropertyType(propType string) {
	p.PropertyType = propType
	p.LastUpdated = time.Now()
}
