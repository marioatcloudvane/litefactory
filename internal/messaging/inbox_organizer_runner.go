package messaging

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/naming"
)

// organizerCall runs one bounded, snapshot-only native session. It never exposes
// factory read commands, tools, credentials or workflow mutation capabilities.
func (r *factoryConversationRun) organizerCall(ctx context.Context, input []byte, correction string, step int) (_ []byte, err error) {
	instructions := r.instructions.InboxOrganizer()
	if instructions == "" {
		return nil, errors.New("captured inbox organizer instructions unavailable")
	}
	if step < 1 || step > 2 {
		return nil, errors.New("inbox organizer correction budget exceeded")
	}
	if len(input) > 256<<10 || !json.Valid(input) {
		return nil, errors.New("inbox organizer input budget or JSON invalid")
	}
	var snapshot struct {
		ID string `json:"snapshot_id"`
	}
	if json.Unmarshal(input, &snapshot) != nil || snapshot.ID == "" {
		return nil, errors.New("missing inbox snapshot identity")
	}
	scope := r.s.inboxScope(r.c.ChannelRef)
	if err := r.s.db.InboxSnapshotAuthorized(ctx, scope, snapshot.ID, r.at); err != nil {
		return nil, err
	}
	runtime := r.s.conversations.Load()
	reasoner := runtime.reasoners[r.c.Provider]
	if reasoner.Model == "" {
		reasoner = ConversationReasoner{Model: r.b.Model}
	}
	if err := conversationAccess(ctx, runtime, r.c.ChannelRef, r.at); err != nil {
		return nil, err
	}
	if err := conversationCapacity(runtime.root); err != nil {
		return nil, err
	}
	provider := runtime.providers[r.c.Provider]
	if provider == nil {
		return nil, agent.ErrConversationIsolation
	}
	encoded, err := json.Marshal(struct {
		Snapshot   json.RawMessage `json:"snapshot"`
		Correction string          `json:"correction_reason,omitempty"`
	}{Snapshot: json.RawMessage(input), Correction: correction})
	if err != nil {
		return nil, err
	}
	prompt, err := r.s.Protect(ctx, string(encoded))
	if err != nil {
		return nil, err
	}
	if len(prompt) > 256<<10 || len(instructions) > 256<<10 {
		return nil, errors.New("inbox organizer prompt byte budget exceeded")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, err
	}
	identity := hex.EncodeToString(nonce[:])
	ref, err := naming.ConversationSessionRef(identity)
	if err != nil {
		return nil, err
	}
	state := filepath.Join(runtime.root, "inbox-organizer-"+identity)
	defer func() { err = errors.Join(err, os.RemoveAll(state)) }()
	attempt, err := r.s.db.StartPresentationMeterAttempt(ctx, 0, fmt.Sprintf("conversation-inbox-organizer-%s-%d", r.t.ID, step), "generation", r.c.Provider)
	if err != nil {
		return nil, err
	}
	defer func() {
		outcome := "success"
		if err != nil {
			outcome = "failure"
		}
		if ctx.Err() != nil {
			outcome = "cancelled"
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		err = errors.Join(err, r.s.db.FinishMeterAttempt(cleanup, attempt.ID, outcome, "unknown", "", "", []string{"conversation_inbox_organizer_usage_not_observed"}))
	}()
	result, err := provider.RunConversation(ctx, agent.ConversationRequest{ConversationID: r.c.ID, TurnID: r.t.ID, Model: reasoner.Model, Effort: reasoner.Effort, StateDirectory: state, Prompt: prompt, NativeInstructions: instructions, Session: agent.SessionRef{Ref: ref}, BeforeLaunch: func(cb context.Context) error {
		if err := conversationAccess(cb, runtime, r.c.ChannelRef, r.at); err != nil {
			return err
		}
		if err := r.s.db.InboxSnapshotAuthorized(cb, scope, snapshot.ID, r.at); err != nil {
			return err
		}
		return r.s.db.RenewConversationTurn(cb, r.t, r.at, 10*time.Minute)
	}, OnSession: func(_ context.Context, session agent.SessionRef) error {
		if session.Ref != ref || !agent.ConversationIdentity(session.Resume) {
			return errors.New("invalid organizer session identity")
		}
		return nil
	}})
	if err != nil {
		return nil, err
	}
	if len(result.Answer) > 256<<10 {
		return nil, errors.New("inbox organizer result byte budget exceeded")
	}
	if err := conversationAccess(ctx, runtime, r.c.ChannelRef, r.at); err != nil {
		return nil, err
	}
	return result.Answer, nil
}
