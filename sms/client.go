package sms

import (
	"bamboo/sms/core"
	"bamboo/sms/handlers"
)

// GetTeamIdByPhoneNumber looks up the teamId associated with a phone number
func GetTeamIdByPhoneNumber(phoneNumber string) (string, error) {
	return core.GetTeamIdByPhoneNumber(phoneNumber)
}

// GetMessagesBetweenPhoneNumbers retrieves SMS conversation history between two phone numbers
func GetMessagesBetweenPhoneNumbers(fromNumber, toNumber string) ([]core.SMSMessage, error) {
	sender, err := core.NewSender()
	if err != nil {
		return nil, err
	}
	return sender.GetMessagesBetweenPhoneNumbers(fromNumber, toNumber)
}

// SendSMS sends an SMS message via Twilio
func SendSMS(to string, from string, message string, automated bool, teamId string) (string, error) {
	sender, err := core.NewSender()
	if err != nil {
		return "", err
	}
	return sender.SendSMS(to, from, message, automated, teamId)
}

// GetTeamSMSConfig fetches the SMS configuration for a team
func GetTeamSMSConfig(teamId string) (*core.SMSConfiguration, error) {
	sender, err := core.NewSender()
	if err != nil {
		return nil, err
	}
	return sender.GetTeamSMSConfiguration(teamId)
}

// NewHandler creates a new SMS handler
func NewHandler() (*handlers.Handler, error) {
	return handlers.NewHandler()
}
