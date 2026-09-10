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
	body := fmt.Sprintf("Story %s — %s\n\nParent: https://github.com/%s/%s/issues/%d; run %d; generation %d; event %d; approved revision `%s`.\n\nValidated implementation digest: `%s`.\n\n    %s\n\n%s", n.Story, state, n.Parent.Owner, n.Parent.Repo, n.Parent.Issue, n.RunID, n.Generation, n.Sequence, n.Revision, n.ImplementationDigest, detail, n.Marker())
	out, err := r.redactor.Apply(ctx, body, r.limit)
	if err != nil {
		return redact.Body{}, err
	}
	if out.Truncated() || !strings.HasSuffix(out.String(), n.Marker()) {
		return redact.Body{}, fmt.Errorf("report: progress notice exceeds limit")
	}
	return out, nil
}
