package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/factory/localteams"
	"github.com/cloudvane/litefactory/internal/teampackage"
)

func newTeamBindingCommand(operation string) *Command {
	args := ArgSpec{}
	usage := "team unlink [--repo registered-clone-path]"
	if operation == "link" {
		args = ArgSpec{Names: []string{"name", "version"}, Min: 2}
		usage = "team link <name> <version> [--repo registered-clone-path] [--source catalogue-url|local]"
	}
	cmd := &Command{Name: "team " + operation, Usage: usage, Short: "Select an installed team version for a registered clone", Args: args}
	cmd.New = func(flags *flag.FlagSet) Handler {
		repoPath := flags.String("repo", "", "registered clone path (defaults to current repository)")
		var source string
		if operation == "link" {
			flags.StringVar(&source, "source", "", "exact installed source")
		}
		return func(ctx context.Context, env *Env, args []string) error {
			return runTeamBinding(ctx, env, operation, *repoPath, source, args)
		}
	}
	return cmd
}

// teamRemovalGuard is a compatibility binding for existing local composition.
// The factory service owns the removal fence and its persistence access.
func teamRemovalGuard(paths config.Paths) func(context.Context, teampackage.Identity) ([]string, error) {
	return localteams.RemovalGuard(paths)
}

func runTeamBinding(ctx context.Context, env *Env, operation, repoPath, source string, args []string) error {
	service, err := localteams.New(localteams.Options{Transport: env.TeamTransport})
	if err != nil {
		return err
	}
	request := contracts.TeamBindingRequest{RepoPath: repoPath, Source: source}
	present := func(result contracts.TeamBindingView) error {
		if result.Unlinked {
			if !result.Changed {
				_, err := fmt.Fprintf(env.Stdout, "Repository %s is already unlinked (built-in workflows)\n", result.RepoPath)
				return err
			}
			_, err := fmt.Fprintf(env.Stdout, "Unlinked repository %s from %s %s from %s (built-in workflows)\n", result.RepoPath, result.Slug, result.Version, result.Source)
			return err
		}
		status := "Linked"
		if !result.Changed {
			status = "Already linked"
		}
		_, err := fmt.Fprintf(env.Stdout, "%s repository %s to %s %s from %s\n", status, result.RepoPath, result.Slug, result.Version, result.Source)
		return err
	}
	if operation == "unlink" {
		err = service.Unlink(ctx, request, present)
	} else {
		request.Slug, request.Version = args[0], args[1]
		err = service.Link(ctx, request, present)
	}
	var unavailable *contracts.TeamRepositoryUnavailable
	if errors.As(err, &unavailable) {
		return Precondition(err)
	}
	return err
}
