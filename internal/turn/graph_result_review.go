package turn

import (
	"context"
	"errors"
	"strings"

	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

// appendGraphResultReview is presentation only. The explicit comment activity
// has already validated and retained its typed candidate; review bytes never
// enter that candidate, graph outputs, adoption or workflow observations.
func (r *Reporter) appendGraphResultReview(ctx context.Context, source store.GraphNodeAttempt, text string) (string, error) {
	binding, err := r.db.GraphResultReviewBinding(ctx, source)
	if errors.Is(err, store.ErrNotFound) {
		return text, nil
	}
	if err != nil {
		return "", err
	}
	mode := binding.Mode
	if binding.Workflow == "bug" {
		mode = "bug"
	}
	reader, err := OpenAgentResultReader(ctx, r.db, binding, agentresult.EditTargets(mode))
	if err != nil {
		return "", err
	}
	review, err := reader.latestReview(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return text, nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text + "\n\n" + review.Markdown()), nil
}
