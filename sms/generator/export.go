package generator

import (
	"sync"

	"bamboo/preference"
	"bamboo/sms/property"
	"bamboo/types"
)

// Singleton AIGenerator — reuses MongoDB + OpenAI connections and caches across calls.
var (
	singletonGen  *AIGenerator
	singletonErr  error
	singletonOnce sync.Once
)

func getGenerator() (*AIGenerator, error) {
	singletonOnce.Do(func() {
		singletonGen, singletonErr = NewAIGenerator()
	})
	return singletonGen, singletonErr
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
	gen, err := getGenerator()
	if err != nil {
		return "", "", err
	}

	return gen.GenerateLiveTextResponse(
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

// GetLastQualCtx returns the last computed qualification context (for test tool).
func GetLastQualCtx() QualificationContext {
	gen, err := getGenerator()
	if err != nil {
		return QualificationContext{}
	}
	return gen.GetLastQualCtx()
}

// GetLastFullPrompt returns the full prompt sent to AI (for test tool).
func GetLastFullPrompt() string {
	gen, err := getGenerator()
	if err != nil {
		return ""
	}
	return gen.GetLastFullPrompt()
}

// GetLastQualStatus returns the last computed qualification status string (for test tool).
func GetLastQualStatus() string {
	gen, err := getGenerator()
	if err != nil {
		return ""
	}
	return gen.GetLastQualStatus()
}

// InvalidateCaches clears cached team config and availability data for a property+team.
// Called by the property-updated webhook and MongoDB change stream listener.
func InvalidateCaches(teamID, propertyID string) {
	gen, err := getGenerator()
	if err != nil {
		return
	}
	gen.teamConfigCache.Invalidate(teamID)
	gen.availCache.Invalidate(propertyID, teamID)
	property.ClearPropertyCache(teamID)
	property.ClearSinglePropertyCache(propertyID)
	preference.ClearPreferenceCache(teamID)
}
