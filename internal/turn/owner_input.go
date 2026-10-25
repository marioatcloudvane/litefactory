package turn

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cloudvane/litefactory/internal/store"
)

func graphOwnerInputSection(ctx context.Context, db *store.DB, attemptID int64) (string, error) {
	inputs, err := db.OwnerInstructionsForAttempt(ctx, attemptID)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, input := range inputs {
		if b.Len() == 0 {
			b.WriteString("\n## Assigned one-time owner instructions\n\nThese authenticated instructions were durably assigned to this logical step. Apply them within the approved scope and permissions; they are separate from standing guidance.\n")
		}
		fmt.Fprintf(&b, "\nOwner comment %d (numeric owner %d):\n%s\n", input.CommentID, input.ActorID, input.Body)
	}
	return b.String(), nil
}

func ownerInstructionForStep(ctx context.Context, db *store.DB, req Request, sessionID int64) (string, error) {
	if req.researchInstance > 0 && req.Trigger.CommentID > 0 {
		if _, err := db.GetOwnerInstruction(ctx, sessionID, req.Trigger.CommentID); err == nil {
			return "Continue this workflow step within its retained scope and context. Current one-time owner direction is supplied only when assigned to this logical step.", nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return "", err
		}
	}
	return req.Command.Text, nil
}
