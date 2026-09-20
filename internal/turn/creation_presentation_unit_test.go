package turn

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
)

func presentationRef(t *testing.T) naming.Ref {
	t.Helper()
	ref, err := naming.ParseRef("github:acme/acme-api#123")
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestCreationPresentationDeliveryIdentity(t *testing.T) {
	t.Parallel()
	ref := presentationRef(t)
	r := &Reporter{botUserID: 99}
	hashA := strings.Repeat("a", 64)
	hashB := strings.Repeat("b", 64)
	a := r.deliveryIntent(ref, ref, 1, 40, 0, creationPresentationPurpose+hashA)
	if again := r.deliveryIntent(ref, ref, 1, 40, 0, creationPresentationPurpose+hashA); again.Key != a.Key || again.Marker != a.Marker {
		t.Fatal("presentation identity is not deterministic")
	}
	if a.RunID != 0 || a.Purpose != "creation-proposal:"+hashA || a.TriggerID != 40 || a.SessionID != 1 {
		t.Fatalf("presentation shape: %+v", a)
	}
	if !strings.Contains(a.Marker, a.Key) {
		t.Fatalf("marker %q does not carry key", a.Marker)
	}
	for name, other := range map[string]string{
		"hash":    r.deliveryIntent(ref, ref, 1, 40, 0, creationPresentationPurpose+hashB).Key,
		"trigger": r.deliveryIntent(ref, ref, 1, 41, 0, creationPresentationPurpose+hashA).Key,
		"run":     r.deliveryIntent(ref, ref, 1, 40, 9, creationPresentationPurpose+hashA).Key,
		"purpose": r.deliveryIntent(ref, ref, 1, 40, 0, "reply:x").Key,
	} {
		if other == a.Key {
			t.Fatalf("changing %s did not change the delivery key", name)
		}
	}
}

func TestCreationPresentationCarveOutAcceptsOnlyThatPurpose(t *testing.T) {
	t.Parallel()
	for purpose, want := range map[string]bool{
		"creation-proposal:" + strings.Repeat("a", 64): true,
		"creation-proposal:":                           true,
		"":                                             false,
		"run-report":                                   false,
		"reply:creation-proposal:schedule:0:proposal:0:confirmation:0": false,
		"reply:creation_proposal:schedule:0:proposal:0:confirmation:0": false,
		"Creation-Proposal:abc":                                      false,
		"\x20creation-proposal:abc":                                  false,
		"creation-proposals:abc":                                     false,
		"reply:unknown_command:schedule:0:proposal:0:confirmation:0": false,
		"reply:" + report.ReplyCreationProposal.String() + ":schedule:0:proposal:0:x": false,
	} {
		if got := isCreationPresentation(purpose); got != want {
			t.Errorf("isCreationPresentation(%q)=%v want %v", purpose, got, want)
		}
	}
}

func presentationRenderer(t *testing.T) *report.Reporter {
	t.Helper()
	renderer, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{}}), Limit: 65536})
	if err != nil {
		t.Fatal(err)
	}
	return renderer
}

func presentationEnvelope(t *testing.T, body string) (string, string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"version": 1, "repository": "acme/acme-api", "source_task": presentationRef(t).String(), "relationship": "linked", "artifacts": []map[string]string{{"title": "T", "body": body}}})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return string(raw), hex.EncodeToString(sum[:])
}

func TestCreationPresentationRenderedBodyCarriesHashAndApprovalCommand(t *testing.T) {
	t.Parallel()
	envelope, hash := presentationEnvelope(t, "exact publishable content")
	body, err := presentationRenderer(t).RenderReply(t.Context(), report.Reply{Ref: presentationRef(t), Kind: report.ReplyCreationProposal, Creation: &report.CreationReply{Envelope: envelope, Hash: hash, Relationship: "linked"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{envelope, hash, "`/mcp-create " + hash + " linked`"} {
		if !strings.Contains(body.String(), want) {
			t.Errorf("body lacks %q", want)
		}
	}
}

func TestCreationPresentationRendererEnforcesSizeAndHash(t *testing.T) {
	t.Parallel()
	renderer := presentationRenderer(t)
	envelope, hash := presentationEnvelope(t, "content")
	huge, hugeHash := presentationEnvelope(t, strings.Repeat("x", 48001))
	for name, creation := range map[string]*report.CreationReply{
		"oversized":     {Envelope: huge, Hash: hugeHash, Relationship: "linked"},
		"wrong hash":    {Envelope: envelope, Hash: strings.Repeat("0", 64), Relationship: "linked"},
		"bad relation":  {Envelope: envelope, Hash: hash, Relationship: "other"},
		"not json":      {Envelope: "{", Hash: hash, Relationship: "linked"},
		"missing reply": nil,
	} {
		if _, err := renderer.RenderReply(t.Context(), report.Reply{Ref: presentationRef(t), Kind: report.ReplyCreationProposal, Creation: creation}); err == nil {
			t.Errorf("%s: renderer accepted the presentation", name)
		}
	}
}
