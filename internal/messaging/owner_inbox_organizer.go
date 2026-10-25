package messaging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

const inboxOrganizationVersion = 1
const inboxOrganizerBytes = 256 << 10

type inboxOrganizerTicket struct {
	Key       string                `json:"key"`
	Class     string                `json:"class"`
	WaitingAt time.Time             `json:"waiting_at"`
	Decisions []store.InboxDecision `json:"decisions"`
	Evidence  []store.InboxEvidence `json:"evidence"`
}
type inboxOrganizerInput struct {
	Version    int                    `json:"version"`
	SnapshotID string                 `json:"snapshot_id"`
	CapturedAt time.Time              `json:"captured_at"`
	Filter     store.InboxFilter      `json:"filter"`
	Coverage   []store.InboxCoverage  `json:"coverage"`
	Tickets    []inboxOrganizerTicket `json:"tickets"`
}
type inboxOrganizerOutput struct {
	Version int                          `json:"version"`
	Layout  string                       `json:"layout"`
	Tickets []store.InboxOrganizedTicket `json:"tickets"`
}

func inboxPriority(kind string) int {
	if kind == "parked" {
		return 0
	}
	if strings.Contains(kind, "review") {
		return 1
	}
	return 2
}
func inboxClass(priority int) string {
	return []string{"Owner recovery", "Decision reviews", "Open questions"}[priority]
}

func inboxBaseline(page store.InboxPage) inboxOrganizerInput {
	input := inboxOrganizerInput{Version: inboxOrganizationVersion, SnapshotID: page.Snapshot.ID, CapturedAt: page.Snapshot.CapturedAt, Filter: page.Snapshot.Filter, Coverage: page.Snapshot.Coverage, Tickets: []inboxOrganizerTicket{}}
	groups := map[string]int{}
	for _, d := range page.Entries {
		key := store.InboxTicketIdentity(d)
		index, ok := groups[key]
		if !ok {
			index = len(input.Tickets)
			groups[key] = index
			input.Tickets = append(input.Tickets, inboxOrganizerTicket{Key: key, Class: inboxClass(inboxPriority(d.Kind)), WaitingAt: d.WaitingAt, Decisions: []store.InboxDecision{}, Evidence: []store.InboxEvidence{}})
		}
		t := &input.Tickets[index]
		priority := inboxPriority(t.DecisionsKind())
		if len(t.Decisions) == 0 || inboxPriority(d.Kind) < priority {
			t.Class = inboxClass(inboxPriority(d.Kind))
			t.WaitingAt = d.WaitingAt
		} else if inboxPriority(d.Kind) == priority && d.WaitingAt.After(t.WaitingAt) {
			t.WaitingAt = d.WaitingAt
		}
		t.Decisions = append(t.Decisions, d)
		t.Evidence = append(t.Evidence, d.Evidence...)
	}
	for i := range input.Tickets {
		sort.Slice(input.Tickets[i].Decisions, func(a, b int) bool {
			x, y := input.Tickets[i].Decisions[a], input.Tickets[i].Decisions[b]
			if inboxPriority(x.Kind) != inboxPriority(y.Kind) {
				return inboxPriority(x.Kind) < inboxPriority(y.Kind)
			}
			if !x.WaitingAt.Equal(y.WaitingAt) {
				return x.WaitingAt.After(y.WaitingAt)
			}
			return x.Key < y.Key
		})
	}
	sort.Slice(input.Tickets, func(i, j int) bool {
		a, b := input.Tickets[i], input.Tickets[j]
		if a.Class != b.Class {
			return inboxPriority(a.DecisionsKind()) < inboxPriority(b.DecisionsKind())
		}
		if !a.WaitingAt.Equal(b.WaitingAt) {
			return a.WaitingAt.After(b.WaitingAt)
		}
		return a.Key < b.Key
	})
	return input
}
func (t inboxOrganizerTicket) DecisionsKind() string {
	switch t.Class {
	case "Owner recovery":
		return "parked"
	case "Decision reviews":
		return "review"
	default:
		return "question"
	}
}
func inboxFallback(input inboxOrganizerInput, reason string) store.InboxOrganization {
	out := store.InboxOrganization{Version: inboxOrganizationVersion, Layout: "action_groups", Mode: "fallback", FallbackReason: reason, Tickets: []store.InboxOrganizedTicket{}}
	for _, t := range input.Tickets {
		row := store.InboxOrganizedTicket{Key: t.Key, DecisionKeys: []string{}, EvidenceIDs: []string{}}
		for _, d := range t.Decisions {
			row.DecisionKeys = append(row.DecisionKeys, d.Key)
		}
		out.Tickets = append(out.Tickets, row)
	}
	return out
}

func validateInboxOrganization(raw []byte, input inboxOrganizerInput) (store.InboxOrganization, error) {
	fail := func() (store.InboxOrganization, error) {
		return store.InboxOrganization{}, errors.New("organizer result must preserve all identities and baseline order except evidenced forward promotions")
	}
	if len(raw) > inboxOrganizerBytes || uniqueJSON(raw) != nil {
		return fail()
	}
	var proposed inboxOrganizerOutput
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&proposed) != nil || decoder.Decode(new(any)) != io.EOF || proposed.Version != 1 || proposed.Layout != "action_groups" || len(proposed.Tickets) != len(input.Tickets) {
		return fail()
	}
	byKey := map[string]inboxOrganizerTicket{}
	remaining := make([]string, 0, len(input.Tickets))
	for _, t := range input.Tickets {
		byKey[t.Key] = t
		remaining = append(remaining, t.Key)
	}
	seen := map[string]bool{}
	for _, row := range proposed.Tickets {
		t, ok := byKey[row.Key]
		if !ok || seen[row.Key] || len(row.DecisionKeys) != len(t.Decisions) || len(row.Summary) > 600 || len(row.PromotionReason) > 600 || strings.ContainsAny(row.Summary+row.PromotionReason, "\x00\r\n") {
			return fail()
		}
		seen[row.Key] = true
		keys := map[string]bool{}
		for _, d := range t.Decisions {
			keys[d.Key] = true
		}
		for _, key := range row.DecisionKeys {
			if !keys[key] {
				return fail()
			}
			delete(keys, key)
		}
		position := -1
		for i, key := range remaining {
			if key == row.Key {
				position = i
				break
			}
		}
		if position < 0 {
			return fail()
		}
		if position > 0 {
			if strings.TrimSpace(row.PromotionReason) == "" || len(row.EvidenceIDs) == 0 {
				return fail()
			}
			evidence := map[string]store.InboxEvidence{}
			for _, e := range t.Evidence {
				evidence[e.ID] = e
			}
			used := map[string]bool{}
			for _, id := range row.EvidenceIDs {
				e, exists := evidence[id]
				if !exists || used[id] || (e.Kind != "urgency" && e.Kind != "dependency") || e.Source == "" || e.Revision == "" || e.Excerpt == "" || (e.Kind == "dependency" && e.Target == "") {
					return fail()
				}
				applies := false
				for _, d := range t.Decisions {
					if e.DecisionKey == d.Key {
						applies = true
					}
				}
				if !applies {
					return fail()
				}
				used[id] = true
			}
		} else if row.PromotionReason != "" || len(row.EvidenceIDs) > 0 {
			return fail()
		}
		remaining = append(remaining[:position], remaining[position+1:]...)
	}
	return store.InboxOrganization{Version: proposed.Version, Layout: proposed.Layout, Mode: "organized", Tickets: proposed.Tickets}, nil
}

func (s *Service) finalizeOwnerInbox(ctx context.Context, scope store.InboxScope, page store.InboxPage, at time.Time, call func(context.Context, []byte, string, int) ([]byte, error)) (store.InboxOrganization, error) {
	saved, err := s.db.ReadInboxOrganization(ctx, scope, page.Snapshot.ID, inboxOrganizationVersion, at)
	if err == nil {
		return saved, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return saved, err
	}
	input := inboxBaseline(page)
	result := inboxFallback(input, "Dedicated organizer unavailable; showing the full factual inbox in baseline order.")
	encoded, err := json.Marshal(input)
	if err != nil {
		return result, err
	}
	if len(encoded) > inboxOrganizerBytes {
		result.FallbackReason = "Complete inbox exceeds the organizer budget; showing the full factual inbox in baseline order."
	} else if call != nil && len(input.Tickets) > 0 {
		bounded, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		correction := ""
		for step := 1; step <= 2; step++ {
			if err = s.db.InboxSnapshotAuthorized(bounded, scope, page.Snapshot.ID, at); err != nil {
				return result, err
			}
			raw, callErr := call(bounded, encoded, correction, step)
			if callErr != nil {
				result.FallbackReason = "Dedicated organization failed or exceeded its budget; showing the full factual inbox in baseline order."
				break
			}
			candidate, validationErr := validateInboxOrganization(raw, input)
			if validationErr == nil {
				result = candidate
				break
			}
			correction = validationErr.Error()
			result.FallbackReason = "Dedicated organizer output could not be validated; showing the full factual inbox in baseline order."
		}
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if err = s.db.InboxSnapshotAuthorized(ctx, scope, page.Snapshot.ID, at); err != nil {
		return result, err
	}
	if runtime := s.conversations.Load(); runtime != nil {
		if err = conversationAccess(ctx, runtime, scope.Conversation, at); err != nil {
			return result, err
		}
	}
	for i := range result.Tickets {
		if s.Protect == nil {
			continue
		}
		row := &result.Tickets[i]
		if row.Summary, err = s.Protect(ctx, row.Summary); err != nil {
			return result, err
		}
		if row.PromotionReason, err = s.Protect(ctx, row.PromotionReason); err != nil {
			return result, err
		}
	}
	return s.db.FinalizeInboxOrganization(ctx, scope, page.Snapshot.ID, result, at)
}

func inboxOrganizationMap(input inboxOrganizerInput) map[string]inboxOrganizerTicket {
	out := make(map[string]inboxOrganizerTicket, len(input.Tickets))
	for _, t := range input.Tickets {
		out[t.Key] = t
	}
	return out
}

func inboxOrganizedRow(t inboxOrganizerTicket, row store.InboxOrganizedTicket, id string, offset, index int) string {
	d := t.Decisions[0]
	kinds := []string{}
	seen := map[string]bool{}
	for _, decision := range t.Decisions {
		if !seen[decision.Kind] {
			seen[decision.Kind] = true
			kinds = append(kinds, decision.Kind)
		}
	}
	waiting := "unknown"
	if !t.WaitingAt.IsZero() {
		waiting = t.WaitingAt.UTC().Format(time.RFC3339)
	}
	text := fmt.Sprintf("%s · %s · %s\n%s; %d requests (%s); waiting since %s\n", inboxShort(d.Source, 20), inboxShort(d.Project, 70), inboxShort(d.Ticket, 110), t.Class, len(t.Decisions), strings.Join(kinds, ", "), waiting)
	summary := row.Summary
	if summary == "" {
		summary = d.Summary
	}
	text += inboxShort(summary, 140) + "\n"
	if row.PromotionReason != "" {
		text += "Promoted: " + inboxShort(row.PromotionReason, 120) + "\n"
		for _, id := range row.EvidenceIDs[:min(1, len(row.EvidenceIDs))] {
			for _, e := range t.Evidence {
				if e.ID == id {
					text += "Evidence: " + inboxShort(e.Source, 55) + " @ " + inboxShort(e.Revision, 40) + ": " + inboxShort(e.Excerpt, 140) + "\n"
				}
			}
		}
	}
	action := d.Action
	if action == "" {
		action = "Unavailable: " + d.ActionUnavailable
	}
	text += "Next: " + inboxShort(action, 120) + "; destination: " + inboxShort(d.Destination, 80) + "\n"
	// Detailed inventories and exact protected actions are reachable without model wording.
	return text + fmt.Sprintf("Open: %s:%d:%d\n", id, offset, index)
}

func renderOrganizedInbox(page store.InboxPage, view store.InboxOrganization, offset int) (string, int) {
	previous := 0
	for position := 0; position < offset; {
		_, next := renderOrganizedInboxPage(page, view, position, previous)
		if next >= offset || next <= position {
			previous = position
			break
		}
		previous = position
		position = next
	}
	return renderOrganizedInboxPage(page, view, offset, previous)
}

func renderOrganizedInboxPage(page store.InboxPage, view store.InboxOrganization, offset, previous int) (string, int) {
	input := inboxBaseline(page)
	byKey := inboxOrganizationMap(input)
	offset = min(offset, len(view.Tickets))
	header := fmt.Sprintf("Organized owner inbox captured %s. Recorded state, not an atomic live view.\nFilters: source=%s project=%s kind=%s.\n", page.Snapshot.CapturedAt.UTC().Format(time.RFC3339), inboxShort(page.Snapshot.Filter.Source, 50), inboxShort(page.Snapshot.Filter.Project, 50), inboxShort(page.Snapshot.Filter.Kind, 50))
	if view.Mode == "fallback" {
		header += "Fallback: " + view.FallbackReason + "\n"
	}
	coverage := fmt.Sprintf("Coverage: %d configured ticket connections; unconfigured tickets are outside this view.\n", len(page.Snapshot.Coverage))
	gaps := 0
	for _, c := range page.Snapshot.Coverage {
		if c.Status != "available" && c.Status != "ok" {
			gaps++
		}
	}
	if gaps > 0 {
		coverage += fmt.Sprintf("Coverage warnings: %d; see coverage details.\n", gaps)
	}
	footer := func(end int) string {
		out := fmt.Sprintf("Tickets: %d; remaining: %d; distinct requests: %d.\n", len(view.Tickets), len(view.Tickets)-end, len(page.Entries)) + coverage
		if end < len(view.Tickets) {
			out += fmt.Sprintf("More: %s:%d\n", page.Snapshot.ID, end)
		}
		if offset > 0 {
			out += fmt.Sprintf("Back: %s:%d\n", page.Snapshot.ID, previous)
		}
		out += fmt.Sprintf("Coverage details: %s:0:%d\nRefresh: %s:%d", page.Snapshot.ID, offset, page.Snapshot.ID, offset)
		return out
	}
	rows := ""
	end := offset
	for end < len(view.Tickets) && end < offset+10 {
		row := view.Tickets[end]
		t, available := byKey[row.Key]
		text := "A captured ticket is unavailable because source access changed.\n"
		if available && len(t.Decisions) > 0 {
			text = inboxOrganizedRow(t, row, page.Snapshot.ID, offset, end)
		}
		if replySize(header+rows+text+footer(end+1)) > 1950 && end > offset {
			break
		}
		// All individual row fields are bounded; every page retains at least one ticket.
		rows += text
		end++
	}
	return header + rows + footer(end), end
}

func (s *Service) readOrganizedOwnerInbox(ctx context.Context, m Message, at time.Time, capture *inboxCapture, id string, offset, entry int) (string, error) {
	scope := s.inboxScope(m.Channel)
	op := m.Arguments["operation"]
	filter := store.InboxFilter{Source: m.Arguments["source"], Project: m.Arguments["project"], Kind: m.Arguments["kind"]}
	fresh := id == "" || op == "organize" || op == "refresh"
	if id != "" {
		saved, err := s.db.ReadInboxSnapshot(ctx, scope, id, 0, 1, at)
		if err != nil {
			return inboxNotice(inboxReadFailure(err))
		}
		old := saved.Snapshot.Filter
		if filter.Source == "" {
			filter.Source = old.Source
		}
		if filter.Project == "" {
			filter.Project = old.Project
		}
		if filter.Kind == "" {
			filter.Kind = old.Kind
		}
		fresh = fresh || filter != old
	}
	if fresh {
		snap, err := s.db.CreateProtectedInboxSnapshot(ctx, scope, filter, at, func(text string) (string, error) {
			if s.Protect == nil {
				return "", errors.New("inbox display protection unavailable")
			}
			return s.Protect(ctx, text)
		})
		if err != nil {
			return inboxNotice(inboxReadFailure(err))
		}
		id = snap.ID
		offset = 0
	}
	var page store.InboxPage
	var view store.InboxOrganization
	var err error
	if fresh {
		page, err = s.db.ReadCompleteInboxSnapshot(ctx, scope, id, at)
		if err == nil {
			var call func(context.Context, []byte, string, int) ([]byte, error)
			if capture != nil {
				call = capture.Organize
			}
			view, err = s.finalizeOwnerInbox(ctx, scope, page, at, call)
		}
	} else {
		page, view, err = s.db.ReadInboxNavigation(ctx, scope, id, inboxOrganizationVersion, at)
	}
	if err != nil {
		return inboxNotice(inboxReadFailure(err))
	}
	if capture != nil {
		capture.Snapshot = id
		for _, d := range page.Entries {
			capture.Sources = append(capture.Sources, d.ConnectionID)
		}
	}
	if op == "coverage" && !fresh {
		if capture != nil {
			capture.Sources = nil
			capture.Navigation = fmt.Sprintf("%s:%d", id, entry)
		}
		return renderInboxCoverage(page.Snapshot, offset, entry), nil
	}
	if op == "open" && !fresh {
		unavailable := func(reason string) string {
			if capture != nil {
				capture.Sources = nil
				capture.Navigation = fmt.Sprintf("%s:%d", id, offset)
			}
			return fmt.Sprintf("%s\nCoverage details: %s:0:%d\nReturn: %s:%d", reason, id, offset, id, offset)
		}
		if entry >= len(view.Tickets) {
			return inboxNotice(unavailable("This organized ticket reference is unavailable. Refresh the inbox."))
		}
		requestOffset := 0
		fields := strings.Split(m.Arguments["reference"], ":")
		if len(fields) == 4 {
			if _, err = fmt.Sscan(fields[3], &requestOffset); err != nil || requestOffset < 0 {
				return inboxNotice("Invalid request reference.")
			}
		}
		row := view.Tickets[entry]
		byKey := inboxOrganizationMap(inboxBaseline(page))
		ticket := byKey[row.Key]
		if len(ticket.Decisions) == 0 {
			return inboxNotice(unavailable("This captured ticket is unavailable because source access changed."))
		}
		if requestOffset >= len(row.DecisionKeys) {
			return inboxNotice(unavailable("This request reference is unavailable. Refresh the inbox."))
		}
		text := unavailable("This captured request is unavailable because source access changed.")
		// Request references select saved key positions, including revoked slots.
		// Filtering available decisions must never retarget a previously issued reference.
		if decision, available := inboxOrganizedDecision(ticket, row, requestOffset); available {
			text, err = s.openOwnerInbox(ctx, scope, id, offset, decision.Ordinal, at, capture)
			if err != nil {
				return text, err
			}
		}
		text = fmt.Sprintf("Ticket requests: %d; showing request %d.\nSnapshot: %s\n", len(row.DecisionKeys), requestOffset+1, page.Snapshot.CapturedAt.UTC().Format(time.RFC3339)) + text
		if requestOffset+1 < len(row.DecisionKeys) {
			text += fmt.Sprintf("\nRequests more: %s:%d:%d:%d", id, offset, entry, requestOffset+1)
		}
		if requestOffset > 0 {
			text += fmt.Sprintf("\nRequests back: %s:%d:%d:%d", id, offset, entry, requestOffset-1)
		}
		return text, nil
	}
	text, next := renderOrganizedInbox(page, view, offset)
	if capture != nil {
		capture.Navigation = fmt.Sprintf("%s:%d", id, offset)
		if next < len(view.Tickets) {
			capture.Navigation = fmt.Sprintf("%s:%d", id, next)
		}
		// Only the tickets this page shows become listed references.
		byKey := inboxOrganizationMap(inboxBaseline(page))
		for _, row := range view.Tickets[min(offset, len(view.Tickets)):min(max(next, offset), len(view.Tickets))] {
			for _, d := range byKey[row.Key].Decisions {
				if item, ok := inboxListedIssue(d); ok {
					capture.Issues = append(capture.Issues, item)
				}
			}
		}
	}
	return text, nil
}

func inboxOrganizedDecision(ticket inboxOrganizerTicket, row store.InboxOrganizedTicket, position int) (store.InboxDecision, bool) {
	if position < 0 || position >= len(row.DecisionKeys) || row.DecisionKeys[position] == "" {
		return store.InboxDecision{}, false
	}
	for _, decision := range ticket.Decisions {
		if decision.Key == row.DecisionKeys[position] {
			return decision, true
		}
	}
	return store.InboxDecision{}, false
}
