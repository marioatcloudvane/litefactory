package webhook

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
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
	selector := r.URL.Query().Get("subscription")
	source := Source{}
	found := false
	if lookup, ok := h.lookup.(QualifiedLookup); ok && selector != "" {
		source, found = lookup.LookupSubscription(selector)
	}
	signature, err := hex.DecodeString(r.Header.Get("Linear-Signature"))
	if !found || source.Provider != "linear" || len(source.Secret) == 0 || err != nil || len(signature) != sha256.Size {
		_ = computeMAC([]byte("litefactory"), body)
		h.unauthorized(w)
		return
	}
	if subtle.ConstantTimeCompare(signature, computeMAC(source.Secret, body)) != 1 {
		h.unauthorized(w)
		return
	}
	var payload linearPayload
	if json.Unmarshal(body, &payload) != nil || payload.WebhookTimestamp == 0 || source.WorkspaceID == "" || payload.OrganizationID != source.WorkspaceID {
		h.unauthorized(w)
		return
	}
	sent := time.UnixMilli(payload.WebhookTimestamp)
	now := h.now()
	if sent.Before(now.Add(-time.Minute)) || sent.After(now.Add(time.Minute)) {
		h.unauthorized(w)
		return
	}
	event := r.Header.Get("Linear-Event")
	if event != payload.Type {
		h.unauthorized(w)
		return
	}
	if event != "Issue" && event != "Comment" {
		respond(w, http.StatusOK, "ignored\n")
		return
	}
	// Comments need not repeat team/project. The issue locator is always reread
	// through the connection selected before authentication.
	id := payload.Data.ID
	if event == "Comment" {
		id = payload.Data.IssueID
	}
	if id == "" || strings.TrimSpace(id) != id || len(id) > 128 {
		h.unauthorized(w)
		return
	}
	if (payload.Data.TeamID != "" && payload.Data.TeamID != source.TeamID) || (payload.Data.ProjectID != "" && payload.Data.ProjectID != source.ProjectID) {
		respond(w, http.StatusOK, "ignored\n")
		return
	}
	sink, ok := h.sink.(OpaqueSink)
	if !ok {
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
		respond(w, http.StatusOK, "duplicate\n")
		return
	}
	sink.WakeOpaqueSubscription(source, id)
	if delivery != "" {
		h.seen.record(key, now)
	}
	respond(w, http.StatusOK, "queued\n")
}
