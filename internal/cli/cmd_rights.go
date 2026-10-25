package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cloudvane/litefactory/internal/daemon"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/rights"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/turn"
)

// The rights commands are local and owner-only: machine access implies the
// owner, and no issue-comment path changes rights.

type rightsFlags struct {
	repo, workflow, stage, action, task, expires string
	json, all, replace                           bool
}

func (o *rightsFlags) bindRepo(fs *flag.FlagSet) {
	fs.StringVar(&o.repo, "repo", "", "registered repository, `owner/name`")
	fs.BoolVar(&o.json, "json", false, "emit JSON")
}

func (o *rightsFlags) bindAction(fs *flag.FlagSet) {
	fs.StringVar(&o.workflow, "workflow", "", "workflow, for example plan")
	fs.StringVar(&o.stage, "stage", "", "stage (graph node name)")
	fs.StringVar(&o.action, "action", "", "action: answer, request_changes or approve")
}

func newRightsCatalogueCommand() *Command {
	return &Command{Name: "rights catalogue", Usage: "rights catalogue --repo owner/name [--json]", Short: "List the actions that rights can be granted for",
		Long: "Lists one workflow | stage | action entry per graph action tagged lf_rights. Untagged actions are not listed.",
		New: func(fs *flag.FlagSet) Handler {
			var o rightsFlags
			o.bindRepo(fs)
			return func(ctx context.Context, env *Env, _ []string) error {
				return withRights(ctx, env, &o, false, func(rc *rightsContext) error {
					entries, err := rc.svc.Entries(ctx, rc.repo.ID)
					if err != nil {
						return err
					}
					if o.json {
						return writeJSON(env, nonNil(entries))
					}
					for _, e := range entries {
						if _, err := fmt.Fprintf(env.Stdout, "%s\t%s\n", e, e.Description); err != nil {
							return err
						}
					}
					return nil
				})
			}
		}}
}

func newRightsListCommand() *Command {
	return &Command{Name: "rights list", Usage: "rights list --repo owner/name [--all] [--json]", Short: "List the rights granted in a repository",
		Long: "Lists active grants. With --all, revoked grants are listed too.",
		New: func(fs *flag.FlagSet) Handler {
			var o rightsFlags
			o.bindRepo(fs)
			fs.BoolVar(&o.all, "all", false, "include revoked grants")
			return func(ctx context.Context, env *Env, _ []string) error {
				return withRights(ctx, env, &o, false, func(rc *rightsContext) error {
					grants, err := rc.svc.ListGrants(ctx, rc.repo.ID, !o.all)
					if err != nil {
						return err
					}
					if o.json {
						return writeJSON(env, nonNil(grants))
					}
					return writeGrants(env, grants)
				})
			}
		}}
}

func newRightsGrantCommand() *Command {
	return &Command{Name: "rights grant", Usage: "rights grant <login> --repo owner/name --workflow W --stage S --action A [--task REF] [--expires RFC3339] [--json]", Short: "Grant a person the right to act on a tagged action",
		Long: "The login is resolved to its numeric GitHub ID once, now. The grant is refused for an unknown repository, an action that is not in the catalogue, an expiry in the past or a duplicate of an active grant.\n--task restricts the grant to one task: a number or a task reference like github:owner/name#12.",
		Args: ArgSpec{Names: []string{"login"}, Min: 1},
		New: func(fs *flag.FlagSet) Handler {
			var o rightsFlags
			o.bindRepo(fs)
			o.bindAction(fs)
			fs.StringVar(&o.task, "task", "", "restrict the grant to one task")
			fs.StringVar(&o.expires, "expires", "", "UTC expiry as RFC 3339")
			return func(ctx context.Context, env *Env, args []string) error {
				return withRights(ctx, env, &o, true, func(rc *rightsContext) error {
					var expiry time.Time
					if o.expires != "" {
						t, err := time.Parse(time.RFC3339, o.expires)
						if err != nil {
							return Usagef(nil, "--expires takes an RFC 3339 time, got %q", o.expires)
						}
						expiry = t.UTC()
					}
					user, err := rc.lookup(ctx, args[0])
					if err != nil {
						return err
					}
					g, err := rc.svc.Grant(ctx, rights.GrantInput{RepoID: rc.repo.ID, SubjectID: user.ID, SubjectLogin: user.Login, Workflow: o.workflow, Stage: o.stage, Action: o.action, TaskRef: rc.taskRef(o.task), ExpiresAt: expiry})
					if err != nil {
						return rightsError(err)
					}
					if o.json {
						return writeJSON(env, g)
					}
					return writeGrants(env, []store.RightsGrant{g})
				})
			}
		}}
}

func newRightsRevokeCommand() *Command {
	return &Command{Name: "rights revoke", Usage: "rights revoke <grant-id> --repo owner/name [--json]", Short: "Revoke a grant; its row and history stay",
		Args: ArgSpec{Names: []string{"grant-id"}, Min: 1},
		New: func(fs *flag.FlagSet) Handler {
			var o rightsFlags
			o.bindRepo(fs)
			return func(ctx context.Context, env *Env, args []string) error {
				id, err := parseID(args[0])
				if err != nil {
					return err
				}
				return withRights(ctx, env, &o, false, func(rc *rightsContext) error {
					g, err := rc.svc.Revoke(ctx, rc.repo.ID, id)
					if err != nil {
						return rightsError(err)
					}
					if o.json {
						return writeJSON(env, g)
					}
					return writeGrants(env, []store.RightsGrant{g})
				})
			}
		}}
}

type historyView struct {
	Sequence int64           `json:"sequence"`
	At       time.Time       `json:"at"`
	ActorID  int64           `json:"actor_id"`
	Kind     string          `json:"kind"`
	RecordID int64           `json:"record_id"`
	Snapshot json.RawMessage `json:"snapshot"`
}

func newRightsHistoryCommand() *Command {
	return &Command{Name: "rights history", Usage: "rights history --repo owner/name [--json]", Short: "Show who changed which rights and reviewers, and when",
		New: func(fs *flag.FlagSet) Handler {
			var o rightsFlags
			o.bindRepo(fs)
			return func(ctx context.Context, env *Env, _ []string) error {
				return withRights(ctx, env, &o, false, func(rc *rightsContext) error {
					rows, err := rc.svc.History(ctx, rc.repo.ID)
					if err != nil {
						return err
					}
					if o.json {
						out := make([]historyView, 0, len(rows))
						for _, r := range rows {
							out = append(out, historyView{r.Sequence, r.At, r.ActorID, r.Kind, r.RecordID, json.RawMessage(r.Snapshot)})
						}
						return writeJSON(env, out)
					}
					for _, r := range rows {
						if _, err := fmt.Fprintf(env.Stdout, "%d\t%s\tactor %d\t%s\t#%d\t%s\n", r.Sequence, store.FormatTime(r.At), r.ActorID, r.Kind, r.RecordID, r.Snapshot); err != nil {
							return err
						}
					}
					return nil
				})
			}
		}}
}

func newReviewerAssignCommand() *Command {
	return &Command{Name: "reviewer assign", Usage: "reviewer assign <login> --repo owner/name --task REF --workflow W --stage S [--replace] [--json]", Short: "Assign a reviewer to a task and stage",
		Long: "An assignment asks the reviewer for attention. It grants no right. With --replace, the open reviewers of the task and stage are removed first, in one change.",
		Args: ArgSpec{Names: []string{"login"}, Min: 1},
		New: func(fs *flag.FlagSet) Handler {
			var o rightsFlags
			o.bindRepo(fs)
			fs.StringVar(&o.workflow, "workflow", "", "workflow, for example plan")
			fs.StringVar(&o.stage, "stage", "", "stage (graph node name)")
			fs.StringVar(&o.task, "task", "", "task: a number or a task reference like github:owner/name#12")
			fs.BoolVar(&o.replace, "replace", false, "remove the open reviewers of this task and stage first")
			return func(ctx context.Context, env *Env, args []string) error {
				return withRights(ctx, env, &o, true, func(rc *rightsContext) error {
					user, err := rc.lookup(ctx, args[0])
					if err != nil {
						return err
					}
					in := rights.AssignInput{RepoID: rc.repo.ID, TaskRef: rc.taskRef(o.task), Workflow: o.workflow, Stage: o.stage, ReviewerID: user.ID, ReviewerLogin: user.Login}
					var a store.ReviewerAssignment
					if o.replace {
						a, err = rc.svc.Replace(ctx, in)
					} else {
						a, err = rc.svc.Assign(ctx, in)
					}
					if err != nil {
						return rightsError(err)
					}
					if o.json {
						return writeJSON(env, a)
					}
					return writeAssignments(env, []store.ReviewerAssignment{a})
				})
			}
		}}
}

func newReviewerRemoveCommand() *Command {
	return &Command{Name: "reviewer remove", Usage: "reviewer remove <assignment-id> --repo owner/name [--json]", Short: "Remove a reviewer assignment; its row and history stay",
		Args: ArgSpec{Names: []string{"assignment-id"}, Min: 1},
		New: func(fs *flag.FlagSet) Handler {
			var o rightsFlags
			o.bindRepo(fs)
			return func(ctx context.Context, env *Env, args []string) error {
				id, err := parseID(args[0])
				if err != nil {
					return err
				}
				return withRights(ctx, env, &o, false, func(rc *rightsContext) error {
					a, err := rc.svc.Remove(ctx, rc.repo.ID, id)
					if err != nil {
						return rightsError(err)
					}
					if o.json {
						return writeJSON(env, a)
					}
					return writeAssignments(env, []store.ReviewerAssignment{a})
				})
			}
		}}
}

func newReviewerListCommand() *Command {
	return &Command{Name: "reviewer list", Usage: "reviewer list --repo owner/name [--task REF] [--all] [--json]", Short: "List reviewer assignments",
		Long: "Lists open assignments. With --all, removed assignments are listed too.",
		New: func(fs *flag.FlagSet) Handler {
			var o rightsFlags
			o.bindRepo(fs)
			fs.StringVar(&o.task, "task", "", "only this task")
			fs.BoolVar(&o.all, "all", false, "include removed assignments")
			return func(ctx context.Context, env *Env, _ []string) error {
				return withRights(ctx, env, &o, false, func(rc *rightsContext) error {
					rows, err := rc.svc.ListReviewers(ctx, rc.repo.ID, rc.taskRef(o.task), !o.all)
					if err != nil {
						return err
					}
					if o.json {
						return writeJSON(env, nonNil(rows))
					}
					return writeAssignments(env, rows)
				})
			}
		}}
}

type rightsContext struct {
	svc    *rights.Service
	repo   store.Repo
	lookup func(ctx context.Context, login string) (github.User, error)
}

// taskRef turns a bare issue number into the canonical task reference.
func (rc *rightsContext) taskRef(task string) string {
	task = strings.TrimPrefix(task, "#")
	if task != "" {
		if _, err := strconv.Atoi(task); err == nil {
			return fmt.Sprintf("%s:%s/%s#%s", rc.repo.Forge, rc.repo.Owner, rc.repo.Name, task)
		}
	}
	return task
}

// withRights opens the store, resolves the repository and the owner, and runs
// fn. needLookup says whether the command resolves a GitHub login.
func withRights(ctx context.Context, env *Env, o *rightsFlags, needLookup bool, fn func(*rightsContext) error) (err error) {
	owner, name, ok := strings.Cut(o.repo, "/")
	if !ok || owner == "" || name == "" {
		return Usagef(nil, "--repo takes owner/name, got %q", o.repo)
	}
	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()
	repo, err := rt.db.GetRepoBySlug(ctx, store.ForgeGitHub, owner, name)
	if errors.Is(err, store.ErrNotFound) {
		return Preconditionf("repository %s is not registered", o.repo)
	}
	if err != nil {
		return err
	}
	ids, err := turn.LoadIdentities(ctx, rt.db, store.ForgeGitHub)
	if err != nil {
		return Preconditionf("configured numeric owner identity required: %v", err)
	}
	resolve, err := daemon.RepositoryWorkflowSource(rt.paths, rt.db)
	if err != nil {
		return err
	}
	var source rights.Source = rights.NewRepositorySource(rights.Resolver(resolve))
	if env.RightsCatalogue != nil {
		source = env.RightsCatalogue
	}
	rc := &rightsContext{repo: repo, svc: &rights.Service{Store: rt.db, Catalogue: source, OwnerID: ids.OwnerUserID}}
	rc.lookup = env.LookupUser
	if needLookup && rc.lookup == nil {
		reference, configured := secrets.ReferenceFrom(rt.cfg.Providers.GitHub.TokenKeychain.Service, rt.cfg.Providers.GitHub.TokenKeychain.Account, rt.cfg.Providers.GitHub.TokenEnv)
		if !configured {
			return Preconditionf("configured bot credential required")
		}
		token, err := (secrets.Resolver{Keychain: env.Keychain}).Resolve(ctx, reference)
		if err != nil {
			return err
		}
		forge, err := github.New(github.Options{APIBase: rt.cfg.Providers.GitHub.APIBase, Token: token, Logger: env.Logger, UserAgent: initUserAgent(env)})
		if err != nil {
			return err
		}
		rc.lookup = forge.UserByLogin
	}
	return fn(rc)
}

func rightsError(err error) error {
	switch {
	case errors.Is(err, rights.ErrInvalid), errors.Is(err, rights.ErrNotInCatalogue), errors.Is(err, rights.ErrExpiryPast),
		errors.Is(err, rights.ErrDuplicate), errors.Is(err, rights.ErrNotFound), errors.Is(err, rights.ErrUnknownRepository):
		return Precondition(err)
	}
	return err
}

func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id < 1 {
		return 0, Usagef(nil, "expected a positive number, got %q", s)
	}
	return id, nil
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func writeJSON(env *Env, v any) error {
	enc := json.NewEncoder(env.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeGrants(env *Env, grants []store.RightsGrant) error {
	tw := tabwriter.NewWriter(env.Stdout, 0, 4, 2, ' ', 0)
	for _, g := range grants {
		state := "active"
		if !g.RevokedAt.IsZero() {
			state = "revoked " + store.FormatTime(g.RevokedAt)
		}
		expires, task := "-", "-"
		if !g.ExpiresAt.IsZero() {
			expires = store.FormatTime(g.ExpiresAt)
		}
		if g.TaskRef != "" {
			task = g.TaskRef
		}
		fmt.Fprintf(tw, "%d\t%s (id %d)\t%s | %s | %s\ttask %s\texpires %s\t%s\n", g.ID, g.SubjectLogin, g.SubjectID, g.Workflow, g.Stage, g.Action, task, expires, state)
	}
	return tw.Flush()
}

func writeAssignments(env *Env, rows []store.ReviewerAssignment) error {
	tw := tabwriter.NewWriter(env.Stdout, 0, 4, 2, ' ', 0)
	for _, a := range rows {
		state := "open"
		if !a.RemovedAt.IsZero() {
			state = "removed " + store.FormatTime(a.RemovedAt)
		}
		fmt.Fprintf(tw, "%d\t%s (id %d)\t%s | %s\ttask %s\t%s\n", a.ID, a.ReviewerLogin, a.ReviewerID, a.Workflow, a.Stage, a.TaskRef, state)
	}
	return tw.Flush()
}
