package messaging

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/messaging/ask"
	"github.com/cloudvane/litefactory/internal/retainedevidence"
	"github.com/cloudvane/litefactory/internal/store"
)

// recoveryTarget resolves owner text before consulting the retained focus. A
// repository reference is a hint only; the current catalog remains authoritative.
func (r *factoryConversationRun) recoveryTarget(ctx context.Context) (string, string, error) {
	repos, err := r.s.db.ListRepos(ctx)
	if err != nil {
		return "", "", err
	}
	project := ""
	for _, token := range strings.Fields(strings.ToLower(r.t.Source)) {
		token = strings.Trim(token, ".,;:!?()[]<>\"'")
		token = strings.SplitN(token, "#", 2)[0]
		for _, repo := range repos {
			if token == strings.ToLower(repo.Slug()) || token == strings.ToLower(repo.Name) {
				if project != "" && project != repo.Slug() {
					return "", "", nil
				}
				project = repo.Slug()
			}
		}
	}
	targets := ask.ExplicitIssues(r.t.Source)
	focus := ""
	if r.focus != nil {
		focus = r.focus.Project
	}
	var resolved ask.Resolution
	if len(targets) == 1 && project == "" {
		resolved, err = r.s.resolveNaturalProjectFocus(ctx, map[string]string{"question": r.t.Source}, focus)
	} else {
		if project == "" {
			project = focus
		}
		resolved, err = ask.ResolveProject(ctx, r.s.db, project, nil)
	}
	if err != nil {
		return "", "", err
	}
	if resolved.Refusal != "" {
		return "", "", nil
	}
	issue := ""
	if len(targets) == 1 {
		issue = strconv.Itoa(targets[0])
	} else if len(targets) == 0 && r.focus != nil && r.focus.Project == resolved.Repository.Slug() {
		issue = r.focus.Issue
	}
	if len(targets) > 1 {
		return "", "", nil
	}
	return resolved.Repository.Slug(), issue, nil
}

func (r *factoryConversationRun) evidenceAuthorized(ctx context.Context, e store.ConversationEvidence) (bool, error) {
	if e.Repository == "" {
		return false, nil
	}
	resolved, err := ask.ResolveProject(ctx, r.s.db, e.Repository, nil)
	if err != nil {
		return false, err
	}
	if resolved.Refusal != "" || resolved.Repository.Slug() != e.Repository {
		return false, nil
	}
	// Repository IDs are pinned in the entry's channel, preventing re-registration
	// under the same name from reviving an old grant.
	if e.Entry.Channel != strconv.FormatInt(resolved.Repository.ID, 10) {
		return false, nil
	}
	if e.Entry.Kind == "ask" {
		return r.askSourceCurrent(ctx, e)
	}
	if e.Entry.Kind == "lookup" || e.Entry.Kind == "reply" {
		sources, err := r.s.db.ListConversationTurnEvidence(ctx, e.SourceTurnID, r.at)
		if err != nil {
			return false, err
		}
		for _, source := range sources {
			if source.Entry.Kind == "ask" {
				current, err := r.askSourceCurrent(ctx, source)
				if err != nil || !current {
					return current, err
				}
			}
		}
	}
	return true, nil
}

// retainView stores an already protected bounded view. The original identity is
// distinct from the digest of the disclosed excerpt and never becomes a fact ID.
func (r *factoryConversationRun) retainView(ctx context.Context, ordinal int, kind, owner, sourceTurn, repository, issue, original, view string, captured time.Time, capture retainedevidence.Coverage) error {
	if repository == "" {
		return nil
	}
	existing, err := r.s.db.ListConversationTurnEvidence(ctx, r.t.ID, r.at)
	if err != nil {
		return err
	}
	for _, e := range existing {
		if e.Ordinal == ordinal {
			if e.Entry.Kind != kind || e.Entry.OwnerID != owner || e.Entry.Revision != retainedevidence.Digest([]byte(original)) {
				return retainedevidence.ErrConflict
			}
			return nil
		}
	}

	resolved, err := ask.ResolveProject(ctx, r.s.db, repository, nil)
	if err != nil {
		return err
	}
	if resolved.Refusal != "" {
		return errors.New("conversation evidence repository unavailable")
	}
	protected, err := r.s.Protect(ctx, view)
	if err != nil {
		return err
	}
	digest := retainedevidence.Digest([]byte(original))
	entry := retainedevidence.Entry{Kind: kind, OwnerID: owner, Revision: digest, Digest: digest, Channel: strconv.FormatInt(resolved.Repository.ID, 10), Bytes: int64(len(original)), Capture: capture, Availability: retainedevidence.Available}
	if len(view) < len(original) {
		entry.Omissions = []string{"Only the supplied byte range is retained in this view; original capture coverage is separate."}
	}
	return r.s.db.SaveConversationEvidence(ctx, r.t, store.ConversationEvidence{TurnID: r.t.ID, SourceTurnID: sourceTurn, Ordinal: ordinal, Repository: repository, Issue: issue, CapturedAt: captured, ExpiresAt: captured.Add(24 * time.Hour), Entry: entry, Content: protected, RangeStart: 0, RangeEnd: int64(len(view)), ViewDigest: retainedevidence.Digest([]byte(protected))})
}

func (r *factoryConversationRun) retainLookup(ctx context.Context, result conversationReadResult, ordinal int) error {
	// Inbox and other privileged navigation results keep their own stricter access
	// checks and are never copied into general conversational history.
	if result.Command == "inbox" || standardsCommand(result.Command) || result.Arguments["project"] == "" {
		return nil
	}
	issue := result.Arguments["task"]
	if issue == "" {
		issue = result.Arguments["issue"]
	}
	if result.Command == "ask" {
		if targets := ask.ExplicitIssues(result.Arguments["question"]); len(targets) == 1 {
			issue = strconv.Itoa(targets[0])
		}
	}

	coverage := retainedevidence.Partial
	if result.Complete {
		coverage = retainedevidence.Unknown
	} // a bounded lookup cannot prove full history
	if err := r.retainView(ctx, ordinal, "lookup", result.FactID, r.t.ID, result.Arguments["project"], issue, result.Result, result.Result, result.CapturedAt, coverage); err != nil {
		return err
	}
	return r.retainAskSources(ctx, result)
}

// projectRetainedEvidence never inserts recovered text into owner_sources or
// current evidence. It accounts for gaps even when no source may be disclosed.
func (r *factoryConversationRun) projectRetainedEvidence(ctx context.Context, prompt *factoryConversationPrompt) error {
	repository, issue, err := r.recoveryTarget(ctx)
	if err != nil {
		return err
	}
	prompt.RetainedEvidence = nil
	prompt.ContextGaps = []string{"Only locally captured, still-retained permitted context is available; uncaptured history and complete remote history are unknown."}
	if len(ask.ExplicitIssues(r.t.Source)) > 0 {
		prompt.ContextGaps = append(prompt.ContextGaps, "Prior Ask questions and answers are excluded for an explicit issue request.")
	}
	entries, err := r.s.db.ListConversationEvidence(ctx, r.c.ID, r.at, 64)
	if err != nil {
		return err
	}
	currentCaptures, err := r.s.db.ListConversationTurnEvidence(ctx, r.t.ID, r.at)
	if err != nil {
		return err
	}
	snapshotSaved := false
	for _, e := range currentCaptures {
		if e.Ordinal >= 49 && e.Ordinal <= 80 {
			snapshotSaved = true
			break
		}
	}
	admittedSources := make(map[string]bool)
	for _, source := range prompt.OwnerSources {
		admittedSources[source.SourceID] = true
	}
	admittedTurns := make(map[string]bool)
	for _, e := range entries {
		if e.Entry.Kind == "owner" && admittedSources[e.Entry.OwnerID] {
			admittedTurns[e.SourceTurnID] = true
		}
	}
	seen := make(map[string]bool)
	budgets := map[string]int{"owner": 32768, "reply": 4096, "lookup": 16384}
	for _, source := range prompt.OwnerSources {
		budgets["owner"] -= len(source.Text)
	}
	if repository != "" {
		prompt.RecentReplies = nil
	}
	for _, e := range entries {
		if e.Entry.Kind == "coverage" {
			continue
		}
		if e.TurnID == r.t.ID && e.SourceTurnID == r.t.ID {
			continue
		}
		if e.Repository != repository || e.Issue != issue {
			prompt.ContextGaps = appendUniqueGap(prompt.ContextGaps, "Captured context for other targets or with unknown issue association was omitted.")
			continue
		}
		if (e.Entry.Kind == "owner" || e.Entry.Kind == "reply") && !admittedTurns[e.SourceTurnID] {
			prompt.ContextGaps = appendUniqueGap(prompt.ContextGaps, "Older owner and reply context was omitted by the existing source admission limits.")
			continue
		}
		allowed, err := r.evidenceAuthorized(ctx, e)
		if err != nil {
			return err
		}
		if !allowed {
			prompt.ContextGaps = appendUniqueGap(prompt.ContextGaps, "Captured context was omitted because its source is unavailable or current access changed.")
			continue
		}
		if e.Entry.Availability != retainedevidence.Available {
			prompt.ContextGaps = appendUniqueGap(prompt.ContextGaps, "Some captured context has expired or is unavailable.")
			continue
		}
		key := e.Entry.Kind + ":" + e.Entry.OwnerID + ":" + e.Entry.Revision
		if seen[key] {
			continue
		}
		seen[key] = true
		if e.Entry.Kind == "ask" {
			e.Content = ""
			e.RangeEnd = e.RangeStart
			e.ViewDigest = retainedevidence.Digest(nil)
			e.Entry.Omissions = appendUniqueGap(e.Entry.Omissions, "Question and answer are supplied through the originating bounded lookup; this entry supplies identity metadata only.")
		}
		remaining := budgets[e.Entry.Kind]
		if e.Entry.Kind == "ask" {
			remaining = 1
		}
		if remaining <= 0 {
			prompt.ContextGaps = appendUniqueGap(prompt.ContextGaps, "Additional captured context was omitted at the existing context budget.")
			continue
		}
		protected, err := r.s.Protect(ctx, e.Content)
		if err != nil {
			return err
		}
		text, cut := boundedConversationText(protected, remaining)
		budgets[e.Entry.Kind] -= len(text)
		e.Content = text
		e.RangeEnd = min(e.RangeEnd, e.RangeStart+int64(len(text)))
		e.ViewDigest = retainedevidence.Digest([]byte(text))
		if cut {
			e.Entry.Omissions = append(e.Entry.Omissions, "Supplied excerpt ends at the context budget; additional retained bytes omitted.")
		}
		if len(prompt.RetainedEvidence) >= 32 {
			prompt.ContextGaps = appendUniqueGap(prompt.ContextGaps, "More captured sources were omitted at the context budget.")
			break
		}
		saved := e
		saved.TurnID = r.t.ID
		saved.Ordinal = 49 + len(prompt.RetainedEvidence)
		if !snapshotSaved {
			if err := r.s.db.SaveConversationEvidence(ctx, r.t, saved); err != nil {
				return err
			}
		}
		prompt.RetainedEvidence = append(prompt.RetainedEvidence, e)
	}
	if len(prompt.RetainedEvidence) == 0 {
		prompt.ContextGaps = append(prompt.ContextGaps, "No matching retained historical evidence was available for the selected target.")
	}
	return nil
}

func appendUniqueGap(gaps []string, gap string) []string {
	for _, existing := range gaps {
		if existing == gap {
			return gaps
		}
	}
	return append(gaps, gap)
}

// authorizeRestoredPrompt checks every repository-bearing saved field before
// launch. A saved provider request is never an access grant.
func (r *factoryConversationRun) authorizeRestoredPrompt(ctx context.Context, p *factoryConversationPrompt) error {
	for _, result := range append(append([]conversationReadResult{}, p.EarlierEvidence...), valueEvidence(p.Evidence)...) {
		if result.Command == "inbox" {
			if err := r.s.authorizeInboxResult(ctx, r.c.ChannelRef, result, r.at); err != nil {
				return err
			}
			continue
		}
		project := result.Arguments["project"]
		if project == "" {
			continue
		}
		resolved, err := ask.ResolveProject(ctx, r.s.db, project, nil)
		if err != nil {
			return err
		}
		if resolved.Refusal != "" || resolved.Repository.Slug() != project || result.ResolvedRepoID > 0 && resolved.Repository.ID != result.ResolvedRepoID {
			return errors.New("saved conversation evidence access changed")
		}
	}
	// Exact historical owner spans retain their original repository grant even
	// when their text has exhausted the separate explanatory-view byte budget.
	saved, err := r.s.db.ListConversationEvidence(ctx, r.c.ID, r.at, 128)
	if err != nil {
		return err
	}
	for _, source := range saved {
		if source.TurnID == r.t.ID && source.Entry.Kind == "ask" {
			current, err := r.askSourceCurrent(ctx, source)
			if err != nil {
				return err
			}
			if !current {
				return errors.New("captured Ask source is unavailable or changed")
			}
		}
	}
	for _, source := range p.OwnerSources {
		if source.SourceID == r.t.SourceID {
			continue
		}
		for _, e := range saved {
			if e.Entry.Kind == "owner" && e.Entry.OwnerID == source.SourceID {
				allowed, err := r.evidenceAuthorized(ctx, e)
				if err != nil {
					return err
				}
				if !allowed {
					return errors.New("saved owner source access changed")
				}
				break
			}
		}
	}
	for _, e := range p.RetainedEvidence {
		allowed, err := r.evidenceAuthorized(ctx, e)
		if err != nil {
			return err
		}
		if !allowed {
			return errors.New("saved conversation evidence access changed")
		}
	}
	return nil
}
func valueEvidence(e *conversationReadResult) []conversationReadResult {
	if e == nil {
		return nil
	}
	return []conversationReadResult{*e}
}

// scopeOwnerSources keeps the current request while excluding historical owner
// sources whose target is unknown or belongs to another selected issue.
func (r *factoryConversationRun) scopeOwnerSources(ctx context.Context, sources []store.ConversationTurn) ([]store.ConversationTurn, error) {
	repository, issue, err := r.recoveryTarget(ctx)
	if err != nil {
		return nil, err
	}
	if repository == "" {
		return sources, nil
	}
	entries, err := r.s.db.ListConversationEvidence(ctx, r.c.ID, r.at, 64)
	if err != nil {
		return nil, err
	}
	out := make([]store.ConversationTurn, 0, len(sources))
	for _, source := range sources {
		if source.ID == r.t.ID {
			out = append(out, source)
			continue
		}
		for _, e := range entries {
			if e.Entry.Kind == "owner" && e.SourceTurnID == source.ID && e.Repository == repository && e.Issue == issue && e.Entry.Availability == retainedevidence.Available {
				allowed, err := r.evidenceAuthorized(ctx, e)
				if err != nil {
					return nil, err
				}
				if allowed {
					out = append(out, source)
				}
				break
			}
		}
	}
	return out, nil
}

// Replies become recoverable only from an actual saved completed answer, never
// from text whose answer transaction failed. Capture scope comes from the
// original admitted owner record, not the new follow-up's selected issue.
func (r *factoryConversationRun) retainPreviousReplies(ctx context.Context, sources []store.ConversationTurn) error {
	for i, source := range sources {
		if source.ID == r.t.ID || source.ReplyID == "" || source.FinishedAt.IsZero() {
			continue
		}
		captures, err := r.s.db.ListConversationTurnEvidence(ctx, source.ID, r.at)
		if err != nil {
			return err
		}
		var scope *store.ConversationEvidence
		for j := range captures {
			e := &captures[j]
			if e.Entry.Kind == "owner" && e.SourceTurnID == source.ID {
				scope = e
				break
			}
		}
		if scope == nil || scope.Entry.Availability != retainedevidence.Available {
			continue
		}
		exchanges, err := r.s.db.ListConversationExchanges(ctx, source.ID)
		if err != nil {
			return err
		}
		privileged := false
		for _, x := range exchanges {
			if x.Kind == "evidence" {
				result, err := decodeConversationEvidence(x)
				if err != nil {
					return err
				}
				if result.Command == "inbox" || standardsCommand(result.Command) {
					privileged = true
					break
				}
			}
		}
		if privileged {
			continue
		}
		reply, err := r.s.db.ReadChatReply(ctx, r.c.Transport, source.ReplyID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if reply.ChannelRef != r.c.ChannelRef || reply.Command != "conversation" {
			continue
		}
		answer, _, err := r.s.db.ConversationReplyAnswer(ctx, reply)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if !answer.ExpiredAt.IsZero() {
			continue
		}
		parts, err := r.s.db.ReadConversationAnswerParts(ctx, answer.ID)
		if err != nil {
			return err
		}
		texts := make([]string, 0, len(parts))
		for _, part := range parts {
			texts = append(texts, part.Text)
		}
		original := strings.Join(texts, "\n\n")
		view, _ := boundedConversationText(original, 4096)
		if err := r.retainView(ctx, 16+i, "reply", source.ID+":reply", source.ID, scope.Repository, scope.Issue, original, view, source.FinishedAt, retainedevidence.Unknown); err != nil {
			return err
		}
	}
	return nil
}

// Coverage updates receive a new immutable identity rather than rewriting an
// earlier projection after expiry or changed permissions.
func (r *factoryConversationRun) retainCoverage(ctx context.Context, repository, issue string, gaps []string) error {
	text := strings.Join(gaps, "\n")
	digest := retainedevidence.Digest([]byte(text))
	saved, err := r.s.db.ListConversationTurnEvidence(ctx, r.t.ID, r.at)
	if err != nil {
		return err
	}
	used := make(map[int]bool)
	for _, e := range saved {
		used[e.Ordinal] = true
		if e.Entry.Kind == "coverage" && e.Entry.Revision == digest {
			return nil
		}
	}
	for ordinal := 81; ordinal <= 95; ordinal++ {
		if !used[ordinal] {
			return r.retainView(ctx, ordinal, "coverage", r.t.ID+":coverage:"+digest[:12], r.t.ID, repository, issue, text, text, r.at, retainedevidence.Partial)
		}
	}
	return errors.New("conversation coverage capture budget exhausted")
}

// A retained Ask link cannot outlive or replace its delivered owning row. This
// check also covers lookup/reply views derived from that row after restart.
func (r *factoryConversationRun) askSourceCurrent(ctx context.Context, e store.ConversationEvidence) (bool, error) {
	identity := strings.SplitN(e.Entry.OwnerID, ":", 2)
	if len(identity) != 2 || identity[0] != r.c.Transport {
		return false, nil
	}
	reply, err := r.s.db.ReadChatReply(ctx, identity[0], identity[1])
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if reply.ChannelRef != r.c.ChannelRef || reply.Command != "ask" || reply.State != "confirmed" {
		return false, nil
	}
	raw := reply.ReplyAttachment
	if raw == "" {
		raw = reply.ReplyText
	}
	return retainedevidence.Digest([]byte(raw)) == e.Entry.Revision, nil
}
