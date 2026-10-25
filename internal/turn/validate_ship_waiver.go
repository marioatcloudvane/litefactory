package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// shippingEvidence excludes delivery progression and the exception overlay.
// Original suite, review, assessment, owner choices and issue effects remain exact.
func (s ValidateState) shippingEvidence() (json.RawMessage, string) {
	raw, _ := json.Marshal(struct {
		Commit          string
		Suite           validateSuiteOutput
		Assessed        bool
		Outcome, Reason string
		Stories         []ValidateJoinedStory
		Reviews         []store.ValidateReview
		Findings        []ValidateFinding
		Issues          map[string]store.ValidateEffect
	}{s.Commit, s.Suite, s.Assessed, s.Outcome, s.Reason, s.Stories, s.Reviews, s.Findings, s.Effects.Issues})
	sum := sha256.Sum256(raw)
	return raw, hex.EncodeToString(sum[:])
}

func (s ValidateState) deliveryCommit() string {
	if s.DeliveryCommit != "" {
		return s.DeliveryCommit
	}
	return s.Commit
}

func (s ValidateState) waiverCurrent() bool {
	if s.Waiver == nil {
		return false
	}
	_, digest := s.shippingEvidence()
	if s.Waiver.TestedCommit != s.Commit || s.Waiver.DeliveryCommit != s.deliveryCommit() || s.Waiver.EvidenceRevision != digest {
		return false
	}
	seen := map[int64]bool{}
	for _, e := range s.deliveryEvidence() {
		seen[e.ID] = true
		if e.ConfirmCommentID == s.Waiver.CommentID {
			continue
		}
		matched := false
		for _, before := range s.Waiver.DeliveryEffects {
			if before.ID == e.ID {
				a, _ := json.Marshal(before)
				b, _ := json.Marshal(e)
				matched = string(a) == string(b)
				break
			}
		}
		if !matched {
			return false
		}
	}
	for _, before := range s.Waiver.DeliveryEffects {
		if before.Commit == s.deliveryCommit() && !seen[before.ID] {
			return false
		}
	}
	return true
}

func retainValidateShippingWaiver(ctx context.Context, opts Options, req ActivityRequest, st ValidateState, wait store.GraphOwnerWait, delivery, reason string, now time.Time) error {
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return err
	}
	session, err := opts.DB.GetSession(ctx, instance.SessionID)
	if err != nil {
		return err
	}
	raw, digest := st.shippingEvidence()
	blockers := append([]string(nil), st.Open...)
	if delivery != st.Commit {
		blockers = append(blockers, fmt.Sprintf("Delivery commit %s differs from tested commit %s; old test evidence applies only to the tested commit.", delivery, st.Commit))
	}
	for _, e := range st.deliveryEvidence() {
		if !effectDone(&e) {
			blockers = append(blockers, fmt.Sprintf("Delivery effect %s (%s at %s) is %s: %s", e.Kind, e.Target, e.Commit, e.State, e.Error))
		}
	}
	return opts.DB.RetainValidateShipWaiver(ctx, store.ValidateShipWaiver{DeliveryEffects: st.deliveryEvidence(), Task: session.TaskRef, InstanceID: req.InstanceID, TestedCommit: st.Commit, DeliveryCommit: delivery, EvidenceRevision: digest, ReportRevision: st.Revision, Blockers: blockers, Evidence: raw, ActorID: wait.ReplyAuthorID, CommentID: wait.ReplyID, Body: wait.ReplyBody, Reason: reason, CreatedAt: now})
}

func (s *ValidateState) loadShippingWaiver(ctx context.Context, opts Options) error {
	w, err := opts.DB.LatestValidateShipWaiver(ctx, s.InstanceID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	s.DeliveryCommit = w.DeliveryCommit
	s.Waiver = &w
	return nil
}

// closeEffectTarget separates delivery attempts at different displayed heads.
func (s ValidateState) closeEffectTarget(numbers []int) string {
	target := validateNumbers(numbers)
	if s.deliveryCommit() != s.Commit {
		target += "@" + s.deliveryCommit()
	}
	return target
}

func (s ValidateState) deliveryEvidence() []store.ValidateEffect {
	var out []store.ValidateEffect
	for _, e := range []*store.ValidateEffect{s.Effects.Push, s.Effects.PR, s.Effects.Merge, s.Effects.Close} {
		if e != nil {
			out = append(out, *e)
		}
	}
	return out
}
