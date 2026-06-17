package core

import "time"

// SMSMessage represents a SMS message with tracking information
type SMSMessage struct {
	MessageID    string    `bson:"messageId"`
	Body         string    `bson:"body"`
	From         string    `bson:"from"`
	To           string    `bson:"to"`
	Status       string    `bson:"status"`
	Direction    string    `bson:"direction"`
	Timestamp    time.Time `bson:"timestamp"`
	MediaCount   int       `bson:"mediaCount"`
	AccountSid   string    `bson:"accountSid"`
	Segments     int       `bson:"segments"`
	ErrorCode    *string   `bson:"errorCode"`
	ErrorMessage *string   `bson:"errorMessage"`
	Automated    bool      `bson:"automated"`
	// SentBy is the Clerk userId of the human agent who sent this
	// message, when applicable. Empty for AI-sent messages. Used to
	// distinguish human vs AI outbound messages in the chat history
	// for HITL (human-in-the-loop) awareness in the prompt.
	SentBy string `bson:"sentBy,omitempty"`
}

// SMSConfiguration represents user SMS settings and configurations
type SMSConfiguration struct {
	UserID          string    `bson:"userId"`
	UserEmail       string    `bson:"userEmail"`
	PhoneNumber     string    `bson:"phoneNumber"`
	DefaultAreaCode string    `bson:"defaultAreaCode"`
	CreatedAt       time.Time `bson:"createdAt"`
	UpdatedAt       time.Time `bson:"updatedAt"`
	SmsSent         int       `bson:"smsSent"`
	SmsAutoRespond  int       `bson:"smsAutoRespond"`
	AutoRespond     bool      `bson:"autoRespond"`
	TeamID          string    `bson:"teamId"`
	SmsReceived     int       `bson:"smsReceived"`
}
