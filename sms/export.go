// Package sms provides SMS sending and receiving functionality via Twilio
package sms

import (
	"bamboo/sms/generator"
	"bamboo/types"
)

// Core provides the core SMS functionality including models and sender
// Handlers provides HTTP handlers for SMS operations
// Utils provides utility functions for phone numbers and SMS messages
//
// Subdirectories:
//   - core: SMS models and sender
//   - handlers: HTTP request/response types and handlers
//   - utils: Phone number validation and SMS utility functions
//   - generator: AI-powered message generation
//   - property: Property-related SMS functionality
//   - intent: Intent detection and response handling

// NewAIGenerator creates a new AI message generator
func NewAIGenerator() (*generator.AIGenerator, error) {
	return generator.NewAIGenerator()
}

// GenerateLiveTextResponse generates a conversational SMS response
// Uses ONLY: chat history, command center, jake training, and lead's single property
func GenerateLiveTextResponse(
	chatHistory string,
	message string,
	teamID string,
	sessionID string,
	leadPropertyID string,
	applicationSending bool,
	tourScheduling bool,
	lead *types.Lead,
	propertySwitchNote string,
	lastAIReply string,
) (string, string, error) {
	return generator.GenerateLiveTextResponse(
		chatHistory,
		message,
		teamID,
		sessionID,
		leadPropertyID,
		applicationSending,
		tourScheduling,
		lead,
		propertySwitchNote,
		lastAIReply,
	)
}

// InvalidateCaches clears cached team config and availability data.
// Called when a property is updated to prevent stale data from reaching the AI.
func InvalidateCaches(teamID, propertyID string) {
	generator.InvalidateCaches(teamID, propertyID)
}

// StartPropertyChangeWatcher starts a MongoDB change stream listener that
// watches properties for changes and invalidates caches automatically.
func StartPropertyChangeWatcher(mongoURI string) {
	generator.StartPropertyChangeWatcher(mongoURI)
}
