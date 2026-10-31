package owneraction

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// Shared service refusals never disclose credentials or storage errors.
var (
	ErrIntegrity          = errors.New("protected owner action facts changed; request a fresh proposal")
	ErrUnavailable        = errors.New("owner action unavailable")
	ErrRequestConflict    = errors.New("request key already belongs to different action facts")
	ErrDiscordUnavailable = errors.New("discord owner approval is not configured")
	ErrDiscordDestination = errors.New("choose a configured Discord approval channel")
)

// Service owns shared policy; daemon composition supplies time and domain effects.
type Service struct {
	db       *store.DB
	domain   Domain
	clock    func() time.Time
	mu       sync.Mutex
	owner    string
	channels map[string]bool
	delivery Delivery
	lifetime context.Context
	running  map[string]bool
}

// New composes the shared service with factory time and existing domain handlers.
func New(db *store.DB, domain Domain, clock func() time.Time) *Service {
	return &Service{db: db, domain: domain, clock: clock, channels: map[string]bool{}, running: map[string]bool{}}
}

// SetDiscord supplies the configured pairing; adapters cannot choose another owner.
func (s *Service) SetDiscord(owner string, channels []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.owner = owner
	s.channels = map[string]bool{}
	for _, c := range channels {
		s.channels[c] = true
	}
}

// SetLifetime binds daemon progression to its serving lifetime. Request cancellation
// never owns admitted work, but daemon shutdown interrupts possible effects truthfully.
func (s *Service) SetLifetime(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lifetime = ctx
}

// SetDelivery supplies the configured Discord sender.
func (s *Service) SetDelivery(d Delivery) { s.mu.Lock(); defer s.mu.Unlock(); s.delivery = d }
func (s *Service) now() time.Time         { return s.clock().UTC() }
func principal(p Principal) store.OwnerActionPrincipal {
	return store.OwnerActionPrincipal{OwnerID: p.OwnerID, OwnerGeneration: p.OwnerGeneration, Transport: p.Transport, ClientID: p.ClientID, GrantID: p.GrantID, ChannelID: p.ChannelID, DiscordOwnerID: p.DiscordOwnerID}
}
func principalFrom(p store.OwnerActionPrincipal) Principal {
	return Principal{OwnerID: p.OwnerID, OwnerGeneration: p.OwnerGeneration, Transport: p.Transport, ClientID: p.ClientID, GrantID: p.GrantID, ChannelID: p.ChannelID, DiscordOwnerID: p.DiscordOwnerID}
}

func (s *Service) pair(p Principal, binding bool) (Principal, error) {
	if p.OwnerID == "" || p.OwnerGeneration < 1 || (p.Transport != "mcp" && p.Transport != "discord") {
		return p, store.ErrConnectorOwner
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.Transport == "mcp" && (p.ClientID == "" || p.GrantID == "") {
		return p, store.ErrConnectorInvalid
	}
	if p.Transport == "discord" {
		if p.DiscordOwnerID != s.owner || !s.channels[p.ChannelID] || s.owner == "" {
			return p, store.ErrConnectorOwner
		}
	}
	if binding {
		if s.owner == "" || len(s.channels) == 0 {
			return p, ErrDiscordUnavailable
		}
		if p.ChannelID == "" && len(s.channels) == 1 {
			for c := range s.channels {
				p.ChannelID = c
			}
		}
		if !s.channels[p.ChannelID] {
			return p, ErrDiscordDestination
		}
		p.DiscordOwnerID = s.owner
	}
	return p, nil
}
func digest(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// Propose resolves implicit selections once; MCP availability starts at response
// readiness, while Discord must explicitly acknowledge confirmed delivery.
func (s *Service) Propose(ctx context.Context, p Principal, inv Invocation, key string) (Proposal, error) {
	tier, scope, err := Classify(inv)
	if err != nil {
		return Proposal{}, err
	}
	p, err = s.pair(p, tier == Binding)
	if err != nil {
		return Proposal{}, err
	}
	if key == "" || len(key) > 200 || len(inv.Arguments) > 32 {
		return Proposal{}, errors.New("invalid bounded action request")
	}
	for k, v := range inv.Arguments {
		if len(k) > 100 || len(v) > 65536 {
			return Proposal{}, errors.New("invalid bounded action argument")
		}
	}
	inv.Arguments = maps.Clone(inv.Arguments)
	requestDigest, err := digest(struct {
		Invocation Invocation
		Channel    string
	}{inv, p.ChannelID})
	if err != nil {
		return Proposal{}, err
	}
	old, err := s.db.OwnerActionByRequest(ctx, principal(p), key)
	if err == nil {
		if err = s.db.CheckOwnerActionAuthority(ctx, principal(p), old, s.now()); err != nil {
			return Proposal{}, err
		}
		if old.RequestDigest != requestDigest {
			return Proposal{}, ErrRequestConflict
		}
		return s.proposed(ctx, old)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return Proposal{}, err
	}
	if s.domain == nil {
		return Proposal{}, ErrUnavailable
	}
	at := s.now()
	if err = s.db.CheckOwnerActionAuthority(ctx, principal(p), store.OwnerAction{Principal: principal(p), Scope: scope}, at); err != nil {
		return Proposal{}, err
	}
	ctx = store.WithOwnerActionResolution(ctx, principal(p), scope, s.clock)
	facts, err := s.domain.Resolve(ctx, p, inv, at)
	if err != nil {
		return Proposal{}, err
	}
	// Domain normalization cannot replace the classified action or choose its tier.
	normalizedTier, normalizedScope, err := Classify(facts.Invocation)
	if err != nil || normalizedTier != tier || normalizedScope != scope || facts.Invocation.Command != inv.Command {
		return Proposal{}, errors.New("resolved action policy changed")
	}
	if facts.TargetKind == "" || facts.TargetID == "" || facts.TargetRevision == "" || facts.Summary == "" || !json.Valid([]byte(facts.Payload)) {
		return Proposal{}, errors.New("action facts lack an exact target")
	}
	if facts.Estimate == "" {
		facts.Estimate = "unknown"
	}
	if scope == "repository:actions" && facts.RepositoryID < 1 {
		return Proposal{}, errors.New("repository action requires a registered repository")
	}
	encoded, err := json.Marshal(facts)
	if err != nil {
		return Proposal{}, err
	}
	contentDigest, err := digest(struct {
		Facts   Facts
		Payload string
	}{facts, facts.Payload})
	if err != nil {
		return Proposal{}, err
	}
	var random [24]byte
	if _, err = rand.Read(random[:]); err != nil {
		return Proposal{}, err
	}
	id := hex.EncodeToString(random[:])
	record := store.OwnerAction{ID: id, PolicyVersion: PolicyVersion, Principal: principal(p), RequestKey: key, RequestDigest: requestDigest, Facts: string(encoded), Payload: facts.Payload, Digest: contentDigest, RepositoryID: facts.RepositoryID, RepositoryRef: facts.RepositoryRef, Scope: scope, Tier: tier, State: "awaiting_confirmation", CreatedAt: at, ExpiresAt: at.Add(15 * time.Minute), ExecutionID: "owner-action:" + id, ApprovalChannel: p.ChannelID}

	if err = s.db.CreateOwnerAction(ctx, record); err != nil {
		// Concurrent identical request keys return the single winning proposal.
		winner, e := s.db.OwnerActionByRequest(ctx, principal(p), key)
		if e == nil && winner.RequestDigest == requestDigest {
			if e = s.db.CheckOwnerActionAuthority(ctx, principal(p), winner, s.now()); e != nil {
				return Proposal{}, e
			}
			return s.proposed(ctx, winner)
		}
		return Proposal{}, err
	}
	return s.proposed(ctx, record)
}

func (s *Service) proposed(ctx context.Context, record store.OwnerAction) (Proposal, error) {
	if record.Principal.Transport == "mcp" && record.AvailableAt.IsZero() && record.State == "awaiting_confirmation" {
		if err := s.Available(ctx, record.ID, ""); err != nil {
			return Proposal{}, err
		}
		current, err := s.db.ReadOwnerAction(ctx, record.ID)
		if err != nil {
			return Proposal{}, err
		}
		record = current
	}
	return s.present(record)
}

func (s *Service) present(p store.OwnerAction) (Proposal, error) {
	var facts Facts
	if err := json.Unmarshal([]byte(p.Facts), &facts); err != nil {
		return Proposal{}, err
	}
	facts.Payload = p.Payload
	if p.State == "awaiting_confirmation" || p.State == "awaiting_discord" || p.State == "admitted" || p.State == "executing" || p.State == "uncertain" {
		computed, err := digest(struct {
			Facts   Facts
			Payload string
		}{facts, p.Payload})
		if err != nil {
			return Proposal{}, err
		}
		if computed != p.Digest {
			return Proposal{}, ErrIntegrity
		}
	}
	var result Result
	if p.Result != "" {
		if err := json.Unmarshal([]byte(p.Result), &result); err != nil {
			return Proposal{}, err
		}
	}
	return Proposal{ID: p.ID, Principal: principalFrom(p.Principal), Facts: facts, Tier: p.Tier, State: p.State, Reason: p.Reason, CreatedAt: p.CreatedAt, AvailableAt: p.AvailableAt, ConfirmNotBefore: p.ConfirmNotBefore, ExpiresAt: p.ExpiresAt, CompletedAt: p.CompletedAt, ExecutionID: p.ExecutionID, ProposalMessage: p.ProposalMessage, ApprovalChannel: p.ApprovalChannel, ApprovalMessage: p.ApprovalMessage, DeliveryState: p.DeliveryState, DeliveryRetryAt: p.DeliveryRetryAt, DeliveryAttempts: p.DeliveryAttempts, Result: result}, nil
}

func (s *Service) authorizeRead(ctx context.Context, p Principal, record store.OwnerAction, at time.Time) error {
	if p.OwnerID != record.Principal.OwnerID {
		return store.ErrNotFound
	}
	// A current action grant can inspect owner history across originating clients.
	current := record
	current.Principal = principal(p)
	if p.Transport == "discord" {
		current.RepositoryID = 0
		current.RepositoryRef = ""
	}
	return s.db.CheckOwnerActionAuthority(ctx, principal(p), current, at)
}

// Inspect returns exact facts and durable decisions under current read authority.
func (s *Service) Inspect(ctx context.Context, p Principal, id string) (Proposal, error) {
	p, err := s.pair(p, false)
	if err != nil {
		return Proposal{}, err
	}
	record, err := s.db.ReadOwnerAction(ctx, id)
	if err != nil {
		return Proposal{}, err
	}
	at := s.now()
	if err = s.authorizeRead(ctx, p, record, at); err != nil {
		return Proposal{}, err
	}
	if err = s.db.ExpireOwnerAction(ctx, id, at); err != nil {
		return Proposal{}, err
	}
	record, err = s.db.ReadOwnerAction(ctx, id)
	if err != nil {
		return Proposal{}, err
	}
	out, err := s.present(record)
	if err != nil {
		return Proposal{}, err
	}
	attempts, err := s.db.OwnerActionAttempts(ctx, id)
	if err != nil {
		return Proposal{}, err
	}
	for _, a := range attempts {
		out.Attempts = append(out.Attempts, Attempt{ID: a.ID, ReceivedAt: a.ReceivedAt, Method: a.Method, Principal: a.Principal, Decision: a.Decision, State: a.State, Evidence: a.Evidence})
	}
	return out, nil
}

// Confirm records a separate conversational decision without executing inline.
func (s *Service) Confirm(ctx context.Context, p Principal, id string) (Proposal, error) {
	return s.control(ctx, p, id, "confirm", "")
}

// Cancel ends only pending work; admitted work retains its execution identity.
func (s *Service) Cancel(ctx context.Context, p Principal, id string) (Proposal, error) {
	return s.control(ctx, p, id, "cancel", "")
}
func (s *Service) control(ctx context.Context, p Principal, id, method, evidence string) (Proposal, error) {
	p, err := s.pair(p, false)
	if err != nil {
		return Proposal{}, err
	}
	record, err := s.db.ReadOwnerAction(ctx, id)
	if err != nil {
		return Proposal{}, err
	}
	if p.OwnerID != record.Principal.OwnerID {
		return Proposal{}, store.ErrNotFound
	}
	if method == "cancel" && p.Transport == "discord" {
		method = "cancel_discord"
	}
	at := s.now()
	// No domain access before authenticated origin checks; attempts stay durable.
	origin := p.Transport == record.Principal.Transport && (p.Transport != "mcp" || p.ClientID == record.Principal.ClientID && p.GrantID == record.Principal.GrantID)
	if method == "confirm" && origin && record.State == "awaiting_confirmation" && !record.AvailableAt.IsZero() && !at.Before(record.ConfirmNotBefore) && at.Before(record.ExpiresAt) {
		if err = s.db.CheckOwnerActionAuthority(ctx, principal(p), record, at); err == nil {
			proposal, e := s.present(record)
			if e != nil {
				return Proposal{}, e
			}
			if e = s.domain.Recheck(ctx, proposal.Principal, proposal.Facts, at); e != nil {
				method = "invalidate"
				evidence = "stale_target_repropose"
			}
		}
	}
	changed, err := s.db.MutateOwnerAction(ctx, principal(p), id, method, evidence, s.now())
	if err != nil {
		return Proposal{}, err
	}
	return s.present(changed)
}

// Available records confirmed transport availability once.
func (s *Service) Available(ctx context.Context, id, message string) error {
	return s.db.MakeOwnerActionAvailable(ctx, id, message, s.now())
}

// Reaction admits only the configured owner tap on a confirmed factory message.
func (s *Service) Reaction(ctx context.Context, r Reaction) (Proposal, error) {
	record, err := s.db.OwnerActionByApprovalMessage(ctx, r.ChannelID, r.MessageID)
	if err != nil {
		return Proposal{}, err
	}
	s.mu.Lock()
	owner := s.owner
	allowed := s.channels[r.ChannelID]
	s.mu.Unlock()
	valid := allowed && owner != "" && r.UserID == owner && !r.Bot && r.Emoji == "✅" && r.EmojiID == "" && record.ApprovalMessage == r.MessageID && record.ApprovalChannel == r.ChannelID && record.Principal.DiscordOwnerID == owner
	actor := Principal{OwnerID: record.Principal.OwnerID, OwnerGeneration: record.Principal.OwnerGeneration, Transport: "discord", ChannelID: r.ChannelID, DiscordOwnerID: r.UserID}
	at := s.now()
	if r.ReceivedAt.IsZero() {
		r.ReceivedAt = at
	}
	if r.ReceivedAt.After(at) {
		return Proposal{}, store.ErrConnectorInvalid
	}
	evidence, _ := json.Marshal(r)
	if !valid {
		return s.refusedReaction(ctx, actor, record, string(evidence), at)
	}
	proposal, err := s.present(record)
	if err != nil {
		return Proposal{}, err
	}
	if err = s.db.CheckOwnerActionAuthority(ctx, principal(actor), record, at); err == nil {
		if err = s.domain.Recheck(ctx, proposal.Principal, proposal.Facts, at); err != nil {
			changed, e := s.db.MutateOwnerAction(ctx, principal(actor), record.ID, "invalidate", "stale_target_repropose", s.now())
			if e != nil {
				return Proposal{}, e
			}
			return s.present(changed)
		}
	}
	changed, err := s.db.MutateOwnerAction(ctx, principal(actor), record.ID, "reaction", string(evidence), s.now())
	if err != nil {
		return Proposal{}, err
	}
	return s.present(changed)
}
func (s *Service) refusedReaction(ctx context.Context, p Principal, record store.OwnerAction, evidence string, at time.Time) (Proposal, error) {
	if err := s.db.RecordOwnerActionAttempt(ctx, record.ID, principal(p), "reaction", "unauthorized_reaction", evidence, at); err != nil {
		return Proposal{}, err
	}
	return Proposal{}, store.ErrConnectorOwner
}

// Tick is daemon-owned progression; it does not automatically retry early consent.
func (s *Service) Tick(ctx context.Context) error {
	ids, err := s.db.ReadyOwnerActions(ctx, 1000, s.now())
	if err != nil {
		return err
	}
	var errs []error
	for _, id := range ids {
		if _, e := s.Step(ctx, id); e != nil {
			errs = append(errs, e)
		}
	}
	if e := s.db.PurgeOwnerActionHistory(ctx, s.now()); e != nil {
		errs = append(errs, e)
	}
	return errors.Join(errs...)
}

// Step claims durable work before effects and uses a bounded daemon-owned context.
func (s *Service) Step(ctx context.Context, id string) (Proposal, error) {
	ctx, stop := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Minute)
	defer stop()
	s.mu.Lock()
	if s.running[id] {
		s.mu.Unlock()
		record, err := s.db.ReadOwnerAction(ctx, id)
		if err != nil {
			return Proposal{}, err
		}
		return s.present(record)
	}
	s.running[id] = true
	delivery := s.delivery
	lifetime := s.lifetime
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.running, id); s.mu.Unlock() }()
	at := s.now()
	if err := s.db.ExpireOwnerAction(ctx, id, at); err != nil {
		return Proposal{}, err
	}
	record, err := s.db.ReadOwnerAction(ctx, id)
	if err != nil {
		return Proposal{}, err
	}
	p, err := s.present(record)
	if err != nil {
		return Proposal{}, err
	}
	parent := context.WithoutCancel(ctx)
	if lifetime != nil {
		parent = lifetime
	}
	work, cancel := context.WithTimeout(parent, 10*time.Minute)
	defer cancel()
	work = store.WithStandardsOwnerActionClock(work, s.clock)
	work = store.WithOwnerActionEffect(work, p.ExecutionID, s.clock)
	switch {
	case record.State == "awaiting_discord" && record.DeliveryState == "pending":
		claimed, e := s.db.ClaimOwnerActionDelivery(work, id, at)
		if e != nil {
			return Proposal{}, e
		}
		if claimed {
			message := ""
			var sendErr error
			if delivery == nil {
				sendErr = errors.New("discord approval delivery unavailable")
			} else {
				message, sendErr = delivery.Send(work, p)
			}
			var retry *DeliveryRetryError
			if errors.As(sendErr, &retry) && retry.RetryAfter > 0 {
				retryAt := s.now().Add(retry.RetryAfter)
				if e = s.db.DeferOwnerActionDelivery(work, id, retryAt, s.now()); e != nil {
					return Proposal{}, e
				}
				record, e = s.db.ReadOwnerAction(work, id)
				if e != nil {
					return Proposal{}, e
				}
				return s.present(record)
			}
			reason := ""
			if sendErr != nil || message == "" {
				reason = "discord_delivery_unconfirmed_no_execution"
				message = ""
			}
			cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			e = s.db.FinishOwnerActionDelivery(cleanup, id, message, reason, s.now())
			done()
			if e != nil {
				return Proposal{}, e
			}
		}
	case record.State == "awaiting_discord" && record.DeliveryState == "sending":
		if err = s.db.StopOwnerActionDelivery(work, id); err != nil {
			return Proposal{}, err
		}
	case record.State == "admitted":
		if err = s.db.CheckOwnerActionAuthority(work, record.Principal, record, s.now()); err != nil {
			return s.saveResult(ctx, p, Result{State: "failed", Summary: "Authority was revoked before execution; no effect was started."}, "authority_revoked")
		}
		if err = s.domain.Recheck(work, p.Principal, p.Facts, at); err != nil {
			return s.saveResult(ctx, p, Result{State: "failed", Summary: "Reviewed target or authority changed before execution; request a fresh proposal."}, "stale_target_repropose")
		}
		claimed, e := s.db.ClaimOwnerActionExecution(work, id, s.now())
		if e != nil {
			return s.saveResult(ctx, p, Result{State: "failed", Summary: "Authority was revoked before execution; no effect was started."}, "authority_revoked")
		}
		if claimed {
			result, e := s.domain.Execute(work, p.Principal, p.Facts, p.ExecutionID, at)
			if e != nil {
				result = Result{State: "uncertain", Summary: "The external outcome is uncertain; inspect retained work before requesting again."}
			}
			return s.saveResult(ctx, p, result, "")
		}
	case record.State == "executing" || record.State == "uncertain":
		result, found, e := s.domain.Recover(work, p.Principal, p.Facts, p.ExecutionID, at)
		if e == nil && found {
			return s.saveResult(ctx, p, result, "")
		}
		if record.State == "executing" {
			return s.saveResult(ctx, p, Result{State: "uncertain", Summary: "Execution was interrupted; inspect retained work before requesting again."}, "interrupted_effect")
		}
	}
	record, err = s.db.ReadOwnerAction(ctx, id)
	if err != nil {
		return Proposal{}, err
	}
	return s.present(record)
}

func (s *Service) saveResult(ctx context.Context, p Proposal, result Result, reason string) (Proposal, error) {
	if result.State == "" {
		result.State = "uncertain"
	}
	if result.State != "succeeded" && result.State != "failed" && result.State != "uncertain" && result.State != "executing" {
		result = Result{State: "uncertain", Summary: "The handler returned no confirmed outcome; inspect retained work."}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return Proposal{}, err
	}
	cleanup, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer done()
	if err = s.db.FinishOwnerAction(cleanup, p.ID, result.State, string(encoded), reason, s.now()); err != nil {
		return Proposal{}, err
	}
	record, err := s.db.ReadOwnerAction(cleanup, p.ID)
	if err != nil {
		return Proposal{}, err
	}
	return s.present(record)
}

type historyCursor struct {
	Authority  string  `json:"authority"`
	Filters    Filters `json:"filters"`
	Owner      string  `json:"owner"`
	Generation int64   `json:"generation"`
	Time       string  `json:"time"`
	ID         string  `json:"id"`
}

// History pages authorized owner history across originating clients.
func (s *Service) History(ctx context.Context, p Principal, cursor string, limit int) (Page, error) {
	return s.HistoryFiltered(ctx, p, Filters{}, cursor, limit)
}

// HistoryFiltered binds a stable cursor to filters and current reader authority.
func (s *Service) HistoryFiltered(ctx context.Context, p Principal, filters Filters, cursor string, limit int) (Page, error) {
	p, err := s.pair(p, false)
	if err != nil {
		return Page{}, err
	}
	if limit < 1 || limit > 100 || len(cursor) > 4096 || len(filters.Action) > 100 || len(filters.Repository) > 1024 || len(filters.State) > 100 {
		return Page{}, errors.New("invalid history page")
	}
	authority, err := s.db.OwnerActionReaderFingerprint(ctx, principal(p), s.now())
	if err != nil {
		return Page{}, err
	}
	fingerprint, _ := digest(authority)
	c := historyCursor{Owner: p.OwnerID, Generation: p.OwnerGeneration, Authority: fingerprint, Filters: filters}
	if cursor != "" {
		c = historyCursor{}
		raw, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil {
			return Page{}, errors.New("invalid history cursor")
		}
		if e = json.Unmarshal(raw, &c); e != nil || c.Owner != p.OwnerID || c.Generation != p.OwnerGeneration || c.Authority != fingerprint || c.Filters != filters {
			return Page{}, errors.New("history cursor is not authorized")
		}
	}
	records, err := s.db.ListOwnerActions(ctx, p.OwnerID, c.Time, c.ID, limit)
	if err != nil {
		return Page{}, err
	}
	page := Page{Proposals: []Proposal{}}
	for _, record := range records {
		if err = s.authorizeRead(ctx, p, record, s.now()); err != nil {
			if errors.Is(err, store.ErrConnectorOwner) || errors.Is(err, store.ErrConnectorInvalid) {
				return Page{}, err
			}
			continue
		}
		proposal, e := s.present(record)
		if e != nil {
			return Page{}, e
		}
		if filters.Action != "" && proposal.Facts.Invocation.Command != filters.Action || filters.Repository != "" && proposal.Facts.RepositoryRef != filters.Repository || filters.State != "" && proposal.State != filters.State {
			continue
		}
		attempts, e := s.db.OwnerActionAttempts(ctx, record.ID)
		if e != nil {
			return Page{}, e
		}
		for _, a := range attempts {
			proposal.Attempts = append(proposal.Attempts, Attempt{ID: a.ID, ReceivedAt: a.ReceivedAt, Method: a.Method, Principal: a.Principal, Decision: a.Decision, State: a.State, Evidence: a.Evidence})
		}
		page.Proposals = append(page.Proposals, proposal)
	}
	if len(records) == limit {
		last := records[len(records)-1]
		c.Time = last.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z")
		c.ID = last.ID
		raw, e := json.Marshal(c)
		if e != nil {
			return Page{}, fmt.Errorf("history cursor: %w", e)
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return page, nil
}
