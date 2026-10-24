package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// conversationListedLimit bounds the references one listing saves and the
// listed references a turn carries, so a long list never crowds out the
// conversation's other references.
const conversationListedLimit = 12

// listedIssue is one GitHub task a factory read showed the owner.
type listedIssue struct {
	RepoID  int64
	Project string
	Issue   int
}

type listedIssuesKey struct{}

// withListedIssues lets a factory reader record the tasks it lists, so the
// owner can act on "that one" without retyping its number.
func withListedIssues(ctx context.Context) (context.Context, *[]listedIssue) {
	listed := new([]listedIssue)
	return context.WithValue(ctx, listedIssuesKey{}, listed), listed
}

func recordListedIssue(ctx context.Context, item listedIssue) {
	if listed, ok := ctx.Value(listedIssuesKey{}).(*[]listedIssue); ok {
		*listed = append(*listed, item)
	}
}

// inboxListedIssue is the GitHub task behind an inbox decision, if any.
func inboxListedIssue(d store.InboxDecision) (listedIssue, bool) {
	n, err := strconv.Atoi(d.Locator.Key)
	if d.Locator.Provider != "github" || d.RepoID <= 0 || d.Locator.Container == "" || err != nil || n <= 0 {
		return listedIssue{}, false
	}
	return listedIssue{RepoID: d.RepoID, Project: d.Locator.Container, Issue: n}, true
}

// saveListed saves one reference per listed issue, carrying project and issue
// like a resolved target. It comes only from this factory read and is
// re-checked as current whenever a later turn loads it.
func (r *factoryConversationRun) saveListed(ctx context.Context, items []listedIssue) error {
	seen := map[string]bool{}
	for _, item := range items {
		key := item.Project + "#" + strconv.Itoa(item.Issue)
		if len(seen) >= conversationListedLimit || seen[key] || item.RepoID <= 0 || item.Project == "" || item.Issue <= 0 {
			continue
		}
		seen[key] = true
		args := map[string]string{"project": item.Project, "issue": strconv.Itoa(item.Issue)}
		raw, _ := json.Marshal(args)
		saved := store.ConversationReference{ID: fmt.Sprintf("%s-listed-%d-%d%s", r.t.ID, r.ordinal, len(seen), r.refSuffix), TurnID: r.t.ID, Kind: "listed", Repository: strconv.FormatInt(item.RepoID, 10), Target: key, Content: string(raw), ExpiresAt: r.at.Add(24 * time.Hour)}
		if err := r.s.db.SaveConversationReference(ctx, r.t, saved); err != nil {
			return err
		}
		r.listed = append(r.listed, ConversationReference{ID: saved.ID, OwnerID: r.s.owner, ChannelID: r.c.ChannelRef, OriginTurn: r.t.ID, RepoID: item.RepoID, ExpiresAt: saved.ExpiresAt, Arguments: args})
	}
	return nil
}

// takeListed hands the references the last lookup listed to the caller.
func (r *factoryConversationRun) takeListed() []ConversationReference {
	listed := r.listed
	r.listed = nil
	return listed
}

// listedCurrent keeps a listed reference only while its task is still known.
func (r *factoryConversationRun) listedCurrent(ctx context.Context, reference store.ConversationReference, args map[string]string) (bool, error) {
	repoID, _ := strconv.ParseInt(reference.Repository, 10, 64)
	n, _ := strconv.Atoi(args["issue"])
	if repoID <= 0 || n <= 0 {
		return false, nil
	}
	repo, err := r.s.db.GetRepo(ctx, repoID)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if _, err = r.s.knownNumericTask(ctx, repo, n); errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	return err == nil, err
}
