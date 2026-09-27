package turn

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

// BugDeliveryPresentation preserves owner discussion and resolves only explicit
// requests for existing evidence sections. It does not answer arbitrary questions
// by guessing from keywords or introduce a new cause, remedy or test result.
func BugDeliveryPresentation(ctx context.Context, db *store.DB, session, childCallID int64) ([]store.BugDeliveryClarification, error) {
	review, err := db.CurrentBugDeliveryReview(ctx, session)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if review.Evidence.Child.ID != childCallID {
		return nil, nil
	}
	discussion, err := db.BugDeliveryDiscussion(ctx, session, review.Digest)
	if err != nil {
		return nil, err
	}
	if len(discussion) > 32 {
		return nil, errors.New("bug clarification history exceeds bound")
	}
	prior := make(map[int64]bool, len(review.Presentation))
	for _, c := range review.Presentation {
		prior[c.CommentID] = true
	}
	seen := make(map[int64]bool, len(discussion))
	for i := range discussion {
		c := &discussion[i]
		if seen[c.CommentID] {
			return nil, errors.New("duplicate retained Bug clarification")
		}
		seen[c.CommentID] = true
		c.References = slices.Clone(c.References)
		if prior[c.CommentID] {
			continue
		}
		c.References = bugRequestedEvidenceSections(c.Body, c.ReviewDigest)
	}
	return discussion, nil
}

func bugRequestedEvidenceSections(body, digest string) []string {
	text := strings.TrimSpace(body)
	prefix := "/bug-review " + digest + " clarify "
	if strings.HasPrefix(text, prefix) {
		text = strings.TrimSpace(strings.TrimPrefix(text, prefix))
	}
	text = strings.TrimPrefix(text, "show ")
	parts := strings.Split(text, ",")
	if len(parts) > 5 {
		return nil
	}
	seen := map[string]bool{}
	var refs []string
	for _, part := range parts {
		ref := strings.TrimSpace(part)
		switch ref {
		case "analysis", "repair", "regression", "focused", "full":
		default:
			return nil
		}
		if seen[ref] {
			return nil
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	return refs
}
