package response

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/cloudvane/litefactory/internal/store"
)

// AnswerAuthoring shares provider isolation, balanced selection and metering
// collection with response-only chat, while retaining independent ownership.
func (m *NativeModel) AnswerAuthoring(ctx context.Context, session, prompt string, fence ...string) ([]byte, error) {
	writer, ok := m.Meter.(interface {
		StartAuthoringMeterAttempt(context.Context, string, string, string, string, string, string) (store.MeterAttempt, error)
	})
	if !ok || session == "" {
		return nil, errors.New("standards authoring: metered response-only provider unavailable")
	}
	if len(prompt) == 0 || len(prompt) > 256<<10 {
		return nil, errors.New("standards authoring: prompt exceeds 256 KiB or is empty; narrow category filters")
	}
	if len(fence) != 2 || fence[0] == "" || fence[1] == "" {
		return nil, errors.New("standards authoring: current attempt identity and fencing token required")
	}
	raw, err := m.run(ctx, prompt, func(ctx context.Context, provider, model string) (store.MeterAttempt, error) {
		attempt, err := writer.StartAuthoringMeterAttempt(ctx, session, provider, model, authoringPromptDigest(prompt), fence[0], fence[1])
		if err == nil && m.AuthoringCall != nil {
			m.AuthoringCall(provider, model, attempt.ID)
		}
		return attempt, err
	})
	if err != nil {
		if errors.Is(err, ErrUnavailable) {
			return nil, fmt.Errorf("standards authoring: provider unavailable; configure an eligible isolated response-only provider: %w", err)
		}
		return nil, err
	}
	if len(raw) > 32<<10 {
		return nil, errors.New("standards authoring: response exceeds 32 KiB; saved draft retained")
	}
	return raw, nil
}

func authoringPromptDigest(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}
