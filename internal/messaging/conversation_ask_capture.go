package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/retainedevidence"
	"github.com/cloudvane/litefactory/internal/store"
)

// retainAskSources keeps delivered Ask identities in the shared contract as
// well as inside the originating lookup. It retains only the admitted excerpt;
// the original legacy question and complete raw-row extent remain unknown.
func (r *factoryConversationRun) retainAskSources(ctx context.Context, result conversationReadResult) error {
	if result.Command != "ask" {
		return nil
	}
	// Legacy bundles and ordinary refusal notices have no admitted Ask turns.
	if !json.Valid([]byte(result.Result)) {
		return nil
	}
	var envelope struct {
		History struct {
			Turns []ask.Turn `json:"turns"`
		} `json:"history"`
	}
	if err := json.Unmarshal([]byte(result.Result), &envelope); err != nil {
		return err
	}
	if len(envelope.History.Turns) == 0 {
		return nil
	}
	resolution, err := ask.ResolveProject(ctx, r.s.db, result.Arguments["project"], nil)
	if err != nil {
		return err
	}
	if resolution.Refusal != "" {
		return errors.New("ask evidence repository unavailable")
	}
	saved, err := r.s.db.ListConversationTurnEvidence(ctx, r.t.ID, r.at)
	if err != nil {
		return err
	}
	used := make(map[int]bool)
	identities := make(map[string]bool)
	for _, e := range saved {
		used[e.Ordinal] = true
		if e.Entry.Kind == "ask" {
			identities[e.Entry.OwnerID+":"+e.Entry.Revision] = true
		}
	}
	for _, turn := range envelope.History.Turns {
		if turn.SourceID == "" || turn.Revision == "" || turn.Project != resolution.Repository.Slug() {
			continue
		}
		key := turn.SourceID + ":" + turn.Revision
		if identities[key] {
			continue
		}
		ordinal := 104
		for ordinal <= 123 && used[ordinal] {
			ordinal++
		}
		if ordinal > 123 {
			return errors.New("ask source capture limit exceeded")
		}
		raw, err := json.Marshal(turn)
		if err != nil {
			return err
		}
		protected, err := r.s.Protect(ctx, string(raw))
		if err != nil {
			return err
		}
		entry := retainedevidence.Entry{Kind: "ask", OwnerID: turn.SourceID, Revision: turn.Revision, Digest: turn.Revision, Channel: strconv.FormatInt(resolution.Repository.ID, 10), Bytes: -1, Capture: retainedevidence.Unknown, Availability: retainedevidence.Available, Omissions: []string{"Delivered local reply with stored question excerpt only; original raw-row extent and uncaptured history are unknown."}}
		e := store.ConversationEvidence{TurnID: r.t.ID, SourceTurnID: r.t.ID, Ordinal: ordinal, Repository: turn.Project, CapturedAt: result.CapturedAt, ExpiresAt: result.CapturedAt.Add(24 * time.Hour), Entry: entry, Content: protected, RangeEnd: int64(len(protected)), ViewDigest: retainedevidence.Digest([]byte(protected))}
		if err := r.s.db.SaveConversationEvidence(ctx, r.t, e); err != nil {
			return err
		}
		used[ordinal] = true
		identities[key] = true
	}
	return nil
}
