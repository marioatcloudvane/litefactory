package messaging

import (
	"github.com/cloudvane/litefactory/internal/messaging/ask"
)

// EnableAsk installs the read-only project fact collector on the existing
// registry. The conversation reads ask facts itself, so the command has no
// handler. It is called once before the daemon starts its single chat worker.
func (s *Service) EnableAsk(service *ask.Service) {
	s.ask = service
	s.RegisterCommand(CommandDefinition{Name: "ask", NaturalLanguage: true, Effect: ReadEffect, Description: "Ask about a project and its open work", Arguments: []Argument{
		{Name: "question", Type: TextArgument, Required: true, Description: "Question, up to 8192 bytes"},
		{Name: "project", Type: TextArgument, Description: "Registered owner/name or unique name"},
	}}, nil)
}
