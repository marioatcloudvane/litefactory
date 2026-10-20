package messaging

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/feature"
)

// FeatureConfirmer grants only confirmation, never workflow admission.
type FeatureConfirmer interface {
	Confirm(context.Context, string, string, string, string, time.Time) (string, error)
}

// EnableFeatures installs typed commands on the existing owner-only intake.
func (s *Service) EnableFeatures(drafter *feature.Drafter, confirmer FeatureConfirmer) {
	s.drafter, s.confirmer = drafter, confirmer
	s.RegisterCommand(CommandDefinition{Name: "draft", NaturalLanguage: true, Effect: ChangeEffect, Description: "Preview a feature issue", Arguments: []Argument{
		{Name: "project", Type: TextArgument, Description: "Registered owner/name or unique name"},
		{Name: "note", Type: TextArgument, Description: "Feature idea or change, up to 2000 bytes"},
	}}, nil)
	s.RegisterCommand(CommandDefinition{Name: "confirm", NaturalLanguage: true, Effect: ChangeEffect, Description: "Create the previewed feature issue", Arguments: []Argument{{Name: "code", Type: TextArgument, Required: true, Description: "8-character preview code"}}}, nil)
}
