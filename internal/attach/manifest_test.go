package attach

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestEveryAttemptProducesExactlyOneRecord is AC-7.1. The turn below mixes
// every outcome the pipeline can reach — landed, unsupported, oversized,
// unfetchable, over-count — so a path that forgot to record, or recorded
// twice, shows up as a count mismatch rather than as a quiet gap in an audit
// trail nobody reads until they need it.
func TestEveryAttemptProducesExactlyOneRecord(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	f := newFetcher().
		serve(assetURL("good"), body(pngMagic, 300)).
		serve(assetURL("text"), []byte("not an image and not a document")).
		serve(assetURL("huge"), body(pngMagic, 90_000)).
		fail(assetURL("gone"), errAlwaysFails).
		serve(assetURL("extra"), body(pngMagic, 100))

	lim := Limits{MaxImageBytes: 5000, MaxDocumentBytes: 5000, MaxTotalBytes: 1 << 20, MaxCount: 4}
	refs := refsFor("good", "text", "huge", "gone", "extra")

	got, err := Ingest(context.Background(), f, dir, refs, lim)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(got.Records) != len(refs) {
		t.Fatalf("got %d records for %d attachments: %+v", len(got.Records), len(refs), got.Records)
	}
	if len(got.Accepted)+len(got.Rejected) != len(refs) {
		t.Errorf("accepted %d + rejected %d != %d attempts", len(got.Accepted), len(got.Rejected), len(refs))
	}

	// Each record must name the attachment it belongs to, in body order.
	for i, want := range []string{"good", "text", "huge", "gone", "extra"} {
		if got.Records[i].Name != want {
			t.Errorf("record %d is for %q, want %q", i, got.Records[i].Name, want)
		}
		if got.Records[i].At.IsZero() {
			t.Errorf("record %d has no timestamp", i)
		}
	}

	wantOutcomes := []Outcome{OutcomeAccepted, OutcomeRejected, OutcomeRejected, OutcomeRejected, OutcomeRejected}
	for i, want := range wantOutcomes {
		if got.Records[i].Outcome != want {
			t.Errorf("record %d outcome = %q, want %q (%+v)", i, got.Records[i].Outcome, want, got.Records[i])
		}
	}
	if got.Records[4].Reason != ReasonOverCount {
		t.Errorf("the fifth attachment should be over count, got %v", got.Records[4].Reason)
	}
	// An accepted record names the file it landed as; a rejected one does not
	// claim a file that is not there.
	if got.Records[0].File == "" {
		t.Error("the accepted record does not name the landed file")
	}
	for _, r := range got.Records[1:] {
		if r.File != "" {
			t.Errorf("rejected record for %q claims a landed file %q", r.Name, r.File)
		}
	}
}

// TestManifestNeverCarriesASignedURL is AC-7.2, and it is the one that keeps
// this file safe to commit. A GitHub asset URL redirects to a signed one, and
// the signature travels in the query string; a manifest that recorded it
// verbatim would write a credential into git history, pushed and readable by
// everyone with access to the repository.
func TestManifestNeverCarriesASignedURL(t *testing.T) {
	t.Parallel()

	const signed = "https://objects.githubusercontent.com/gh/asset/9?X-Amz-Signature=DEADBEEFSECRET&token=v2_abc#frag"
	dir := t.TempDir()
	f := newFetcher().serve(signed, body(pngMagic, 128))

	got, err := Ingest(context.Background(), f, dir, []Ref{{Name: "shot", URL: signed}}, generous)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	rendered := Manifest("github:acme/api#1", 1, got.Records)

	for _, leak := range []string{"DEADBEEFSECRET", "X-Amz-Signature", "token=", "v2_abc", "?", "#frag"} {
		if strings.Contains(rendered, leak) {
			t.Errorf("manifest leaked %q:\n%s", leak, rendered)
		}
	}
	// The stable part is still there — the record has to identify which
	// attachment it is about.
	if !strings.Contains(rendered, "objects.githubusercontent.com/gh/asset/9") {
		t.Errorf("manifest does not identify the attachment source:\n%s", rendered)
	}
}

func TestStripQueryKeepsTheAddressAndDropsTheCredential(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"https://github.com/user-attachments/assets/abc?jwt=secret": "https://github.com/user-attachments/assets/abc",
		"https://host/a/b#section":                                  "https://host/a/b",
		"https://user:pass@host/a":                                  "https://host/a",
		"https://host/a":                                            "https://host/a",
		"://nonsense":                                               "unparseable",
	}
	for in, want := range tests {
		if got := stripQuery(in); got != want {
			t.Errorf("stripQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestManifestNeverCarriesAttachmentBytes is the other half of AC-7.2. The
// Record type has no field that could hold content, and this pins that: the
// rendered file is composed only of litefactory's own words plus a sanitized
// name and a stripped URL.
func TestManifestNeverCarriesAttachmentBytes(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	secret := body(pngMagic, 400)
	copy(secret[16:], []byte("PIXELS-THAT-MUST-NOT-BE-QUOTED"))
	f := newFetcher().serve(assetURL("shot"), secret)

	got, err := Ingest(context.Background(), f, dir, refsFor("shot"), generous)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	rendered := Manifest("github:acme/api#1", 1, got.Records)
	if strings.Contains(rendered, "PIXELS-THAT-MUST-NOT-BE-QUOTED") {
		t.Errorf("manifest quoted attachment bytes:\n%s", rendered)
	}
}

// TestManifestSanitisesTheAttachmentName keeps a hostile filename from
// breaking the table it is rendered into. A name with a pipe would otherwise
// split the row and misalign every column after it, which in an audit record
// means reporting the wrong size against the wrong file.
func TestManifestSanitisesTheAttachmentName(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	rendered := Manifest("github:acme/api#1", 2, []Record{{
		Name:      "a|b`c\n| 999 | rewritten |",
		Source:    "https://github.com/user-attachments/assets/x",
		MediaType: "image/png",
		Bytes:     10,
		File:      "01-a.png",
		Outcome:   OutcomeAccepted,
		Route:     RouteFirstAttempt,
		At:        at,
	}})

	for _, line := range strings.Split(rendered, "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		// Only unescaped pipes separate columns; "\|" is a literal pipe
		// inside a cell, so it is removed before counting.
		separators := strings.Count(strings.ReplaceAll(line, `\|`, ""), "|")
		if separators != 10 {
			t.Errorf("table row has %d column separators, want 10 — a name broke the columns:\n%q", separators, line)
		}
	}
	if strings.Contains(rendered, "999") {
		t.Errorf("an injected cell survived into the table:\n%s", rendered)
	}
}

func TestManifestRecordsTheFetchRoute(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)
	rendered := Manifest("github:acme/api#1", 3, []Record{
		{Name: "first", Source: "https://h/a", Outcome: OutcomeAccepted, Route: RouteFirstAttempt, At: at},
		{Name: "second", Source: "https://h/b", Outcome: OutcomeAccepted, Route: RouteRetry, At: at},
	})
	if !strings.Contains(rendered, "first attempt") {
		t.Errorf("manifest does not record the first-attempt route:\n%s", rendered)
	}
	if !strings.Contains(rendered, "retry") {
		t.Errorf("manifest does not record the retry route (AC-8.4):\n%s", rendered)
	}
}

func TestManifestIsEmptyWithoutRecords(t *testing.T) {
	t.Parallel()

	if got := Manifest("github:acme/api#1", 1, nil); got != "" {
		t.Errorf("Manifest(nil) = %q, want empty so the caller writes no file", got)
	}
}
