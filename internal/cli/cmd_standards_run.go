package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

func newStandardsRunCommand() *Command {
	c := &Command{Name: "standards run", Usage: "standards run <task> [--run N] [--repo owner/name] [--json]", Short: "Inspect a run's retained standards and retrieval status", Long: "Read the retained book binding, verify its contents afresh, and show every consumer's selection and retrieval gaps. Defaults to the newest run. Inspection cannot replace or repair a binding.", Args: ArgSpec{Names: []string{"task"}, Min: 1}}
	c.New = func(fs *flag.FlagSet) Handler {
		var ordinal int
		var repo string
		var asJSON bool
		fs.IntVar(&ordinal, "run", 0, "run ordinal (default newest)")
		fs.StringVar(&repo, "repo", "", "disambiguate with owner/name")
		fs.BoolVar(&asJSON, "json", false, "emit JSON")
		return func(ctx context.Context, env *Env, args []string) (err error) {
			issue, owner, name, err := parseTaskArg(c, args[0])
			if err != nil {
				return err
			}
			if owner == "" {
				owner, name, err = parseRepoFlag(c, repo)
				if err != nil {
					return err
				}
			} else if repo != "" {
				return Usagef(c, "a task ref already names its repository; drop --repo")
			}
			if ordinal < 0 {
				return Usagef(c, "--run must be zero or a positive run ordinal")
			}
			paths, err := config.Locate()
			if err != nil {
				return err
			}
			if _, err = os.Stat(paths.DBPath()); err != nil {
				return Preconditionf("initialized database required: %v", err)
			}
			db, err := store.Open(ctx, paths.DBPath())
			if err != nil {
				return err
			}
			defer func() { err = errors.Join(err, db.Close()) }()
			session, err := db.FindSessionByIssue(ctx, issue, owner, name)
			if err != nil {
				return lookupError(err, issue, firstNonEmpty(repo, joinRepo(owner, name)))
			}
			runs, err := db.ListRuns(ctx, session.ID)
			if err != nil {
				return err
			}
			run, err := selectRun(runs, ordinal, issue)
			if err != nil {
				return err
			}
			return inspectRunStandards(ctx, env, db, run.ID, asJSON)
		}
	}
	return c
}

func inspectRunStandards(ctx context.Context, env *Env, db *store.DB, runID int64, asJSON bool) error {
	root, err := db.ResolveStandardsRoot(ctx, runID)
	if err != nil {
		return err
	}
	view, err := (&standards.Runtime{Store: db, RootRunID: fmt.Sprint(root)}).Inspect(ctx)
	if err != nil {
		return err
	}
	if asJSON {
		return writeJSON(env, view)
	}
	return writeRunStandardsText(env.Stdout, view)
}

func writeRunStandardsText(out io.Writer, view standards.RunInspection) error {
	s := view.Status
	b := s.Binding
	if _, err := fmt.Fprintf(out, "Standards invocation %s\nRevision: %d\nSource: %s\nSchema: %d\nDigest: %s\nCaptured: %s (%s)\nFresh verification: %s at %s\nReason: %s\n", b.RootRunID, b.RevisionID, b.Source, b.SchemaVersion, b.Digest, logsTime(b.CapturedAt), b.Outcome, s.Integrity, logsTime(s.VerifiedAt), s.Reason); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Selection status: %s\nSelection diagnostic: %s\n", s.SelectionStatus, s.SelectionReason); err != nil {
		return err
	}
	for _, selection := range view.Selections {
		if _, err := fmt.Fprintf(out, "Consumer %s selection %d (%s)\n  Categories: %v\n  Standards: %v\n  Reason: %s\n", selection.ConsumerKey, selection.Version, selection.Digest, selection.CategoryIDs, selection.StandardIDs, selection.Reason); err != nil {
			return err
		}
	}
	for _, consumer := range view.Consumers {
		if _, err := fmt.Fprintf(out, "Consumer %s: selection=%s retrieval=%s\n  Selection diagnostic: %s\n  Retrieval gaps: %v\n", consumer.ConsumerKey, consumer.Status.SelectionStatus, consumer.Status.RetrievalStatus, consumer.Status.SelectionReason, consumer.RetrievalGaps); err != nil {
			return err
		}
	}
	for _, observation := range view.Observations {
		if _, err := fmt.Fprintf(out, "Observation %s consumer=%s attempt=%s selection=%d outcome=%s standard=%s range=%d:%d/%d reason=%s\n", observation.Kind, observation.ConsumerKey, observation.AttemptID, observation.SelectionVersion, observation.Outcome, observation.StandardID, observation.Start, observation.End, observation.Total, observation.Reason); err != nil {
			return err
		}
	}
	return nil
}
