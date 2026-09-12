package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

// taskCleanOptions holds the parsed flags of "litefactory task clean".
type taskCleanOptions struct {
	// force removes the worktree even when it has uncommitted changes.
	force bool
}

// bind registers the task clean flags on fs.
func (o *taskCleanOptions) bind(fs *flag.FlagSet) {
	fs.BoolVar(&o.force, "force", false, "remove the worktree even with uncommitted changes")
}

// newTaskCleanCommand declares "litefactory task clean <issue> [--force]".
func newTaskCleanCommand() *Command {
	cmd := &Command{
		Name:  "task clean",
		Usage: "task clean <issue> [--force]",
		Short: "Remove the worktree for an issue",
		Long: "Remove the git worktree created for <issue> and clear its recorded path.\n" +
			"Refuses to touch a worktree with uncommitted changes unless --force is given.\n\n" +
			"The daemon checks closed issues daily. worktree_retain_days counts from the\n" +
			"last completed run, or creation for a certainly owned task with no runs.\n" +
			"Use task retire to preview conservative automatic cleanup and its skips.",
		Args: ArgSpec{Names: []string{"issue"}, Min: 1},
	}
	cmd.New = func(fs *flag.FlagSet) Handler {
		var opts taskCleanOptions
		opts.bind(fs)
		return func(ctx context.Context, env *Env, args []string) error {
			return runTaskClean(ctx, env, cmd, opts, args[0])
		}
	}
	return cmd
}

// runTaskClean removes an issue's worktree and clears its recorded path
// (AC-3.25).
//
// The refusal is the point of the command. `git worktree remove` without
// --force is what decides whether there is uncommitted work, so the check is
// git's own rather than a guess made here, and a refusal leaves the directory
// exactly as it was — no partial removal, no cleared column, nothing to undo.
func runTaskClean(ctx context.Context, env *Env, cmd *Command, opts taskCleanOptions, issueArg string) (err error) {
	issue, owner, name, err := parseTaskArg(cmd, issueArg)
	if err != nil {
		return err
	}

	rt, err := openRepoRuntime(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rt.close()) }()

	session, err := rt.db.FindSessionByIssue(ctx, issue, owner, name)
	if err != nil {
		return taskLookupError(err, issue)
	}
	if session.WorktreePath == "" {
		fmt.Fprintf(env.Stdout, "%s has no worktree recorded; nothing to remove\n", session.TaskRef)
		writeRetentionNote(env.Stdout, rt.cfg.Settings.WorktreeRetainDays)
		return nil
	}
	repo, err := rt.db.GetRepo(ctx, session.RepoID)
	if err != nil {
		return err
	}

	if err := removeWorktree(ctx, env, repo, session, opts.force); err != nil {
		return err
	}
	if err := rt.db.SetSessionWorktree(ctx, session.ID, ""); err != nil {
		return err
	}
	writeRetentionNote(env.Stdout, rt.cfg.Settings.WorktreeRetainDays)
	return nil
}

// removeWorktree runs the removal itself, or explains why it did not.
//
// A directory that is already gone is not a failure: the recorded path is
// cleared and the command says what it found. That is the difference between a
// command a user can re-run after deleting a worktree by hand and one that
// leaves a dangling column they cannot clear.
func removeWorktree(
	ctx context.Context,
	env *Env,
	repo store.Repo,
	session store.Session,
	force bool,
) error {
	if _, statErr := os.Stat(session.WorktreePath); errors.Is(statErr, fs.ErrNotExist) {
		fmt.Fprintf(env.Stdout, "%s: %s is already gone; clearing the recorded path\n",
			session.TaskRef, session.WorktreePath)
		return nil
	}

	binary, err := git.Resolve(ctx)
	if err != nil {
		return Precondition(err)
	}
	clone, err := binary.Open(ctx, repo.Path)
	if err != nil {
		return Precondition(err)
	}

	err = clone.WorktreeRemove(ctx, session.WorktreePath, git.WorktreeRemoveOptions{Force: force})
	var dirty *git.WorktreeDirtyError
	switch {
	case errors.As(err, &dirty):
		return dirtyWorktreeError(dirty, session)
	case err != nil:
		return err
	}

	fmt.Fprintf(env.Stdout, "%s: removed the worktree at %s\n", session.TaskRef, session.WorktreePath)
	if session.Branch != "" {
		fmt.Fprintf(env.Stdout, "  branch %s is untouched; the work is still on it\n", session.Branch)
	}
	return nil
}

// dirtyWorktreeError renders the refusal, listing what is in the way.
//
// The list is the whole value of the message: "it has uncommitted changes" sends
// the user to go and look, and the point of a refusal is that they should be
// able to decide without looking.
func dirtyWorktreeError(dirty *git.WorktreeDirtyError, session store.Session) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s has uncommitted changes; nothing was removed\n",
		session.TaskRef, dirty.Path)
	for _, change := range dirty.Changes {
		fmt.Fprintf(&b, "  %s\n", change)
	}
	fmt.Fprint(&b, "re-run with --force to remove it anyway")
	return errors.New(b.String())
}

// writeRetentionNote explains the active automatic policy alongside manual cleanup.
func writeRetentionNote(out io.Writer, retainDays int) {
	fmt.Fprintf(out, "worktree_retain_days is %d, days after the last completed run (creation for certainly owned tasks with no runs); the daemon checks closed issues daily; preview with task retire\n",
		retainDays)
}

// taskLookupError turns a failed issue lookup into something a person can act
// on. `task clean` takes no --repo flag, so an ambiguous issue number is a
// refusal that names the candidates rather than an invitation to disambiguate.
func taskLookupError(err error, issue int) error {
	var ambiguous *store.AmbiguousIssueError
	if errors.As(err, &ambiguous) {
		var b strings.Builder
		fmt.Fprintf(&b, "issue %d exists in more than one registered repository:\n", issue)
		for _, candidate := range ambiguous.Candidates {
			fmt.Fprintf(&b, "  %s\n", candidate.TaskRef)
		}
		fmt.Fprint(&b, "remove the worktree with `git worktree remove` in the clone you mean")
		return errors.New(b.String())
	}
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("no task for issue %d: litefactory has never run it", issue)
	}
	return err
}
