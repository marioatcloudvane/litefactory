// Package doctor owns issue-scoped diagnosis independently of workflow execution.
package doctor

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cloudvane/litefactory/internal/config"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
)

// PromptVersion identifies the fixed diagnosis authority.
const PromptVersion = "doctor-diagnosis-v1"

// GuideVersion identifies the separately maintained factory guide.
const GuideVersion = "doctor-field-guide-v1"

// ErrIsolationUnavailable refuses unverified adapters without a weaker fallback.
var ErrIsolationUnavailable = errors.New("doctor provider isolation is not verified; diagnosis unavailable; input retained")

// Service shares durable history and control between CLI and future forge intake.
// No provider is enabled until its tool isolation has authentic evidence.
type Service struct {
	RestartRoute      RestartRoute
	RepairEnvironment RepairEnvironment
	RepairRunner      RepairRunner
	mu                sync.Mutex
	active            map[string]activeInvocation
	DB                *store.DB
	Paths             config.Paths
	Config            *config.Config
	Redactor          *redact.Redactor
	Runner            Runner
}

// DiagnoseRequest carries a trusted explicit target and the owner's instruction.
type DiagnoseRequest struct {
	Target       naming.Ref
	Instructions string
	SourceID     string
}

// Conversation is bounded display data; complete history remains in the store.
type Conversation struct {
	ProposalID      int64                    `json:"proposal_id,omitempty"`
	Repair          *store.DoctorRepair      `json:"repair,omitempty"`
	RepairSteps     []store.DoctorRepairStep `json:"repair_steps,omitempty"`
	Restart         *store.DoctorRestart     `json:"restart,omitempty"`
	CurrentProposal []RepairStep             `json:"current_proposal,omitempty"`
	Conversation    store.DoctorConversation `json:"conversation"`
	Attempts        []store.DoctorAttempt    `json:"attempts"`
	HistoryWindow   string                   `json:"history_window"`
	Diagnosis       *Diagnosis               `json:"diagnosis,omitempty"`
	ApplyAvailable  bool                     `json:"apply_available"`
}

// Diagnose preserves input and fresh evidence even when the provider refuses.
func (s *Service) Diagnose(ctx context.Context, r DiagnoseRequest) (out Conversation, err error) {
	r.Target = canonicalTarget(r.Target)
	if err = s.validate(ctx, r.Target); err != nil {
		return out, err
	}
	if strings.TrimSpace(r.Instructions) == "" || len(r.Instructions) > 32768 {
		return out, errors.New("doctor requires instructions of 1–32768 bytes")
	}
	body, e := s.Redactor.Apply(ctx, r.Instructions, 65536)
	if e != nil {
		return out, e
	}
	if body.Truncated() {
		return out, errors.New("doctor instructions exceed redaction budget")
	}
	unlock, e := s.lock(r.Target)
	if e != nil {
		return out, e
	}
	defer func() { err = errors.Join(err, unlock()) }()
	if e = s.recoverResult(ctx, r.Target); e != nil {
		return out, e
	}
	id := randomID()
	if r.SourceID == "" {
		r.SourceID = "cli:" + id
	}
	c, e := s.DB.BeginDoctorAttempt(ctx, r.Target.String(), store.DoctorAttempt{ID: id, SourceID: r.SourceID, OwnerText: body.String(), PromptVersion: PromptVersion, GuideVersion: GuideVersion})
	if e != nil {
		return out, e
	}
	failure := func(cause error) (Conversation, error) {
		state := "failed"
		if ctx.Err() != nil {
			state = "interrupted"
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		safe, redactionErr := s.Redactor.Apply(cleanup, cause.Error(), 8192)
		reason := "Diagnosis failed; error redaction unavailable"
		if redactionErr == nil {
			reason = safe.String()
		}
		finishErr := s.DB.FailDoctorAttempt(cleanup, id, state, reason)
		if errors.Is(finishErr, store.ErrDoctorFenced) {
			finishErr = nil
		}
		saved, readErr := s.ReadConversation(cleanup, r.Target)
		return saved, &safeFailure{cause: errors.Join(cause, redactionErr, finishErr, readErr), message: reason}
	}
	evidence, e := s.DB.ReadDoctorEvidence(ctx, r.Target)
	if e != nil {
		return failure(e)
	}
	s.collectLocal(ctx, r.Target, &evidence)
	if e = s.sanitizeEvidence(ctx, &evidence); e != nil {
		return failure(e)
	}
	provider, model, _, isolationErr, e := s.selectProvider(ctx, config.Kind(evidence.Provider))
	if e != nil {
		return failure(e)
	}
	encoded, e := json.Marshal(evidence)
	if e != nil {
		return failure(e)
	}
	safe, e := s.Redactor.Apply(ctx, string(encoded), 131072)
	if e != nil {
		return failure(e)
	}
	if safe.Truncated() {
		return failure(errors.New("doctor evidence exceeds redaction budget"))
	}
	if e = s.DB.PrepareDoctorAttempt(ctx, store.DoctorAttempt{ID: id, ConversationID: c.ID, Provider: string(provider), ConfiguredModel: model, Evidence: safe.String()}); e != nil {
		return failure(e)
	}
	if isolationErr != nil {
		return failure(isolationErr)
	}
	return s.invoke(ctx, r, c, id, string(provider), model, evidence, body.String(), failure)
}

// selectProvider tries the issue's own agent first, then every configured
// agent in config order, each at its flagship model. The first one whose
// isolation is available wins. When none is, it still returns the first
// candidate with a flagship mapping so the attempt is recorded, together with
// that candidate's isolation refusal.
func (s *Service) selectProvider(ctx context.Context, preferred config.Kind) (provider config.Kind, model, observation string, isolationErr, err error) {
	candidates := s.Config.AgentOrder()
	if preferred != "" {
		candidates = append([]config.Kind{preferred}, candidates...)
	}
	seen := map[config.Kind]bool{}
	for _, kind := range candidates {
		if seen[kind] {
			continue
		}
		seen[kind] = true
		m, ok := s.Config.Tiers.ResolveModel(kind, config.TierFlagship)
		if !ok || strings.TrimSpace(m) == "" {
			continue
		}
		refusal := ErrIsolationUnavailable
		evidence := ""
		if s.Runner != nil {
			evidence, refusal = s.Runner.IsolationEvidence(ctx, string(kind))
			if refusal == nil && evidence == "" {
				refusal = ErrIsolationUnavailable
			}
		}
		if refusal == nil {
			return kind, m, evidence, nil, nil
		}
		if provider == "" {
			provider, model, isolationErr = kind, m, refusal
		}
	}
	if provider != "" {
		return provider, model, "", isolationErr, nil
	}
	if len(seen) == 0 {
		return "", "", "", nil, errors.New("doctor has no configured provider")
	}
	return "", "", "", nil, errors.New("doctor provider has no flagship mapping")
}

func (s *Service) validate(ctx context.Context, ref naming.Ref) error {
	if err := ref.Validate(); err != nil {
		return err
	}
	if ref.Forge != naming.ForgeGitHub {
		return errors.New("doctor supports GitHub issue targets only")
	}
	if s.DB == nil || s.Config == nil || s.Redactor == nil {
		return errors.New("doctor service dependencies are missing")
	}
	return s.DB.CheckDoctorSchema(ctx)
}

// ReadConversation redacts again at the display boundary and never starts work.
func (s *Service) ReadConversation(ctx context.Context, ref naming.Ref) (Conversation, error) {
	ref = canonicalTarget(ref)
	var out Conversation
	if err := s.validate(ctx, ref); err != nil {
		return out, err
	}
	c, err := s.DB.ReadDoctorConversation(ctx, ref.String())
	if err != nil {
		return out, err
	}
	attempts, err := s.DB.DoctorAttempts(ctx, c.ID)
	if err != nil {
		return out, err
	}
	for i := range attempts {
		for _, field := range []*string{&attempts[i].OwnerText, &attempts[i].Evidence, &attempts[i].Failure} {
			body, err := s.Redactor.Apply(ctx, *field, 131072)
			if err != nil {
				return out, err
			}
			*field = body.String()
		}
	}
	diagnosis, err := s.currentDiagnosis(ctx, c.ID)
	if err != nil {
		return out, err
	}
	proposalID, proposal, err := s.proposalForPresentation(ctx, c.ID)
	if err != nil {
		return out, err
	}
	out = Conversation{ProposalID: proposalID, Conversation: c, Attempts: attempts, Diagnosis: diagnosis, CurrentProposal: proposal, HistoryWindow: "Latest 30 attempts in this generation; earlier history remains retained", ApplyAvailable: false}
	repair, repairErr := s.DB.ReadDoctorRepair(ctx, ref.String())
	if repairErr != nil && !errors.Is(repairErr, store.ErrNotFound) {
		return out, repairErr
	}
	if repairErr == nil {
		body, e := s.Redactor.Apply(ctx, repair.Failure, 8192)
		if e != nil {
			return out, e
		}
		repair.Failure = body.String()
		out.Repair = &repair
		out.RepairSteps, err = s.DB.DoctorRepairSteps(ctx, repair.ID)
		if err != nil {
			return out, err
		}
		for i := range out.RepairSteps {
			for _, field := range []*string{&out.RepairSteps[i].After, &out.RepairSteps[i].Before, &out.RepairSteps[i].BackupReference} {
				body, e := s.Redactor.Apply(ctx, *field, 65536)
				if e != nil {
					return out, e
				}
				*field = body.String()
			}
		}
		restart, e := s.DB.ReadDoctorRestart(ctx, repair.ID)
		if e != nil && !errors.Is(e, store.ErrNotFound) {
			return out, e
		}
		if e == nil {
			body, e := s.Redactor.Apply(ctx, restart.Report, 65536)
			if e != nil {
				return out, e
			}
			restart.Report = body.String()
			out.Restart = &restart
		}
	}
	return out, nil
}

// Stop requests cancellation of issue work and fences pending doctor results.
func (s *Service) Stop(ctx context.Context, ref naming.Ref) error {
	_, err := s.StopIssue(ctx, ref)
	return err
}

// StopIssue retains the local stop source without admitting a GitHub command.
func (s *Service) StopIssue(ctx context.Context, ref naming.Ref) (store.OwnerStop, error) {
	var stop store.OwnerStop
	ref = canonicalTarget(ref)
	if err := s.validate(ctx, ref); err != nil {
		return stop, err
	}
	var err error
	stop, err = s.DB.RequestOwnerStop(ctx, ref.String())
	if err != nil {
		return stop, err
	}
	if err := s.stopRepair(ctx, ref, "Owner stopped repair"); err != nil {
		return stop, err
	}
	if err := s.DB.StopDoctorConversation(ctx, ref.String()); err != nil {
		return stop, err
	}
	s.mu.Lock()
	active := s.active[ref.String()]
	s.mu.Unlock()
	if active.cancel != nil {
		active.cancel()
	}
	return stop, nil
}

func randomID() string { return rand.Text() }

func (s *Service) lock(ref naming.Ref) (func() error, error) {
	dir := s.Paths.DoctorLocksDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	digest := sha256.Sum256([]byte(ref.String()))
	path := filepath.Join(dir, hex.EncodeToString(digest[:])+".lock")
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		closeErr := f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errors.Join(store.ErrDoctorBusy, closeErr)
		}
		return nil, errors.Join(err, closeErr)
	}
	return f.Close, nil
}

// Target validates the explicit repository/issue syntax shared by CLI controls.
func Target(repository string, issue int) (naming.Ref, error) {
	owner, repo, ok := strings.Cut(repository, "/")
	if !ok {
		return naming.Ref{}, errors.New("doctor repository must be owner/name")
	}
	ref := naming.Ref{Forge: naming.ForgeGitHub, Owner: strings.ToLower(owner), Repo: strings.ToLower(repo), Issue: issue}
	if err := ref.Validate(); err != nil {
		return naming.Ref{}, fmt.Errorf("doctor target: %w", err)
	}
	return ref, nil
}

// safeFailure keeps error identity for callers while only disclosing redacted text.
type safeFailure struct {
	cause   error
	message string
}

func (e *safeFailure) Error() string { return e.message }
func (e *safeFailure) Unwrap() error { return e.cause }

func (s *Service) sanitizeEvidence(ctx context.Context, e *store.DoctorEvidence) error {
	for i := range e.Records {
		r := &e.Records[i]
		for _, field := range []*string{&r.ID, &r.State, &r.Content} {
			body, err := s.Redactor.Apply(ctx, *field, 4096)
			if err != nil {
				return err
			}
			if body.Truncated() {
				e.Gaps = appendUnique(e.Gaps, r.Kind+" excerpts truncated to disclosure budget")
			}
			*field = body.String()
		}
	}
	for {
		data, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if len(data) <= 98304 {
			return nil
		}
		if len(e.Records) == 0 {
			return errors.New("doctor evidence metadata exceeds total byte budget")
		}
		e.Records = e.Records[:len(e.Records)-1]
		e.Gaps = appendUnique(e.Gaps, "Evidence records omitted to fit total 96 KiB byte budget")
	}
}

func (s *Service) recoverResult(ctx context.Context, ref naming.Ref) error {
	attempt, requestID, err := s.DB.PendingDoctorResult(ctx, ref.String())
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	var evidence store.DoctorEvidence
	if err := json.Unmarshal([]byte(attempt.Evidence), &evidence); err != nil {
		return errors.New("retained doctor evidence cannot be recovered safely")
	}
	results, err := BindResults(ctx, s.DB, attempt.ID, evidence, s.Redactor)
	if err != nil {
		return err
	}
	if err := results.adopt(ctx, requestID, true); err != nil {
		return err
	}
	return s.DB.InterruptDoctorMeters(ctx, attempt.ID)
}

func canonicalTarget(ref naming.Ref) naming.Ref {
	ref.Owner = strings.ToLower(ref.Owner)
	ref.Repo = strings.ToLower(ref.Repo)
	return ref
}

// activeInvocation binds cancellation to the generation that actually launched.
type activeInvocation struct {
	cancel     context.CancelFunc
	generation int64
}

// CancelStopped interrupts only an invocation whose generation was already fenced.
// Replayed GitHub stop input must not stop a newer active conversation.
func (s *Service) CancelStopped(ctx context.Context, ref naming.Ref) error {
	ref = canonicalTarget(ref)
	unlock, err := s.repairEffectLock(ctx, ref)
	if err != nil {
		return err
	}
	fenceErr := s.DB.FenceStoppedDoctorRepair(ctx, ref.String())
	if err = errors.Join(fenceErr, unlock()); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	active := s.active[ref.String()]
	if active.cancel == nil {
		return nil
	}
	c, err := s.DB.ReadDoctorConversation(ctx, ref.String())
	if err != nil {
		return err
	}
	if c.Generation > active.generation || (c.Generation == active.generation && c.State == "stopped") {
		active.cancel()
	}
	return nil
}

// StopSummary identifies the work selected by a settled stop request.
func StopSummary(stop store.OwnerStop) string {
	switch {
	case stop.RunID != 0 && stop.Conversation:
		return "Workflow and doctor conversation stopped by the owner. Worktree and history retained."
	case stop.RunID != 0:
		return "Workflow stopped by the owner. Worktree retained."
	case stop.Conversation:
		return "Doctor conversation stopped by the owner. History retained."
	default:
		return "Nothing was running."
	}
}

// StopRunSummary keeps save/publication uncertainty visible after execution ends.
func StopRunSummary(run store.Run) string {
	workflow := run.Workflow
	if workflow == "" {
		workflow = run.Mode
	}
	publication := run.Publication
	if publication == "" {
		publication = "Saving was not confirmed; retained work needs review."
	}
	return fmt.Sprintf("Run %d (%s). %s", run.Ordinal, workflow, publication)
}
