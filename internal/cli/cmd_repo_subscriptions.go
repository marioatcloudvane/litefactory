package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"github.com/cloudvane/litefactory/internal/factory/contracts"
	"strconv"
)

func newRepoSubscriptionCommand(action string) *Command {
	cmd := &Command{Name: "repo webhook " + action, Usage: "repo webhook " + action + " <project-id> [<subscription-id>]", Short: "Inspect or manage owned project ticket subscriptions", Args: ArgSpec{Names: []string{"project-id", "subscription-id"}, Min: 1}}
	if action == "create" {
		cmd.Usage += " --expected-revision=0"
	}
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
	request := contracts.ProjectSubscriptionRequest{ProjectID: id, Revision: revision, Callback: callback, Events: eventNames, Enabled: enabled, HasID: len(args) > 1}
	if request.HasID {
		request.ID = args[1]
	}
	administration := projectAdministration(rt, env)
	present := subscriptionPresentation(env)
	switch action {
	case "list", "status":
		err = administration.ListSubscriptions(ctx, request, present)
	case "create":
		err = administration.CreateSubscription(ctx, request, present)
	case "repair":
		err = administration.RepairSubscription(ctx, request, present)
	case "update":
		err = administration.UpdateSubscription(ctx, request, present)
	case "disable":
		err = administration.DisableSubscription(ctx, request, present)
	default:
		return Usagef(cmd, "unknown subscription operation")
	}
	return projectCommandError(cmd, err)
}

func subscriptionPresentation(env *Env) contracts.ProjectSubscriptionPresentation {
	return contracts.ProjectSubscriptionPresentation{
		Subscription: func(s contracts.ProjectSubscriptionView) error {
			_, err := fmt.Fprintf(env.Stdout, "project %d subscription %s revision %d source-version %d %s %s %s desired-enabled=%t observed=%s hook=%s\n  checked=%s valid-delivery=%s reason=%s; remedy: inspect provider access and repair using the current revision; GitHub never returns the signing secret\n", s.RepoID, s.ID, s.Revision, s.ConnectionVersionID, s.Provider, s.Origin, s.Container, s.DesiredEnabled, s.ObservedState, s.RemoteHookID, s.CheckedAt.UTC().Format("2006-01-02T15:04:05Z"), s.DeliveredAt.UTC().Format("2006-01-02T15:04:05Z"), s.Failure)
			if err != nil || s.Failure != "hook_access_denied" || s.CallbackURL == "" || s.SecretAccount == "" {
				return err
			}
			// The bot was refused; name the values a hook made by hand needs.
			_, err = fmt.Fprintf(env.Stdout, "  hand-made hook: payload URL %s, content type application/json, events issues, issue_comment and sub_issues, secret from Keychain service %s account %s\n", s.CallbackURL, s.SecretService, s.SecretAccount)
			return err
		},
		Recovery: func(p contracts.ProjectRecoveryView) error {
			if !p.Known {
				_, err := fmt.Fprintln(env.Stdout, "  completed recovery: not yet observed; polling remains active")
				return err
			}
			_, err := fmt.Fprintf(env.Stdout, "  completed recovery=%s enrollment=%s reconciliation=%s\n", p.CompletedThrough.UTC().Format("2006-01-02T15:04:05Z"), p.EnrollmentAt.UTC().Format("2006-01-02T15:04:05Z"), p.ReconcileCompletedAt.UTC().Format("2006-01-02T15:04:05Z"))
			return err
		},
		Empty: func(id int64) error {
			_, err := fmt.Fprintf(env.Stdout, "project %d has no owned webhook subscriptions; polling remains active\n", id)
			return err
		},
	}
}
