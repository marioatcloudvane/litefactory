package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/cloudvane/litefactory/internal/store"
)

// conversationFocus is the project and issue a conversation discussed last.
// It is read from the newest target the factory resolved in this binding, so
// the newest mention wins and follow-ups need not name the project again.
type conversationFocus struct {
	Project string `json:"project"`
	Issue   string `json:"issue,omitempty"`
}

// loadFocus re-resolves the newest saved target against the registered projects.
// A target whose project is no longer registered under the same name is no
// focus at all.
func (r *factoryConversationRun) loadFocus(ctx context.Context) (*conversationFocus, error) {
	saved, err := r.s.db.LatestConversationTarget(ctx, r.c.ID)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var args map[string]string
	if json.Unmarshal([]byte(saved.Content), &args) != nil || args["project"] == "" {
		return nil, nil
	}
	repoID, _ := strconv.ParseInt(saved.Repository, 10, 64)
	if repoID <= 0 {
		return nil, nil
	}
	repo, err := r.s.db.GetRepo(ctx, repoID)
	if err != nil || repo.Slug() != args["project"] {
		return nil, nil
	}
	issue := args["task"]
	if issue == "" {
		issue = args["issue"]
	}
	return &conversationFocus{Project: repo.Slug(), Issue: issue}, nil
}
