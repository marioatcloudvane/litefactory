package cli

import (
	"context"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/factory/localprojects"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/webhookreg"
)

// verifySubscriptionContainer retains test compatibility while the owned
// implementation accepts only the destination identity it needs.
func verifySubscriptionContainer(ctx context.Context, registrar webhookreg.Subscriptions, version store.ConnectionVersion) (string, error) {
	return localprojects.VerifySubscriptionContainer(ctx, registrar, contracts.ProjectContainerClaim{Destination: version.Destination, RemoteID: version.RemoteID})
}
