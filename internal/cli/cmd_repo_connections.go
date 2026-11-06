package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"github.com/cloudvane/litefactory/internal/factory/localprojects"
	"strconv"
)

type connectionFlags struct {
	provider, workspace, team, project, authKind, modeLabels, states string
	revision                                                         int64
	origin, destination, credential, remoteID                        string
	codingAgentLabels, bugLabel                                      string
}

func newRepoConnectionCommand(action string) *Command {
	minArgs := 2
	if action == "list" {
		minArgs = 1
	}
	cmd := &Command{Name: "repo connection " + action, Usage: "repo connection " + action + " <project-id> <role>", Short: "Manage saved project connections", Args: ArgSpec{Names: []string{"project-id", "role"}, Min: minArgs}}
	if action == "list" {
		cmd.Usage = "repo connection list <project-id> [<role>]"
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var f connectionFlags
		fs.StringVar(&f.provider, "provider", "github", "ticket provider: github or linear")
		fs.StringVar(&f.workspace, "workspace", "", "verified Linear workspace ID")
		fs.StringVar(&f.team, "team", "", "verified Linear team ID")
		fs.StringVar(&f.project, "project", "", "verified Linear project ID")
		fs.StringVar(&f.authKind, "auth-kind", "api_key", "Linear authentication kind: api_key or oauth")
		fs.StringVar(&f.modeLabels, "mode-labels", "", "JSON mapping from canonical mode names to Linear label IDs")
		fs.StringVar(&f.states, "states", "", "JSON mapping: completed, retired and open to Linear state IDs")
		fs.Int64Var(&f.revision, "revision", -1, "expected current revision (0 creates a missing connection)")
		fs.StringVar(&f.origin, "origin", "", "canonical provider API root")
		fs.StringVar(&f.destination, "destination", "", "GitHub owner/name or Linear team/project")
		fs.StringVar(&f.credential, "credential", "", "external reference env:NAME or keychain:SERVICE:ACCOUNT")
		fs.StringVar(&f.remoteID, "remote-id", "", "verified immutable repository ID, when known")
		return func(ctx context.Context, env *Env, args []string) error {
			return runRepoConnection(ctx, env, cmd, action, f, args)
		}
	}
	return cmd
}

func runRepoConnection(ctx context.Context, env *Env, cmd *Command, action string, f connectionFlags, args []string) (err error) {
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return Usagef(cmd, "project-id must be a positive registered repository ID")
	}
	var role string
	if len(args) > 1 {
		role = args[1]
		if role != "code" && role != "ticket" {
			return Usagef(cmd, "role must be code or ticket")
		}
	}
	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()
	service, err := localprojects.Open(ctx, rt.db, id, localprojects.Options{Logger: env.Logger, Keychain: env.Keychain})
	if err != nil {
		return projectCommandError(cmd, err)
	}
	present := connectionPresentation(env)
	switch action {
	case "list":
		err = service.ListConnections(ctx, role, present)
	case "set":
		err = service.SetConnection(ctx, contracts.SetProjectConnectionRequest{Role: role, Provider: f.provider, Workspace: f.workspace, Team: f.team, Project: f.project, AuthKind: f.authKind, ModeLabels: f.modeLabels, CodingAgentLabels: f.codingAgentLabels, BugLabel: f.bugLabel, States: f.states, Revision: f.revision, Origin: f.origin, Destination: f.destination, Credential: f.credential, RemoteID: f.remoteID}, present)
	case "disable":
		err = service.DisableConnection(ctx, role, f.revision, present)
	case "check":
		v, observation, checkErr := service.CheckConnection(ctx, role)
		if checkErr != nil {
			return projectCommandError(cmd, checkErr)
		}
		_, err = fmt.Fprintf(env.Stdout, "project %d connection %d (%s) %s version %d checked %s: %s (%s); capabilities %v; remedy %s\n", v.RepoID, v.ID, v.Destination, role, v.Revision, observation.CheckedAt.UTC().Format("2006-01-02T15:04:05Z"), observation.Status, observation.Reason, observation.Capabilities, observation.Remedy)
	default:
		return Usagef(cmd, "unknown connection operation")
	}
	return projectCommandError(cmd, err)
}

func writeConnection(env *Env, v contracts.ProjectConnectionView) error {
	_, err := fmt.Fprintf(env.Stdout, "project %d %s version %d (id %d) %s %s %s enabled=%t provenance=%s\n", v.RepoID, v.Role, v.Revision, v.ID, v.Provider, v.Origin, v.Destination, v.Enabled, v.Provenance)
	return err
}

type bindingFlags struct {
	key                 string
	revision, id        int64
	remoteID, container string
	number              int
}

func newRepoBindingCommand(action string) *Command {
	cmd := &Command{Name: "repo binding " + action, Usage: "repo binding " + action + " <project-id>", Short: "Manage qualified project ticket bindings", Args: ArgSpec{Names: []string{"project-id"}, Min: 1}}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var f bindingFlags
		fs.Int64Var(&f.revision, "revision", -1, "expected binding revision")
		fs.Int64Var(&f.id, "binding", 0, "binding ID")
		fs.StringVar(&f.key, "key", "", "opaque Linear issue ID or display key")
		fs.StringVar(&f.remoteID, "remote-id", "", "immutable ticket ID")
		fs.StringVar(&f.container, "container", "", "ticket repository owner/name (default: current ticket destination)")
		fs.IntVar(&f.number, "number", 0, "display issue number")
		return func(ctx context.Context, env *Env, args []string) error {
			return runRepoBinding(ctx, env, cmd, action, f, args[0])
		}
	}
	return cmd
}
func runRepoBinding(ctx context.Context, env *Env, cmd *Command, action string, f bindingFlags, rawID string) (err error) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id <= 0 {
		return Usagef(cmd, "project-id must be positive")
	}
	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()
	service, err := localprojects.Open(ctx, rt.db, id, localprojects.Options{Logger: env.Logger, Keychain: env.Keychain})
	if err != nil {
		return projectCommandError(cmd, err)
	}
	request := contracts.ProjectBindingRequest{ID: f.id, Revision: f.revision, Key: f.key, RemoteID: f.remoteID, Container: f.container, Number: f.number}
	if action == "list" {
		return service.ListBindings(ctx, func(binding contracts.ProjectBindingView) error { return writeBinding(env, binding) })
	}
	var binding contracts.ProjectBindingView
	switch action {
	case "disable":
		binding, err = service.DisableBinding(ctx, request)
	case "add":
		binding, err = service.AddBinding(ctx, request)
	case "correct":
		binding, err = service.CorrectBinding(ctx, request)
	default:
		return Usagef(cmd, "unknown binding operation")
	}
	if err != nil {
		return projectCommandError(cmd, err)
	}
	return writeBinding(env, binding)
}

func writeBinding(env *Env, b contracts.ProjectBindingView) error {
	_, err := fmt.Fprintf(env.Stdout, "binding %d project %d revision %d %s %s ticket %s at %s#%s connection %d enabled=%t\n", b.ID, b.RepoID, b.Revision, b.Ref.Identity.Provider, b.Ref.Identity.Origin, b.Ref.Identity.RemoteID, b.Ref.Locator.Container, b.Ref.Locator.Key, b.ConnectionVersionID, b.Enabled)
	return err
}

func connectionPresentation(env *Env) contracts.ConnectionPresentation {
	return contracts.ConnectionPresentation{
		Connection: func(view contracts.ProjectConnectionView) error { return writeConnection(env, view) },
		BindingConflict: func(view contracts.ProjectBindingConflict) error {
			_, err := fmt.Fprintf(env.Stdout, "  binding %d cannot inherit source from version id %d to current version id %d: %s; verify or restore the intended ticket source\n", view.BindingID, view.PriorVersionID, view.CurrentVersionID, view.Reason)
			return err
		},
		Health: func(view contracts.ProjectConnectionHealth) error {
			if !view.Checked {
				status := "unchecked"
				if !view.Enabled {
					status = "disabled"
				}
				_, err := fmt.Fprintf(env.Stdout, "  health %s; write access unknown\n", status)
				return err
			}
			observation := view.Observation
			_, err := fmt.Fprintf(env.Stdout, "  checked %s: %s (%s); capabilities %v; remedy %s\n", observation.CheckedAt.UTC().Format("2006-01-02T15:04:05Z"), observation.Status, observation.Reason, observation.Capabilities, observation.Remedy)
			return err
		},
		Import: func(view contracts.ProjectImportView) error {
			_, err := fmt.Fprintf(env.Stdout, "  legacy import %s (%s)\n", view.Status, view.Reason)
			return err
		},
	}
}

func projectCommandError(cmd *Command, err error) error {
	var invalid *contracts.ProjectUsageError
	var unavailable *contracts.ProjectUnavailable
	switch {
	case errors.As(err, &invalid):
		return Usagef(cmd, "%v", err)
	case errors.As(err, &unavailable):
		return Precondition(err)
	default:
		return err
	}
}
