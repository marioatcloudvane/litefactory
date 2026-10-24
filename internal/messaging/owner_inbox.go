package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudvane/litefactory/internal/store"
)

// OwnerInboxTicketDetail contains only display facts returned by a ticket reader.
type OwnerInboxTicketDetail struct{ Title, URL, Body, Unavailable string }

// OwnerInboxReader enriches a selected, currently authorized decision without mutations.
type OwnerInboxReader interface {
	ReadOwnerInboxTicket(context.Context, store.InboxDecision) (OwnerInboxTicketDetail, error)
}

// EnableOwnerInbox exposes recorded owner waits and read-only navigation.
func (s *Service) EnableOwnerInbox(reader OwnerInboxReader) {
	s.inboxReader = reader
	s.RegisterCommand(CommandDefinition{Name: "inbox", Description: "Browse or organize recorded decisions waiting on you; open, more, back, return or refresh", NaturalLanguage: true, Effect: ReadEffect, Arguments: []Argument{
		{Name: "source", Type: TextArgument, Description: "github or linear"},
		{Name: "project", Type: TextArgument, Description: "Configured project or ticket destination"},
		{Name: "kind", Type: TextArgument, Description: "Recorded waiting kind"},
		{Name: "operation", Type: TextArgument, Description: "browse, organize, more, back, return, refresh, coverage or open"},
		{Name: "reference", Type: TextArgument, Description: "Factory-issued inbox navigation reference"},
	}}, s.ownerInbox)
}

func (s *Service) inboxScope(channel string) store.InboxScope {
	return store.InboxScope{Owner: s.owner, Transport: s.transport, Conversation: channel}
}

func inboxPosition(raw string) (string, int, int, error) {
	fields := strings.Split(raw, ":")
	if len(fields) < 2 || len(fields) > 4 || fields[0] == "" {
		return "", 0, 0, errors.New("invalid inbox reference")
	}
	offset, err := strconv.Atoi(fields[1])
	if err != nil || offset < 0 {
		return "", 0, 0, errors.New("invalid inbox reference")
	}
	entry := offset
	if len(fields) >= 3 {
		entry, err = strconv.Atoi(fields[2])
		if err != nil || entry < 0 {
			return "", 0, 0, errors.New("invalid inbox reference")
		}
	}
	if len(fields) == 4 {
		request, err := strconv.Atoi(fields[3])
		if err != nil || request < 0 {
			return "", 0, 0, errors.New("invalid inbox request reference")
		}
	}
	return fields[0], offset, entry, nil
}

type inboxCapture struct {
	Snapshot, Navigation string
	Sources              []int64
	Organize             func(context.Context, []byte, string, int) ([]byte, error)
}

func (s *Service) ownerInbox(ctx context.Context, m Message, at time.Time) (string, error) {
	return s.readOwnerInbox(ctx, m, at, nil)
}

func (s *Service) readOwnerInbox(ctx context.Context, m Message, at time.Time, capture *inboxCapture) (string, error) {
	if !s.channels[m.Channel] {
		return "Inbox access is unavailable in this conversation.", nil
	}
	if runtime := s.conversations.Load(); runtime != nil {
		if err := conversationAccess(ctx, runtime, m.Channel, at); err != nil {
			return inboxNotice("Inbox access is unavailable; retry after access is restored.")
		}
	}
	scope := s.inboxScope(m.Channel)
	filter := store.InboxFilter{Source: m.Arguments["source"], Project: m.Arguments["project"], Kind: m.Arguments["kind"]}
	operation := m.Arguments["operation"]
	var id string
	offset, entry := 0, 0
	if ref := m.Arguments["reference"]; ref != "" {
		var err error
		id, offset, entry, err = inboxPosition(ref)
		if err != nil {
			return inboxNotice("This inbox reference is unavailable. Browse again to refresh.")
		}
	}
	switch operation {
	case "", "browse", "organize", "more", "back", "return", "refresh", "open", "coverage":
	default:
		return "Use inbox browse, organize, more, back, return, refresh, coverage or open.", nil
	}
	if operation == "organize" {
		return s.readOrganizedOwnerInbox(ctx, m, at, capture, id, offset, entry)
	}
	if id != "" {
		_, organizationErr := s.db.ReadInboxOrganization(ctx, scope, id, inboxOrganizationVersion, at)
		if organizationErr == nil {
			return s.readOrganizedOwnerInbox(ctx, m, at, capture, id, offset, entry)
		}
		if !errors.Is(organizationErr, store.ErrNotFound) {
			return inboxNotice(inboxReadFailure(organizationErr))
		}
	}
	if operation == "open" {
		if id == "" || len(strings.Split(m.Arguments["reference"], ":")) != 3 {
			return "Choose an Open reference from an inbox page.", nil
		}
		return s.openOwnerInbox(ctx, scope, id, offset, entry, at, capture)
	}
	if operation == "coverage" {
		page, err := s.db.ReadInboxSnapshot(ctx, scope, id, 0, 1, at)
		if err != nil {
			return inboxReadFailure(err), nil
		}
		if capture != nil {
			capture.Snapshot = id
			capture.Navigation = fmt.Sprintf("%s:%d", id, entry)
		}
		return renderInboxCoverage(page.Snapshot, offset, entry), nil
	}
	changedFilter := filter.Source != "" || filter.Project != "" || filter.Kind != ""
	if id != "" && changedFilter {
		page, err := s.db.ReadInboxSnapshot(ctx, scope, id, 0, 1, at)
		if err != nil {
			return inboxReadFailure(err), nil
		}
		old := page.Snapshot.Filter
		if filter.Source == "" {
			filter.Source = old.Source
		}
		if filter.Project == "" {
			filter.Project = old.Project
		}
		if filter.Kind == "" {
			filter.Kind = old.Kind
		}
		changedFilter = filter != old
	}
	if id == "" || operation == "refresh" || changedFilter {
		if id != "" && !changedFilter {
			old, err := s.db.ReadInboxSnapshot(ctx, scope, id, 0, 1, at)
			if err != nil {
				return inboxReadFailure(err), nil
			}
			filter = old.Snapshot.Filter
		}
		snap, err := s.db.CreateInboxSnapshot(ctx, scope, filter, at)
		if err != nil {
			return inboxReadFailure(err), nil
		}
		id = snap.ID
		offset = 0
	}
	page, err := s.db.ReadInboxSnapshot(ctx, scope, id, offset, 1, at)
	if err != nil {
		return inboxReadFailure(err), nil
	}
	if capture != nil {
		capture.Snapshot = page.Snapshot.ID
		capture.Navigation = fmt.Sprintf("%s:%d", page.Snapshot.ID, page.Offset)
		if page.Next >= 0 {
			capture.Navigation = fmt.Sprintf("%s:%d", page.Snapshot.ID, page.Next)
		}
		for _, decision := range page.Entries {
			capture.Sources = append(capture.Sources, decision.ConnectionID)
		}
	}
	return renderInboxPage(page), nil
}

func inboxReadFailure(err error) string {
	if errors.Is(err, store.ErrInboxFilter) {
		return err.Error()
	}
	if errors.Is(err, store.ErrInboxExpired) {
		return "This inbox snapshot expired after 24 hours. Browse again to refresh."
	}
	return "The inbox snapshot is unavailable or access changed. Browse again to refresh; this does not mean nothing is waiting."
}

func renderInboxPage(page store.InboxPage) string {
	snap := page.Snapshot
	var b strings.Builder
	fmt.Fprintf(&b, "Recorded owner waits captured %s. This is recorded state, not an atomic live view.\nOrder: source, configured project/destination, canonical ticket identity, decision identity; ascending bytewise.\n", snap.CapturedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "Filters: source=%s project=%s kind=%s.\n", inboxShort(snap.Filter.Source, 70), inboxShort(snap.Filter.Project, 70), inboxShort(snap.Filter.Kind, 70))
	if len(page.Entries) == 0 {
		b.WriteString("No matching recorded waits on this page.\n")
	}
	for _, d := range page.Entries {
		action := d.Action
		if action == "" {
			action = "Unavailable: " + d.ActionUnavailable
		}
		fmt.Fprintf(&b, "%s · %s · %s\nWaiting: %s\nNext: %s\nOpen: %s:%d:%d\n", inboxShort(d.Source, 30), inboxShort(d.Project, 65), inboxShort(d.Ticket, 90), inboxShort(d.Summary, 120), inboxShort(action, 120), snap.ID, page.Offset, d.Ordinal)
	}
	fmt.Fprintf(&b, "Coverage: %d configured ticket connections; unconfigured tickets are outside this view.\n", len(snap.Coverage))
	gaps := 0
	for _, c := range snap.Coverage {
		if c.Status != "available" && c.Status != "ok" {
			gaps++
			if gaps <= 2 {
				fmt.Fprintf(&b, "Coverage %s/%s: %s", inboxShort(c.Source, 20), inboxShort(c.Project, 40), inboxShort(c.Status, 60))
				if !c.ObservedAt.IsZero() {
					fmt.Fprintf(&b, "; last observed %s", c.ObservedAt.UTC().Format(time.RFC3339))
				}
				b.WriteByte('\n')
			}
		}
	}
	if gaps > 2 {
		fmt.Fprintf(&b, "%d additional coverage gaps.\n", gaps-2)
	}
	if page.Next >= 0 {
		fmt.Fprintf(&b, "More: %s:%d\n", snap.ID, page.Next)
	}
	if page.Offset > 0 {
		fmt.Fprintf(&b, "Back: %s:%d\n", snap.ID, max(0, page.Offset-1))
	}
	fmt.Fprintf(&b, "Coverage details: %s:0:%d\nOpen shows complete ticket identity and supported actions.\nRefresh: %s:%d", snap.ID, page.Offset, snap.ID, page.Offset)
	return b.String()
}

func (s *Service) openOwnerInbox(ctx context.Context, scope store.InboxScope, id string, offset, entry int, at time.Time, capture *inboxCapture) (string, error) {
	page, err := s.db.ReadInboxSnapshot(ctx, scope, id, entry, 1, at)
	if err != nil {
		return inboxReadFailure(err), nil
	}
	if capture != nil {
		capture.Snapshot = id
		capture.Navigation = fmt.Sprintf("%s:%d", id, offset)
	}
	back := fmt.Sprintf("\nReturn: %s:%d", id, offset)
	if len(page.Entries) == 0 {
		return "This decision is unavailable or source access changed." + back, nil
	}
	saved := page.Entries[0]
	current, err := s.db.CurrentInboxDecision(ctx, saved.Key)
	if err != nil {
		return "This recorded wait has resolved, changed, or become unavailable. Refresh to see current waits." + back, nil
	}
	if capture != nil {
		capture.Sources = []int64{current.ConnectionID}
	}
	status := "Current recorded wait."
	if current.Summary != saved.Summary || current.Action != saved.Action || current.Destination != saved.Destination {
		status = "This wait changed since capture; current facts follow."
	}
	action := current.Action
	if action == "" {
		action = "Unavailable: " + current.ActionUnavailable
	}
	text := fmt.Sprintf("%s\n%s · %s · %s\nWaiting: %s\nNext: %s\nDestination: %s", status, current.Source, inboxLine(current.Project), inboxLine(current.Ticket), inboxShort(current.Summary, 450), inboxLine(action), inboxLine(current.Destination))
	if s.inboxReader == nil {
		text += "\nLive ticket detail unavailable: no reader is configured."
	} else {
		bounded, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		detail, readErr := s.inboxReader.ReadOwnerInboxTicket(bounded, current)
		switch {
		case readErr != nil:
			text += "\nLive ticket refresh unavailable or deferred; recorded wait retained."
		case detail.Unavailable != "":
			text += "\n" + inboxShort(detail.Unavailable, 250)
		default:
			text += "\n" + inboxShort(detail.Title, 200) + "\n" + inboxShort(detail.URL, 350) + "\n" + inboxShort(detail.Body, 650)
		}
	}
	if err = s.db.InboxSourcesAuthorized(ctx, scope, id, []int64{current.ConnectionID}, at); err != nil {
		return "Source access changed while opening; refresh the inbox." + back, nil
	}
	latest, latestErr := s.db.CurrentInboxDecision(ctx, saved.Key)
	if latestErr != nil {
		return "This recorded wait resolved, changed, or became unavailable while refreshing the ticket. Refresh to see current waits." + back, nil
	}
	if latest.Action != current.Action || latest.Summary != current.Summary || latest.Destination != current.Destination {
		return "This wait changed while refreshing the ticket. Open it again for current actions." + back, nil
	}
	return text + back, nil
}

// authorizeInboxAnswer fences saved bytes independently of their original read.
func (s *Service) authorizeInboxAnswer(ctx context.Context, a store.ConversationAnswer, at time.Time) error {
	exchanges, err := s.db.ListConversationExchanges(ctx, a.TurnID)
	if err != nil {
		return err
	}
	for _, x := range exchanges {
		if x.Kind != "evidence" {
			continue
		}
		result, err := decodeConversationEvidence(x)
		if err != nil {
			return err
		}
		if result.Command == "inbox" && result.Arguments["inbox_snapshot"] != "" {
			if err = s.authorizeInboxResult(ctx, a.ChannelRef, result, at); err != nil {
				return err
			}
		}
	}
	return nil
}

// inboxNavigation is the only inbox history exposed to the conversation model.
// It deliberately omits retained ticket facts, even while access is allowed.
func inboxNavigation(text string) string {
	lines := []string{"A recorded owner inbox page was delivered. Navigation references:"}
	for _, line := range strings.Split(text, "\n") {
		for _, prefix := range []string{"More: ", "Back: ", "Open: ", "Return: ", "Refresh: ", "Coverage details: ", "Coverage more: ", "Coverage back: ", "Requests more: ", "Requests back: "} {
			if strings.HasPrefix(line, prefix) {
				lines = append(lines, line)
				break
			}
		}
	}
	return strings.Join(lines, "\n")
}

func inboxLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func inboxShort(text string, limit int) string { return shortenLine(inboxLine(text), limit) }

func renderInboxCoverage(snap store.InboxSnapshot, offset, browseOffset int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Coverage captured %s; recorded observation state.\n", snap.CapturedAt.UTC().Format(time.RFC3339))
	if offset > len(snap.Coverage) {
		offset = len(snap.Coverage)
	}
	end := min(offset+4, len(snap.Coverage))
	for _, c := range snap.Coverage[offset:end] {
		observed := "unknown"
		if !c.ObservedAt.IsZero() {
			observed = c.ObservedAt.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(&b, "%s · %s: %s; last observed %s\n", inboxShort(c.Source, 25), inboxShort(c.Project, 120), inboxShort(c.Status, 180), observed)
	}
	if end < len(snap.Coverage) {
		fmt.Fprintf(&b, "Coverage more: %s:%d:%d\n", snap.ID, end, browseOffset)
	}
	if offset > 0 {
		fmt.Fprintf(&b, "Coverage back: %s:%d:%d\n", snap.ID, max(0, offset-4), browseOffset)
	}
	fmt.Fprintf(&b, "Return: %s:%d", snap.ID, browseOffset)
	return b.String()
}

// inboxNotice persists safe read refusals as ordinary owner-visible results.
func inboxNotice(text string) (string, error) { return text, nil }

func (s *Service) authorizeInboxResult(ctx context.Context, channel string, result conversationReadResult, at time.Time) error {
	var sources []int64
	if raw, ok := result.Arguments["inbox_sources"]; ok {
		if err := json.Unmarshal([]byte(raw), &sources); err != nil {
			return err
		}
		return s.db.InboxSourcesAuthorized(ctx, s.inboxScope(channel), result.Arguments["inbox_snapshot"], sources, at)
	}
	// Older saved evidence did not record rendered source IDs, so fail closed.
	return s.db.InboxSnapshotAuthorized(ctx, s.inboxScope(channel), result.Arguments["inbox_snapshot"], at)
}
