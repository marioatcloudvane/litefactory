package store

import (
	"slices"

	"github.com/cloudvane/litefactory/internal/agent"
)

// DoctorRepairToolKinds returns the closed typed repair catalog in stable order.
// Legacy repair kinds are retained in the database for history, but are not
// members of this catalog. The returned slice belongs to the caller.
func DoctorRepairToolKinds() []string {
	return agent.TypedRepairToolKinds()
}

// DoctorRepairToolKind distinguishes approved typed tools from retained legacy
// kinds. Catalog membership alone grants no approval or execution authority.
func DoctorRepairToolKind(kind string) bool {
	return slices.Contains(DoctorRepairToolKinds(), kind)
}
