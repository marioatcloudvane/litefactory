package messaging

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/factory/owneractions"
	"github.com/cloudvane/litefactory/internal/messaging/feature"
)

// FeatureConfirmer grants only confirmation, never workflow admission.
type FeatureConfirmer interface {
	Confirm(context.Context, string, string, string, string, time.Time) (string, error)
}

// EnableFeatures installs typed commands on the existing owner-only intake.
func (s *Service) EnableFeatures(drafter *feature.Drafter, confirmer FeatureConfirmer) {
	s.drafter, s.confirmer = drafter, confirmer
	draft, _ := owneractions.ChangeDefinition("draft")
	confirm, _ := owneractions.ChangeDefinition("confirm")
	s.RegisterCommand(draft, nil)
	s.RegisterCommand(confirm, nil)
}
