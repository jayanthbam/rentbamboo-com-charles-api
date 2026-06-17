package handlers

import "bamboo/sms/core"

// SendSMSRequest represents a request to send an SMS
type SendSMSRequest struct {
	To        string `json:"to"`
	From      string `json:"from"`
	Message   string `json:"message"`
	TeamID    string `json:"teamId"`
	Automated bool   `json:"automated"`
}

// SendSMSResponse represents the response from sending an SMS
type SendSMSResponse struct {
	Success   bool   `json:"success"`
	MessageID string `json:"messageId,omitempty"`
	Error     string `json:"error,omitempty"`
}

// GetConversationRequest represents a request to get SMS conversation
type GetConversationRequest struct {
	FromNumber string `json:"fromNumber"`
	ToNumber   string `json:"toNumber"`
	TeamID     string `json:"teamId"`
}

// GetConversationResponse represents SMS conversation response
type GetConversationResponse struct {
	Success  bool              `json:"success"`
	Messages []core.SMSMessage `json:"messages"`
	Error    string            `json:"error,omitempty"`
}

// GetConfigRequest represents a request to get SMS configuration
type GetConfigRequest struct {
	TeamID string `json:"teamId"`
}

// GetConfigResponse represents SMS configuration response
type GetConfigResponse struct {
	Success bool                   `json:"success"`
	Config  *core.SMSConfiguration `json:"config,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

// GetStatsRequest represents a request to get SMS statistics
type GetStatsRequest struct {
	TeamID string `json:"teamId"`
}

// GetStatsResponse represents SMS statistics response
type GetStatsResponse struct {
	Success bool                   `json:"success"`
	Stats   map[string]interface{} `json:"stats,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

// WebhookRequest represents a webhook request from Twilio
type WebhookRequest struct {
	MessageSid   string   `form:"MessageSid"`
	AccountSid   string   `form:"AccountSid"`
	MessagingSid string   `form:"MessagingServiceSid"`
	From         string   `form:"From"`
	To           string   `form:"To"`
	Body         string   `form:"Body"`
	NumMedia     string   `form:"NumMedia"`
	MediaURLs    []string `form:"MediaUrl0,MediaUrl1,MediaUrl2,MediaUrl3,MediaUrl4,MediaUrl5,MediaUrl6,MediaUrl7,MediaUrl8,MediaUrl9"`
	MediaTypes   []string `form:"MediaContentType0,MediaContentType1,MediaContentType2,MediaContentType3,MediaContentType4,MediaContentType5,MediaContentType6,MediaContentType7,MediaContentType8,MediaContentType9"`
	APIVersion   string   `form:"ApiVersion"`
	SmsSid       string   `form:"SmsSid"`
	SmsStatus    string   `form:"SmsStatus"`
	NumSegments  string   `form:"NumSegments"`
}

// BulkSendRequest represents a bulk SMS send request
type BulkSendRequest struct {
	TeamID   string `json:"teamId"`
	From     string `json:"from"`
	Messages []struct {
		To      string `json:"to"`
		Message string `json:"message"`
	} `json:"messages"`
}

// BulkSendResponse represents the response from bulk SMS send
type BulkSendResponse struct {
	Success      bool         `json:"success"`
	Results      []sendResult `json:"results"`
	TotalSent    int          `json:"totalSent"`
	SuccessCount int          `json:"successCount"`
	FailedCount  int          `json:"failedCount"`
	SuccessRate  float64      `json:"successRate"`
}

type sendResult struct {
	To        string `json:"to"`
	Success   bool   `json:"success"`
	MessageID string `json:"messageId,omitempty"`
	Error     string `json:"error,omitempty"`
}
