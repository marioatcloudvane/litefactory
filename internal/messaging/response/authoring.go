package response

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/projectread"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// Authoring answers standards authoring calls through the response-only model,
// with its provider isolation, balanced selection and metering. A call scoped
// to a project also gets that project's read tools.
type Authoring struct {
	Model *NativeModel
	// Reads serves a project's read tools; without a DB it serves none and a
	// project-scoped call is refused.
	Reads projectread.Session

	mu                     sync.Mutex
	provider, model, usage string
}

// Metadata names the provider, model and usage reference of the last call.
func (a *Authoring) Metadata() (string, string, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.provider, a.model, a.usage
}

// Answer runs one fenced authoring call.
func (a *Authoring) Answer(ctx context.Context, call standards.AuthoringCall) ([]byte, error) {
	m := a.Model
	writer, ok := m.Meter.(interface {
		StartAuthoringMeterAttempt(context.Context, string, string, string, string, string, string) (store.MeterAttempt, error)
	})
	if !ok || call.Session == "" {
		return nil, errors.New("standards authoring: metered response-only provider unavailable")
	}
	if len(call.Prompt) == 0 || len(call.Prompt) > 256<<10 {
		return nil, errors.New("standards authoring: prompt exceeds 256 KiB or is empty")
	}
	if call.Attempt == "" || call.Token == "" {
		return nil, errors.New("standards authoring: current attempt identity and fencing token required")
	}
	var launch *agent.MCPLaunchSpec
	if call.Project != nil {
		var stop func() error
		var err error
		if launch, stop, err = a.Reads.Start(ctx, *call.Project, call.Attempt); err != nil {
			return nil, fmt.Errorf("standards authoring: %w", err)
		}
		defer func() { _ = stop() }()
	}
	raw, err := m.run(ctx, call.Prompt, launch, func(ctx context.Context, provider, model string) (store.MeterAttempt, error) {
		// Like the attempt itself, the meter row is bookkeeping, not the
		// conversation operation's effect; only the finished draft commits it.
		attempt, err := writer.StartAuthoringMeterAttempt(store.WithoutStandardsConversationOperation(ctx), call.Session, provider, model, authoringPromptDigest(call.Prompt), call.Attempt, call.Token)
		if err == nil {
			a.mu.Lock()
			a.provider, a.model, a.usage = provider, model, attempt.ID
			a.mu.Unlock()
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
