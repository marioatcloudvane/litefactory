package turn

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/store"
)

// JudgeComment is a captured comment, identified by its provider-returned URL.
type JudgeComment struct{ ID, URL, Body string }

// The two context files the judge reads beside the repository checkout. Their
// names are also how the judge cites them in a report.
const (
	judgeResultFile     = "result.md"
	judgeDiscussionFile = "discussion.md"
)

// judgeContext is pinned at admission and retained in the request's evidence
// column: the redacted saved result and discussion, and the repository commit
// the judge reads. Requests admitted before it carried a code snapshot instead;
// their unknown fields are ignored and only cutoff and commit are reused.
type judgeContext struct {
	Cutoff        time.Time      `json:"cutoff"`
	Repository    string         `json:"repository,omitempty"`
	Commit        string         `json:"commit,omitempty"`
	RepositoryGap string         `json:"repository_gap,omitempty"`
	Source        string         `json:"source,omitempty"`
	Comments      []JudgeComment `json:"comments,omitempty"`
}

// pinJudgeContext redacts the source and discussion and records the commit at
// the task's repository HEAD. A missing repository is recorded, not fatal: the
// judge then reviews the result and discussion and says what it could not check.
func (s *JudgeService) pinJudgeContext(ctx context.Context, in JudgeRequest, source store.JudgeSource) (judgeContext, error) {
	pinned := judgeContext{Cutoff: in.Cutoff}
	var err error
	if pinned.Source, err = s.redactJudgeText(ctx, judgeSourceText(source)); err != nil {
		return pinned, err
	}
	for _, c := range in.Comments {
		body, err := s.redactJudgeText(ctx, c.Body)
		if err != nil {
			return pinned, err
		}
		pinned.Comments = append(pinned.Comments, JudgeComment{ID: c.ID, URL: c.URL, Body: body})
	}
	switch {
	case s.Git == nil || in.Root == "":
		pinned.RepositoryGap = "No repository checkout is known for this task."
	default:
		clone, openErr := s.Git.Open(ctx, in.Root)
		commit := ""
		if openErr == nil {
			commit, openErr = clone.RevParse(ctx, "HEAD", git.RevParseOptions{})
		}
		if openErr != nil {
			if err := ctx.Err(); err != nil {
				return pinned, err
			}
			s.logJudge(ctx, "judge repository commit unavailable", openErr)
			pinned.RepositoryGap = "The task repository's commit could not be read."
			break
		}
		pinned.Repository, pinned.Commit = clone.Path(), commit
	}
	return pinned, nil
}

func (s *JudgeService) redactJudgeText(ctx context.Context, text string) (string, error) {
	body, err := s.Redactor.Apply(ctx, text, len(text)+4096)
	if err != nil {
		return "", err
	}
	if body.Truncated() {
		return "", errors.New("judge context redaction exceeded bound")
	}
	return body.String(), nil
}

func (s *JudgeService) logJudge(ctx context.Context, message string, err error) {
	if s.Logger != nil {
		s.Logger.WarnContext(ctx, message, "err", err)
	}
}

// judgeSourceText renders the saved result (and a Plan's context) readably.
func judgeSourceText(source store.JudgeSource) string {
	var b strings.Builder
	b.WriteString(judgeIndentJSON(source.Snapshot))
	if len(source.PlanContext) > 0 {
		b.WriteString("\n\n## Plan context and constituent revisions\n\n")
		b.WriteString(judgeIndentJSON(source.PlanContext))
	}
	if source.PlanContextGap != "" {
		b.WriteString("\n\nPlan context gap: " + source.PlanContextGap)
	}
	return b.String()
}

func judgeIndentJSON(raw []byte) string {
	var out bytes.Buffer
	if json.Indent(&out, raw, "", "  ") == nil {
		return out.String()
	}
	return string(raw)
}

// judgeWorkspace is one review's private directory: a detached checkout of the
// pinned commit (the judge's working directory) and a sibling context
// directory with the saved result and discussion. Both are removed afterwards.
type judgeWorkspace struct {
	root, repo, context string
	commit, gap         string
	clone               *git.Clone
}

func (s *JudgeService) openJudgeWorkspace(ctx context.Context, q store.JudgeRequest, pinned judgeContext) (_ *judgeWorkspace, returnedErr error) {
	root, err := os.MkdirTemp("", "lf-judge-")
	if err != nil {
		return nil, err
	}
	w := &judgeWorkspace{root: root, gap: pinned.RepositoryGap}
	defer func() {
		if returnedErr != nil {
			returnedErr = errors.Join(returnedErr, w.close(context.WithoutCancel(ctx)))
		}
	}()
	// Providers match paths after resolving symlinks (macOS /var is /private/var).
	if w.root, err = filepath.EvalSymlinks(root); err != nil {
		w.root = root
		return nil, err
	}
	w.repo, w.context = filepath.Join(w.root, "repo"), filepath.Join(w.root, "context")
	if err = os.Mkdir(w.context, 0o700); err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(w.context, judgeResultFile), []byte(judgeResultMarkdown(q, pinned)), 0o600); err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(w.context, judgeDiscussionFile), []byte(judgeDiscussionMarkdown(pinned)), 0o600); err != nil {
		return nil, err
	}
	if pinned.Commit != "" && s.Git != nil {
		clone, checkoutErr := s.Git.Open(ctx, pinned.Repository)
		if checkoutErr == nil {
			_, checkoutErr = clone.WorktreeAdd(ctx, w.repo, git.WorktreeAddOptions{Branch: pinned.Commit, Detach: true})
		}
		if checkoutErr == nil {
			w.clone, w.commit = clone, pinned.Commit
		} else {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			s.logJudge(ctx, "judge repository checkout unavailable", checkoutErr)
			w.gap = "The repository could not be checked out at commit " + pinned.Commit + "."
		}
	}
	if w.clone == nil {
		if err = os.MkdirAll(w.repo, 0o700); err != nil {
			return nil, err
		}
	}
	return w, nil
}

// close removes the checkout's worktree registration and every file.
func (w *judgeWorkspace) close(ctx context.Context) error {
	var removeErr error
	if w.clone != nil {
		removeErr = w.clone.WorktreeRemove(ctx, w.repo, git.WorktreeRemoveOptions{Force: true})
	}
	return errors.Join(removeErr, os.RemoveAll(w.root))
}

func judgeResultMarkdown(q store.JudgeRequest, pinned judgeContext) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Pinned result: %s\n\nReference: %s\n\n", judgeOneLine(judgeSourceName(q.Source)), judgeOneLine(q.Source.Reference))
	b.WriteString(pinned.Source)
	b.WriteString("\n")
	return b.String()
}

func judgeDiscussionMarkdown(pinned judgeContext) string {
	var b strings.Builder
	b.WriteString("# Discussion\n\n")
	if !pinned.Cutoff.IsZero() {
		fmt.Fprintf(&b, "Issue comments up to %s, oldest first.\n", pinned.Cutoff.UTC().Format(time.RFC3339))
	} else {
		b.WriteString("Issue comments, oldest first.\n")
	}
	if len(pinned.Comments) == 0 {
		b.WriteString("\nNo comments were captured.\n")
	}
	for _, c := range pinned.Comments {
		fmt.Fprintf(&b, "\n---\n\n## Comment %s", judgeOneLine(c.ID))
		if c.URL != "" {
			fmt.Fprintf(&b, " (%s)", judgeOneLine(c.URL))
		}
		b.WriteString("\n\n")
		b.WriteString(c.Body)
		b.WriteString("\n")
	}
	return b.String()
}

// judgePrompt is the role followed by a short labelled block naming what is
// reviewed and where to read it. The role digest stays in the stored record.
func judgePrompt(role string, q store.JudgeRequest, focus string, w *judgeWorkspace) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(role, "\n"))
	b.WriteString("\n\n# This review\n\n")
	fmt.Fprintf(&b, "- Reviewing: %s (%s)\n", judgeOneLine(judgeSourceName(q.Source)), judgeOneLine(q.Source.Reference))
	if focus = judgeOneLine(focus); focus != "" {
		fmt.Fprintf(&b, "- Owner's focus: %s\n", focus)
	} else {
		b.WriteString("- Owner's focus: none given\n")
	}
	if w.commit != "" {
		fmt.Fprintf(&b, "- Repository: %s (your working directory), checked out read-only at commit %s\n", w.repo, w.commit)
	} else {
		fmt.Fprintf(&b, "- Repository: not available. %s Your working directory %s is empty.\n", w.gap, w.repo)
	}
	fmt.Fprintf(&b, "- Pinned result: %s\n", filepath.Join(w.context, judgeResultFile))
	fmt.Fprintf(&b, "- Discussion: %s\n", filepath.Join(w.context, judgeDiscussionFile))
	return b.String()
}
