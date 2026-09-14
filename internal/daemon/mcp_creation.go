package daemon

import (
	"context"
	"errors"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/turn"
)

// AdmitMCPOwnerCommand is the offline-testable trusted intake seam. The factory
// passes a comment retrieved for this source issue and the selected mode; no
// agent tool reaches this method. Production comment/launch wiring is #264.
// Authorizer construction fixes configured owner/bot IDs independently of the
// public comment body, so copied commands cannot mint someone else's approval.
func AdmitMCPOwnerCommand(ctx context.Context, authorizer *turn.CreationAuthorizer, repo github.Repo, issue int, mode string, comment github.Comment) (int64, error) {
	if authorizer == nil {
		return 0, errors.New("creation owner intake unavailable")
	}
	source := repo.Ref(issue)
	if err := source.Validate(); err != nil {
		return 0, err
	}
	return authorizer.AdmitOwnerGrantIntent(ctx, turn.OwnerGrantControl{Repository: repo.String(), SourceTask: source.String(), Mode: mode, Body: comment.Body, CommentID: comment.ID, ActorID: comment.Author.ID, CreatedAt: comment.CreatedAt, UpdatedAt: comment.UpdatedAt, Bot: comment.Author.IsBot})
}
