package daemon

import (
	"time"

	"github.com/cloudvane/litefactory/internal/factory/contracts"
	factoryruntime "github.com/cloudvane/litefactory/internal/factory/runtime"
)

// These aliases retain daemon host and view compatibility while discovery
// scheduling policy belongs exclusively to the factory runtime service.
type discoverySchedule = factoryruntime.DiscoverySchedule

// RepoSchedule is the bounded factory discovery schedule view.
type RepoSchedule = contracts.RepoSchedule

const maxAnnounced = factoryruntime.MaxAnnounced

func newDiscoverySchedule(interval time.Duration) *discoverySchedule {
	return factoryruntime.NewDiscoverySchedule(interval)
}
