package report_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
	"github.com/cloudvane/litefactory/internal/submission"
)

func creationPreview(t *testing.T, relationship, body string) *report.CreationReply {
	t.Helper()
	artifact := submission.CreationArtifact{Title: "Exact title ", Body: body, LinkIssues: []int{123, 264}}
	if relationship == "attach" {
		artifact.ParentIssue = 123
	}
	envelope := submission.CreationEnvelope{Version: 1, Repository: "acme/acme-api", SourceTask: "github:acme/acme-api#123", Relationship: relationship, Artifacts: []submission.CreationArtifact{artifact}}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	return &report.CreationReply{Envelope: string(raw), Hash: hex.EncodeToString(hash[:]), Relationship: relationship}
}

func TestReporter_CreationProposalPreservesCompleteApproval(t *testing.T) {
	t.Parallel()
	for _, relationship := range []string{"linked", "attach"} {
		t.Run(relationship+"/exact content and command", func(t *testing.T) {
			t.Parallel()
			preview := creationPreview(t, relationship, "Reviewed body\nwith ``` and https://github.com/acme/acme-api/issues/264\n")
			got, err := newReporter(t).RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyCreationProposal, Creation: preview})
			if err != nil {
				t.Fatal(err)
			}
			if got.Truncated() || !strings.Contains(got.String(), preview.Envelope) || !strings.Contains(got.String(), "/mcp-create "+preview.Hash+" "+relationship) {
				t.Fatalf("complete reviewed bytes or exact approval command absent: %s", got.String())
			}
		})
	}
}

func TestReporter_CreationProposalRefusesChangedOrIncompletePreview(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*report.CreationReply)
		limit  int
	}{
		{"wrong fingerprint", func(p *report.CreationReply) { p.Hash = strings.Repeat("f", 64) }, 65536},
		{"changed body after retention", func(p *report.CreationReply) {
			p.Envelope = strings.Replace(p.Envelope, "Reviewed body", "Other body", 1)
		}, 65536},
		{"malformed JSON", func(p *report.CreationReply) { p.Envelope = "{" }, 65536},
		{"unsupported relationship", func(p *report.CreationReply) { p.Relationship = "unrelated" }, 65536},
		{"truncated proposal", func(*report.CreationReply) {}, 128},
		{"presentation exceeds intake bound", func(p *report.CreationReply) { *p = *creationPreview(t, "linked", strings.Repeat("x", 48001)) }, 65536},
		{"redaction changes reviewed bytes", func(p *report.CreationReply) { *p = *creationPreview(t, "linked", "secret-preview-token-123456789") }, 65536},
	}
	for _, tt := range cases {
		t.Run(tt.name+"/no approvable output", func(t *testing.T) {
			t.Parallel()
			preview := creationPreview(t, "linked", "Reviewed body")
			tt.change(preview)
			rep, err := report.New(report.Options{Redactor: redact.New(redact.Options{Environ: []string{"API_TOKEN=secret-preview-token-123456789"}}), Limit: tt.limit})
			if err != nil {
				t.Fatal(err)
			}
			got, err := rep.RenderReply(t.Context(), report.Reply{Ref: testRef, Kind: report.ReplyCreationProposal, Creation: preview})
			if err == nil || got.String() != "" {
				t.Fatalf("unsafe preview returned body=%q error=%v", got.String(), err)
			}
		})
	}
}
