package webhook

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// OpaqueSink receives a qualified opaque ticket locator. The daemon must reread
// its workspace/team/project before durable handoff or admission.
type OpaqueSink interface{ WakeOpaqueSubscription(Source, string) }

type linearPayload struct {
	Type             string `json:"type"`
	OrganizationID   string `json:"organizationId"`
	WebhookTimestamp int64  `json:"webhookTimestamp"`
	Data             struct {
		ID        string `json:"id"`
		IssueID   string `json:"issueId"`
		TeamID    string `json:"teamId"`
		ProjectID string `json:"projectId"`
	} `json:"data"`
}

func (h *Handler) linear(w http.ResponseWriter, r *http.Request, body []byte) {
	outcome, reason := "rejected", "selector_unknown_or_unavailable"
	defer func() { h.linearOutcome(r, outcome, reason) }()
	selector := r.URL.Query().Get("subscription")
	source := Source{}
	found := false
	if lookup, ok := h.lookup.(QualifiedLookup); ok && selector != "" {
		source, found = lookup.LookupSubscription(selector)
	}
	if !found || source.Provider != "linear" || len(source.Secret) == 0 {
		_ = computeMAC([]byte("litefactory"), body)
		h.unauthorized(w)
		return
	}
	reason = "invalid_signature"
	if r.Header.Get("Linear-Signature") == "" {
		reason = "missing_signature"
	}
	signature, err := hex.DecodeString(r.Header.Get("Linear-Signature"))
	if err != nil || len(signature) != sha256.Size {
		_ = computeMAC([]byte("litefactory"), body)
		h.unauthorized(w)
		return
	}
	if subtle.ConstantTimeCompare(signature, computeMAC(source.Secret, body)) != 1 {
		h.unauthorized(w)
		return
	}
	reason = "malformed_payload"
	var payload linearPayload
	if json.Unmarshal(body, &payload) != nil {
		h.unauthorized(w)
		return
	}
	reason = "workspace_mismatch"
	if source.WorkspaceID == "" || payload.OrganizationID != source.WorkspaceID {
		h.unauthorized(w)
		return
	}
	reason = "invalid_timestamp"
	sent := time.UnixMilli(payload.WebhookTimestamp)
	now := h.now()
	if sent.Before(now.Add(-time.Minute)) || sent.After(now.Add(time.Minute)) {
		h.unauthorized(w)
		return
	}
	reason = "event_mismatch"
	event := r.Header.Get("Linear-Event")
	if event != payload.Type {
		h.unauthorized(w)
		return
	}
	if event != "Issue" && event != "Comment" {
		outcome, reason = "ignored", "unsupported_event"
		respond(w, http.StatusOK, "ignored\n")
		return
	}
	// Comments need not repeat team/project. The issue locator is always reread
	// through the connection selected before authentication.
	id := payload.Data.ID
	if event == "Comment" {
		id = payload.Data.IssueID
	}
	reason = "invalid_issue_id"
	if id == "" || strings.TrimSpace(id) != id || len(id) > 128 {
		h.unauthorized(w)
		return
	}
	if (payload.Data.TeamID != "" && payload.Data.TeamID != source.TeamID) || (payload.Data.ProjectID != "" && payload.Data.ProjectID != source.ProjectID) {
		outcome, reason = "ignored", "project_or_team_mismatch"
		respond(w, http.StatusOK, "ignored\n")
		return
	}
	sink, ok := h.sink.(OpaqueSink)
	if !ok {
		outcome, reason = "ignored", "sink_unavailable"
		respond(w, http.StatusOK, "ignored\n")
		return
	}
	h.admit.Lock()
	defer h.admit.Unlock()
	delivery := r.Header.Get("Linear-Delivery")
	// Delivery IDs are optional optimization only; durable reply consumption owns
	// deduplication after an authoritative read.
	key := source.Provider + "\x00" + source.Origin + "\x00" + selector + "\x00" + delivery
	if delivery != "" && h.seen.seen(key, now) {
		outcome, reason = "duplicate", "delivery_seen"
		respond(w, http.StatusOK, "duplicate\n")
		return
	}
	sink.WakeOpaqueSubscription(source, id)
	if delivery != "" {
		h.seen.record(key, now)
	}
	outcome, reason = "queued", "verification_pending"
	respond(w, http.StatusOK, "queued\n")
}

// HTTP receipt is a hint, not durable observation or admission.
func (h *Handler) linearOutcome(r *http.Request, outcome, reason string) {
	h.log.WarnContext(r.Context(), "Linear webhook delivery answered",
		slog.String("subscription", clip(r.URL.Query().Get("subscription"))),
		slog.String("outcome", outcome), slog.String("reason", reason))
}
