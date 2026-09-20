package store

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestRetainedCreationProposalReturnsExactRetainedBytes(t *testing.T) {
	t.Parallel()
	f := newCreationDBFixture(t)
	_, p := f.proposal(t, "api", 313, 2)
	if err := f.c.RetainProposal(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	got, err := f.c.RetainedCreationProposal(t.Context(), p.Hash)
	if err != nil {
		t.Fatal(err)
	}
	if got.Hash != p.Hash || got.Repository != p.Repository || got.SourceTask != p.SourceTask || got.Relationship != p.Relationship || got.Artifacts != p.Artifacts || !bytes.Equal(got.Envelope, p.Envelope) {
		t.Fatalf("retained proposal differs: %+v", got)
	}
	if _, err := f.c.RetainedCreationProposal(t.Context(), strings.Repeat("0", 64)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("absent hash: %v", err)
	}
}
