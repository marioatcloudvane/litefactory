package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/ticket"
	"github.com/cloudvane/litefactory/internal/turn"
)

type connectionFlags struct {
	provider, workspace, team, project, authKind, modeLabels, states string
	revision                                                         int64
	origin, destination, credential, remoteID                        string
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
	var role store.ConnectionRole
	if len(args) > 1 {
		role = store.ConnectionRole(args[1])
		if role != store.CodeConnection && role != store.TicketConnection {
			return Usagef(cmd, "role must be code or ticket")
		}
	}
	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()
	repo, err := rt.db.GetRepo(ctx, id)
	if err != nil {
		return Precondition(err)
	}
	if action == "list" {
		versions, err := rt.db.ListConnections(ctx, id)
		if err != nil {
			return err
		}
		for _, v := range versions {
			if role != "" && v.Role != role {
				continue
			}
			if err = writeConnectionWithBindings(ctx, env, rt.db, v); err != nil {
				return err
			}
			observation, e := rt.db.GetConnectionObservation(ctx, v.ID)
			switch {
			case errors.Is(e, store.ErrNotFound):
				status := "unchecked"
				if !v.Enabled {
					status = "disabled"
				}
				_, err = fmt.Fprintf(env.Stdout, "  health %s; write access unknown\n", status)
			case e != nil:
				return e
			default:
				_, err = fmt.Fprintf(env.Stdout, "  checked %s: %s (%s); capabilities %v; remedy %s\n", observation.CheckedAt.UTC().Format("2006-01-02T15:04:05Z"), observation.Status, observation.Reason, observation.Capabilities, observation.Remedy)
			}
			if err != nil {
				return err
			}

		}
		state, e := rt.db.GetProjectImportState(ctx, id)
		if e == nil {
			_, err = fmt.Fprintf(env.Stdout, "  legacy import %s (%s)\n", state.Status, state.Reason)
			return err
		}
		if !errors.Is(e, store.ErrNotFound) {
			return e
		}
		return nil
	}
	switch action {
	case "set":
		if f.revision < 0 {
			return Usagef(cmd, "--revision is required")
		}
		origin, e := ticket.CanonicalOrigin(f.origin)
		if e != nil {
			return Usagef(cmd, "invalid API origin")
		}
		owner, name, ok := strings.Cut(f.destination, "/")
		if !ok || (github.Repo{Owner: owner, Name: name}).Validate() != nil {
			return Usagef(cmd, "destination must be owner/name")
		}
		v := store.ConnectionVersion{RepoID: id, Role: role, Provider: ticket.Provider(f.provider), Origin: origin, Destination: f.destination, RemoteID: f.remoteID, Enabled: true, Provenance: "explicit"}
		if v.Provider == ticket.Linear {
			if role != store.TicketConnection || f.destination != f.team+"/"+f.project || f.workspace == "" || f.team == "" || f.project == "" {
				return Usagef(cmd, "Linear requires ticket role and workspace/team/project IDs; destination must be team/project")
			}
			v.Linear = store.LinearConfiguration{WorkspaceID: f.workspace, TeamID: f.team, ProjectID: f.project, AuthKind: f.authKind, ModeLabelsJSON: f.modeLabels, LifecycleStatesJSON: f.states}
		} else if v.Provider != ticket.GitHub {
			return Usagef(cmd, "provider must be github or linear")
		}
		parts := strings.Split(f.credential, ":")
		switch {
		case len(parts) == 2 && parts[0] == "env":
			v.CredentialSource = "env"
			v.CredentialEnv = parts[1]
		case len(parts) == 3 && parts[0] == "keychain":
			v.CredentialSource = "keychain"
			v.CredentialService = parts[1]
			v.CredentialAccount = parts[2]
		default:
			return Usagef(cmd, "credential must reference env:NAME or keychain:SERVICE:ACCOUNT")
		}
		if role == store.CodeConnection {
			v.ClonePath = repo.Path
			v.RemoteURL = repo.RemoteURL
			v.BaseBranch = repo.BaseBranch
			v.FeaturePrefix = repo.FeaturePrefix
			v.WorktreeRoot = repo.WorktreeRoot
		}
		if v.Provider == ticket.Linear {
			checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			factory := projectconnection.NewFactory(projectconnection.Options{DB: rt.db, Logger: env.Logger, Resolver: secrets.Resolver{Keychain: env.Keychain}})
			candidate := v
			candidate.ID = 1
			clients, e := factory.Clients(checkCtx, candidate, 0)
			if e != nil {
				return Precondition(e)
			}
			if _, e = clients.Linear.Inspect(checkCtx); e != nil {
				return Preconditionf("Linear configuration could not be verified; check workspace, team, project and mappings")
			}
		}
		saved, e := rt.db.SetConnection(ctx, v, f.revision)
		if e != nil {
			return Precondition(e)
		}
		return writeConnectionWithBindings(ctx, env, rt.db, saved)
	case "disable":
		if f.revision < 1 {
			return Usagef(cmd, "--revision is required")
		}
		saved, e := rt.db.DisableConnection(ctx, id, role, f.revision)
		if e != nil {
			return Precondition(e)
		}
		return writeConnectionWithBindings(ctx, env, rt.db, saved)
	case "check":
		v, e := rt.db.CurrentConnection(ctx, id, role)
		if e != nil {
			return Precondition(e)
		}
		identities, e := turn.LoadIdentities(ctx, rt.db, store.ForgeGitHub)
		if e != nil {
			return Precondition(e)
		}
		factory := projectconnection.NewFactory(projectconnection.Options{DB: rt.db, Logger: env.Logger, ExpectedActorID: identities.BotUserID, Resolver: secrets.Resolver{Keychain: env.Keychain}})
		observation := factory.Check(ctx, v)
		if e = rt.db.SaveConnectionObservation(ctx, observation); e != nil {
			return Precondition(e)
		}
		_, e = fmt.Fprintf(env.Stdout, "project %d connection %d (%s) %s version %d checked %s: %s (%s); capabilities %v; remedy %s\n", v.RepoID, v.ID, v.Destination, role, v.Revision, observation.CheckedAt.UTC().Format("2006-01-02T15:04:05Z"), observation.Status, observation.Reason, observation.Capabilities, observation.Remedy)
		return e
	default:
		return Usagef(cmd, "unknown connection operation")
	}
}

func writeConnection(env *Env, v store.ConnectionVersion) error {
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
	if _, err = rt.db.GetRepo(ctx, id); err != nil {
		return Precondition(err)
	}
	if action == "list" {
		bindings, e := rt.db.ListTicketBindings(ctx, id)
		if e != nil {
			return e
		}
		for _, b := range bindings {
			if err = writeBinding(env, b); err != nil {
				return err
			}
		}
		return nil
	}
	if action == "disable" {
		if f.id <= 0 || f.revision < 1 {
			return Usagef(cmd, "--binding and --revision are required")
		}
		old, e := rt.db.GetTicketBindingByID(ctx, f.id)
		if e != nil {
			return Precondition(e)
		}
		if old.RepoID != id {
			return Preconditionf("binding belongs to a different project")
		}
		b, e := rt.db.DisableTicketBinding(ctx, f.id, f.revision)
		if e != nil {
			return Precondition(e)
		}
		return writeBinding(env, b)
	}
	v, err := rt.db.CurrentConnection(ctx, id, store.TicketConnection)
	if err != nil {
		return Precondition(err)
	}
	if f.container == "" {
		f.container = v.Destination
	}
	owner, name, ok := strings.Cut(f.container, "/")
	if !ok || (github.Repo{Owner: owner, Name: name}).Validate() != nil || (v.Provider == ticket.GitHub && f.number <= 0) || (v.Provider == ticket.Linear && f.key == "") || f.remoteID == "" {
		return Usagef(cmd, "valid --container, --number and --remote-id are required")
	}
	key := strconv.Itoa(f.number)
	if v.Provider == ticket.Linear {
		key = f.key
	}
	ref := ticket.Ref{Identity: ticket.Identity{Provider: v.Provider, Origin: v.Origin, RemoteID: f.remoteID}, Locator: ticket.Locator{Provider: v.Provider, Origin: v.Origin, Container: f.container, Key: key}}
	proposed := store.TicketBinding{RepoID: id, ConnectionVersionID: v.ID, Ref: ref, Enabled: true, Provenance: "explicit"}
	var b store.TicketBinding
	switch action {
	case "add":
		b, err = rt.db.AddTicketBinding(ctx, proposed)
	case "correct":
		if f.id <= 0 || f.revision < 1 {
			return Usagef(cmd, "--binding and --revision are required")
		}
		proposed.ID = f.id
		b, err = rt.db.CorrectTicketBinding(ctx, proposed, f.revision)
	default:
		return Usagef(cmd, "unknown binding operation")
	}
	if err != nil {
		return Precondition(err)
	}
	return writeBinding(env, b)
}
func writeBinding(env *Env, b store.TicketBinding) error {
	_, err := fmt.Fprintf(env.Stdout, "binding %d project %d revision %d %s %s ticket %s at %s#%s connection %d enabled=%t\n", b.ID, b.RepoID, b.Revision, b.Ref.Identity.Provider, b.Ref.Identity.Origin, b.Ref.Identity.RemoteID, b.Ref.Locator.Container, b.Ref.Locator.Key, b.ConnectionVersionID, b.Enabled)
	return err
}

func writeConnectionWithBindings(ctx context.Context, env *Env, db *store.DB, v store.ConnectionVersion) error {
	if err := writeConnection(env, v); err != nil {
		return err
	}
	if v.Role != store.TicketConnection {
		return nil
	}
	bindings, err := db.ListTicketBindings(ctx, v.RepoID)
	if err != nil {
		return err
	}
	var findings []error
	for _, b := range bindings {
		if !b.Enabled || b.ConnectionVersionID == v.ID {
			continue
		}
		err := db.CheckTicketBindingSource(ctx, b, v)
		if errors.Is(err, store.ErrBindingConflict) {
			_, err = fmt.Fprintf(env.Stdout, "  binding %d cannot inherit source from version id %d to current version id %d: %s; verify or restore the intended ticket source\n", b.ID, b.ConnectionVersionID, v.ID, err)
		}
		findings = append(findings, err)
	}
	return errors.Join(findings...)
}
