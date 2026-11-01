package daemon

import (
	"context"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/loganalysis"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/turn"
)

// validateFactoryBugs files Validate's owner-confirmed factory Bugs in the
// configured upstream repository through the log-analysis tracker, the same
// path log analysis files its Bugs on.
type validateFactoryBugs struct{ tracker loganalysis.Tracker }

// FileFactoryBug implements turn.ValidateFactoryBugFiler.
func (v validateFactoryBugs) FileFactoryBug(ctx context.Context, title, body redact.Body, marker string, since time.Time) (string, string, error) {
	hit, err := v.tracker.FileBug(ctx, title, body, marker, since)
	return hit.Reference, hit.URL, err
}

// newValidateFactoryBugs binds the upstream repository of log analysis. It is
// nil when none is configured or the factory has no GitHub credential; Validate
// then records each confirmation as failed with the reason.
func newValidateFactoryBugs(s Setup, redactor *redact.Redactor) (turn.ValidateFactoryBugFiler, error) {
	if s.Config == nil {
		return nil, nil
	}
	owner, name, ok := s.Config.Standards.LogAnalysis.UpstreamRepository()
	if !ok || s.Token.Empty() {
		return nil, nil
	}
	client, err := github.New(github.Options{Token: s.Token, APIBase: s.Config.Providers.GitHub.APIBase, Redactor: redactor, Logger: s.Logger, UserAgent: s.UserAgent})
	if err != nil {
		return nil, err
	}
	return validateFactoryBugs{tracker: loganalysis.GitHubTracker{Client: client, Repo: github.Repo{Owner: owner, Name: name}}}, nil
}
