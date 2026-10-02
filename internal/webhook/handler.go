package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// Path is the one URL path the handler serves.
	Path = "/litefactory/webhook"
	// MaxBodyBytes caps the request body; GitHub's own cap is 25 MiB, and a
	// payload this daemon cares about is a few kilobytes.
	MaxBodyBytes = 1 << 20

	headerEvent     = "X-GitHub-Event"
	headerDelivery  = "X-GitHub-Delivery"
	headerSignature = "X-Hub-Signature-256"
	signaturePrefix = "sha256="

	// maxLoggedField bounds an authenticated header value copied into a log line.
	maxLoggedField = 64

	rejectLogEvery = 10 * time.Second
)

// unauthorizedBody is the 401 body for every authentication failure. It is one
// constant on purpose: byte-for-byte identity is the property.
const unauthorizedBody = "unauthorized\n"

// Lookup resolves a registered repository and its cached webhook secret. It
// must never block on I/O: the handler calls it on the request path.
type Lookup interface {
	// Lookup receives the lower-cased "owner/name". found is false for a
	// repository that is not registered; an empty secret means the repository
	// has none configured.
	Lookup(fullName string) (repoID int64, secret []byte, found bool)
}

// Sink receives wake-up hints. Wake must not block.
type Sink interface {
	Wake(repoID int64, issueNumber int)
}

// Options configures a [Handler].
type Options struct {
	Lookup Lookup
	Sink   Sink
	Logger *slog.Logger
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Handler is the webhook http.Handler.
type Handler struct {
	lookup Lookup
	sink   Sink
	log    *slog.Logger
	now    func() time.Time

	seen *deliveries
	// admit serialises the duplicate check, the hint and the recording, so two
	// concurrent redeliveries of one id cannot both queue a hint.
	admit sync.Mutex

	rejectMu   sync.Mutex
	rejectNext time.Time
	suppressed int
}

// NewHandler validates options and builds the handler.
func NewHandler(opts Options) (*Handler, error) {
	switch {
	case opts.Lookup == nil:
		return nil, errors.New("webhook: a lookup is required")
	case opts.Sink == nil:
		return nil, errors.New("webhook: a hint sink is required")
	case opts.Logger == nil:
		return nil, errors.New("webhook: a logger is required")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	return &Handler{
		lookup: opts.Lookup,
		sink:   opts.Sink,
		log:    opts.Logger,
		now:    now,
		seen:   newDeliveries(MaxDeliveryIDs, DeliveryIDTTL),
	}, nil
}

type repositoryPayload struct {
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

type numberRef struct {
	Number int `json:"number"`
}

type eventPayload struct {
	Issue       *numberRef `json:"issue"`
	ParentIssue *numberRef `json:"parent_issue"`
	SubIssue    *numberRef `json:"sub_issue"`
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != Path {
		h.reject(w, http.StatusNotFound, "not found")
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		h.reject(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if r.ContentLength > MaxBodyBytes {
		h.reject(w, http.StatusRequestEntityTooLarge, "payload too large")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, MaxBodyBytes))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.reject(w, http.StatusRequestEntityTooLarge, "payload too large")
			return
		}
		h.reject(w, http.StatusBadRequest, "unreadable body")
		return
	}

	event := r.Header.Get(headerEvent)
	delivery := r.Header.Get(headerDelivery)
	if event == "" || delivery == "" {
		h.reject(w, http.StatusBadRequest, "missing required header")
		return
	}
	var minimal repositoryPayload
	if err := json.Unmarshal(body, &minimal); err != nil {
		h.reject(w, http.StatusBadRequest, "malformed payload")
		return
	}

	repoID, secret, found := h.lookup.Lookup(strings.ToLower(minimal.Repository.FullName))
	if !found || len(secret) == 0 {
		// A throwaway MAC keeps the unknown-repository path as expensive as
		// the known one.
		_ = computeMAC([]byte("litefactory"), body)
		h.unauthorized(w)
		return
	}
	given, ok := parseSignature(r.Header.Get(headerSignature))
	if !ok || subtle.ConstantTimeCompare(given, computeMAC(secret, body)) != 1 {
		h.unauthorized(w)
		return
	}

	// From here on the sender holds the repository's secret.
	h.accepted(w, event, delivery, minimal.Repository.FullName, repoID, body)
}

func (h *Handler) accepted(w http.ResponseWriter, event, delivery, fullName string, repoID int64, body []byte) {
	log := h.log.With(
		slog.String("delivery", clip(delivery)),
		slog.String("event", clip(event)),
		slog.String("repo", clip(fullName)))

	if event == "ping" {
		log.Info("webhook delivery answered", slog.String("outcome", "ping"))
		respond(w, http.StatusOK, "pong\n")
		return
	}
	if event != "issues" && event != "issue_comment" && event != "sub_issues" {
		log.Info("webhook delivery answered", slog.String("outcome", "ignored_event"))
		respond(w, http.StatusAccepted, "ignored\n")
		return
	}

	var payload eventPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Info("webhook delivery answered", slog.String("outcome", "no_issue"))
		respond(w, http.StatusAccepted, "no issue\n")
		return
	}
	var numbers []int
	switch event {
	case "sub_issues":
		numbers = appendNumber(numbers, payload.ParentIssue)
		numbers = appendNumber(numbers, payload.SubIssue)
	default:
		numbers = appendNumber(numbers, payload.Issue)
	}
	if len(numbers) == 0 {
		log.Info("webhook delivery answered", slog.String("outcome", "no_issue"))
		respond(w, http.StatusAccepted, "no issue\n")
		return
	}

	h.admit.Lock()
	defer h.admit.Unlock()
	now := h.now()
	if h.seen.seen(delivery, now) {
		log.Info("webhook delivery answered", slog.String("outcome", "duplicate"))
		respond(w, http.StatusOK, "duplicate\n")
		return
	}
	for _, n := range numbers {
		h.sink.Wake(repoID, n)
	}
	h.seen.record(delivery, now)
	log.Info("webhook delivery answered", slog.String("outcome", "queued"), slog.Any("issues", numbers))
	respond(w, http.StatusAccepted, "queued\n")
}

func appendNumber(numbers []int, ref *numberRef) []int {
	if ref == nil || ref.Number <= 0 {
		return numbers
	}
	return append(numbers, ref.Number)
}

func (h *Handler) unauthorized(w http.ResponseWriter) {
	h.reject(w, http.StatusUnauthorized, "")
}

// reject answers a request that never proved it holds a secret. The 401 body
// is always [unauthorizedBody]; the log line is debug-level, rate limited, and
// carries no request-controlled text.
func (h *Handler) reject(w http.ResponseWriter, status int, text string) {
	if status == http.StatusUnauthorized {
		text = unauthorizedBody
	} else {
		text += "\n"
	}
	h.logReject(status)
	respond(w, status, text)
}

func (h *Handler) logReject(status int) {
	now := h.now()
	h.rejectMu.Lock()
	if now.Before(h.rejectNext) {
		h.suppressed++
		h.rejectMu.Unlock()
		return
	}
	h.rejectNext = now.Add(rejectLogEvery)
	suppressed := h.suppressed
	h.suppressed = 0
	h.rejectMu.Unlock()
	h.log.Debug("webhook request rejected",
		slog.Int("status", status),
		slog.Int("suppressed_since_last", suppressed))
}

func respond(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func computeMAC(secret, body []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return mac.Sum(nil)
}

// parseSignature decodes "sha256=<64 hex>" and reports whether it was well
// formed.
func parseSignature(header string) ([]byte, bool) {
	hexPart, ok := strings.CutPrefix(header, signaturePrefix)
	if !ok || len(hexPart) != 2*sha256.Size {
		return nil, false
	}
	sum, err := hex.DecodeString(hexPart)
	if err != nil {
		return nil, false
	}
	return sum, true
}

func clip(s string) string {
	if len(s) > maxLoggedField {
		return s[:maxLoggedField]
	}
	return s
}

// Sign returns the header value GitHub would send for body under secret. It
// exists for the daemon's tests and for operators writing a replay script.
func Sign(secret, body []byte) string {
	return signaturePrefix + hex.EncodeToString(computeMAC(secret, body))
}
