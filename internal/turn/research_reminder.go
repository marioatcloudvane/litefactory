package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// ReportResearchReminder delivers an independent notice without consuming input,
// changing the owner wait, or inventing a graph attempt. at is daemon-owned.
func (r *Reporter) ReportResearchReminder(ctx context.Context, waitKey string, at time.Time) (Posted, error) {
	reminder, err := r.db.GetResearchReminder(ctx, waitKey)
	if err != nil {
		return Posted{}, err
	}
	if reminder.Suppressed {
		return Posted{Skipped: true}, nil
	}
	if reminder.DeliveryKey != "" {
		delivery, e := r.db.GetReportDelivery(ctx, reminder.DeliveryKey)
		if e != nil {
			return Posted{}, e
		}
		return r.dispatchDelivery(ctx, delivery)
	}
	ctx = ExecutionContext(ctx, reminder.SessionID)
	ref, err := naming.ParseRef(reminder.Task)
	if err != nil {
		return Posted{}, err
	}
	digest := sha256.Sum256([]byte("research-reminder\x00" + waitKey))
	key := hex.EncodeToString(digest[:])
	marker := "<!-- lf:delivery v=1 key=" + key + " -->"
	link := "https://github.com/" + ref.Owner + "/" + ref.Repo + "/issues/" + strconv.Itoa(ref.Issue) + "#issuecomment-" + strconv.FormatInt(reminder.CommentID, 10)
	_, observedLink, err := reportSourceLinks(ctx, r.forge, ref, ref.Issue, reminder.CommentID)
	if err != nil {
		return Posted{}, err
	}
	if observedLink != "" {
		link = observedLink
	}
	publisherID, err := retainedTicketPublisher(ctx, r.db, reminder.SessionID, r.botUserID)
	if err != nil {
		return Posted{}, err
	}
	body, err := r.renderer.RenderReportDelivery(ctx, fmt.Sprintf("Reminder: [this Research question or review](%s) has been waiting for your reply for seven days. You may reply later; Research will keep waiting.", link), marker)
	if err != nil {
		return Posted{}, err
	}
	intent := store.ReportDelivery{Key: key, Version: 1, SessionID: reminder.SessionID, Task: reminder.Task, Destination: reminder.Destination, TriggerNamespace: store.ResearchReminderNamespace, Purpose: store.ResearchReminderNamespace, BotUserID: publisherID, Body: body.String(), Marker: marker}
	saved, err := r.db.EnsureReportDelivery(ctx, intent)
	if err != nil {
		return Posted{}, err
	}
	if err = r.db.BindResearchReminder(ctx, waitKey, key, at); err != nil {
		return Posted{}, err
	}
	return r.dispatchDelivery(ctx, saved)
}
