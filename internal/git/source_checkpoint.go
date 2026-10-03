package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// CheckpointSource saves dirty work locally only after the factory retained an
// intent containing original HEAD, content and immutable commit options. It
// reconciles a commit whose acknowledgment was lost before doing any mutation.
// It neither pushes nor claims the preserved content was validated.
func (w *Worktree) CheckpointSource(ctx context.Context, original SourceIdentity, options SourceOptions, commit CommitOptions) (CommitResult, error) {
	return w.saveSource(ctx, original, options, commit, true)
}

// SaveSource reconciles a local evidenced save without publication. The caller
// retains intent and verifies validation authority independently.
func (w *Worktree) SaveSource(ctx context.Context, original SourceIdentity, options SourceOptions, commit CommitOptions) (CommitResult, error) {
	return w.saveSource(ctx, original, options, commit, false)
}

func (w *Worktree) saveSource(ctx context.Context, original SourceIdentity, options SourceOptions, commit CommitOptions, checkpoint bool) (CommitResult, error) {
	if err := w.check(); err != nil {
		return CommitResult{}, err
	}
	if err := commit.validate(); err != nil {
		return CommitResult{}, err
	}
	if original.Head == "" || original.Digest == "" || (checkpoint && !strings.HasPrefix(commit.Summary, "Unvalidated recovery checkpoint ")) {
		return CommitResult{}, errors.New("checkpoint requires immutable unvalidated preservation binding")
	}
	current, err := w.ObserveSource(ctx, options)
	if err != nil {
		return CommitResult{}, err
	}
	if current.Digest != original.Digest {
		return CommitResult{}, errors.New("checkpoint content changed; preserve work and reconcile original intent")
	}
	if current.Head != original.Head {
		parent, e := w.runner.run(ctx, readTimeout, "rev-parse", "HEAD^")
		if e != nil {
			return CommitResult{}, e
		}
		message, e := w.runner.runRaw(ctx, readTimeout, "show", "-s", "--format=%B", "HEAD")
		if e != nil {
			return CommitResult{}, e
		}
		if parent != original.Head || strings.TrimRight(message, "\n") != strings.TrimRight(commitMessage(commit), "\n") {
			return CommitResult{}, errors.New("checkpoint HEAD has unknown parent or operation identity")
		}
		status, e := w.Status(ctx)
		if e != nil {
			return CommitResult{}, e
		}
		if len(status) != 0 {
			return CommitResult{}, errors.New("checkpoint acknowledgment lost with remaining dirty content")
		}
		return CommitResult{Head: current.Head, SHA: current.Head, Committed: true, Message: commitMessage(commit)}, nil
	}
	if err = w.checkCheckpointIndex(ctx, options); err != nil {
		return CommitResult{}, err
	}
	result, err := w.Commit(ctx, commit)
	if err != nil {
		return result, err
	}
	after, err := w.ObserveSource(ctx, options)
	if err != nil {
		return result, err
	}
	if after.Digest != original.Digest {
		return result, errors.New("checkpoint did not preserve original content")
	}
	if !result.Committed && checkpoint {
		return result, errors.New("checkpoint produced no preservation commit")
	}
	if !result.Committed {
		return result, nil
	}
	parent, err := w.runner.run(ctx, readTimeout, "rev-parse", "HEAD^")
	if err != nil {
		return result, err
	}
	if parent != original.Head {
		return result, errors.New("checkpoint parent does not match original HEAD")
	}
	return result, nil
}

func (w *Worktree) checkCheckpointIndex(ctx context.Context, options SourceOptions) error {
	reservedArgs := append(append([]string{}, statusArgs...), "--untracked-files=no", "--", ".litefactory-plan-proposal.json", ".litefactory-plan-outcome.json", ".litefactory-story-events")
	reserved, err := w.runner.run(ctx, readTimeout, reservedArgs...)
	if err != nil {
		return err
	}
	if reserved != "" {
		return errors.New("checkpoint refuses staged or tracked reserved transport changes")
	}
	unmerged, err := w.runner.runRawLimit(ctx, readTimeout, 1<<20, "ls-files", "--unmerged", "-z")
	if err != nil {
		return err
	}
	if unmerged != "" {
		return errors.New("unmerged index cannot be checkpointed safely")
	}
	staged, err := w.runner.runRawLimit(ctx, readTimeout, 1<<20, "diff", "--cached", "--name-only", "-z")
	if err != nil {
		return err
	}
	working, err := w.runner.runRawLimit(ctx, readTimeout, 1<<20, "diff", "--name-only", "-z")
	if err != nil {
		return err
	}
	changed := make(map[string]bool)
	for _, p := range strings.Split(working, "\x00") {
		if p != "" {
			changed[p] = true
		}
	}
	for _, p := range strings.Split(staged, "\x00") {
		if changed[p] {
			return fmt.Errorf("partially staged path %s cannot be preserved safely", p)
		}
	}
	files, err := w.Status(ctx)
	if err != nil {
		return err
	}
	for _, f := range files {
		if sourceExcluded(f.Path, options.NonInputs) && !sourceTransport(f.Path) {
			return fmt.Errorf("checkpoint would save excluded non-input %s", f.Path)
		}
	}
	return nil
}
