package attach

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"
)

// ooxml builds a real ZIP container shaped like an Office file: the content
// types part first, as Office writes it, then the part that says which
// application owns the document. Real archives, so the tests exercise the
// same archive/zip path production does.
func ooxml(t *testing.T, bodyPart string, pad int) []byte {
	t.Helper()

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	// Stored uncompressed and padded so [Content_Types].xml alone runs past
	// the 512 bytes the sniffer sees — which is the whole reason DOCX and
	// PPTX cannot be told apart by magic number and have to be classified
	// after landing.
	ct, err := w.Create("[Content_Types].xml")
	if err != nil {
		t.Fatalf("create content types: %v", err)
	}
	if _, err := ct.Write([]byte(strings.Repeat("<!-- padding -->", pad))); err != nil {
		t.Fatalf("write content types: %v", err)
	}
	part, err := w.Create(bodyPart)
	if err != nil {
		t.Fatalf("create %s: %v", bodyPart, err)
	}
	if _, err := part.Write([]byte("<xml/>")); err != nil {
		t.Fatalf("write %s: %v", bodyPart, err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

// TestIngestLandsOfficeDocumentsByTheirParts is AC-4.1 for the two formats a
// magic number cannot distinguish. Both are ZIP archives; only the parts
// inside say which is which.
func TestIngestLandsOfficeDocumentsByTheirParts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		part     string
		wantType string
		wantExt  string
	}{
		{"docx", "word/document.xml", mediaDOCX, ".docx"},
		{"pptx", "ppt/presentation.xml", mediaPPTX, ".pptx"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			url := assetURL("the-spec")
			f := newFetcher().serve(url, ooxml(t, tc.part, 64))

			got, err := Ingest(context.Background(), f, dir, []Ref{{Name: "the spec", URL: url}}, generous)
			if err != nil {
				t.Fatalf("Ingest: %v", err)
			}
			if len(got.Accepted) != 1 {
				t.Fatalf("accepted %d, want 1: %+v", len(got.Accepted), got)
			}
			att := got.Accepted[0]
			if att.MediaType != tc.wantType {
				t.Errorf("MediaType = %q, want %q", att.MediaType, tc.wantType)
			}
			if att.Kind != KindDocument {
				t.Errorf("Kind = %v, want KindDocument", att.Kind)
			}
			if !strings.HasSuffix(att.Path, tc.wantExt) {
				t.Errorf("landed at %s, want a %s", att.Path, tc.wantExt)
			}
			// The landed file keeps the .part suffix off and nothing else is
			// left over from classification.
			if left := dirEntries(t, dir); len(left) != 1 {
				t.Errorf("directory holds %v, want exactly one file", left)
			}
		})
	}
}

// TestIngestDeclinesAPlainArchive is the reason classification reads the
// parts rather than trusting the magic number: holiday-photos.zip renamed to
// report.docx is a ZIP exactly like a real DOCX is, and the difference is
// only visible inside.
func TestIngestDeclinesAPlainArchive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	url := assetURL("report.docx")
	f := newFetcher().serve(url, ooxml(t, "photos/beach.jpg", 64))

	got, err := Ingest(context.Background(), f, dir, []Ref{{Name: "report.docx", URL: url}}, generous)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(got.Accepted) != 0 {
		t.Fatalf("accepted %+v, want none: a plain archive is not a document", got.Accepted)
	}
	if len(got.Rejected) != 1 || got.Rejected[0].Reason != ReasonUnsupportedType {
		t.Fatalf("rejections = %+v, want one ReasonUnsupportedType", got.Rejected)
	}
	// The archive was written to disk before it could be classified. It must
	// not still be there.
	if left := dirEntries(t, dir); len(left) != 0 {
		t.Errorf("directory holds %v, want nothing", left)
	}
}

// TestDocumentsUseTheDocumentCeiling is AC-4.2: a document is measured
// against attachment_max_document_bytes, and it is refused through the same
// mechanism an oversized image is, not a separate path.
func TestDocumentsUseTheDocumentCeiling(t *testing.T) {
	t.Parallel()

	// The image ceiling is tiny and the document ceiling is generous. A PDF
	// larger than the image ceiling must still land: if the wrong ceiling
	// were applied it would be refused.
	lim := Limits{MaxImageBytes: 100, MaxDocumentBytes: 50_000, MaxTotalBytes: 1 << 20, MaxCount: 10}

	dir := t.TempDir()
	f := newFetcher().serve(assetURL("spec"), body(pdfMagic, 10_000))
	got, err := Ingest(context.Background(), f, dir, refsFor("spec"), lim)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(got.Accepted) != 1 {
		t.Fatalf("accepted %d, want 1 — the image ceiling must not bind a document: %+v", len(got.Accepted), got.Rejected)
	}

	// And past its own ceiling it is refused with the same reason and the
	// same mid-stream abort an image gets.
	dir2 := t.TempDir()
	f2 := newFetcher().serve(assetURL("huge-spec"), body(pdfMagic, 80_000))
	got2, err := Ingest(context.Background(), f2, dir2, refsFor("huge-spec"), lim)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(got2.Rejected) != 1 || got2.Rejected[0].Reason != ReasonOversized {
		t.Fatalf("rejections = %+v, want one ReasonOversized", got2.Rejected)
	}
	if left := dirEntries(t, dir2); len(left) != 0 {
		t.Errorf("directory holds %v, want nothing", left)
	}
}

// TestPromptSectionTellsTheAgentDocumentsAreRaw is AC-4.3.
func TestPromptSectionTellsTheAgentDocumentsAreRaw(t *testing.T) {
	t.Parallel()

	withDoc := PromptSection([]Attachment{
		{Name: "spec", Path: "/lf/01-spec.pdf", MediaType: mediaPDF, Bytes: 10, Kind: KindDocument},
	})
	if !strings.Contains(withDoc, "does not") || !strings.Contains(withDoc, "convert") {
		t.Errorf("document section does not say litefactory performs no conversion:\n%s", withDoc)
	}

	// An images-only turn gets no conversion note: there is nothing to
	// convert, and a standing instruction that never applies is noise.
	imagesOnly := PromptSection([]Attachment{
		{Name: "shot", Path: "/lf/01-shot.png", MediaType: "image/png", Bytes: 10, Kind: KindImage},
	})
	if strings.Contains(imagesOnly, "convert") {
		t.Errorf("images-only section mentions conversion:\n%s", imagesOnly)
	}
}

// TestImagesAndDocumentsLandTogether is AC-4.1's "alongside any images from
// the same turn", and AC-4.4: nothing is gated on whether the agent can
// actually read the document.
func TestImagesAndDocumentsLandTogether(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	f := newFetcher().
		serve(assetURL("shot"), body(pngMagic, 300)).
		serve(assetURL("spec"), body(pdfMagic, 400)).
		serve(assetURL("deck"), ooxml(t, "ppt/presentation.xml", 64))

	got, err := Ingest(context.Background(), f, dir, refsFor("shot", "spec", "deck"), generous)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(got.Accepted) != 3 {
		t.Fatalf("accepted %d, want 3: %+v / %+v", len(got.Accepted), got.Accepted, got.Rejected)
	}
	wantTypes := []string{"image/png", mediaPDF, mediaPPTX}
	for i, want := range wantTypes {
		if got.Accepted[i].MediaType != want {
			t.Errorf("attachment %d is %q, want %q", i, got.Accepted[i].MediaType, want)
		}
	}
	if left := dirEntries(t, dir); len(left) != 3 {
		t.Errorf("directory holds %v, want three files", left)
	}
}
