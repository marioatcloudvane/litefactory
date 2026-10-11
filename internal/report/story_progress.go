package report

import (
	"context"
	"fmt"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"strings"
)

// StoryProgressNotice contains the factory identities and scoped outcome to report.
type StoryProgressNotice struct {
	Parent                                              naming.Ref
	ParentURL                                           string
	Generation                                          int
	RunID, EffectID                                     int64
	Sequence                                            int
	Story, Revision, Kind, Detail, ImplementationDigest string
}

// Marker identifies a durable effect for recovery after an uncertain post.
func (n StoryProgressNotice) Marker() string {
	return fmt.Sprintf("<!-- litefactory:story-progress:%s:%d:%d -->", n.Parent.String(), n.RunID, n.EffectID)
}

// RenderStoryProgress revalidates identities and frames evidence before redaction.
//
// The rendered body no longer prints the run number, generation, event/
// sequence number, approved revision hash or implementation digest: those are
// factory bookkeeping that made the thread read like a build log rather than
// a story update. They are still validated exactly as strictly as before —
// n.Generation, n.RunID, n.EffectID and n.Revision are still checked below,
// and n.ImplementationDigest is still required and pattern-checked for
// engineering_complete and verified — because [StoryProgressNotice.Marker]
// still keys on Parent, RunID and EffectID for recovery/dedup, and a body
// whose identities were never validated could carry a digest recovery later
// trusts without ever having proven it was genuine.
func (r *Reporter) RenderStoryProgress(ctx context.Context, n StoryProgressNotice) (redact.Body, error) {
	if err := n.Parent.Validate(); err != nil {
		return redact.Body{}, err
	}
	if n.Generation < 1 || n.RunID < 1 || n.EffectID < 1 || !scopeKeyPattern.MatchString(n.Story) || !scopeDigestPattern.MatchString(n.Revision) || len(n.Detail) > 20000 {
		return redact.Body{}, fmt.Errorf("report: invalid story progress notice")
	}
	var state string
	switch n.Kind {
	case "started":
		state = "Admitted for launch (process start is not yet proven)."
	case "engineering_complete":
		if !scopeDigestPattern.MatchString(n.ImplementationDigest) {
			return redact.Body{}, fmt.Errorf("report: missing implementation digest")
		}
		state = "Engineering and executed checks complete. Acceptance is pending Validate; this story remains open. Parent engineering checks and save remain pending."
	case "verified":
		if !scopeDigestPattern.MatchString(n.ImplementationDigest) {
			return redact.Body{}, fmt.Errorf("report: missing implementation digest")
		}
		state = "Story acceptance checks, scoped review and scoped tests passed. Closing this story now; parent aggregate checks, save and push remain pending."
	case "invalidated":
		state = "Previous verification is invalidated. Reopening this story with the same locked approved scope."
	case "blocked":
		state = "Story is unfinished or blocked; no completion is claimed."
	default:
		return redact.Body{}, fmt.Errorf("report: invalid progress kind")
	}
	// Indented lines keep arbitrary evidence inside a code block; it cannot add a
	// forged marker at column zero or impersonate the surrounding factory prose.
	detail := strings.ReplaceAll(strings.ReplaceAll(n.Detail, "\r", ""), "\n", "\n    ")
	parent := fmt.Sprintf("https://github.com/%s/%s/issues/%d", n.Parent.Owner, n.Parent.Repo, n.Parent.Issue)
	if n.ParentURL != "" {
		if !ticketBrowserURL(n.ParentURL) {
			return redact.Body{}, fmt.Errorf("report: invalid parent browser link")
		}
		parent = n.ParentURL
	}
	body := fmt.Sprintf("Story %s — %s\n\nParent: %s.\n\n    %s\n\n%s", n.Story, state, parent, detail, n.Marker())
	out, err := r.redactor.Apply(ctx, body, r.limit)
	if err != nil {
		return redact.Body{}, err
	}
	if out.Truncated() || !strings.HasSuffix(out.String(), n.Marker()) {
		return redact.Body{}, fmt.Errorf("report: progress notice exceeds limit")
	}
	return out, nil
}
