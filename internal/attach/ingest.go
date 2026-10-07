package attach

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// maxConcurrentFetches bounds how many attachments are in flight at once
// (T-3.11).
//
// Small on purpose. The work is one HTTPS GET each against the same handful
// of GitHub hosts, so the win over sequential is real but flattens quickly,
// and every extra worker is another socket and another buffered read
// competing for the same turn's memory. Four covers the overwhelmingly
// common case — a reporter pasting a screenshot or three — without letting
// a body carrying twenty images open twenty connections to a host that
// would rather it did not.
const maxConcurrentFetches = 4

// partialSuffix marks a file that is still being streamed.
//
// Bytes land under this suffix and are renamed into place only once the
// attachment has been fetched whole, sniffed as a supported type, and found
// to fit inside every ceiling. A file that fails any of those is removed
// while still carrying the suffix, so nothing an agent could mistake for an
// ingested attachment is ever left in the landing directory (AC-3.4).
const partialSuffix = ".part"

// The fetch timing budgets (US-8, T-8.2). They live here rather than in
// internal/github because CONC-6 holds that package to "retries nothing,
// sleeps nowhere" — a rule worth keeping, since it is what makes every forge
// call in this codebase cost a predictable single round trip. The retry and
// the clock therefore sit one layer up, where a slow attachment is a
// rejection rather than a stuck turn.
const (
	// perFetchTimeout bounds one attachment's download, including its retry
	// attempt separately.
	perFetchTimeout = 30 * time.Second
	// perTurnTimeout bounds all of a turn's attachment work together, so a
	// body carrying ten slow files cannot hold a run open for five minutes
	// no matter how well each individual fetch behaves.
	perTurnTimeout = 120 * time.Second
)

// Route records which attempt actually produced an attachment's bytes, so
// the manifest can say whether the first try worked or the retry saved it
// (US-8, T-8.3). Operationally that is the difference between "attachments
// are fine" and "attachments only work because we retry", which is exactly
// what the unverified installation-token report this retry defends against
// would look like from the outside.
type Route int

const (
	// RouteNone is an attachment that never produced bytes.
	RouteNone Route = iota
	// RouteFirstAttempt means the first fetch succeeded.
	RouteFirstAttempt
	// RouteRetry means the first fetch failed and the retry succeeded.
	RouteRetry
)

// String renders the route for the manifest.
func (r Route) String() string {
	switch r {
	case RouteFirstAttempt:
		return "first attempt"
	case RouteRetry:
		return "retry"
	default:
		return "not fetched"
	}
}

// cancelledDetail is the [Rejection.Detail] for an attachment the run ended
// before reaching. It is litefactory's own words, like every other detail
// this package composes.
const cancelledDetail = "the run ended before it was fetched"

// Fetcher is the one thing this package needs from the outside world: the
// bytes behind an attachment URL.
//
// It is declared here, at the point of use, rather than exported by the
// package that implements it — the same reason internal/turn declares
// SubIssueCreator for itself. *github.Client satisfies it, and
// internal/github stays free of a Forge-shaped abstraction of its own.
//
// An implementation is expected to enforce its own SSRF allow-list before
// dialing and on every redirect hop. [IsAssetURL] in this package filters
// what is worth asking for; it is not the security boundary and must not be
// mistaken for one.
type Fetcher interface {
	FetchAsset(ctx context.Context, rawURL string) (io.ReadCloser, string, error)
}

// Limits are the ceilings one turn's ingestion runs under, resolved from
// configuration by the caller.
//
// A non-positive value disables that ceiling. That is a deliberate escape
// hatch for tests and for an operator who has decided a limit is not for
// them; it is not the default, which internal/config supplies from
// DefaultAttachmentMax* (5 MiB per image, 25 MiB total, 10 files).
type Limits struct {
	// MaxImageBytes is the per-image ceiling.
	MaxImageBytes int64
	// MaxDocumentBytes is the per-document ceiling (US-4). Documents are
	// allowed more room than images because a spec PDF or a slide deck is
	// routinely larger than a screenshot, and the point of admitting them at
	// all is that the agent works from the real document.
	MaxDocumentBytes int64
	// MaxTotalBytes is the ceiling on every accepted attachment combined.
	MaxTotalBytes int64
	// MaxCount is the ceiling on how many attachments are accepted.
	MaxCount int
}

// Attachment is one attachment that made it: fetched whole, sniffed as a
// supported type, inside every ceiling, and sitting on disk.
type Attachment struct {
	// Name is what the body called it — third-party text, kept for the
	// prompt and never used as a path.
	Name string
	// Path is the absolute path the bytes landed at. Its base name is
	// litefactory's own (see mintFileName).
	Path string
	// MediaType is the sniffed type, not the declared one.
	MediaType string
	// Bytes is the size on disk.
	Bytes int64
	// Kind is the family the sniffed bytes put it in, which is what decides
	// how the prompt describes it: an image the agent can look at, or a
	// document it has to open with its own tooling.
	Kind Kind
	// Route is which attempt produced these bytes (US-8).
	Route Route
}

// Result is everything one turn's ingestion decided: what landed, and what
// did not and why.
//
// Both slices are in the order the attachments appeared in the body, so a
// reader of the prompt or of a later manifest sees them in the order the
// reporter wrote them rather than in whatever order the fetches finished.
type Result struct {
	Accepted []Attachment
	Rejected []Rejection
	// Records is one entry per attachment considered, accepted or rejected,
	// in the same order (US-7). Accepted and Rejected are the views the
	// prompt and the reply need; this is the audit trail, and it is the only
	// one of the three that accounts for every attachment in one list.
	Records []Record
}

// Empty reports whether the turn had no attachments at all — nothing
// accepted and nothing rejected. It is the condition under which a caller
// must leave the prompt byte-for-byte as it was before this feature existed
// (AC-3.3).
func (r Result) Empty() bool { return len(r.Accepted) == 0 && len(r.Rejected) == 0 }

// Ingest fetches every attachment in refs into dir and reports what landed.
//
// It never returns an error for an attachment that did not qualify: a fetch
// that failed, a type that is not supported, a file over a ceiling — each
// becomes a [Rejection] in the result and the rest of the turn continues
// (AC-6.1). The error return is reserved for a failure that makes the whole
// operation meaningless, which in practice is only "the landing directory
// could not be created".
//
// # Order of the ceilings
//
// The turn-wide ceilings are applied before any per-file one. Count comes
// first and needs no bytes: everything past [Limits.MaxCount] is refused
// before a single connection is opened, so a body listing a hundred images
// costs one hundred rejections and zero fetches. The total-bytes ceiling
// then bounds each individual stream and is settled again when results are
// committed in order, so an attachment that would have fit its own type's
// limit is still refused once the turn's budget is gone — and it says so,
// carrying [ReasonOverTotalBytes] rather than [ReasonOversized].
//
// # Why fetches race but results do not
//
// The fetches run concurrently, bounded by [maxConcurrentFetches] and
// cancellable through ctx. The results are then committed strictly in refs
// order, so which attachment wins the last few bytes of the turn's budget is
// decided by where the reporter put it in the body, not by which host
// answered first. Two runs over the same body produce the same result.
func Ingest(ctx context.Context, f Fetcher, dir string, refs []Ref, lim Limits) (Result, error) {
	if len(refs) == 0 {
		return Result{}, nil
	}
	if f == nil {
		return Result{}, fmt.Errorf("attach: ingest: %w: nil fetcher", ErrInvalidRejection)
	}

	// Count first, before the directory exists and before anything is
	// dialed: an over-count attachment costs nothing to refuse.
	admitted := refs
	var overCount []Ref
	if lim.MaxCount > 0 && len(refs) > lim.MaxCount {
		admitted, overCount = refs[:lim.MaxCount], refs[lim.MaxCount:]
	}

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Result{}, fmt.Errorf("attach: create the attachments directory: %w", err)
	}

	// Every file this function creates, renames or removes goes through the
	// root rather than through a joined path string. mintFileName already
	// guarantees a bare, traversal-free base name, and name_test.go pins that
	// against a list of hostile inputs — but the name is still derived from
	// text a stranger typed on a public issue, and this is the one place in
	// litefactory where such text reaches a file creation. os.Root moves the
	// containment from "the sanitizer is correct" to "the kernel refuses",
	// which is the difference between a property that has to stay true under
	// every future edit and one that is enforced whether it does or not.
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Result{}, fmt.Errorf("attach: open the attachments directory: %w", err)
	}
	defer func() { _ = root.Close() }()

	// The whole turn's attachment work shares one budget (AC-8.2). Every
	// per-fetch budget below is taken off this context, so it can shorten an
	// attempt but never extend the turn past this point.
	ctx, cancel := context.WithTimeout(ctx, perTurnTimeout)
	defer cancel()

	fetched := fetchAll(ctx, f, root, admitted, lim)

	var result Result
	var total int64
	// One record per attachment, built here rather than in each worker, so
	// that "exactly one" (AC-7.1) is a property of this single loop and not
	// of every early return above it.
	record := func(ref Ref, out outcome, state Outcome, file string) {
		rec := Record{
			Name:      ref.Name,
			Source:    stripQuery(ref.URL),
			MediaType: out.attachment.MediaType,
			Bytes:     out.attachment.Bytes,
			File:      file,
			Outcome:   state,
			Route:     out.route,
			At:        time.Now(),
		}
		if out.rejection != nil {
			rec.Reason = out.rejection.Reason
			rec.Detail = out.rejection.Detail
		}
		result.Records = append(result.Records, rec)
	}

	for i, out := range fetched {
		switch {
		case out.rejection != nil:
			result.Rejected = append(result.Rejected, *out.rejection)
			record(admitted[i], out, OutcomeRejected, "")
		case lim.MaxTotalBytes > 0 && total+out.attachment.Bytes > lim.MaxTotalBytes:
			// The stream itself fit; the turn's remaining budget did not.
			// Remove the partial rather than leaving bytes nothing will
			// reference.
			removeQuietly(root, out.partName)
			rj := reject(admitted[i].Name, ReasonOverTotalBytes,
				fmt.Sprintf("%d bytes would exceed the turn's %d byte budget", out.attachment.Bytes, lim.MaxTotalBytes))
			result.Rejected = append(result.Rejected, rj)
			out.rejection = &rj
			record(admitted[i], out, OutcomeRejected, "")
		default:
			final := out.finalName
			if err := root.Rename(out.partName, final); err != nil {
				removeQuietly(root, out.partName)
				rj := reject(admitted[i].Name, ReasonFetchFailed, "could not be saved")
				result.Rejected = append(result.Rejected, rj)
				out.rejection = &rj
				record(admitted[i], out, OutcomeRejected, "")
				continue
			}
			total += out.attachment.Bytes
			att := out.attachment
			// The absolute path is composed only now, for the prompt. It is
			// never used to open anything: every write above went through the
			// root by relative name.
			att.Path = filepath.Join(dir, final)
			result.Accepted = append(result.Accepted, att)
			record(admitted[i], out, OutcomeAccepted, final)
		}
	}

	for _, ref := range overCount {
		rj := reject(ref.Name, ReasonOverCount, fmt.Sprintf("the turn accepts at most %d attachments", lim.MaxCount))
		result.Rejected = append(result.Rejected, rj)
		record(ref, outcome{rejection: &rj}, OutcomeRejected, "")
	}
	return result, nil
}

// outcome is one worker's finding: either a rejection, or an attachment
// whose bytes are sitting at partName — relative to the run's landing root —
// waiting for the total-bytes ceiling to be settled in order.
type outcome struct {
	rejection  *Rejection
	attachment Attachment
	partName   string
	// route is which attempt produced the bytes.
	route Route
	// finalName is the name to rename partName to on commit. It is carried
	// rather than derived by trimming partialSuffix because a ZIP container
	// does not know its own extension until [classifyZip] has read the
	// landed file (US-4).
	finalName string
}

// fetchAll runs the fetches concurrently and returns their outcomes in refs
// order.
//
// The slot channel is the bound; the WaitGroup is the join. Results are
// written to a pre-sized slice by index rather than sent down a channel, so
// no ordering has to be reconstructed afterwards and no mutex guards the
// collection — each worker owns exactly one element.
func fetchAll(ctx context.Context, f Fetcher, root *os.Root, refs []Ref, lim Limits) []outcome {
	out := make([]outcome, len(refs))
	slots := make(chan struct{}, maxConcurrentFetches)
	var wg sync.WaitGroup

	for i, ref := range refs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				out[i] = outcome{rejection: ptr(reject(ref.Name, ReasonFetchFailed, cancelledDetail))}
				return
			}
			// Re-checked after the slot is held, and not folded into the
			// select above: when a slot is free and ctx is already done both
			// cases are ready, and select picks between ready cases at
			// random. Without this, cancelling a run would still dial for
			// however many workers happened to win that coin toss.
			if ctx.Err() != nil {
				out[i] = outcome{rejection: ptr(reject(ref.Name, ReasonFetchFailed, cancelledDetail))}
				return
			}
			out[i] = fetchOne(ctx, f, root, i+1, ref, lim)
		}()
	}
	wg.Wait()
	return out
}

// fetchOne fetches, sniffs, bounds and lands a single attachment.
// fetchOne fetches a single attachment, retrying exactly once if the first
// attempt failed to produce bytes (US-8, T-8.1).
//
// # Exactly one retry, and only for a failure
//
// The retry fires only for [ReasonFetchFailed]. An attachment refused for
// its type or its size was not a failure — it was a decision, and repeating
// it would waste a round trip to reach the same answer. Structuring it as
// two straight-line calls rather than a loop is deliberate: "never a loop"
// (AC-8.1) is then a property of the shape, not of a bound someone has to
// maintain.
//
// # What "re-resolved URL" means here
//
// [Ref.URL] is the stable github.com/user-attachments address, not the
// signed one. GitHub answers it with a redirect to a short-lived signed URL
// on an object host, and FetchAsset follows that redirect fresh on every
// call. So asking again with the same Ref really does re-resolve the
// signature — which is precisely the failure this retry exists for, since a
// signed URL that expired between resolution and download is the shape of
// the problem the story describes.
//
// # Why any fetch failure, not just 401/404
//
// The story names ErrAssetUnauthorized and 404 specifically. This package
// cannot see that sentinel: it does not import internal/github, by design
// and by test, and consuming the fetcher through an interface is what keeps
// the package holding the installation token out of the import graph of the
// package parsing stranger-supplied markdown. Retrying every fetch failure
// is a superset of the required behaviour and costs at most one extra round
// trip on an attachment that was going to be reported as unfetchable
// anyway. If the distinction ever earns its keep, the honest way to get it
// is an error-classifying method on the Fetcher interface, not an import.
func fetchOne(ctx context.Context, f Fetcher, root *os.Root, ordinal int, ref Ref, lim Limits) outcome {
	out := attemptFetch(ctx, f, root, ordinal, ref, lim, RouteFirstAttempt)
	if out.rejection == nil || out.rejection.Reason != ReasonFetchFailed {
		return out
	}
	// A turn that has already run out of time does not get a second attempt:
	// the retry would fail on the same expired context and only delay the
	// report.
	if ctx.Err() != nil {
		return out
	}
	return attemptFetch(ctx, f, root, ordinal, ref, lim, RouteRetry)
}

// attemptFetch is one download: fetch, sniff, bound, land. It is called at
// most twice per attachment.
func attemptFetch(ctx context.Context, f Fetcher, root *os.Root, ordinal int, ref Ref, lim Limits, route Route) outcome {
	// Each attempt gets its own budget, so a retry is not punished for how
	// long the attempt before it took (AC-8.2).
	ctx, cancel := context.WithTimeout(ctx, perFetchTimeout)
	defer cancel()

	body, _, err := f.FetchAsset(ctx, ref.URL)
	if err != nil {
		// The declared Content-Type is discarded deliberately: sniffing
		// decides the type (AC-3.5), so keeping the header around would
		// only invite someone to consult it.
		return outcome{rejection: ptr(reject(ref.Name, ReasonFetchFailed, fetchDetail(err)))}
	}
	defer body.Close()

	head := make([]byte, sniffLen)
	n, err := io.ReadFull(body, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return outcome{rejection: ptr(reject(ref.Name, ReasonFetchFailed, fetchDetail(err)))}
	}
	head = head[:n]

	mediaType, ext, kind := sniff(head)
	if kind == KindUnsupported {
		// The sniffed type rides along on the rejection so the manifest can
		// say what the bytes actually were, not merely that they were wrong.
		return outcome{
			rejection:  ptr(reject(ref.Name, ReasonUnsupportedType, unsupportedDetail(mediaType))),
			attachment: Attachment{MediaType: mediaType},
			route:      route,
		}
	}

	// The binding ceiling is whichever is smaller, and which one it was
	// decides whether an overrun is reported as a per-file or a turn-wide
	// refusal. The per-file half depends on the kind: a slide deck is
	// measured against the document ceiling, not the image one (AC-4.2).
	bound, reason := bindingLimit(lim, kind)

	partName := mintFileName(ordinal, ref.Name, ext) + partialSuffix
	file, err := root.OpenFile(partName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return outcome{rejection: ptr(reject(ref.Name, ReasonFetchFailed, "could not be saved"))}
	}

	written, overflow, err := copyBounded(file, body, head, bound)
	closeErr := file.Close()
	switch {
	case err != nil || closeErr != nil:
		removeQuietly(root, partName)
		return outcome{rejection: ptr(reject(ref.Name, ReasonFetchFailed, fetchDetail(cmp(err, closeErr))))}
	case overflow:
		// Aborted mid-stream: the reader was never drained past the bound,
		// and the partial file goes away here, so no oversized bytes
		// survive anywhere (AC-3.4).
		removeQuietly(root, partName)
		return outcome{
			rejection:  ptr(reject(ref.Name, reason, fmt.Sprintf("exceeds the %d byte limit", bound))),
			attachment: Attachment{MediaType: mediaType, Bytes: written, Kind: kind},
			route:      route,
		}
	}

	// A ZIP is only provisionally accepted by sniff: it may be a DOCX, a
	// PPTX, or an archive of holiday photos, and the part that says which is
	// past the 512 bytes sniff saw. Now that the whole file has landed —
	// under the document ceiling, so this reads nothing unbounded — its
	// entry names settle it (US-4).
	if mediaType == zipMediaType {
		settled, settledExt, ok := classifyLandedZip(root, partName, written)
		if !ok {
			removeQuietly(root, partName)
			return outcome{
				rejection:  ptr(reject(ref.Name, ReasonUnsupportedType, unsupportedDetail(zipMediaType))),
				attachment: Attachment{MediaType: zipMediaType, Bytes: written, Kind: kind},
				route:      route,
			}
		}
		mediaType, ext = settled, settledExt
	}

	return outcome{
		attachment: Attachment{Name: ref.Name, MediaType: mediaType, Bytes: written, Kind: kind, Route: route},
		partName:   partName,
		route:      route,
		finalName:  mintFileName(ordinal, ref.Name, ext),
	}
}

// bindingLimit reports the ceiling a fetch of this kind is bounded by, and
// which [RejectionReason] an overflow of it should carry.
//
// The per-file half depends on the kind: images are measured against
// [Limits.MaxImageBytes] and documents against [Limits.MaxDocumentBytes],
// which is US-4's only change to how sizing works — the mechanism that
// aborts an oversized stream is US-3's, unchanged (AC-4.2).
//
// The turn-wide total then competes with it, and the smaller wins. When the
// turn's whole budget is smaller than one file's allowance, a file that
// overruns has overrun the turn, and saying [ReasonOversized] would point
// the reader at a per-file setting that was never the constraint.
func bindingLimit(lim Limits, kind Kind) (int64, RejectionReason) {
	perFile := lim.MaxImageBytes
	if kind == KindDocument {
		perFile = lim.MaxDocumentBytes
	}
	total := lim.MaxTotalBytes
	switch {
	case perFile <= 0 && total <= 0:
		return 0, ReasonOversized // no ceiling at all
	case perFile <= 0:
		return total, ReasonOverTotalBytes
	case total <= 0 || perFile <= total:
		return perFile, ReasonOversized
	default:
		return total, ReasonOverTotalBytes
	}
}

// classifyLandedZip opens a landed ZIP through the run's root and settles
// what it is from its entry names.
//
// The file is opened, read and closed here so the handle's lifetime is one
// function long: zip.Reader reads lazily from whatever it is given, so
// handing it a file whose close is somebody else's problem is how a run ends
// up holding descriptors for every archive it ever looked at.
func classifyLandedZip(root *os.Root, name string, size int64) (mediaType, ext string, ok bool) {
	f, err := root.Open(name)
	if err != nil {
		return zipMediaType, "", false
	}
	defer func() { _ = f.Close() }()

	r, err := zip.NewReader(f, size)
	if err != nil {
		return zipMediaType, "", false
	}
	return classifyZip(r)
}

// copyBounded writes head and then the rest of src to dst, stopping as soon
// as the total would pass bound.
//
// It reports overflow rather than an error because an oversized attachment
// is not a failure of anything — it is an attachment that did not qualify,
// which this package models as a value (see the package doc). A bound of
// zero or less means unbounded.
//
// The reader is abandoned at the first byte past the bound rather than
// drained. That is the whole point of streaming the ceiling instead of
// trusting Content-Length: a body that lies about its size, or never ends,
// costs bound+1 bytes and not one more.
func copyBounded(dst io.Writer, src io.Reader, head []byte, bound int64) (written int64, overflow bool, err error) {
	if bound > 0 && int64(len(head)) > bound {
		return 0, true, nil
	}
	n, err := dst.Write(head)
	written = int64(n)
	if err != nil {
		return written, false, err
	}

	rest := src
	if bound > 0 {
		// One byte past the bound: enough to know it was exceeded, never
		// enough to write an oversized file.
		rest = io.LimitReader(src, bound-written+1)
	}
	copied, err := io.Copy(dst, rest)
	written += copied
	if err != nil {
		return written, false, err
	}
	if bound > 0 && written > bound {
		return written, true, nil
	}
	return written, false, nil
}

// PromptSection renders the accepted attachments as the body of the prompt's
// "## Attachments" section, one line per file.
//
// It states the absolute path, because the agent's job is to open the file
// and the path is the only part of this that is actionable. The name the
// body gave the attachment follows in parentheses, sanitized the same way a
// rejection line sanitizes it — it is third-party text arriving in a prompt,
// and the fact that it is going to an agent rather than to a comment does
// not make it trustworthy.
//
// When any document is present, a closing note says litefactory did not
// convert it (AC-4.3). That is not politeness: litefactory deliberately
// performs no conversion, installs no pandoc or Poppler, and carries no
// DOCX parser, so a PDF or a slide deck arrives exactly as it was uploaded.
// Whether the agent's own tooling can open it is genuinely unknown — US-4
// ships this plumbing without gating ingestion on proof that it can
// (AC-4.4) — and an agent told plainly that the file is raw will try its own
// tools instead of assuming litefactory already extracted the text.
//
// Rejections are not rendered here. What the agent needs is the list of
// files it can actually open; why some other file did not make it is the
// operator's business, and belongs in the run report and the manifest.
//
// The empty string means there is nothing to say, and the caller must then
// render no section at all (AC-3.3).
func PromptSection(accepted []Attachment) string {
	if len(accepted) == 0 {
		return ""
	}
	lines := make([]string, 0, len(accepted)+2)
	documents := false
	for _, att := range accepted {
		name, named := attachmentNameSpan(att.Name)
		if !named {
			name = "`attachment`"
		}
		lines = append(lines, fmt.Sprintf("- %s — %s (%s, %d bytes)", att.Path, name, att.MediaType, att.Bytes))
		if att.Kind == KindDocument {
			documents = true
		}
	}
	if documents {
		lines = append(lines, "",
			"The documents above are exactly as they were uploaded. litefactory does not "+
				"convert them, so open or extract them with your own tools if you need their contents.")
	}
	return strings.Join(lines, "\n")
}

// reject builds a [Rejection] for an attachment that did not qualify.
//
// NewRejection's validation exists to catch a caller mistake, and the only
// one reachable here is an empty name — a body can reference an attachment
// with no alt text and a URL whose last path segment is empty. That is not
// worth propagating an error for, so it falls back to a generic name and the
// turn carries on.
func reject(name string, reason RejectionReason, detail string) Rejection {
	if strings.TrimSpace(name) == "" {
		name = "attachment"
	}
	rj, err := NewRejection(name, reason, detail)
	if err != nil {
		return Rejection{Name: "attachment", Reason: reason, Detail: detail}
	}
	return rj
}

// fetchDetail reduces a transport error to litefactory's own words.
//
// The error's text is never echoed. It can carry the URL it failed on, and a
// signed attachment URL carries a credential in its query string — putting
// that in a comment or a manifest would undo AC-6.5 at the last step.
func fetchDetail(err error) string {
	if err == nil {
		return "the fetch did not complete"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return cancelledDetail
	}
	return "the fetch did not complete"
}

// cmp returns the first non-nil of its arguments.
func cmp(a, b error) error {
	if a != nil {
		return a
	}
	return b
}

// ptr boxes a Rejection so an outcome can distinguish "rejected" from the
// zero value.
func ptr(rj Rejection) *Rejection { return &rj }

// removeQuietly deletes a partial file, ignoring the error.
//
// There is nothing useful to do about a failed cleanup: the attachment is
// already being rejected for a reason the caller will report, and turning a
// stale temp file into a second, louder failure would replace a precise
// message with a confusing one. The file is inside the run's own directory
// and carries the .part suffix, so nothing downstream will mistake it for an
// ingested attachment.
func removeQuietly(root *os.Root, name string) {
	if name != "" {
		_ = root.Remove(name)
	}
}
