package types

import (
	"time"
)

// LeadStatus represents the status of a lead
type LeadStatus string

const (
	Interested    LeadStatus = "Interested"
	Nurture       LeadStatus = "Nurture"
	TourScheduled LeadStatus = "Tour scheduled"
	Application   LeadStatus = "Application"
	ClosedWon     LeadStatus = "Closed won"
	ClosedLost    LeadStatus = "Closed Lost"
)

// Lead represents a lead
type Lead struct {
	ID         string    `bson:"id"`
	TeamID     string    `bson:"teamId"`
	FirstName  string    `bson:"firstName"`
	LastName   string    `bson:"lastName"`
	Email      string    `bson:"email"`
	Phone      string    `bson:"phone"`
	PropertyID string    `bson:"propertyId,omitempty"` // Legacy: top-level property ID
	UnitID     string    `bson:"unitId,omitempty"`     // Legacy: top-level unit ID
	JobTitle   string    `bson:"jobTitle,omitempty"`
	Industry   string    `bson:"industry"`
	LeadSource string    `bson:"leadSource"`
	Status     string    `bson:"status"`
	Comments   []string  `bson:"comments"`
	Budget     string    `bson:"budget,omitempty"`
	MoveInDate string    `bson:"moveInDate,omitempty"`
	Tags       []string  `bson:"tags"`
	CreatedAt  time.Time `bson:"createdAt"`
	UpdatedAt  time.Time `bson:"updatedAt"`
	LeadOwner  struct {
		ID    string `bson:"id"`
		Email string `bson:"email"`
		Name  string `bson:"name"`
	} `bson:"leadOwner"`
	OutreachPreference string `bson:"outreachPreference,omitempty"` // "sms", "email", or "both"

	// Nested property and unit objects (new format)
	Property LeadProperty `bson:"property,omitempty"`
	Unit     LeadUnit     `bson:"unit,omitempty"`

	// Qualification tracking — structured Q&A (Phase 1)
	QualificationQuestions []QualificationQuestion `bson:"qualificationQuestions,omitempty" json:"qualificationQuestions,omitempty"`

	// Persisted bedroom preference detected from conversation
	BedroomPreference string `bson:"bedroomPreference,omitempty" json:"bedroomPreference,omitempty"`

	// Pets information extracted from conversation
	Pets string `bson:"pets,omitempty" json:"pets,omitempty"`
}

// QualificationQuestion tracks a single qualification question asked by the AI.
// Stored on the lead document so answers survive property changes and cache refreshes.
type QualificationQuestion struct {
	ID         string     `bson:"id" json:"id"`                                   // UUID for this question instance
	Category   string     `bson:"category" json:"category"`                       // "qualifications", "highlights", "keyInfo", "priorities"
	Question   string     `bson:"question" json:"question"`                       // The question text that was asked
	AskedAt    *time.Time `bson:"askedAt,omitempty" json:"askedAt,omitempty"`     // When the AI asked this question
	AnsweredAt *time.Time `bson:"answeredAt,omitempty" json:"answeredAt,omitempty"` // When the lead answered
	Answer     string     `bson:"answer,omitempty" json:"answer,omitempty"`       // The lead's answer text
	Confidence *float64   `bson:"confidence,omitempty" json:"confidence,omitempty"` // 0.0-1.0 confidence in answer detection
}

// LeadNote is a private team-internal note attached to a lead. Stored in
// the `leadNotes` collection. NEVER exposed to the lead — used by the
// AI for context only.
type LeadNote struct {
	ID         string    `bson:"id" json:"id"`
	TeamID     string    `bson:"teamId" json:"teamId"`
	LeadID     string    `bson:"leadId" json:"leadId"`
	AuthorName string    `bson:"authorName,omitempty" json:"authorName,omitempty"`
	AuthorEmail string   `bson:"authorEmail,omitempty" json:"authorEmail,omitempty"`
	AuthorID   string    `bson:"authorId,omitempty" json:"authorId,omitempty"`
	Content    string    `bson:"content" json:"content"`
	CreatedAt  time.Time `bson:"createdAt" json:"createdAt"`
	UpdatedAt  time.Time `bson:"updatedAt" json:"updatedAt"`
}

// LeadProperty represents the nested property object in a lead
type LeadProperty struct {
	ID           string `bson:"id"`
	PropertyName string `bson:"propertyName"`
	Description  string `bson:"description"`
	Location     struct {
		FullAddress   string `bson:"fullAddress"`
		State         string `bson:"state"`
		City          string `bson:"city"`
		PostalCode    string `bson:"postalCode"`
		StreetAddress string `bson:"streetAddress"`
	} `bson:"location"`
	Rent   float64  `bson:"rent"`
	Photos []string `bson:"photos"`
}

// LeadUnit represents the nested unit object in a lead
type LeadUnit struct {
	ID              string   `bson:"id"`
	UnitName        string   `bson:"unitName"`
	UnitDescription string   `bson:"unitDescription"`
	UnitType        string   `bson:"unitType"`
	Bedrooms        int      `bson:"bedrooms"`
	Bathrooms       int      `bson:"bathrooms"`
	SquareFootage   float64  `bson:"squareFootage"`
	Rent            float64  `bson:"rent"`
	IsVacant        bool     `bson:"isVacant"`
	Photos          []string `bson:"photos"`
	Amenities       []string `bson:"amenities"`
	Deposit         float64  `bson:"deposit"`
}

// LeadFormSubmission represents the incoming lead form data from the widget
type LeadFormSubmission struct {
	FirstName          string `json:"firstName"`
	LastName           string `json:"lastName"`
	Email              string `json:"email"`
	Phone              string `json:"phone"`
	OutreachPreference string `json:"outreachPreference"` // "sms", "email", or "both"
	Message            string `json:"message,omitempty"`
	PropertyID         string `json:"propertyId,omitempty"`
	PropertyName       string `json:"propertyName,omitempty"`
	ClientID           string `json:"clientId"`
	SessionID          string `json:"sessionId"`
	Page               string `json:"page,omitempty"`
	Timestamp          string `json:"timestamp"`
}

// Unit represents a unit in a property
type Unit struct {
	ID              string   `json:"id" bson:"id"`
	UnitName        string   `json:"unitName" bson:"unitName"`
	UnitDescription string   `json:"unitDescription" bson:"unitDescription"`
	UnitType        string   `json:"unitType" bson:"unitType"`
	Bedrooms        int      `json:"bedrooms" bson:"bedrooms"`
	Bathrooms       int      `json:"bathrooms" bson:"bathrooms"`
	SquareFootage   float64  `json:"squareFootage" bson:"squareFootage"`
	Rent            float64  `json:"rent" bson:"rent"`
	Photos          []string `json:"photos" bson:"photos"`
	Amenities       []string `json:"amenities" bson:"amenities"`
	Deposit         float64  `json:"deposit" bson:"deposit"`
	Availability    *string  `json:"availability,omitempty" bson:"availability,omitempty"`
	IsOccupied      *bool    `json:"isOccupied,omitempty" bson:"isOccupied,omitempty"`
	FloorPlan       *string  `json:"floorPlan,omitempty" bson:"floorPlan,omitempty"`
	CurrentTenant   *string  `json:"currentTenant,omitempty" bson:"currentTenant,omitempty"`
	LeaseEndDate    *string  `json:"leaseEndDate,omitempty" bson:"leaseEndDate,omitempty"`
}

// UnitType represents the type of unit
type UnitType string

const (
	Studio       UnitType = "Studio"
	OneBed       UnitType = "1 bed"
	TwoBed       UnitType = "2 bed"
	ThreeBed     UnitType = "3 bed"
	FourBed      UnitType = "4 bed"
	FiveBedPlus  UnitType = "5 bed+"
	Loft         UnitType = "Loft"
	Duplex       UnitType = "Duplex"
	Penthouse    UnitType = "Penthouse"
	Townhouse    UnitType = "Townhouse"
	SingleFamily UnitType = "Single Family Home"
)

// SingleFamilyHome represents a single family home property
type SingleFamilyHome struct {
	ID           string `json:"id" bson:"id"`
	TeamID       string `json:"teamId" bson:"teamId"`
	PropertyName string `json:"propertyName" bson:"propertyName"`
	Location     struct {
		FullAddress   string `json:"fullAddress" bson:"fullAddress"`
		State         string `json:"state" bson:"state"`
		City          string `json:"city" bson:"city"`
		PostalCode    string `json:"postalCode" bson:"postalCode"`
		StreetAddress string `json:"streetAddress" bson:"streetAddress"`
	} `json:"location" bson:"location"`
	Photos      []string `json:"photos" bson:"photos"`
	Amenities   []string `json:"amenities" bson:"amenities"`
	Description string   `json:"description" bson:"description"`
	IsVerified  bool     `json:"isVerified" bson:"isVerified"`
	IsPublic    bool     `json:"isPublic" bson:"isPublic"`
	Contact     struct {
		Phone string `json:"phone" bson:"phone"`
		Logo  string `json:"logo,omitempty" bson:"logo,omitempty"`
		Name  string `json:"name" bson:"name"`
		Email string `json:"email" bson:"email"`
	} `json:"contact" bson:"contact"`
	Bedrooms          int      `json:"bedrooms" bson:"bedrooms"`
	Bathrooms         int      `json:"bathrooms" bson:"bathrooms"`
	SquareFootage     float64  `json:"squareFootage" bson:"squareFootage"`
	YearBuilt         *int     `json:"yearBuilt,omitempty" bson:"yearBuilt,omitempty"`
	LotSize           *string  `json:"lotSize,omitempty" bson:"lotSize,omitempty"`
	Garage            *bool    `json:"garage,omitempty" bson:"garage,omitempty"`
	GarageSize        *string  `json:"garageSize,omitempty" bson:"garageSize,omitempty"`
	Basement          *bool    `json:"basement,omitempty" bson:"basement,omitempty"`
	HasYard           *bool    `json:"hasYard,omitempty" bson:"hasYard,omitempty"`
	YardSize          *string  `json:"yardSize,omitempty" bson:"yardSize,omitempty"`
	ApplicationUrl    *string  `json:"applicationUrl,omitempty" bson:"applicationUrl,omitempty"`
	CustomScheduleUrl *string  `json:"customScheduleUrl,omitempty" bson:"customScheduleUrl,omitempty"`
	NeighborhoodDesc  *string  `json:"neighborhoodDescription,omitempty" bson:"neighborhoodDescription,omitempty"`
	Rating            *float64 `json:"rating,omitempty" bson:"rating,omitempty"`
	Scores            *struct {
		WalkScore    *float64 `json:"walkScore,omitempty" bson:"walkScore,omitempty"`
		TransitScore *float64 `json:"transitScore,omitempty" bson:"transitScore,omitempty"`
	} `json:"scores,omitempty" bson:"scores,omitempty"`
	Specials    []string `json:"specials,omitempty" bson:"specials,omitempty"`
	Coordinates *struct {
		Latitude  *float64 `json:"latitude,omitempty" bson:"latitude,omitempty"`
		Longitude *float64 `json:"longitude,omitempty" bson:"longitude,omitempty"`
	} `json:"coordinates,omitempty" bson:"coordinates,omitempty"`
	Fees         []string `json:"fees,omitempty" bson:"fees,omitempty"`
	RequiredFees []string `json:"requiredFees,omitempty" bson:"requiredFees,omitempty"`
	PetFees      []string `json:"petFees,omitempty" bson:"petFees,omitempty"`
	Rent         *float64 `json:"rent,omitempty" bson:"rent,omitempty"`
	Deposit      *float64 `json:"deposit,omitempty" bson:"deposit,omitempty"`
}

// PropertyInfo represents the property information
type PropertyInfo struct {
	ID           string `json:"id" bson:"id"`
	TeamID       string `json:"teamId" bson:"teamId"`
	PropertyName string `json:"propertyName" bson:"propertyName"`
	Location     struct {
		FullAddress   string `json:"fullAddress" bson:"fullAddress"`
		State         string `json:"state" bson:"state"`
		City          string `json:"city" bson:"city"`
		PostalCode    string `json:"postalCode" bson:"postalCode"`
		StreetAddress string `json:"streetAddress" bson:"streetAddress"`
	} `json:"location" bson:"location"`
	Photos      []string `json:"photos" bson:"photos"`
	Amenities   []string `json:"amenities" bson:"amenities"`
	Description string   `json:"description" bson:"description"`
	IsVerified  bool     `json:"isVerified" bson:"isVerified"`
	IsPublic    bool     `json:"isPublic" bson:"isPublic"`
	Contact     struct {
		Phone string `json:"phone" bson:"phone"`
		Logo  string `json:"logo,omitempty" bson:"logo,omitempty"`
		Name  string `json:"name" bson:"name"`
		Email string `json:"email" bson:"email"`
	} `json:"contact" bson:"contact"`
	ApplicationUrl    *string  `json:"applicationUrl,omitempty" bson:"applicationUrl,omitempty"`
	CustomScheduleUrl *string  `json:"customScheduleUrl,omitempty" bson:"customScheduleUrl,omitempty"`
	Sqft              *int     `json:"sqft,omitempty" bson:"sqft,omitempty"`
	NeighborhoodDesc  *string  `json:"neighborhoodDescription,omitempty" bson:"neighborhoodDescription,omitempty"`
	Rating            *float64 `json:"rating,omitempty" bson:"rating,omitempty"`
	Scores            *struct {
		WalkScore    *float64 `json:"walkScore,omitempty" bson:"walkScore,omitempty"`
		TransitScore *float64 `json:"transitScore,omitempty" bson:"transitScore,omitempty"`
	} `json:"scores,omitempty" bson:"scores,omitempty"`
	Specials    []string `json:"specials,omitempty" bson:"specials,omitempty"`
	Coordinates *struct {
		Latitude  *float64 `json:"latitude,omitempty" bson:"latitude,omitempty"`
		Longitude *float64 `json:"longitude,omitempty" bson:"longitude,omitempty"`
	} `json:"coordinates,omitempty" bson:"coordinates,omitempty"`
	Fees         []string `json:"fees,omitempty" bson:"fees,omitempty"`
	RequiredFees []string `json:"requiredFees,omitempty" bson:"requiredFees,omitempty"`
	PetFees      []string `json:"petFees,omitempty" bson:"petFees,omitempty"`
	ParkingFees  []string `json:"parkingFees,omitempty" bson:"parkingFees,omitempty"`
	Units        []Unit   `json:"units,omitempty" bson:"units,omitempty"`
}

// SMTPSettings represents the structure for SMTP settings
type SMTPSettings struct {
	Host     string `json:"host" validate:"required"`
	Port     string `json:"port" validate:"required"`
	Username string `json:"username" validate:"required,max=64"`
	Password string `json:"password" validate:"required,max=64"`
	DKIM     string `json:"dkim,omitempty"`
	Status   string `json:"status"`
}

// IMAPSettings represents the structure for IMAP settings
type IMAPSettings struct {
	Host     string `json:"host" validate:"required"`
	Port     string `json:"port" validate:"required"`
	Username string `json:"username" validate:"required,min=6,max=64"`
	Password string `json:"password" validate:"required,min=6,max=64"`
	Status   string `json:"status"`
}

// EmailConfiguration represents an email account configuration
// Email field is the owner's email (user metadata), NOT the email account being accessed
// The actual email account username is stored in SMTP.Username and IMAP.Username (should be identical)
// All API operations use ConfigID + TeamID, never requiring the owner's email in requests
type EmailConfiguration struct {
	ConfigID         string       `json:"configId" bson:"configId" validate:"required"`       // Unique configuration identifier
	UserID           string       `json:"userId" bson:"userId"`                               // Owner user ID
	Email            string       `json:"email" bson:"email" validate:"required,email"`       // Owner's email (metadata only, NOT the email account)
	TeamID           string       `json:"teamId" bson:"teamId,omitempty"`                     // Team ID for authorization
	SMTP             SMTPSettings `json:"smtp" bson:"smtp"`                                   // SMTP settings (Username is actual email account)
	IMAP             IMAPSettings `json:"imap" bson:"imap"`                                   // IMAP settings (Username is actual email account)
	CompanyGiven     bool         `json:"companyGiven" bson:"companyGiven"`                   // Company info provided
	HasAutoRespond   bool         `json:"hasAutoRespond" bson:"hasAutoRespond"`               // Auto-respond enabled
	TourTimeInterval *int         `json:"tourTimeInterval" bson:"tourTimeInterval,omitempty"` // Tour scheduling interval
	Name             *string      `json:"name,omitempty" bson:"name,omitempty"`               // Friendly name for this config
	IsDefault        *bool        `json:"isDefault,omitempty" bson:"isDefault,omitempty"`     // Default config for user
	Scan             *bool        `json:"scan,omitempty" bson:"scan,omitempty"`               // Email scanning enabled
	CreatedAt        time.Time    `json:"createdAt" bson:"createdAt"`                         // Creation timestamp
	UpdatedAt        time.Time    `json:"updatedAt" bson:"updatedAt"`                         // Last update timestamp
}

// Personality represents the personality type of a team member
type Personality string

const (
	PersonalityConsultant   Personality = "consultant"
	PersonalityChallenger   Personality = "challenger"
	PersonalityRelationship Personality = "relationship"
	PersonalityExpert       Personality = "expert"
	PersonalityCloser       Personality = "closer"
)

// EmailTracker represents a tracked email for analytics
// EmailSender and EmailReceiver contain actual email addresses (from SMTP/IMAP usernames)
type EmailTracker struct {
	ID            string    `bson:"id"`            // Unique tracker ID
	HasBeenOpened bool      `bson:"hasBeenOpened"` // Whether email was opened
	EmailReceiver string    `bson:"emailReceiver"` // Recipient's actual email address
	EmailSender   string    `bson:"emailSender"`   // Sender's actual email address (from SMTP.Username)
	OriginalEmail string    `bson:"originalEmail"` // Original email being replied to
	Subject       string    `bson:"subject"`       // Email subject
	HTML          string    `bson:"html"`          // HTML content
	Text          string    `bson:"text"`          // Plain text content
	CreatedAt     time.Time `bson:"createdAt"`     // Creation timestamp
	UpdatedAt     time.Time `bson:"updatedAt"`     // Last update timestamp
	IsAutomated   bool      `bson:"isAutomated"`   // Whether this was automated
	MessageID     string    `bson:"messageId"`     // Message-ID header
	TeamID        string    `bson:"teamId"`        // Team ID for authorization
	ConfigID      string    `bson:"configId"`      // Configuration ID for analytics tracking
	FollowUpSent  bool      `bson:"followUpSent"`  // Whether follow-up was sent
	Type          string    `bson:"type"`          // Email type (reply, forward, etc)
}

// CommandCenter represents settings for team command center
type CommandCenter struct {
	Questions              string      `json:"questions" validate:"required,min=3"`
	Priorities             string      `json:"priorities" validate:"required,min=3"`
	Personality            Personality `json:"personality" validate:"required"`
	Name                   *string     `json:"name,omitempty" validate:"min=3"`
	KeyInfo                string      `json:"keyInfo" validate:"required,min=3"`
	Highlights             string      `json:"highlights" validate:"required,min=3"`
	TeamID                 string      `json:"teamId" validate:"required,uuid"`
	QualificationMode      string      `json:"qualificationMode,omitempty" bson:"qualificationMode,omitempty"`           // "free-text" (default) or "structured"
	QualifyWithoutProperty bool        `json:"qualifyWithoutProperty,omitempty" bson:"qualifyWithoutProperty,omitempty"` // Allow AI to qualify leads without assigned property
}

// Create a struct to hold the cached data
type TrainingExample struct {
	Email         string
	NeedsResponse bool
	Embedding     []float32
}

type CacheStorage struct {
	Examples []TrainingExample
}

// MessageContent stores the parsed email content
type MessageContent struct {
	PlainText string
	HTML      string
}

// GoogleAuthConfig represents the Google authentication configuration
type GoogleAuthConfig struct {
	ID           string    `bson:"_id"`
	Email        string    `bson:"email"`
	AccessToken  string    `bson:"accessToken"`
	AutoRespond  bool      `bson:"autoRespond"`
	LastUpdated  time.Time `bson:"lastUpdated"`
	RefreshToken string    `bson:"refreshToken"`
	Scope        string    `bson:"scope"`
}

// OutlookDocument represents the Outlook authentication configuration
type OutlookDocument struct {
	UserID       string `bson:"userId"`
	UserEmail    string `bson:"userEmail"`
	AccessToken  string `bson:"accessToken"`
	RefreshToken string `bson:"refreshToken"`
	ExpiresAt    int64  `bson:"expiresAt"`
	OutlookID    string `bson:"outlookId"`
	Email        string `bson:"email"`
	Mail         string `bson:"mail"`
	DisplayName  string `bson:"displayName"`
	AutoRespond  bool   `bson:"autoRespond"`
}

// OutlookEmailListItem represents a simplified structure of an email item from Outlook API list response
type OutlookEmailListItem struct {
	ID      string `json:"id"`
	Subject string `json:"subject"`
	Sender  struct {
		EmailAddress struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"sender"`
}

// OutlookEmail represents the structure of an email from Outlook API
type OutlookEmail struct {
	ID                      string `json:"id"`
	CreatedDateTime         string `json:"createdDateTime"`
	LastModifiedDateTime    string `json:"lastModifiedDateTime"`
	ReceivedDateTime        string `json:"receivedDateTime"`
	SentDateTime            string `json:"sentDateTime"`
	Subject                 string `json:"subject"`
	BodyPreview             string `json:"bodyPreview"`
	Importance              string `json:"importance"`
	ConversationID          string `json:"conversationId"`
	IsRead                  bool   `json:"isRead"`
	IsDraft                 bool   `json:"isDraft"`
	WebLink                 string `json:"webLink"`
	InferenceClassification string `json:"inferenceClassification"`
	HasAttachments          bool   `json:"hasAttachments"`
	InternetMessageID       string `json:"internetMessageId"`
	Body                    struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
	Sender struct {
		EmailAddress struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"sender"`
	From struct {
		EmailAddress struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"from"`
	ToRecipients []struct {
		EmailAddress struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"toRecipients"`
	CcRecipients []struct {
		EmailAddress struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"ccRecipients"`
	BccRecipients []struct {
		EmailAddress struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"bccRecipients"`
	ReplyTo []struct {
		EmailAddress struct {
			Name    string `json:"name"`
			Address string `json:"address"`
		} `json:"emailAddress"`
	} `json:"replyTo"`
	Flag struct {
		FlagStatus string `json:"flagStatus"`
	} `json:"flag"`
}

// JakeTrainingFile represents a single training file for Jake
type JakeTrainingFile struct {
	ID        string `json:"id" bson:"id"`
	Name      string `json:"name" bson:"name"`
	Content   string `json:"content" bson:"content"`
	CreatedAt string `json:"createdAt" bson:"createdAt"`
}

// JakeChannelTraining represents training files for a specific channel
type JakeChannelTraining struct {
	Files []JakeTrainingFile `json:"files" bson:"files"`
}

// JakeTraining represents the training configuration for Jake AI responses
type JakeTraining struct {
	ID              string              `json:"id" bson:"_id"`
	TeamID          string              `json:"teamId" bson:"teamId"`
	UserID          string              `json:"userId" bson:"userId"`
	CommonInquiries string              `json:"commonInquiries" bson:"commonInquiries"`
	JakeCall        JakeChannelTraining `json:"jakeCall" bson:"jakeCall"`
	JakeEmail       JakeChannelTraining `json:"jakeEmail" bson:"jakeEmail"`
	JakeSMS         JakeChannelTraining `json:"jakeSMS" bson:"jakeSMS"`
	CreatedAt       time.Time           `json:"createdAt" bson:"createdAt"`
	UpdatedAt       time.Time           `json:"updatedAt" bson:"updatedAt"`
}

type Export struct {
}
