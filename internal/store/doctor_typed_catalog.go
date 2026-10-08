package store

import "slices"

// DoctorRepairToolKinds returns the closed typed repair catalog in stable order.
// Legacy repair kinds are retained in the database for history, but are not
// members of this catalog. The returned slice belongs to the caller.
func DoctorRepairToolKinds() []string {
	return []string{
		"release_instance", "resume_story", "park_story", "stop_run",
		"requeue_schedule", "cancel_schedule", "consume_comment", "sync_branch",
	}
}

// DoctorRepairToolKind distinguishes approved typed tools from retained legacy
// kinds. Catalog membership alone grants no approval or execution authority.
func DoctorRepairToolKind(kind string) bool {
	return slices.Contains(DoctorRepairToolKinds(), kind)
}
