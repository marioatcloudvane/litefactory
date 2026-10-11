package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
)

func newRepoApproverCommand(action string) *Command {
	cmd := &Command{Name: "repo approver " + action, Usage: "repo approver " + action + " <project-id>", Short: "Manage the verified project ticket approver", Args: ArgSpec{Names: []string{"project-id"}, Min: 1}}
	if action == "set" || action == "replace" {
		cmd.Usage += " <login> --connection-revision N --revision N"
		cmd.Args = ArgSpec{Names: []string{"project-id", "login"}, Min: 2}
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var revision, connectionRevision int64
		fs.Int64Var(&revision, "revision", -1, "expected approver revision (0 for initial set)")
		fs.Int64Var(&connectionRevision, "connection-revision", -1, "expected ticket connection revision")
		return func(ctx context.Context, env *Env, args []string) error {
			return runRepoApprover(ctx, env, cmd, action, revision, connectionRevision, args)
		}
	}
	return cmd
}

func runRepoApprover(ctx context.Context, env *Env, cmd *Command, action string, revision, connectionRevision int64, args []string) (err error) {
	if len(args) < 1 {
		return Usagef(cmd, "project-id is required")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return Usagef(cmd, "project-id must be a positive registered repository ID")
	}
	if action != "inspect" && (revision < 0 || connectionRevision < 1) {
		return Usagef(cmd, "--revision and --connection-revision are required")
	}
	if action == "set" && revision != 0 {
		return Usagef(cmd, "set requires --revision 0; use replace or refresh for an existing appointment")
	}
	if (action == "replace" || action == "refresh") && revision < 1 {
		return Usagef(cmd, "replacement or refresh requires a positive current --revision")
	}
	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()
	if _, err = rt.db.GetRepo(ctx, id); err != nil {
		return Precondition(err)
	}
	service := projectconnection.ApproverService{DB: rt.db}
	if action == "inspect" {
		inspection, e := service.Inspect(ctx, id)
		if e != nil && !errors.Is(e, store.ErrApproverMissing) && !errors.Is(e, store.ErrNotFound) {
			return Precondition(e)
		}
		fmt.Fprintf(env.Stdout, "project %d ticket connection revision %d\n", id, inspection.Connection.Revision)
		if e == nil {
			writeApprover(env, inspection.Appointment)
			if inspection.Appointment.Actor.Provider != inspection.Connection.Provider || inspection.Appointment.Actor.Origin != inspection.Connection.Origin {
				fmt.Fprintln(env.Stdout, "  authority blocked: approver provider or origin differs from the ticket connection; correct the connection or verified appointment")
			}
			if !inspection.Connection.Enabled {
				fmt.Fprintln(env.Stdout, "  authority blocked: ticket connection is disabled")
			}
		} else {
			fmt.Fprintln(env.Stdout, "no verified approver; use `litefactory repo approver set` before authorizing ticket work")
		}
		if inspection.Initialization.Status != "" {
			fmt.Fprintf(env.Stdout, "  legacy authority %s: %s\n", inspection.Initialization.Status, inspection.Initialization.Reason)
		}
		for _, blocker := range inspection.Blockers {
			fmt.Fprintf(env.Stdout, "  blocker %s: %s\n", blocker.Kind, blocker.Key)
		}
		return nil
	}
	publisher, err := rt.db.GetIdentity(ctx, store.ForgeGitHub, store.RoleBot)
	if err != nil {
		return Precondition(err)
	}
	service.Factory = projectconnection.NewFactory(projectconnection.Options{Logger: env.Logger, ExpectedActorID: publisher.UserID, Resolver: secrets.Resolver{Keychain: env.Keychain}})
	var appointment store.ApproverAppointment
	switch action {
	case "set", "replace":
		if len(args) < 2 {
			return Usagef(cmd, "login is required")
		}
		appointment, err = service.Set(ctx, id, args[1], connectionRevision, revision)
	case "refresh":
		appointment, err = service.Refresh(ctx, id, connectionRevision, revision)
	default:
		return Usagef(cmd, "unknown approver operation")
	}
	if err != nil {
		return Precondition(err)
	}
	writeApprover(env, appointment)
	return nil
}

func writeApprover(env *Env, a store.ApproverAppointment) {
	fmt.Fprintf(env.Stdout, "project %d approver %s %s id %s (@%s), revision %d appointment %d; verified %s provenance=%s; activated %s fresh-replies=%t\n", a.RepoID, a.Actor.Provider, a.Actor.Origin, a.Actor.RemoteID, a.Handle, a.Revision, a.ID, a.VerifiedAt.UTC().Format(time.RFC3339Nano), a.Provenance, a.ActivatedAt.UTC().Format(time.RFC3339Nano), a.RequireFresh)
}
