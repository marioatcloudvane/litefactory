package github

import (
	"fmt"
	"strconv"

	"github.com/cloudvane/litefactory/internal/naming"
)

// Repo identifies a repository on GitHub. It is the unit the per-repo issue
// listing polls (D-A).
type Repo struct {
	// Owner is the user or organisation.
	Owner string
	// Name is the repository name, without the owner.
	Name string
}

// String renders owner/name, which is how a repository is written everywhere a
// human reads one.
func (r Repo) String() string { return r.Owner + "/" + r.Name }

// Ref returns the canonical task ref for an issue in this repository.
func (r Repo) Ref(issue int) naming.Ref {
	return naming.Ref{Forge: naming.ForgeGitHub, Owner: r.Owner, Repo: r.Name, Issue: issue}
}

// RepoFromRef takes the repository half of a task ref. The forge is not
// re-checked: GitHub is the only forge M0 supports, and a ref carrying another
// one would have failed long before it reached this package.
func RepoFromRef(ref naming.Ref) Repo {
	return Repo{Owner: ref.Owner, Name: ref.Repo}
}

// Validate checks that both components can be interpolated into a request path
// as they stand.
//
// This is what makes [Client.endpoint] safe without escaping. The accepted set
// is GitHub's own owner and repository charset, which contains no '/', no '%'
// and nothing else that would change the shape of a URL — so a repository named
// "../../user" cannot be used to reach another endpoint.
func (r Repo) Validate() error {
	if err := checkPathSegment("repository owner", r.Owner); err != nil {
		return err
	}
	return checkPathSegment("repository name", r.Name)
}

// repoPath builds the /repos/{owner}/{repo} prefix. The components have been
// through [Repo.Validate], so no escaping is involved (see [Client.endpoint]).
func repoPath(repo Repo) string {
	return "/repos/" + repo.Owner + "/" + repo.Name
}

// issuePath builds the /repos/{owner}/{repo}/issues/{number} prefix that the
// issue-scoped endpoints share.
func issuePath(repo Repo, issue int) string {
	return repoPath(repo) + "/issues/" + strconv.Itoa(issue)
}

// checkPathSegment rejects a value that is not usable verbatim as one path
// segment.
func checkPathSegment(field, value string) error {
	fail := func(reason string) error {
		return fmt.Errorf("github: %s %q: %w: %s", field, value, ErrInvalidArgument, reason)
	}
	switch value {
	case "":
		return fail("is empty")
	case ".", "..":
		return fail("is a relative path component")
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-':
		default:
			return fail(fmt.Sprintf("contains %q", r))
		}
	}
	return nil
}

// checkIssueNumber rejects an issue number no forge would ever mint.
func checkIssueNumber(issue int) error {
	if issue <= 0 {
		return fmt.Errorf("github: issue number %d: %w: must be positive", issue, ErrInvalidArgument)
	}
	return nil
}

// checkCommentID rejects a comment id no forge would ever mint.
func checkCommentID(id int64) error {
	if id <= 0 {
		return fmt.Errorf("github: comment id %d: %w: must be positive", id, ErrInvalidArgument)
	}
	return nil
}
