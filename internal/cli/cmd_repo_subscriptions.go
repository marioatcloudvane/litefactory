package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/projectconnection"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/webhookreg"
)

func newRepoSubscriptionCommand(action string) *Command {
	cmd := &Command{Name: "repo webhook " + action, Usage: "repo webhook " + action + " <project-id> [<subscription-id>]", Short: "Inspect or manage owned project ticket subscriptions", Args: ArgSpec{Names: []string{"project-id", "subscription-id"}, Min: 1}}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var revision int64
		var callback, eventNames string
		var enabled bool
		fs.Int64Var(&revision, "expected-revision", -1, "expected subscription revision for mutation")
		fs.StringVar(&callback, "callback", "", "new public callback URL")
		fs.StringVar(&eventNames, "events", "", "comma-separated issues, issue_comment and sub_issues events")
		fs.BoolVar(&enabled, "enabled", true, "desired webhook delivery state")
		return func(ctx context.Context, env *Env, args []string) error {
			return runRepoSubscription(ctx, env, cmd, action, revision, callback, eventNames, enabled, args)
		}
	}
	return cmd
}

func runRepoSubscription(ctx context.Context, env *Env, cmd *Command, action string, revision int64, callback, eventNames string, enabled bool, args []string) (err error) {
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id < 1 {
		return Usagef(cmd, "project-id must be a positive registered repository ID")
	}
	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()
	_, err = rt.db.GetRepo(ctx, id)
	if err != nil {
		return err
	}
	subscriptions, err := rt.db.ListTicketSubscriptions(ctx, id)
	if err != nil {
		return err
	}
	if action == "list" || action == "status" {
		for _, s := range subscriptions {
			if len(args) > 1 && args[1] != s.ID {
				continue
			}
			if err := writeSubscription(ctx, env, rt.db, s); err != nil {
				return err
			}
		}
		if len(subscriptions) == 0 {
			_, err = fmt.Fprintf(env.Stdout, "project %d has no owned webhook subscriptions; polling remains active\n", id)
		}
		return err
	}
	if action == "create" && revision != 0 {
		return Usagef(cmd, "create requires --expected-revision=0")
	}
	if action == "create" && (eventNames != "" || !enabled) {
		return Usagef(cmd, "create uses the default event selection and enabled delivery; use update to change them")
	}
	if action != "create" && revision < 1 {
		return Usagef(cmd, "--expected-revision is required")
	}
	var selected store.TicketSubscription
	if action != "create" {
		if len(args) != 2 {
			return Usagef(cmd, "subscription-id is required")
		}
		selected, err = rt.db.GetTicketSubscription(ctx, args[1])
		if err != nil {
			return err
		}
		if selected.RepoID != id {
			return errors.New("subscription belongs to another project")
		}
	}
	version, err := rt.db.CurrentConnection(ctx, id, store.TicketConnection)
	if action != "create" {
		version, err = rt.db.GetConnectionVersion(ctx, selected.ConnectionVersionID)
	}
	if err != nil {
		return err
	}
	if action == "create" && !version.Enabled {
		return store.ErrConnectionDisabled
	}
	if action == "disable" || action == "update" {
		desired := enabled
		if action == "disable" {
			desired = false
		}
		var events []string
		if eventNames != "" {
			events = strings.Split(eventNames, ",")
		}
		if action == "disable" {
			callback = ""
			events = nil
		}
		saved, saveErr := (webhookreg.Subscriptions{Store: rt.db}).Update(ctx, selected.ID, revision, desired, callback, events)
		if saved.Revision != revision+1 {
			return saveErr
		}
		selected = saved
		revision = saved.Revision
	}
	registrar, err := newProjectSubscriptions(ctx, env, rt, version)
	if err != nil {
		if selected.ID != "" {
			return errors.Join(err, writeSubscription(ctx, env, rt.db, selected))
		}
		return err
	}
	if selected.ID != "" {
		actual, verifyErr := verifySubscriptionContainer(ctx, registrar, version)
		if verifyErr != nil || actual != selected.ContainerID {
			return errors.Join(errors.New("owned subscription repository could not be verified; desired state remains saved"), writeSubscription(ctx, env, rt.db, selected))
		}
	}
	switch action {
	case "create":
		if callback != "" {
			registrar.PublicURL = callback
		}
		for _, s := range subscriptions {
			if s.ConnectionVersionID == version.ID {
				return errors.New("this connection already has an owned subscription; inspect or repair it")
			}
		}
		containerID, e := verifySubscriptionContainer(ctx, registrar, version)
		if e != nil {
			return e
		}
		selected, err = registrar.Create(ctx, version, containerID)
	case "repair":
		selected, err = registrar.Repair(ctx, selected.ID, revision)
	case "disable", "update":
		selected, err = registrar.Repair(ctx, selected.ID, revision)
	default:
		return Usagef(cmd, "unknown subscription operation")
	}
	if selected.ID != "" {
		err = errors.Join(err, writeSubscription(ctx, env, rt.db, selected))
	}
	return err
}

func newProjectSubscriptions(ctx context.Context, env *Env, rt *repoRuntime, v store.ConnectionVersion) (webhookreg.Subscriptions, error) {
	factory := projectconnection.NewFactory(projectconnection.Options{DB: rt.db, Logger: env.Logger, Resolver: secrets.Resolver{Keychain: env.Keychain}, UserAgent: initUserAgent(env)})
	clients, err := factory.Clients(ctx, v, 0)
	if err != nil {
		return webhookreg.Subscriptions{}, err
	}
	return webhookreg.Subscriptions{Store: rt.db, Forge: clients.Tickets, Keychain: webhookreg.SystemKeychain{Resolver: secrets.Resolver{Keychain: env.Keychain}, Writer: secrets.Writer{Keychain: env.KeychainWrite}}, PublicURL: rt.cfg.Webhook.PublicURL}, nil
}

func verifySubscriptionContainer(ctx context.Context, r webhookreg.Subscriptions, v store.ConnectionVersion) (string, error) {
	adapter, ok := r.Forge.(*github.Tickets)
	if !ok {
		return "", errors.New("ticket subscription adapter is unavailable")
	}
	// Resolve a repository identity without trusting callback payload names.
	identity, err := adapter.ContainerIdentity(ctx, v.Destination)
	if err != nil {
		return "", err
	}
	if v.RemoteID != "" && v.RemoteID != identity.RemoteID {
		return "", errors.New("ticket connection repository identity changed")
	}
	return identity.RemoteID, nil
}

func writeSubscription(ctx context.Context, env *Env, db *store.DB, s store.TicketSubscription) error {
	_, err := fmt.Fprintf(env.Stdout, "project %d subscription %s revision %d source-version %d %s %s %s desired-enabled=%t observed=%s hook=%s\n  checked=%s valid-delivery=%s reason=%s; remedy: inspect provider access and repair using the current revision; signing secret cannot be read back\n", s.RepoID, s.ID, s.Revision, s.ConnectionVersionID, s.Provider, s.Origin, s.Container, s.DesiredEnabled, s.ObservedState, s.RemoteHookID, s.CheckedAt.UTC().Format("2006-01-02T15:04:05Z"), s.DeliveredAt.UTC().Format("2006-01-02T15:04:05Z"), s.Failure)
	if err != nil {
		return err
	}
	p, e := db.GetTicketRecoveryPosition(ctx, s.ConnectionVersionID)
	if errors.Is(e, store.ErrNotFound) {
		_, err = fmt.Fprintln(env.Stdout, "  completed recovery: not yet observed; polling remains active")
		return err
	}
	if e != nil {
		return e
	}
	_, err = fmt.Fprintf(env.Stdout, "  completed recovery=%s enrollment=%s reconciliation=%s\n", p.CompletedThrough.UTC().Format("2006-01-02T15:04:05Z"), p.EnrollmentAt.UTC().Format("2006-01-02T15:04:05Z"), p.ReconcileCompletedAt.UTC().Format("2006-01-02T15:04:05Z"))
	return err
}
