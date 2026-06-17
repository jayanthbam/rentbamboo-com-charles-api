package property

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/k0kubun/pp/v3"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Package-level cache (avoiding global variables)
var (
	propertyCache     = make(map[string]cacheEntry)
	propertyCacheLock sync.RWMutex

	singlePropertyCache     = make(map[string]propertyCacheEntry)
	singlePropertyCacheLock sync.RWMutex
)

// getCachedProperties returns cached properties if available and not expired
func GetCachedProperties(teamID string) ([]MinifiedProperty, bool) {
	propertyCacheLock.RLock()
	defer propertyCacheLock.RUnlock()

	entry, exists := propertyCache[teamID]
	if !exists {
		return nil, false
	}

	if time.Now().After(entry.expiresAt) {
		return nil, false
	}

	return entry.properties, true
}

// SetCachedProperties caches properties for 5 minutes
func SetCachedProperties(teamID string, properties []MinifiedProperty) {
	propertyCacheLock.Lock()
	defer propertyCacheLock.Unlock()

	propertyCache[teamID] = cacheEntry{
		properties: properties,
		expiresAt:  time.Now().Add(5 * time.Minute),
	}
}

// getCachedProperty returns a cached property by ID if available
func getCachedProperty(propertyID string) (*MinifiedProperty, bool) {
	singlePropertyCacheLock.RLock()
	defer singlePropertyCacheLock.RUnlock()

	entry, exists := singlePropertyCache[propertyID]
	if !exists {
		return nil, false
	}

	if time.Now().After(entry.expiresAt) {
		return nil, false
	}

	return entry.property, true
}

// setCachedProperty caches a single property for 5 minutes
func setCachedProperty(propertyID string, property *MinifiedProperty) {
	singlePropertyCacheLock.Lock()
	defer singlePropertyCacheLock.Unlock()

	singlePropertyCache[propertyID] = propertyCacheEntry{
		property:  property,
		expiresAt: time.Now().Add(5 * time.Minute),
	}
}

// GetTeamProperties fetches team properties from MongoDB and converts to MinifiedProperty
func GetTeamProperties(teamID string) []MinifiedProperty {
	// Check cache first
	if cached, ok := GetCachedProperties(teamID); ok {
		return cached
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		pp.Printf("Error connecting to MongoDB: %v\n", err)
		return nil
	}
	defer client.Disconnect(ctx)

	dbName := os.Getenv("MONGODB_DATABASE")
	collName := os.Getenv("MONGODB_COLLECTION_PROPERTIES")

	if dbName == "" || collName == "" {
		pp.Printf("Database or collection name not set in environment\n")
		return nil
	}

	collection := client.Database(dbName).Collection(collName)

	filter := bson.M{
		"teamId":   teamID,
		"isPublic": true,
	}

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		pp.Printf("Error finding properties: %v\n", err)
		return nil
	}
	defer cursor.Close(ctx)

	var properties []bson.M
	err = cursor.All(ctx, &properties)
	if err != nil {
		pp.Printf("Error decoding properties: %v\n", err)
		return nil
	}

	// Filter out non-vacant units and convert to MinifiedProperty
	var minifiedProperties []MinifiedProperty
	for _, property := range properties {
		minified := ConvertToMinifiedProperty(property)
		if minified != nil {
			minifiedProperties = append(minifiedProperties, *minified)
		}
	}

	// Filter out properties that are entirely off-market
	var filteredProperties []MinifiedProperty
	for _, p := range minifiedProperties {
		if p.Type == "single" && p.Status == "off-market" {
			continue
		}
		if p.Type == "multi" && len(p.Units) == 0 {
			continue
		}
		filteredProperties = append(filteredProperties, p)
	}
	minifiedProperties = filteredProperties

	// Cache for 5 minutes
	SetCachedProperties(teamID, minifiedProperties)

	return minifiedProperties
}

// GetPropertyByID fetches a single property by ID
func GetPropertyByID(teamID, propertyID string) *MinifiedProperty {
	// Check cache first
	if cached, ok := getCachedProperty(propertyID); ok {
		return cached
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		pp.Printf("Error connecting to MongoDB: %v\n", err)
		return nil
	}
	defer client.Disconnect(ctx)

	dbName := os.Getenv("MONGODB_DATABASE")
	collName := os.Getenv("MONGODB_COLLECTION_PROPERTIES")

	if dbName == "" || collName == "" {
		pp.Printf("Database or collection name not set in environment\n")
		return nil
	}

	collection := client.Database(dbName).Collection(collName)

	filter := bson.M{
		"teamId": teamID,
		"id":     propertyID,
	}

	var property bson.M
	err = collection.FindOne(ctx, filter).Decode(&property)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			pp.Printf("Property %s not found for team %s\n", propertyID, teamID)
		} else {
			pp.Printf("Error finding property: %v\n", err)
		}
		return nil
	}

	minified := ConvertToMinifiedProperty(property)
	if minified != nil {
		setCachedProperty(propertyID, minified)
	}

	return minified
}

// GetPropertyContextFromDB fetches property context from email tracking database
func GetPropertyContextFromDB(teamID, leadEmail string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	mongoURI := os.Getenv("MONGODB_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		pp.Printf("Error connecting to MongoDB: %v\n", err)
		return ""
	}
	defer client.Disconnect(ctx)

	collection := client.Database("emails").Collection("tracking")

	// Find the most recent email with this lead
	filter := bson.M{
		"teamId":          teamID,
		"emailReceiver":   leadEmail,
		"propertyContext": bson.M{"$exists": true, "$ne": ""},
	}

	opts := options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: -1}})

	var email bson.M
	err = collection.FindOne(ctx, filter, opts).Decode(&email)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return ""
		}
		pp.Printf("Error finding email context: %v\n", err)
		return ""
	}

	if propertyContext, ok := email["propertyContext"].(string); ok {
		return propertyContext
	}

	return ""
}

// ConvertToMinifiedProperty converts MongoDB property document to MinifiedProperty
func ConvertToMinifiedProperty(property bson.M) *MinifiedProperty {
	if property == nil {
		return nil
	}

	minified := &MinifiedProperty{}

	// Type
	if propType, ok := property["type"].(string); ok {
		minified.Type = propType
	}

	// ID
	if id, ok := property["id"].(string); ok {
		minified.ID = id
	}

	// Name
	if name, ok := property["propertyName"].(string); ok {
		minified.Name = name
	}

	// Address
	if location, ok := property["location"].(primitive.M); ok {
		var addressParts []string

		if fullAddr, ok := location["fullAddress"].(string); ok && fullAddr != "" {
			addressParts = append(addressParts, fullAddr)
		}

		if streetAddr, ok := location["streetAddress"].(string); ok && streetAddr != "" {
			addressParts = append(addressParts, streetAddr)
		}

		if city, ok := location["city"].(string); ok && city != "" {
			addressParts = append(addressParts, city)
		}

		if state, ok := location["state"].(string); ok && state != "" {
			addressParts = append(addressParts, state)
		}

		if postalCode, ok := location["postalCode"].(string); ok && postalCode != "" {
			addressParts = append(addressParts, postalCode)
		}

		if len(addressParts) > 0 {
			minified.Address = strings.Join(addressParts, ", ")
		}
	}

	// For single properties
	if minified.Type == "single" {
		// Beds
		if beds, ok := property["bedrooms"].(int32); ok {
			minified.Beds = int(beds)
		} else if beds, ok := property["bedrooms"].(int64); ok {
			minified.Beds = int(beds)
		} else if beds, ok := property["bedrooms"].(int); ok {
			minified.Beds = beds
		}

		// Baths (can be float like 1.5 for half-baths)
		if baths, ok := property["bathrooms"].(float64); ok {
			minified.Baths = baths
		} else if baths, ok := property["bathrooms"].(int32); ok {
			minified.Baths = float64(baths)
		} else if baths, ok := property["bathrooms"].(int64); ok {
			minified.Baths = float64(baths)
		} else if baths, ok := property["bathrooms"].(int); ok {
			minified.Baths = float64(baths)
		}

		// Sqft
		if sqft, ok := property["squareFootage"].(int32); ok {
			minified.Sqft = int(sqft)
		} else if sqft, ok := property["squareFootage"].(int64); ok {
			minified.Sqft = int(sqft)
		} else if sqft, ok := property["squareFootage"].(int); ok {
			minified.Sqft = sqft
		}

		// Rent
		if rent, ok := property["rent"].(float64); ok {
			minified.Rent = rent
		}

		// Deposit
		if deposit, ok := property["deposit"].(float64); ok {
			minified.Deposit = deposit
		}

		// Vacant
		if vacant, ok := property["isVacant"].(bool); ok {
			minified.Vacant = vacant
		}

		// Status
		if status, ok := property["status"].(string); ok {
			minified.Status = status
		}
	}

	// For multi properties
	if minified.Type == "multi" {
		if units, ok := property["units"].(primitive.A); ok {
			var minifiedUnits []MinifiedUnit
			for _, unit := range units {
				if unitMap, ok := unit.(primitive.M); ok {
					// Only include vacant units that are not off-market
					if isVacant, ok := unitMap["isVacant"].(bool); ok && isVacant {
						unit := convertToMinifiedUnit(unitMap)
						if unit == nil {
							continue
						}
						if unit.Status == "off-market" {
							continue
						}
						minifiedUnits = append(minifiedUnits, *unit)
					}
				}
			}
			minified.Units = minifiedUnits
		}
	}

	// Amenities
	if amenities, ok := property["amenities"].(primitive.A); ok {
		for _, amenity := range amenities {
			if amenityStr, ok := amenity.(string); ok {
				minified.Amenities = append(minified.Amenities, amenityStr)
			}
		}
	}

	// Required Fees
	if requiredFees, ok := property["requiredFees"].(primitive.A); ok {
		for _, fee := range requiredFees {
			if feeStr, ok := fee.(string); ok {
				minified.RequiredFees = append(minified.RequiredFees, feeStr)
			}
		}
	}

	// Fees
	if fees, ok := property["fees"].(primitive.A); ok {
		for _, fee := range fees {
			if feeStr, ok := fee.(string); ok {
				minified.Fees = append(minified.Fees, feeStr)
			}
		}
	}

	// Pet Fees
	if petFees, ok := property["petFees"].(primitive.A); ok {
		for _, fee := range petFees {
			if feeStr, ok := fee.(string); ok {
				minified.PetFees = append(minified.PetFees, feeStr)
			}
		}
	}

	// Specials
	if specials, ok := property["specials"].(primitive.A); ok {
		for _, special := range specials {
			if specialStr, ok := special.(string); ok {
				minified.Specials = append(minified.Specials, specialStr)
			}
		}
	}

	// Contact
	if contact, ok := property["contact"].(primitive.M); ok {
		minified.Contact = MinifiedContact{}
		if phone, ok := contact["phone"].(string); ok {
			minified.Contact.Phone = phone
		}
		if email, ok := contact["email"].(string); ok {
			minified.Contact.Email = email
		}
	}

	// Application URL
	if appURL, ok := property["applicationUrl"].(string); ok {
		minified.ApplicationURL = appURL
	}

	// Schedule URL
	if scheduleURL, ok := property["customScheduleUrl"].(string); ok {
		minified.ScheduleURL = scheduleURL
	}

	return minified
}

// convertToMinifiedUnit converts MongoDB unit document to MinifiedUnit
func convertToMinifiedUnit(unit primitive.M) *MinifiedUnit {
	if unit == nil {
		return nil
	}

	minified := &MinifiedUnit{}

	// ID
	if id, ok := unit["id"].(string); ok {
		minified.ID = id
	}

	// UnitName (e.g. "Unit 8209", "A101")
	if unitName, ok := unit["unitName"].(string); ok {
		minified.UnitName = unitName
	}

	// Beds
	if beds, ok := unit["bedrooms"].(int32); ok {
		minified.Beds = int(beds)
	} else if beds, ok := unit["bedrooms"].(int64); ok {
		minified.Beds = int(beds)
	} else if beds, ok := unit["bedrooms"].(int); ok {
		minified.Beds = beds
	}

	// Baths (can be float like 1.5 for half-baths)
	if baths, ok := unit["bathrooms"].(float64); ok {
		minified.Baths = baths
	} else if baths, ok := unit["bathrooms"].(int32); ok {
		minified.Baths = float64(baths)
	} else if baths, ok := unit["bathrooms"].(int64); ok {
		minified.Baths = float64(baths)
	} else if baths, ok := unit["bathrooms"].(int); ok {
		minified.Baths = float64(baths)
	}

	// Sqft
	if sqft, ok := unit["squareFootage"].(int32); ok {
		minified.Sqft = int(sqft)
	} else if sqft, ok := unit["squareFootage"].(int64); ok {
		minified.Sqft = int(sqft)
	} else if sqft, ok := unit["squareFootage"].(int); ok {
		minified.Sqft = sqft
	}

	// Rent
	if rent, ok := unit["rent"].(float64); ok {
		minified.Rent = rent
	} else if rent, ok := unit["rent"].(int32); ok {
		minified.Rent = float64(rent)
	} else if rent, ok := unit["rent"].(int64); ok {
		minified.Rent = float64(rent)
	} else if rent, ok := unit["rent"].(int); ok {
		minified.Rent = float64(rent)
	}

	// Deposit
	if deposit, ok := unit["deposit"].(float64); ok {
		minified.Deposit = deposit
	} else if deposit, ok := unit["deposit"].(int32); ok {
		minified.Deposit = float64(deposit)
	} else if deposit, ok := unit["deposit"].(int64); ok {
		minified.Deposit = float64(deposit)
	} else if deposit, ok := unit["deposit"].(int); ok {
		minified.Deposit = float64(deposit)
	}

	// Vacant
	if vacant, ok := unit["isVacant"].(bool); ok {
		minified.Vacant = vacant
	}

	// Status
	if status, ok := unit["status"].(string); ok {
		minified.Status = status
	}

	return minified
}

// ExtractUUIDFromContext extracts property ID from property context string
func ExtractUUIDFromContext(propertyContext string) string {
	if propertyContext == "" {
		return ""
	}

	// Look for UUID pattern in the context
	lines := strings.Split(propertyContext, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "Property ID:") || strings.Contains(line, "propertyId:") || strings.Contains(line, "ID:") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				id := strings.TrimSpace(parts[1])
				// Check if it looks like a UUID
				if len(id) == 36 && strings.Count(id, "-") == 4 {
					return id
				}
			}
		}
	}

	return ""
}

// ClearPropertyCache clears the cache for a specific team
func ClearPropertyCache(teamID string) {
	propertyCacheLock.Lock()
	defer propertyCacheLock.Unlock()
	delete(propertyCache, teamID)
}

// ClearSinglePropertyCache clears the cached entry for a specific property.
func ClearSinglePropertyCache(propertyID string) {
	singlePropertyCacheLock.Lock()
	defer singlePropertyCacheLock.Unlock()
	delete(singlePropertyCache, propertyID)
}
