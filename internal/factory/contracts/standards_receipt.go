package contracts

import (
	"encoding/json"
	"time"
)

// StandardsConversationOperation is an immutable admitted request and its durable result.
type StandardsConversationOperation struct {
	Key             string
	RequestDigest   string
	Kind            string
	Transport       string
	OwnerID         string
	ChannelID       string
	SourceID        string
	RequestJSON     json.RawMessage
	Started         bool
	Status          string
	EffectCommitted bool
	EffectJSON      json.RawMessage
	ResultJSON      json.RawMessage
	Error           string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
