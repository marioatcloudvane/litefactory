package naming

import (
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
)

const (
	// AITeamDirName is the directory at the repository root that every aiteam
	// session artefact lives under: the ".aiteam" of
	// .aiteam/<branch>/<date>/<feature>/.
	AITeamDirName = ".aiteam"

	// ResearchBriefName is the file Research mode writes into a session
	// directory, and the artefact a run report links to.
	ResearchBriefName = "RESEARCH_BRIEF.md"

	// ResearchTechnicalName holds the evidence behind the research brief.
	ResearchTechnicalName = "RESEARCH_TECHNICAL.md"

	// ManifestName is the session-lifecycle file every mode writes into a
	// session directory first, before either brief exists: a spike or a
	// greenfield run has one when it has nothing else. It is tried before
	// [ResearchBriefName] and planbrief.FileName when locating a task's
	// session directory, so a session that never ran Research or Plan is
	// still found.
	ManifestName = "MANIFEST.md"

	// SessionDateLayout is the date component of a session directory. It is
	// YYYY-MM-DD, which is what CLAUDE.md's `date +%Y-%m-%d` produces.
	SessionDateLayout = "2006-01-02"
)

// AITeamSessionDir returns the repository-relative directory one aiteam session
// writes its artefacts to:
//
//	.aiteam/<branch, slashes to hyphens, lowercased>/<YYYY-MM-DD>/<feature slug>
//
// It is the Go form of the three shell lines CLAUDE.md derives the same path
// with, and it exists so that a report can name a session artefact without
// looking at a filesystem or at a run's list of touched files. Deriving it is
// the whole point: the brief is worth linking to on every run of the task, not
// only on the run that happened to rewrite it.
//
// Separators are forward slashes, not [path/filepath] ones, because the result
// is a git path — it is interpolated into a forge URL and compared against what
// git reports, both of which use "/" on every platform.
//
// day is the day the session directory was named on, in the location it was
// named in: `date +%Y-%m-%d` reads the machine's local clock, so a caller
// passing a UTC time on a machine east of Greenwich derives yesterday's
// directory. Pass the run's own start time, which is local.
//
// The derivation cannot confirm that the directory exists — this package does
// no I/O — so a caller rendering the path to a human should say so. What it
// does guarantee is that the path is a well-formed relative one: every
// component is validated as a single path segment, so no input can produce a
// path that climbs out of .aiteam/.
func AITeamSessionDir(branch string, day time.Time, slug string) (string, error) {
	dir, err := AITeamBranchDir(branch)
	if err != nil {
		return "", fmt.Errorf("aiteam session directory for branch %q: %w", branch, err)
	}
	if day.IsZero() {
		return "", fmt.Errorf("%w: aiteam session directory for branch %q has no date", ErrInvalidPath, branch)
	}
	if err := ValidateSlug(slug); err != nil {
		return "", fmt.Errorf("aiteam session directory for branch %q: %w", branch, err)
	}
	return path.Join(dir, day.Format(SessionDateLayout), slug), nil
}

// AITeamBranchDir returns the repository-relative directory every aiteam
// session on one branch shares: .aiteam/<branch, slashes to hyphens,
// lowercased> — the branch half of [AITeamSessionDir], without a date or a
// feature slug.
//
// It exists for a caller that has to enumerate a branch's sessions because it
// does not yet know which day one was named on — [AITeamSessionDir] itself
// cannot help there, since it requires the day up front. Exporting this is
// what lets that caller list `.aiteam/<branch>/` for its date directories
// without re-deriving the `tr '/' '-' | tr [:upper:] [:lower:]` transform a
// second time; [branchDirName] still does the transform, once, for both
// functions.
func AITeamBranchDir(branch string) (string, error) {
	dir := branchDirName(branch)
	if err := validatePathSegment("branch directory", dir); err != nil {
		return "", fmt.Errorf("aiteam branch directory for branch %q: %w", branch, err)
	}
	return path.Join(AITeamDirName, dir), nil
}

// ResearchBriefPath returns the repository-relative path of the research brief
// for one aiteam session: [AITeamSessionDir] plus [ResearchBriefName].
//
// A session that has not run Research has no such file. This is a derivation,
// not a discovery, and it says where the brief would be — see
// [AITeamSessionDir].
func ResearchBriefPath(branch string, day time.Time, slug string) (string, error) {
	dir, err := AITeamSessionDir(branch, day, slug)
	if err != nil {
		return "", err
	}
	return path.Join(dir, ResearchBriefName), nil
}

// SlugFromBranch recovers the feature slug frozen into a task branch. It is the
// inverse of [Branch]: given "lf/123-fix-the-thing" and issue 123 it returns
// "fix-the-thing".
//
// It exists so that a caller holding a branch name — which every report does,
// because the branch is a fact about the run — does not have to also carry the
// slug alongside it, and cannot carry one that disagrees with the branch.
// A feature prefix may itself contain slashes ("bots/lf/123-x"), so the split
// is at the last one.
//
// The recovered slug is put through [ValidateSlug], so a branch that was not
// built by [Branch] for this issue is an error rather than a plausible-looking
// wrong answer.
func SlugFromBranch(branch string, issue int) (string, error) {
	if err := validateIssue(issue); err != nil {
		return "", err
	}
	fail := func(reason string) (string, error) {
		return "", fmt.Errorf("%w: %q %s", ErrInvalidBranch, branch, reason)
	}

	i := strings.LastIndexByte(branch, '/')
	if i < 0 {
		return fail("has no feature prefix, so it was not built by Branch")
	}
	want := strconv.Itoa(issue) + "-"
	last := branch[i+1:]
	if !strings.HasPrefix(last, want) {
		return fail(fmt.Sprintf("does not name issue %d", issue))
	}

	slug := last[len(want):]
	if err := ValidateSlug(slug); err != nil {
		return "", fmt.Errorf("slug of branch %q: %w", branch, err)
	}
	return slug, nil
}

// branchDirName is CLAUDE.md's `tr '/' '-' | tr '[:upper:]' '[:lower:]'`, which
// is what turns a branch name into one path component.
func branchDirName(branch string) string {
	return strings.ToLower(strings.ReplaceAll(branch, "/", "-"))
}

// validatePathSegment checks that value can stand as one component of a
// relative path. It is the same rule [validateRefComponent] applies, reported
// against [ErrInvalidPath] rather than [ErrInvalidRef] because the value here
// is a derived directory name and not part of a task ref.
func validatePathSegment(field, value string) error {
	fail := func(reason string) error {
		return fmt.Errorf("%w: %s %q %s", ErrInvalidPath, field, value, reason)
	}
	if value == "" {
		return fail("is empty")
	}
	if value == "." || value == ".." {
		return fail("is a relative path component")
	}
	for _, r := range value {
		if !isRefComponentRune(r) {
			return fail(fmt.Sprintf("contains %q, which is not allowed in a path segment", r))
		}
	}
	return nil
}
