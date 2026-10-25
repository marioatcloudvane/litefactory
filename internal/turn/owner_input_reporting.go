package turn

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
)

// ReportOwnerInput uses the existing immutable delivery journal and confirmed
// post-before-consume boundary. Its caller has already authenticated and saved
// the input. Status is recomputed only before the first delivery is retained.
func (r *Reporter) ReportOwnerInput(ctx context.Context, ref naming.Ref, sessionID, commentID int64, guidance bool) (Posted, error) {
	intent := r.deliveryIntent(ref, ref, sessionID, commentID, 0, "owner-input")
	return r.deliver(ctx, intent, func() (redact.Body, error) {
		var b strings.Builder
		if guidance {
			g, err := r.db.CurrentOwnerGuidance(ctx, sessionID)
			if err != nil {
				return redact.Body{}, err
			}
			if g.Body == "" {
				b.WriteString("Standing owner guidance is cleared.\n")
			} else {
				fmt.Fprintf(&b, "Standing owner guidance, revision %d:\n\n%s\n", g.Revision, g.Body)
			}
		} else {
			b.WriteString("Your instruction is retained and undelivered. It will be assigned once to the next eligible step matching its explicit target, if any. No new run or scope is authorized by retaining it.\n")
		}
		pending, err := r.db.ListOwnerInstructions(ctx, sessionID, 0)
		if err != nil {
			return redact.Body{}, err
		}
		if len(pending) > 0 {
			b.WriteString("\nUndelivered owner instructions:\n")
			for _, p := range pending {
				fmt.Fprintf(&b, "\n- Owner comment %d", p.CommentID)
				if p.Target.InstanceID > 0 {
					fmt.Fprintf(&b, " for workflow instance %d, node %s", p.Target.InstanceID, p.Target.Node)
					if p.Target.LaunchID > 0 {
						fmt.Fprintf(&b, ", launch %d", p.Target.LaunchID)
					}
				}
				b.WriteString(".\n")
			}
		}
		return r.renderer.RenderReportDelivery(ctx, b.String(), "")
	})
}
