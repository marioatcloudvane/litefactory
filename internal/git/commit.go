package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// The provenance trailers litefactory writes on every commit it makes.
//
// They are exported because they are a contract: this package writes them, the
// report reads them, and a human greps for them. TrailerPrefix is the whole
// namespace and is what [sanitizeSummary] strips out of the untrusted summary.
const (
	// TrailerPrefix is the namespace litefactory reserves in a commit message.
	// No line of a summary may start with it.
	TrailerPrefix = "Litefactory-"
	// TrailerTask carries the canonical task ref, "github:owner/name#123".
	TrailerTask = TrailerPrefix + "Task"
	// TrailerMode carries the instruction that triggered the run, "spike".
	TrailerMode = TrailerPrefix + "Mode"
	// TrailerRun carries the per-task run ordinal.
	TrailerRun = TrailerPrefix + "Run"
	// TrailerConflictProposal carries the id of the confirmed conflict-
	// resolution proposal a merge commit applied (US-12). It is what lets a
	// reader connect a commit back to the suggestion an owner actually
	// confirmed, without trusting anything the commit message's own prose
	// says about it.
	TrailerConflictProposal = TrailerPrefix + "Conflict-Proposal"
	// TrailerConfirmedBy carries the forge handle of the human who confirmed
	// the application (US-12, S-16). It is never a "Co-authored-by:"
	// trailer: that would attribute authorship to whoever confirmed, when
	// the confirming act was approval, not authorship, and the commit's
	// actual Author is already the identity [Worktree.CompleteMerge] commits
	// as.
	TrailerConfirmedBy = TrailerPrefix + "Confirmed-By"
	// TrailerModel carries the coding-agent-tool name whose suggestion was
	// confirmed and applied (US-12, S-16). It is litefactory-controlled data
	// — an [agent.Kind] rendered as a string — never a value the model's own
	// output could have influenced.
	TrailerModel = TrailerPrefix + "Model"
)

const (
	// MaxCommitSummaryBytes caps the untrusted summary. The message travels as
	// an argv element, so an unbounded summary is an E2BIG exec failure at the
	// worst possible moment — after the work is done and before it is saved.
	// The cap matches the 8 KB caps the prompt uses for untrusted material.
	MaxCommitSummaryBytes = 8 << 10

	// truncationMarker is appended when the cap fires, so a reader can tell a
	// short summary from a cut-off one.
	truncationMarker = "\n[truncated]"

	// fallbackSummary is the subject when nothing survives sanitisation — a
	// summary that was empty, whitespace, or entirely forged trailers.
	fallbackSummary = "litefactory run"
)

// Note: there is deliberately no "nothing to commit" sentinel. A run that
// changed nothing is a normal outcome (AC-3.23), reported by
// [CommitResult.Committed] being false; an error would push every caller into
// treating a successful no-op run as a failure.

// Author is the identity a commit is made under.
//
// litefactory commits as the human who registered the repository, not as a bot:
// branch protection rules, CODEOWNERS and CI all key off the author, and a
// commit from a synthetic identity behaves differently from one the user made
// by hand. Both fields are required rather than inherited from the ambient git
// configuration — the daemon's git child gets a deliberately minimal
// environment (SD-5), and "who am I" is not something to leave to whatever
// launchd happened to provide.
type Author struct {
	// Name is the value for user.name.
	Name string
	// Email is the value for user.email.
	Email string
}

// CommitOptions configures [Worktree.Commit].
type CommitOptions struct {
	// Summary is the human-readable description of the run.
	//
	// It is produced by Claude and is therefore untrusted. It is placed above
	// the trailers and every line matching the Litefactory- namespace is
	// removed from it before the trailers are appended, so a run cannot write
	// its own provenance metadata.
	Summary string
	// Task is the canonical task ref for the Litefactory-Task trailer, as
	// produced by naming.Ref.String(). Required.
	Task string
	// Mode is the instruction that triggered the run, for the
	// Litefactory-Mode trailer. Required.
	Mode string
	// Run is the per-task run ordinal for the Litefactory-Run trailer. Must be
	// positive.
	Run int
	// Author is the identity to commit as. Required.
	Author Author
}

// CommitResult reports what [Worktree.Commit] did.
type CommitResult struct {
	// Committed is false when the run produced no changes. That is the
	// AC-3.23 path: no commit, nothing to push, and a report that says so.
	Committed bool
	// SHA is the commit this call made. It is the run's commit_sha, and is
	// empty when Committed is false.
	SHA string
	// Head is where the branch points now, set whether or not anything was
	// committed. It is the run's sha_after (AC-3.24).
	//
	// It is a separate field from SHA because the two genuinely differ. claude
	// runs with the Bash tool and may commit on its own, leaving a clean
	// worktree and a moved branch: Committed is then false, SHA is empty, and
	// Head is nevertheless ahead of the sha_before the caller recorded before
	// the subprocess started. A caller that decided whether to push from SHA
	// alone would strand that work unpushed on a local branch, which is the
	// AC-3.24 failure this field exists to prevent. Push when Head differs from
	// sha_before, not when Committed is true.
	//
	// The matching sha_before is deliberately not a field here: it belongs to
	// the moment before the subprocess started, not to the moment of the
	// commit, and it is read with Worktree.RevParse(ctx, "HEAD", ...) there.
	Head string
	// Message is the commit message exactly as written, sanitised summary and
	// trailers included, for the run's metadata document.
	Message string
	// Files are the changes that were committed — or, when Committed is
	// false, the changes that were found and could not be staged.
	Files []FileChange
}

// Commit stages every change in the worktree and commits it on the task branch.
//
// Staging is "git add --all", so it picks up modifications, deletions and
// untracked files and skips ignored ones — the same set [Worktree.Status]
// reports. A worktree with nothing to commit is not an error (AC-3.23): the
// result comes back with Committed false and no commit is made. Either way the
// result carries Head, so a caller can still see that the branch moved — a run
// that committed for itself leaves a clean worktree and work worth pushing.
//
// The message is the sanitised summary, a blank line, and the three
// Litefactory- trailers. Signing is switched off for the duration: a signature
// needs an interactive passphrase, the git child is deliberately non-interactive
// (GIT_TERMINAL_PROMPT=0, ssh in BatchMode), and a user with commit.gpgsign set
// globally would otherwise get a hung or failing commit on every run.
func (w *Worktree) Commit(ctx context.Context, opts CommitOptions) (CommitResult, error) {
	if err := w.check(); err != nil {
		return CommitResult{}, err
	}
	if err := opts.validate(); err != nil {
		return CommitResult{}, err
	}

	// Factory transport must be unstaged even when it is the only change.
	if _, err := w.runner.run(ctx, commitTimeout, "reset", "--quiet", "HEAD", "--", ".litefactory-plan-proposal.json", ".litefactory-story-events"); err != nil {
		return CommitResult{}, fmt.Errorf("exclude factory transport: %w", err)
	}
	files, err := w.Status(ctx)
	if err != nil {
		return CommitResult{}, fmt.Errorf("read worktree status before commit: %w", err)
	}

	var message string
	if len(files) > 0 {
		if _, err := w.runner.run(ctx, commitTimeout, "add", "--all", "--", "."); err != nil {
			return CommitResult{}, fmt.Errorf("stage changes in %s: %w", w.path, err)
		}
		// Transport is excluded even if the agent staged it before returning.
		if _, err := w.runner.run(ctx, commitTimeout, "reset", "--quiet", "HEAD", "--", ".litefactory-plan-proposal.json", ".litefactory-story-events"); err != nil {
			return CommitResult{}, fmt.Errorf("exclude plan transport: %w", err)
		}
		staged, err := w.hasStagedChanges(ctx)
		if err != nil {
			return CommitResult{}, err
		}
		// A status entry git will not stage is rare and still not an error:
		// there is nothing to commit, so say so rather than let "git commit"
		// fail on an empty index.
		if staged {
			message = commitMessage(opts)
			args := []string{
				"-c", "user.name=" + opts.Author.Name,
				"-c", "user.email=" + opts.Author.Email,
				"-c", "commit.gpgsign=false",
				"commit", "--quiet", "--message", message,
			}
			if _, err := w.runner.run(ctx, commitTimeout, args...); err != nil {
				return CommitResult{}, fmt.Errorf("commit on branch %s: %w", w.branch, err)
			}
		}
	}

	// HEAD is read on every path, including the one that committed nothing:
	// this is sha_after, and a run that committed for itself leaves the branch
	// ahead even though this call did not move it.
	head, err := revParse(ctx, w.runner, "HEAD", RevParseOptions{})
	if err != nil {
		return CommitResult{}, fmt.Errorf("resolve HEAD on branch %s: %w", w.branch, err)
	}
	if message == "" {
		return CommitResult{Head: head, Files: files}, nil
	}
	return CommitResult{Committed: true, SHA: head, Head: head, Message: message, Files: files}, nil
}

// hasStagedChanges reports whether the index differs from HEAD. "git diff
// --cached --quiet" answers with its exit status — 0 for no difference, 1 for a
// difference — which is the authoritative answer, where counting status entries
// is only a good guess.
func (w *Worktree) hasStagedChanges(ctx context.Context) (bool, error) {
	_, err := w.runner.run(ctx, readTimeout, "diff", "--cached", "--quiet")
	if err == nil {
		return false, nil
	}
	var cmdErr *CommandError
	if errors.As(err, &cmdErr) && !cmdErr.TimedOut && cmdErr.ExitCode == 1 {
		return true, nil
	}
	return false, fmt.Errorf("inspect the staged changes in %s: %w", w.path, err)
}

// validate checks the options that reach git or the commit message.
func (o CommitOptions) validate() error {
	if err := checkTrailerValue(TrailerTask, o.Task); err != nil {
		return err
	}
	if err := checkTrailerValue(TrailerMode, o.Mode); err != nil {
		return err
	}
	if o.Run < 1 {
		return fmt.Errorf("run ordinal %d: %w: must be positive", o.Run, ErrInvalidArgument)
	}
	if err := checkConfigValue("author name", o.Author.Name); err != nil {
		return err
	}
	return checkConfigValue("author email", o.Author.Email)
}

// checkTrailerValue refuses a trailer value that would break out of its line.
// A newline inside a value is how a caller would otherwise inject a trailer of
// its own — the same class of problem as the forged summary, one layer down.
func checkTrailerValue(key, value string) error {
	if value == "" {
		return fmt.Errorf("%s trailer: %w: must not be empty", key, ErrInvalidArgument)
	}
	if strings.ContainsAny(value, "\x00\n\r") {
		return fmt.Errorf("%s trailer %q: %w: must not contain control characters", key, value, ErrInvalidArgument)
	}
	return nil
}

// checkConfigValue refuses a value that could not be passed as "-c key=value".
func checkConfigValue(what, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s: %w: must not be empty", what, ErrInvalidArgument)
	}
	if strings.ContainsAny(value, "\x00\n\r") {
		return fmt.Errorf("%s %q: %w: must not contain control characters", what, value, ErrInvalidArgument)
	}
	return nil
}

// commitMessage assembles the message: sanitised summary, blank line, trailers.
func commitMessage(opts CommitOptions) string {
	var b strings.Builder
	b.WriteString(sanitizeSummary(opts.Summary))
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "%s: %s\n", TrailerTask, opts.Task)
	fmt.Fprintf(&b, "%s: %s\n", TrailerMode, opts.Mode)
	fmt.Fprintf(&b, "%s: %d\n", TrailerRun, opts.Run)
	return b.String()
}

// sanitizeSummary makes an untrusted, model-produced summary safe to put above
// the trailers.
//
// The summary is the one part of the commit message litefactory did not write,
// and the trailers are what a later reader — a report, a person, a script —
// trusts to say which task and which run produced the commit. A summary
// containing its own "Litefactory-Task:" line would forge exactly that, so
// every line in the reserved namespace is removed.
//
// The match is deliberately wider than the "^Litefactory-" of AC-3.21: leading
// horizontal whitespace is trimmed first and the comparison is
// case-insensitive, because a reader that parses trailers loosely — as git's
// own interpret-trailers does — would be fooled by " litefactory-task:" while a
// strict prefix test would let it through. Legitimate prose does not begin a
// line this way; a forgery attempt does.
func sanitizeSummary(summary string) string {
	normalized := strings.ReplaceAll(summary, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	normalized = strings.ReplaceAll(normalized, "\x00", "")

	lines := strings.Split(normalized, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if isReservedTrailerLine(line) {
			continue
		}
		kept = append(kept, line)
	}

	cleaned := strings.TrimSpace(strings.Join(kept, "\n"))
	if len(cleaned) > MaxCommitSummaryBytes {
		// Truncating after the strip is safe in a way the reverse would not
		// be: removing trailing bytes cannot create a new line, so no forged
		// trailer can appear between the two steps.
		cleaned = truncateSummary(cleaned, MaxCommitSummaryBytes-len(truncationMarker)) + truncationMarker
	}
	if cleaned == "" {
		return fallbackSummary
	}
	return cleaned
}

// isReservedTrailerLine reports whether line claims litefactory's namespace.
func isReservedTrailerLine(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	if len(trimmed) < len(TrailerPrefix) {
		return false
	}
	return strings.EqualFold(trimmed[:len(TrailerPrefix)], TrailerPrefix)
}

// truncateSummary cuts s to at most limit bytes, preferring the last line
// boundary inside the budget and never leaving a half-written rune behind.
func truncateSummary(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(s) <= limit {
		return s
	}
	cut := s[:limit]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		return strings.TrimRight(cut[:i], "\n")
	}
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut
}
