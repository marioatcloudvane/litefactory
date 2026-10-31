package taskdiscussion

import "context"

func (s *Service) freshness(ctx context.Context, basis Basis, operation string) string {
	current, err := s.capture.Capture(ctx, basis.TaskRef, operation)
	if err == nil {
		if hash(current) == hash(basis) {
			return "current"
		}
		return "outdated"
	}
	// A task can advance into a stage that no longer permits advice. Its current
	// stored identities still prove that the old basis is out of date.
	if basis.StoreGuard != "" {
		guard, e := s.db.CurrentTaskAdvisoryGuard(ctx, basis.TaskRef)
		if e == nil && guard != basis.StoreGuard {
			return "outdated"
		}
	}
	return "unknown"
}
