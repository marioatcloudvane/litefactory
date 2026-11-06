package projectconnection

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/store"
)

// ErrDestinationChanged means the repository found is not the one saved.
var ErrDestinationChanged = fmt.Errorf("destination identity differs from the saved connection")

// LabelSetupCommand names the command that creates the missing labels.
func LabelSetupCommand(repoID int64) string {
	return fmt.Sprintf("litefactory repo labels setup %d", repoID)
}

// VerifyDestination runs the repository inspection and the retained-ID comparison.
func VerifyDestination(ctx context.Context, code *github.Client, v store.ConnectionVersion) (github.Repo, error) {
	parts := strings.Split(v.Destination, "/")
	if len(parts) != 2 {
		return github.Repo{}, ErrInvalidConnection
	}
	repo := github.Repo{Owner: parts[0], Name: parts[1]}
	access, err := code.InspectRepository(ctx, repo)
	if err != nil {
		return repo, err
	}
	if !strings.EqualFold(access.FullName, v.Destination) || (v.RemoteID != "" && v.RemoteID != strconv.FormatInt(access.ID, 10)) {
		return repo, ErrDestinationChanged
	}
	return repo, nil
}

// labelRemedy reads every label and reports missing labels and reader warnings.
func (f *Factory) labelRemedy(ctx context.Context, code *github.Client, repo github.Repo, v store.ConnectionVersion) string {
	names, err := code.ListLabelNames(ctx, repo)
	if err != nil {
		return " Labels could not be read."
	}
	wanted := agent.WantedLabels(f.options.LabelAgents)
	plan := agent.PlanLabels(wanted, names)
	var out strings.Builder
	if len(plan.Create) > 0 {
		missing := make([]string, 0, len(plan.Create))
		for _, w := range plan.Create {
			missing = append(missing, w.Name)
		}
		fmt.Fprintf(&out, " Warning: missing labels: %s; run `%s`.", strings.Join(missing, ", "), LabelSetupCommand(v.RepoID))
	}
	for _, w := range plan.Warnings {
		fmt.Fprintf(&out, " Warning: label %q cannot be used by the factory: %s.", w.Label, w.Reason)
	}
	return out.String()
}
