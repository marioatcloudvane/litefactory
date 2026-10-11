package presentation

import "context"

type assessmentKey struct{}

// Assessment is the source producer's status, scoped to one synchronous
// command call. It never grants a command or persistence capability.
type Assessment struct {
	Status Status
	RepoID int64
}

// WithAssessment creates an explicit conservative source status. Producers
// declare certainty at the branch that knows the outcome, never by prose parsing.
func WithAssessment(ctx context.Context) (context.Context, *Assessment) {
	a := &Assessment{Status: Uncertain}
	return context.WithValue(ctx, assessmentKey{}, a), a
}

// Assess records source status when the caller provided an assessment. Direct
// source calls without a presentation consumer keep their existing behavior.
func Assess(ctx context.Context, status Status) {
	if a, ok := ctx.Value(assessmentKey{}).(*Assessment); ok {
		a.Status = status
	}
}

// AssessRepository attributes usage only to a repository resolved by the source
// command. Zero leaves factory ownership intact.
func AssessRepository(ctx context.Context, repoID int64) {
	if a, ok := ctx.Value(assessmentKey{}).(*Assessment); ok && repoID > 0 {
		a.RepoID = repoID
	}
}

// AssessedRepository returns only the repository identity the source resolved.
func AssessedRepository(ctx context.Context) int64 {
	if a, ok := ctx.Value(assessmentKey{}).(*Assessment); ok {
		return a.RepoID
	}
	return 0
}
