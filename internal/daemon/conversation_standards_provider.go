package daemon

import (
	"context"
	"github.com/cloudvane/litefactory/internal/messaging/response"
)

// conversationStandardsAuthoring uses the existing isolated response-only
// authoring entry point, including its attempt fence and metering.
type conversationStandardsAuthoring struct{ model *response.NativeModel }

func (r *conversationStandardsAuthoring) Answer(ctx context.Context, session, prompt string) ([]byte, error) {
	return r.model.AnswerAuthoring(ctx, session, prompt)
}
func (r *conversationStandardsAuthoring) AnswerAttempt(ctx context.Context, session, attempt, token, prompt string) ([]byte, error) {
	return r.model.AnswerAuthoring(ctx, session, prompt, attempt, token)
}
