package report

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
)

// StoryScopeNotice contains only factory identities and a content digest. Story
// prose cannot inject instructions into the bot's scope announcement.
type StoryScopeNotice struct {
	Parent      naming.Ref
	ParentURL   string
	Generation  int
	Key, Digest string
	Planning    bool
}

var scopeKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)
var scopeDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Marker identifies a notice by generation and kind so delivery can be reconciled.
func (n StoryScopeNotice) Marker() string {
	kind := "freeze"
	if n.Planning {
		kind = "planning"
	}
	return fmt.Sprintf("<!-- litefactory:scope:%s:%d:%s:%s -->", n.Parent.String(), n.Generation, n.Key, kind)
}

// RenderStoryScope validates factory identities and preserves the reconciliation marker after redaction.
func (r *Reporter) RenderStoryScope(ctx context.Context, n StoryScopeNotice) (redact.Body, error) {
	if err := n.Parent.Validate(); err != nil {
		return redact.Body{}, err
	}
	if n.Generation < 1 || !scopeKeyPattern.MatchString(n.Key) || (!n.Planning && !scopeDigestPattern.MatchString(n.Digest)) {
		return redact.Body{}, fmt.Errorf("report: invalid scope notice")
	}
	parent := n.Parent.IssueURL()
	if parent == "" {
		parent = "this issue"
	}
	if n.ParentURL != "" {
		if !ticketBrowserURL(n.ParentURL) {
			return redact.Body{}, fmt.Errorf("report: invalid parent browser link")
		}
		parent = n.ParentURL
	}
	body := fmt.Sprintf("Story %s is locked for implementation under parent %s, generation %d, approved revision `%s`. Later comments cannot change this approved scope. To reopen planning for unfinished stories, comment `replan` on the parent after the active run finishes. Completed stories remain completed; new scope needs a successor story.", n.Key, parent, n.Generation, n.Digest)
	if n.Planning {
		body = fmt.Sprintf("Planning is available again for unfinished story %s under parent %s, generation %d. Owner comments here refine that parent plan. Completed stories remain completed.", n.Key, parent, n.Generation)
	}
	rendered, err := r.redactor.Apply(ctx, body+"\n\n"+n.Marker(), r.limit)
	if err != nil {
		return redact.Body{}, err
	}
	if !strings.HasSuffix(rendered.String(), n.Marker()) {
		return redact.Body{}, fmt.Errorf("report: scope notice exceeds supported comment size")
	}
	return rendered, nil
}
