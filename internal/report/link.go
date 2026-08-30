package report

import (
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
)

// gitHubWebRoot is where a github.com repository is browsed, as opposed to
// where its API lives.
//
// It is a literal here rather than an import of internal/github because this
// package renders and does no forge work at all: it holds no client, no token
// and no base URL, and importing the client for one string would put a network
// dependency in the one package whose doc.go promises it has none.
const gitHubWebRoot = "https://github.com/"

// maxSHALength is the length of a full git object id, and minSHALength is the
// shortest abbreviation git itself will accept. A value outside that range is
// not a commit and must not become half of a permalink.
const (
	minSHALength = 7
	maxSHALength = 64
)

// blobURL builds a commit-pinned permalink to one file in the task's own
// repository:
//
//	https://github.com/<owner>/<repo>/blob/<sha>/<path>
//
// Every part of it comes from [naming.Ref] and the run's own sha, so the
// permalink needs no extra field on [Run], no forge client and no I/O.
//
// It is pinned to a commit rather than to the branch on purpose. A
// branch-relative URL shows whatever the branch holds when it is opened, which
// for a report of run 3 means the reader may be looking at what run 7 wrote. A
// report is a statement about one run, so its links are too.
//
// The second result is false for anything that cannot be linked to honestly: a
// forge other than github.com, a sha that is not an object id, or a path that
// is not a plain relative path. Refusing is always safe — the caller renders no
// link — and a wrong URL is worse than none.
func blobURL(ref naming.Ref, sha, repoPath string) (string, bool) {
	if ref.Forge != naming.ForgeGitHub {
		// M0 supports one forge. Another one would need its own URL shape, and
		// guessing github.com's for it would produce a link to the wrong host.
		return "", false
	}
	if ref.Validate() != nil || !validSHA(sha) || !validRepoPath(repoPath) {
		return "", false
	}

	var b strings.Builder
	b.WriteString(gitHubWebRoot)
	b.WriteString(ref.Owner)
	b.WriteByte('/')
	b.WriteString(ref.Repo)
	b.WriteString("/blob/")
	b.WriteString(sha)
	b.WriteByte('/')
	b.WriteString(repoPath)
	return b.String(), true
}

// validSHA reports whether s is shaped like a git object id: lowercase
// hexadecimal, between an abbreviation and a full id in length.
func validSHA(s string) bool {
	if len(s) < minSHALength || len(s) > maxSHALength {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// validRepoPath reports whether p is a plain relative path safe to append to a
// forge URL and to quote in markdown.
//
// internal/naming has already validated every component of the paths this
// package builds. The check is repeated here for the same reason
// [Reply.body] re-declares the profile-name pattern rather than importing it:
// this is the boundary where a string becomes part of a URL in a comment
// litefactory signs its own name to, and a boundary that trusts its caller is
// not a boundary. The accepted set has no "%", no "?", no "#" and no "..", so
// nothing here can escape the repository, start a query string or close the
// markdown link it sits inside.
func validRepoPath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.HasSuffix(p, "/") {
		return false
	}
	for _, segment := range strings.Split(p, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, r := range segment {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			case r == '.', r == '_', r == '-':
			default:
				return false
			}
		}
	}
	return true
}

// briefLink renders the markdown link to this session's research brief, and
// reports whether there is one to render.
//
// The path is derived, not discovered. Scanning [Run.Files] for the brief would
// only ever link it on the run that rewrote it, and the run that most needs the
// link is the one that merely read it — so the path is computed from the branch,
// the run's own day and the slug frozen into the branch, exactly as CLAUDE.md
// computes it. That derivation cannot know whether the file is there, which is
// why the rendered line says so rather than promising a page.
//
// It is refused outright in three cases, each of which would otherwise produce a
// link that is worse than no link:
//
//   - Not pushed. A permalink into a commit that exists only in a local
//     worktree is a guaranteed 404 — and [Run.Pushed] is false on two ordinary
//     paths, a branch that did not move and an abandoned run, not just on a push
//     that failed. This is the same fact [metadataList] gates its "(pushed)"
//     suffix on, and the two must not disagree.
//   - No sha_after. [Run.CommitSHA] is deliberately not used as a fallback: it
//     is empty whenever claude committed for itself through its own Bash tool,
//     while the branch moved and sha_after holds where to. Pinning to CommitSHA
//     would leave the link blank on precisely the supported path where the work
//     is real (see git.CommitResult.Head).
//   - No branch, no day, or a branch this issue's slug cannot be read out of.
func (r Run) briefLink() (string, bool) {
	if !r.Pushed || r.SHAAfter == "" {
		return "", false
	}
	day, ok := r.startedOn()
	if !ok {
		return "", false
	}
	slug, err := naming.SlugFromBranch(r.Branch, r.Ref.Issue)
	if err != nil {
		return "", false
	}
	brief, err := naming.ResearchBriefPath(r.Branch, day, slug)
	if err != nil {
		return "", false
	}
	url, ok := blobURL(r.Ref, r.SHAAfter, brief)
	if !ok {
		return "", false
	}
	// validRepoPath has already refused every character that could close the
	// code span or the link, so the path needs no further quoting.
	return "[`" + brief + "`](" + url + ")", true
}
