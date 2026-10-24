package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"strings"
	"unicode/utf8"

	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/graphexec"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
)

// candidateEvidence separates measured facts from the agent's account. Unknown
// input is retained for inspection, but never becomes an adoptable candidate.
type candidateEvidence struct {
	Finish             store.RepairFinish
	OriginalReport     *store.ValidateReport `json:",omitempty"`
	FileCount          int
	AutomaticFileLimit int
	ForbiddenKinds     []string
	Complete           bool
	Uncertainty        string
}

func candidateHash(v any) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (b ValidateFlowBindings) repairScope(ctx context.Context, opts Options, req ActivityRequest) (string, error) {
	if b.Stories == nil {
		return "", errors.New("retained repair requires frozen story scope")
	}
	stories, err := b.Stories(ctx, opts, req)
	if err != nil {
		return "", err
	}
	if len(stories) == 0 {
		return "", errors.New("retained repair has no frozen stories")
	}
	return candidateHash(stories), nil
}

func verifyCandidateEvidence(c store.RepairCandidate) error {
	expected := c.ReviewDigest
	c.ID, c.ReviewDigest = "", ""
	if candidateHash(c) != expected {
		return errors.New("retained review evidence digest changed")
	}
	sum := sha256.Sum256([]byte(c.Patch))
	if hex.EncodeToString(sum[:]) != c.PatchDigest {
		return errors.New("retained patch digest changed")
	}
	return nil
}

func candidateSnapshot(c store.RepairCandidate) (git.RepairSnapshot, error) {
	s := git.RepairSnapshot{Base: c.BaseCommit, Tree: c.Tree, Commit: c.Commit, Ref: c.Ref, Patch: c.Patch}
	err := json.Unmarshal(c.Manifest, &s.Changes)
	return s, err
}

// retainRepair makes capture durable before any destructive reset. A failure
// leaves the round running and its source tree untouched, holding graph work.
func (b ValidateFlowBindings) retainRepair(ctx context.Context, opts Options, req ActivityRequest, round store.RepairRound, snapshot git.RepairSnapshot, in store.RepairFinish, complete bool) (store.RepairCandidate, error) {
	candidates, err := opts.DB.ListRepairCandidates(ctx, req.InstanceID)
	if err != nil {
		return store.RepairCandidate{}, err
	}
	for _, c := range candidates {
		if c.RoundID == round.ID {
			return c, b.completeRepairCapture(ctx, opts, c)
		}
	}
	scope, err := b.repairScope(ctx, opts, req)
	if err != nil {
		return store.RepairCandidate{}, err
	}
	instance, err := opts.DB.GetGraphInstance(ctx, req.InstanceID)
	if err != nil {
		return store.RepairCandidate{}, err
	}
	session, err := opts.DB.GetSession(ctx, instance.SessionID)
	if err != nil {
		return store.RepairCandidate{}, err
	}
	evidence := candidateEvidence{Finish: in, Complete: complete, FileCount: len(in.Files)}
	if b.Profile != nil {
		for _, node := range b.Profile.Nodes {
			if node.Action == graphexec.ValidateRepairCheckAction {
				maxFiles, forbid, err := decodeValidateLimits(node.Attrs)
				if err != nil {
					return store.RepairCandidate{}, err
				}
				evidence.AutomaticFileLimit = maxFiles
				for _, kind := range in.Kinds {
					for _, rule := range forbid {
						if kind == rule {
							evidence.ForbiddenKinds = append(evidence.ForbiddenKinds, kind)
						}
					}
				}
			}
		}
	}

	if !complete {
		evidence.Uncertainty = "Complete risk measurement is unavailable; this candidate cannot be accepted."
	}
	original, reportErr := opts.DB.GetValidateReport(ctx, req.InstanceID)
	if reportErr == nil {
		evidence.OriginalReport = &original
	} else if !errors.Is(reportErr, store.ErrNotFound) {
		return store.RepairCandidate{}, reportErr
	}
	manifest, _ := json.Marshal(snapshot.Changes)
	ev, _ := json.Marshal(evidence)
	sum := sha256.Sum256([]byte(snapshot.Patch))
	c := store.RepairCandidate{TaskID: session.TaskRef, InstanceID: req.InstanceID, RoundID: round.ID, NodeAttemptID: in.NodeAttemptID, BaseCommit: snapshot.Base, Tree: snapshot.Tree, Commit: snapshot.Commit, Ref: snapshot.Ref, Patch: snapshot.Patch, PatchDigest: hex.EncodeToString(sum[:]), Manifest: manifest, Evidence: ev, ScopeRevision: scope, ReportRevision: candidateHash([]string{scope, round.PreCommit})[:12], Eligible: complete, CreatedAt: b.now(opts)}
	if evidence.OriginalReport != nil && evidence.OriginalReport.Revision != "" {
		c.ReportRevision = evidence.OriginalReport.Revision
	}
	for _, kind := range in.Kinds {
		if kind == "criteria" {
			c.Eligible = false
		}
	}
	c.ReviewDigest = candidateHash(c)
	c.ID = candidateHash([]int64{req.InstanceID, round.ID})
	if err := opts.DB.JournalRepairCandidate(ctx, c.ID, store.RepairCandidateJournal{InstanceID: req.InstanceID, RoundID: round.ID, Phase: "capture", State: "intent", ExpectedBase: c.BaseCommit, At: b.now(opts)}); err != nil {
		return c, err
	}
	if err := opts.DB.PutRepairCandidate(ctx, c); err != nil {
		return c, err
	}
	err = opts.DB.JournalRepairCandidate(ctx, c.ID, store.RepairCandidateJournal{InstanceID: req.InstanceID, RoundID: round.ID, Phase: "capture", State: "complete", ExpectedBase: c.BaseCommit, At: b.now(opts)})
	return c, err
}

func (b ValidateFlowBindings) completeRepairCapture(ctx context.Context, opts Options, c store.RepairCandidate) error {
	if err := verifyCandidateEvidence(c); err != nil {
		return err
	}
	w, _, err := openValidateWorktree(b)
	if err != nil {
		return err
	}
	snapshot, err := candidateSnapshot(c)
	if err != nil {
		return err
	}
	if err = w.VerifyRepairCandidate(ctx, snapshot); err != nil {
		return err
	}
	j := store.RepairCandidateJournal{InstanceID: c.InstanceID, RoundID: c.RoundID, ExpectedBase: c.BaseCommit, Phase: "capture", State: "intent", At: b.now(opts)}
	if err = opts.DB.JournalRepairCandidate(ctx, c.ID, j); err != nil {
		return err
	}
	j.State = "complete"
	return opts.DB.JournalRepairCandidate(ctx, c.ID, j)
}

func candidateReview(c store.RepairCandidate) string {
	return fmt.Sprintf("Retained Validate repair candidate %s\nReview digest: %s\nBase: %s\nTree: %s\nFrozen scope: %s\nEligible: %t\n\nComplete path/mode manifest:\n%s\n\nFlag reasons, diagnosis, measured facts and uncertainty:\n%s\n\nComplete binary-capable patch:\n%s", c.ID, c.ReviewDigest, c.BaseCommit, c.Tree, c.ScopeRevision, c.Eligible, c.Manifest, c.Evidence, c.Patch)
}

// publishRepairReview reuses durable ordered multipart delivery and its receipt
// reconciliation. Redaction that changes even one byte disables acceptance.
func (r *Reporter) publishRepairReview(ctx context.Context, c store.RepairCandidate) (string, error) {
	instance, err := r.db.GetGraphInstance(ctx, c.InstanceID)
	if err != nil {
		return "", err
	}
	session, err := r.db.GetSession(ctx, instance.SessionID)
	if err != nil {
		return "", err
	}
	ref, err := naming.ParseRef(session.TaskRef)
	if err != nil {
		return "", err
	}
	text := "<pre>" + html.EscapeString(candidateReview(c)) + "</pre>"
	validator, ok := r.renderer.(interface {
		ValidateUnchangedReview(context.Context, string) error
	})
	unchanged := ok && validator.ValidateUnchangedReview(ctx, text) == nil

	intent := r.deliveryIntent(ref, ref, session.ID, 0, 0, "validate-repair-review:"+c.ID+":"+c.ReviewDigest)
	intent.TriggerNamespace = store.PlanNoticeNamespace
	posted, err := r.deliverFullText(ctx, intent, text)
	if err != nil {
		return "", err
	}
	saved, err := r.db.GetReportDelivery(ctx, intent.Key)
	if err != nil {
		return "", err
	}
	if !saved.Completed || posted.CommentID <= 0 {
		return "", errors.New("repair review delivery is incomplete")
	}
	_, link, err := reportSourceLinks(ctx, r.forge, ref, ref.Issue, posted.CommentID)
	if err != nil {
		return "", err
	}
	if unchanged {
		err = r.db.SetRepairCandidateReviewComplete(ctx, c.ID, c.ReviewDigest, c.CreatedAt)
	}
	return link, err
}

func (h ValidateResolveActivity) resolveCandidate(ctx context.Context, opts Options, req ActivityRequest, st ValidateState, wait store.GraphOwnerWait, cmd ValidateCommand) (ActivityResult, error) {
	refuse := func(why string) (ActivityResult, error) {
		if err := saveValidateNotice(ctx, opts, req.InstanceID, st.Revision, why, h.b.now(opts)); err != nil {
			return ActivityResult{}, err
		}
		return validateResolveResult("wait", why, st)
	}
	c, err := opts.DB.GetRepairCandidate(ctx, cmd.Candidate)
	if err != nil || c.InstanceID != req.InstanceID || c.ReviewDigest != cmd.ReviewDigest {
		return refuse("The retained candidate or review digest does not match this run.")
	}
	if err := verifyCandidateEvidence(c); err != nil {
		return refuse(err.Error())
	}
	if cmd.RepairAction == "inspect" {
		if opts.ResearchReporter == nil {
			return refuse("The complete review publisher is unavailable.")
		}
		link, err := opts.ResearchReporter.publishRepairReview(ctx, c)
		if err != nil {
			return refuse("Complete review remains pending: " + err.Error())
		}
		return refuse("Complete retained repair review: " + link)
	}
	if c.Resolution != "pending" {
		if c.Resolution == cmd.RepairAction && c.ResultCommit == "" && cmd.RepairAction == "accept" {
			return validateResolveResult("apply_repair", "recorded owner authorization", st)
		}
		return refuse("This candidate already has a terminal owner decision.")
	}
	if cmd.RepairAction == "accept" {
		if head, err := h.b.Delivery.Head(ctx); err != nil || head != c.BaseCommit {
			return refuse("The reviewed candidate base has drifted.")
		}
		w, _, err := openValidateWorktree(h.b)
		if err != nil {
			return refuse(err.Error())
		}
		snapshot, err := candidateSnapshot(c)
		if err != nil {
			return refuse(err.Error())
		}
		if err = w.VerifyRepairCandidate(ctx, snapshot); err != nil {
			return refuse(err.Error())
		}
		if !c.Eligible || !c.ReviewComplete {
			return refuse("Acceptance requires complete unchanged review delivery and eligible measured changes; frozen criteria cannot be adopted.")
		}
		scope, err := h.b.repairScope(ctx, opts, req)
		if err != nil || scope != c.ScopeRevision {
			return refuse("The frozen scope changed; this candidate cannot be adopted.")
		}
		if !profileHasAction(h.b.Profile, graphexec.ValidateRepairApplyAction) {
			return refuse("This graph does not declare exact repair adoption.")
		}
	}
	_, err = opts.DB.ResolveRepairCandidate(ctx, c.ID, store.RepairCandidateDecision{Action: cmd.RepairAction, ReviewDigest: cmd.ReviewDigest, ReportRevision: cmd.Revision, Principal: fmt.Sprint(wait.ReplyAuthorID), SourceID: fmt.Sprint(wait.ReplyID), At: h.b.now(opts)})
	if err != nil {
		return refuse("Repair decision refused: " + err.Error())
	}
	outcome := "wait"
	if cmd.RepairAction == "accept" {
		outcome = "apply_repair"
	}
	return validateResolveResult(outcome, "repair decision recorded", st)
}

func profileHasAction(p *graphexec.Profile, action string) bool {
	for _, n := range p.Nodes {
		if n.Action == action {
			return true
		}
	}
	return false
}

// ValidateRepairApplyActivity is the sole tree mutation following an exact
// authenticated owner decision. Its graph edge reruns the suite and reviewers.
type ValidateRepairApplyActivity struct{ b ValidateFlowBindings }

// Execute reconciles one authorized exact-tree application and records its result.
func (h ValidateRepairApplyActivity) Execute(ctx context.Context, opts Options, req ActivityRequest) (ActivityResult, error) {
	if _, err := loadValidateStep(ctx, opts, req, h.b.Profile, graphexec.ValidateRepairApplyAction); err != nil {
		return ActivityResult{}, err
	}
	candidates, err := opts.DB.ListRepairCandidates(ctx, req.InstanceID)
	if err != nil {
		return ActivityResult{}, err
	}
	var c store.RepairCandidate
	for _, v := range candidates {
		if v.Resolution == "accept" && v.ResultCommit == "" {
			c = v
		}
	}
	if c.ID == "" {
		for i := len(candidates) - 1; i >= 0; i-- {
			v := candidates[i]
			if v.Resolution == "accept" && v.ResultCommit != "" {
				w, session, e := openValidateWorktree(h.b)
				if e != nil {
					return ActivityResult{}, e
				}
				snapshot, e := candidateSnapshot(v)
				if e != nil {
					return ActivityResult{}, e
				}
				if e = verifyCandidateEvidence(v); e != nil {
					return ActivityResult{}, e
				}
				if e = w.VerifyRepairCandidate(ctx, snapshot); e != nil {
					return ActivityResult{}, e
				}
				head, e := w.RevParse(ctx, "HEAD", git.RevParseOptions{})
				if e != nil {
					return ActivityResult{}, e
				}
				if head != v.ResultCommit {
					return validateJSONResult("blocked", map[string]string{"reason": "Completed adoption branch drifted"})
				}
				changes, e := w.Status(ctx)
				if e != nil {
					return ActivityResult{}, e
				}
				for _, change := range changes {
					if session == "" || change.Path != session && !strings.HasPrefix(change.Path, strings.TrimSuffix(session, "/")+"/") {
						return validateJSONResult("blocked", map[string]string{"reason": "Completed adoption checkout drifted"})
					}
				}

				return validateJSONResult("applied", map[string]string{"candidate": v.ID, "result_commit": v.ResultCommit})
			}
		}
		return validateJSONResult("blocked", map[string]string{"reason": "No unapplied authorized candidate"})
	}
	block := func(cause error) (ActivityResult, error) {
		return validateJSONResult("blocked", map[string]string{"reason": cause.Error()})
	}
	if err := verifyCandidateEvidence(c); err != nil {
		return block(err)
	}
	scope, err := h.b.repairScope(ctx, opts, req)
	if err != nil {
		return block(err)
	}
	if scope != c.ScopeRevision {
		return block(errors.New("frozen scope drift"))
	}
	w, session, err := openValidateWorktree(h.b)
	if err != nil {
		return block(err)
	}
	snapshot, err := candidateSnapshot(c)
	if err != nil {
		return block(err)
	}
	if err = w.VerifyRepairCandidate(ctx, snapshot); err != nil {
		return block(err)
	}
	journal, err := opts.DB.ListRepairCandidateJournal(ctx, c.ID)
	if err != nil {
		return ActivityResult{}, err
	}
	intended := ""
	for _, j := range journal {
		if j.Phase == "apply" && j.State == "intent" {
			intended = j.ExpectedResult
		}
	}
	if intended == "" {
		if h.b.RepairAuthor == nil {
			return block(errors.New("repair author binding unavailable"))
		}
		author, err := h.b.RepairAuthor(ctx)
		if err != nil {
			return block(err)
		}
		intended, err = w.PrepareRepairAdoption(ctx, snapshot, author)
		if err != nil {
			return block(err)
		}
		if err = opts.DB.JournalRepairCandidate(ctx, c.ID, store.RepairCandidateJournal{InstanceID: c.InstanceID, RoundID: c.RoundID, Phase: "apply", State: "intent", ExpectedBase: c.BaseCommit, ExpectedResult: intended, At: h.b.now(opts)}); err != nil {
			return ActivityResult{}, err
		}
	}
	if err = w.ApplyRepairAdoption(ctx, snapshot, intended, session); err != nil {
		return block(err)
	}
	if err = opts.DB.JournalRepairCandidate(ctx, c.ID, store.RepairCandidateJournal{InstanceID: c.InstanceID, RoundID: c.RoundID, Phase: "apply", State: "complete", ExpectedBase: c.BaseCommit, ExpectedResult: intended, ResultCommit: intended, At: h.b.now(opts)}); err != nil {
		return ActivityResult{}, err
	}
	return validateJSONResult("applied", map[string]string{"candidate": c.ID, "result_commit": intended})
}

// candidateMeasurementComplete treats opaque bytes as unknown analysis, never
// safe absence of deleted tests or skipped assertions.
func candidateMeasurementComplete(s git.RepairSnapshot) bool {
	if !utf8.ValidString(s.Patch) {
		return false
	}
	for _, c := range s.Changes {
		if c.OldMode == "160000" || c.NewMode == "160000" || strings.ContainsAny(c.Path, " \t\n\r\"") {
			return false
		}
	}
	return !strings.Contains(s.Patch, "GIT binary patch") && !strings.Contains(s.Patch, "Binary files ")
}
