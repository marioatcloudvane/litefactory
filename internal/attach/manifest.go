package attach

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// ManifestFile is the manifest's name inside the worktree's .aiteam
// directory.
//
// It is a plain file in the run's own worktree, committed by litefactory
// alongside everything else the turn changed. That is the whole design: the
// owner's first decision in this feature was that litefactory, not the
// agent, does the fetching, specifically so what happened stays inspectable.
// This is that promise made concrete, and it is deliberately no more durable
// than the rest of the turn's committed output — the spec's Out of Scope
// list rules out a table and a migration for it (AC-7.4), so the record's
// history is git's history and nothing else.
const ManifestFile = "attachments.md"

// Outcome is what happened to one attachment: it landed, or it did not.
type Outcome string

const (
	// OutcomeAccepted means the bytes are on disk and named in the prompt.
	OutcomeAccepted Outcome = "accepted"
	// OutcomeRejected means litefactory declined it, for the reason the
	// record carries.
	OutcomeRejected Outcome = "rejected"
)

// Record is one attachment ingestion attempt, accepted or rejected.
//
// Exactly one is produced per attachment litefactory considered (AC-7.1),
// including the ones refused before a connection was opened — an operator
// asking "did the run see the screenshot?" is owed an answer for every file
// on the issue, not only the ones that got as far as being downloaded.
type Record struct {
	// Name is what the body called the attachment. Third-party text,
	// sanitized on the way into the rendered manifest.
	Name string
	// Source is the attachment URL with any query string removed
	// (AC-7.2). A GitHub asset URL redirects to a signed one whose query
	// carries a credential; the stable address is what identifies the
	// attachment, and the signature is what must never be written down.
	Source string
	// MediaType is what the bytes sniffed as, where litefactory got far
	// enough to look. Empty for an attachment refused before it was
	// fetched.
	MediaType string
	// Bytes is the size measured on the way in, or 0 if it never got that
	// far.
	Bytes int64
	// File is the name the attachment landed under, empty when rejected.
	File string
	// Outcome is accepted or rejected.
	Outcome Outcome
	// Reason is why it was rejected. Meaningless when accepted.
	Reason RejectionReason
	// Detail is litefactory's own extra context for the reason.
	Detail string
	// Route is which fetch attempt produced the bytes (US-8, T-8.3).
	Route Route
	// At is when the outcome was decided.
	At time.Time
}

// stripQuery removes the query string and fragment from a URL, leaving the
// part that identifies the attachment.
//
// This is AC-7.2's mechanism and it is the reason the manifest can be
// committed to a repository at all. GitHub answers a stable
// user-attachments URL with a redirect to a signed one, and that signature
// travels in the query string. A manifest that recorded the URL verbatim
// would be a credential written into git history — durable, pushed, and
// visible to everyone with read access.
//
// A URL that does not parse is reported as "unparseable" rather than echoed.
// If it could not be parsed it cannot be trimmed, and writing an
// unparseable, third-party-supplied string into the file is exactly the
// thing this function exists to prevent.
func stripQuery(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "unparseable"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.User = nil
	return parsed.String()
}

// Manifest renders the run's attachment records as the body of
// attachments.md.
//
// It is a worktree artefact, not a reply. That distinction matters
// operationally: reply text passes through internal/redact on its way to a
// comment, and this does not (AC-7.3). The safety of what it contains is
// therefore a property of how records are built, not of a filter at the end
// — which is why [stripQuery] runs at construction, why nothing here ever
// touches attachment bytes, and why the only third-party text in the file,
// the attachment's name, goes through the same sanitizer a rejection line
// uses.
//
// An empty record set renders the empty string, and the caller writes no
// file: a manifest saying nothing happened is worse than no manifest,
// because it invites the reader to conclude the run considered attachments
// and found none when in fact it never looked.
func Manifest(ref string, ordinal int, records []Record) string {
	if len(records) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# Attachments — %s, run %04d\n\n", ref, ordinal)
	b.WriteString("Every attachment litefactory considered for this run, accepted or not.\n")
	b.WriteString("Fetched by litefactory, never by the agent. URLs are recorded without their\n")
	b.WriteString("query strings: a GitHub asset URL redirects to a signed one, and the signature\n")
	b.WriteString("is a credential.\n\n")

	b.WriteString("| # | Attachment | Outcome | Type | Bytes | Landed as | Route | When | Notes |\n")
	b.WriteString("|---|---|---|---|---|---|---|---|---|\n")
	for i, r := range records {
		name, named := attachmentNameSpan(r.Name)
		if !named {
			name = "`attachment`"
		}
		// The span sanitizer stops at the characters that would break a
		// comment; a table cell has one more. A pipe is legal in a filename
		// and survives the span — and it survives inside backticks too,
		// because a markdown table is split before code spans are parsed —
		// so it is escaped here, where the table is built, rather than in the
		// shared sanitizer, which has no idea it is being put in one.
		name = escapePipes(name)
		outcome := string(r.Outcome)
		if r.Outcome == OutcomeRejected {
			outcome = fmt.Sprintf("rejected — %s", r.Reason)
		}
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s | %s | %s | %s | %s |\n",
			i+1,
			name,
			outcome,
			cell(r.MediaType),
			bytesCell(r.Bytes),
			cell(r.File),
			r.Route,
			r.At.UTC().Format(time.RFC3339),
			cell(r.Detail),
		)
	}

	b.WriteString("\n## Sources\n\n")
	for i, r := range records {
		fmt.Fprintf(&b, "%d. %s\n", i+1, cell(r.Source))
	}
	return b.String()
}

// cell renders a table cell, escaping the one character that would break the
// row and substituting a dash for an empty value.
//
// The pipe escape is not cosmetic. An attachment name reaches this file, and
// a name containing a pipe would otherwise split into extra columns and
// misalign every value after it — an audit record that silently reports the
// wrong byte count against the wrong file is worse than one that is ugly.
func cell(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return escapePipes(s)
}

// escapePipes makes a string safe to put in a markdown table cell.
//
// This is not cosmetic. A pipe is a legal filename character, so an
// attachment named "a|b" would otherwise split into extra columns and shift
// every value after it — an audit record that reports the wrong byte count
// against the wrong file is worse than one that is merely ugly, because it
// is wrong while looking right.
func escapePipes(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// bytesCell renders a byte count, or a dash when nothing was measured.
func bytesCell(n int64) string {
	if n <= 0 {
		return "—"
	}
	return fmt.Sprintf("%d", n)
}
