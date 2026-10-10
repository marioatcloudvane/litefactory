package naming

import (
	"fmt"
	"strconv"
	"strings"
)

// ForgeGitHub is the forge identifier for github.com. It is the only forge M0
// supports; the value is stored on repos rows and appears in the task ref and
// in run log paths.
const ForgeGitHub = "github"

// Ref identifies one task: an issue on a repository on a forge. It is the unit
// litefactory locks, names and logs against.
//
// The canonical string form is forge:owner/name#issue, for example
// "github:acme/acme-api#123". That string is what lands in sessions.task_ref,
// in comment markers and in prompt headers.
type Ref struct {
	// Forge is the forge identifier, [ForgeGitHub] at M0.
	Forge string
	// Owner is the repository owner or organisation.
	Owner string
	// Repo is the repository name, without the owner.
	Repo string
	// Issue is the issue number. Always positive; never truncated or
	// abbreviated anywhere in this package.
	Issue int
}

// String renders the canonical task ref. It does not validate: call
// [Ref.Validate] first if the components came from outside the database.
func (r Ref) String() string {
	return fmt.Sprintf("%s:%s/%s#%d", r.Forge, r.Owner, r.Repo, r.Issue)
}

// Validate checks that every component is present, is usable as a single
// filesystem path segment, and that the issue number is positive.
//
// The path-segment rule is load-bearing rather than cosmetic: forge, owner and
// repo are interpolated into $LF_HOME/logs/runs/<forge>/<owner>/<repo>/<issue>,
// so an owner of ".." or "a/b" would write outside the run directory. Refusing
// separators and dot components here is what makes [RunLogPath] safe.
func (r Ref) Validate() error {
	if err := validateRefComponent("forge", r.Forge); err != nil {
		return err
	}
	if err := validateRefComponent("owner", r.Owner); err != nil {
		return err
	}
	if err := validateRefComponent("repository", r.Repo); err != nil {
		return err
	}
	return validateIssue(r.Issue)
}

// ParseRef parses the canonical forge:owner/name#issue form produced by
// [Ref.String] and validates the result. Only the exact canonical form is
// accepted — no whitespace, no "+7" or "007" issue numbers — so parsing and
// rendering round-trip.
func ParseRef(s string) (Ref, error) {
	malformed := func() (Ref, error) {
		return Ref{}, fmt.Errorf("%w: %q is not in forge:owner/name#issue form", ErrInvalidRef, s)
	}

	forge, rest, ok := strings.Cut(s, ":")
	if !ok {
		return malformed()
	}
	owner, rest, ok := strings.Cut(rest, "/")
	if !ok {
		return malformed()
	}
	repo, number, ok := strings.Cut(rest, "#")
	if !ok {
		return malformed()
	}

	issue, err := strconv.Atoi(number)
	if err != nil || strconv.Itoa(issue) != number {
		return Ref{}, fmt.Errorf("%w: %q has a non-canonical issue number %q", ErrInvalidRef, s, number)
	}

	ref := Ref{Forge: forge, Owner: owner, Repo: repo, Issue: issue}
	if err := ref.Validate(); err != nil {
		return Ref{}, err
	}
	return ref, nil
}

// validateRefComponent checks one path-segment-shaped component of a ref. The
// accepted set is GitHub's own owner and repository charset, which is already
// a subset of what a single path segment may safely contain.
func validateRefComponent(field, value string) error {
	fail := func(reason string) error {
		return fmt.Errorf("%w: %s %q %s", ErrInvalidRef, field, value, reason)
	}
	if value == "" {
		return fail("is empty")
	}
	if value == "." || value == ".." {
		return fail("is a relative path component")
	}
	for _, r := range value {
		if !isRefComponentRune(r) {
			return fail(fmt.Sprintf("contains %q, which is not allowed in a %s", r, field))
		}
	}
	return nil
}

// isRefComponentRune reports whether r may appear in a forge, owner or
// repository name. Note the absence of '/', '\\' and every control character.
func isRefComponentRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '.', r == '_', r == '-':
		return true
	default:
		return false
	}
}

// validateIssue rejects issue numbers a forge would never mint.
func validateIssue(issue int) error {
	if issue <= 0 {
		return fmt.Errorf("%w: issue number %d is not positive", ErrInvalidRef, issue)
	}
	return nil
}

// IssueURL returns the supported forge's task link. Unknown forges have no link.
func (r Ref) IssueURL() string {
	if r.Forge != ForgeGitHub {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/%s/issues/%d", r.Owner, r.Repo, r.Issue)
}
