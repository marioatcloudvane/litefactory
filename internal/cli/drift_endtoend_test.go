package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/drift"
	"github.com/cloudvane/litefactory/internal/git"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/gittest"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/secrets"
	"github.com/cloudvane/litefactory/internal/standards"
	"github.com/cloudvane/litefactory/internal/store"
)

// This controlled analyst proves the application path, not live provider isolation.
func TestDriftEndToEndNoninteractiveRetainedProposal(t *testing.T) {
	paths := newRepoHome(t)
	repo := gittest.New(t)
	repo.WriteFile("architecture.md", "Use direct handlers.\n")
	base := repo.Commit("base architecture")
	repo.WriteFile("architecture.md", "Use a bounded worker queue.\nCredential: fixture-secret-value\n")
	head := repo.Commit("bound architecture work")
	db, err := store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.UpsertRepo(t.Context(), store.Repo{Path: repo.Dir, Forge: store.ForgeGitHub, Owner: "acme", Name: "api", RemoteURL: "git@github.com:acme/api.git", BaseBranch: "main", FeaturePrefix: "lf"}); err != nil {
		t.Fatal(err)
	}
	book := &standards.Service{Store: db, OwnerID: 71}
	seed, err := book.Create(t.Context(), "seed-guidance", 0, []standards.Operation{{Kind: "category.add", Category: &standards.Category{ID: "architecture", Name: "Architecture"}}, {Kind: "standard.add", Standard: &standards.Standard{ID: "bound-work", Title: "Bound work", Body: "Declare work limits.", CategoryIDs: []string{"architecture"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = book.Approve(t.Context(), seed.ProposalID, seed.Version, seed.ChangeDigest); err != nil {
		t.Fatal(err)
	}
	revision, err := book.Publish(t.Context(), seed.ProposalID, seed.Version, seed.ChangeDigest)
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	binary, err := git.Resolve(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	analyst := &driftFixtureAnalyst{}
	configure := func(_ context.Context, s *drift.Service, rt *repoRuntime, _ *Env, _ drift.Request) error {
		redactor := redact.New(redact.Options{})
		redactor.AddToken(secrets.NewToken("fixture-secret-value"))
		s.Acquisition = &drift.Acquirer{Store: rt.db, Git: binary, ClonePath: repo.Dir, Repo: github.Repo{Owner: "acme", Name: "api"}, Discussion: driftFixtureDiscussion{}, Redactor: redactor}
		s.Runner = analyst
		return nil
	}
	run := func(command *Command, args ...string) string {
		t.Helper()
		var out, diagnostic bytes.Buffer
		env := &Env{Stdout: &out, Stderr: &diagnostic, Stdin: strings.NewReader(""), Logger: slog.New(slog.DiscardHandler)}
		commands := Commands()
		if command != nil {
			for i, c := range commands {
				if c.Name == command.Name {
					commands[i] = command
				}
			}
		}
		if code := NewRouter("litefactory", env, commands).Run(t.Context(), args); code != ExitOK {
			t.Fatalf("%v: exit %d: %s", args, code, diagnostic.String())
		}
		return out.String()
	}
	output := run(newStandardsDriftCommand(configure), "standards", "analyze", "drift", "--repo", "acme/api", "--base", base, "--head", head, "--issue", "123", "--key", "fixture-analysis", "--json")
	var created store.DriftAnalysis
	if err = json.Unmarshal([]byte(output), &created); err != nil {
		t.Fatal(err)
	}
	if created.Execution != "completed" || created.Disposition != "proposals" || len(created.ProposalLinks) != 1 {
		t.Fatalf("analysis did not retain proposal: %s", output)
	}
	var inspection drift.Inspection
	if err = json.Unmarshal([]byte(run(nil, "standards", "analysis", "show", created.ID, "--json")), &inspection); err != nil {
		t.Fatal(err)
	}
	retained := inspection.Analysis
	if inspection.Standards.Status.Integrity != "verified" || len(inspection.Standards.Selections) != 1 || inspection.Standards.Status.Binding.RevisionID != revision.ID {
		t.Fatalf("reopened standards lost binding or selection: %+v", inspection.Standards)
	}
	if retained.ID != created.ID || len(retained.ProposalLinks) != 1 || retained.ProposalLinks[0] != created.ProposalLinks[0] || !bytes.Contains(retained.Pinned, []byte(base)) || !bytes.Contains(retained.Pinned, []byte(head)) || !bytes.Contains(retained.Standards, []byte("verified")) {
		t.Fatalf("reopened analysis lost provenance: %+v", retained)
	}
	var listed []store.DriftAnalysis
	if err = json.Unmarshal([]byte(run(nil, "standards", "analysis", "list", "--repo", "acme/api", "--json")), &listed); err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("analysis list lost retained identity: %+v %v", listed, err)
	}
	text := run(nil, "standards", "analysis", "show", created.ID)
	if !strings.Contains(text, created.ProposalLinks[0].Digest) || !strings.Contains(text, "Coverage:") {
		t.Fatal(text)
	}
	var evidence []store.DriftEvidence
	if err = json.Unmarshal([]byte(run(nil, "standards", "analysis", "evidence", created.ID, "--json")), &evidence); err != nil {
		t.Fatal(err)
	}
	var code, discussion bool
	for _, e := range evidence {
		if strings.Contains(e.Text, "fixture-secret-value") {
			t.Fatal("retained evidence exposes credential")
		}
		code = code || strings.Contains(e.Text, "bounded worker queue")
		discussion = discussion || strings.Contains(e.Text, "queue limits")
	}
	if len(evidence) > 0 {
		var excerpt store.DriftEvidence
		if err = json.Unmarshal([]byte(run(nil, "standards", "analysis", "evidence", created.ID, "--evidence", evidence[0].ID, "--json")), &excerpt); err != nil {
			t.Fatal(err)
		}
		if excerpt.Digest != evidence[0].Digest || excerpt.Text != evidence[0].Text {
			t.Fatal("evidence read differs from retained manifest")
		}
	}
	if !code || !discussion {
		t.Fatalf("retained evidence incomplete: %+v", evidence)
	}
	var report struct {
		Report   drift.Report   `json:"report"`
		Coverage drift.Coverage `json:"coverage"`
	}
	if err = json.Unmarshal(retained.Report, &report); err != nil || len(report.Report.Findings) != 1 || len(report.Coverage.Gaps) != 0 || report.Coverage.UnknownRemainder {
		t.Fatalf("retained report lost bounded findings or coverage: %+v %v", report, err)
	}
	if report.Report.SelectionDigest != inspection.Standards.Selections[0].Digest || len(report.Report.Findings[0].Citations) != len(evidence) {
		t.Fatal("retained report lost standards selection or evidence citations")
	}
	for _, citation := range report.Report.Findings[0].Citations {
		matched := false
		for _, item := range evidence {
			if citation.EvidenceID == item.ID && citation.Digest == item.Digest && citation.Start == 0 && citation.End == len(item.Text) {
				matched = true
			}
		}
		if !matched {
			t.Fatalf("retained citation does not match inspected evidence: %+v", citation)
		}
	}
	link := retained.ProposalLinks[0]
	db, err = store.Open(t.Context(), paths.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if e := db.Close(); e != nil {
			t.Error(e)
		}
	}()
	proposal, err := (&standards.Service{Store: db}).Inspect(t.Context(), link.ProposalID, link.Version)
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Proposal.ChangeDigest != link.Digest || proposal.Approval != nil || proposal.Publication != nil {
		t.Fatalf("proposal was changed or approved: %+v", proposal)
	}
	current, err := db.GetStandardsRevision(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if current.ID != revision.ID {
		t.Fatal("analysis published guidance")
	}
	run(nil, "standards", "analysis", "expire-evidence", created.ID, "--json")
	var expired store.DriftEvidence
	if err = json.Unmarshal([]byte(run(nil, "standards", "analysis", "evidence", created.ID, "--evidence", evidence[0].ID, "--json")), &expired); err != nil {
		t.Fatal(err)
	}
	if expired.Text != "" || !strings.Contains(expired.Omission, "expired") || expired.Digest != evidence[0].Digest || expired.Locator != evidence[0].Locator {
		t.Fatalf("expired evidence lost provenance or retained its body: %+v", expired)
	}
	var afterExpiry drift.Inspection
	if err = json.Unmarshal([]byte(run(nil, "standards", "analysis", "show", created.ID, "--json")), &afterExpiry); err != nil {
		t.Fatal(err)
	}
	if len(afterExpiry.Analysis.ProposalLinks) != 1 || afterExpiry.Analysis.ProposalLinks[0] != link || !bytes.Equal(afterExpiry.Analysis.Report, retained.Report) {
		t.Fatal("evidence expiry rewrote the accepted report or original proposal references")
	}
}

type driftFixtureDiscussion struct{}

func (driftFixtureDiscussion) ReadDriftDiscussion(_ context.Context, _ github.Repo, _ int, _ int) (github.DriftDiscussion, error) {
	return github.DriftDiscussion{Issue: github.Issue{ID: 1001, Number: 123, Body: "Document queue limits.", Title: "Architecture limits", HTMLURL: "https://github.com/acme/api/issues/123", CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}, Bytes: 200}, nil
}
func (driftFixtureDiscussion) ReadDriftComments(_ context.Context, _ github.Repo, _ int, _ int, _ int, _ int) (github.DriftComments, error) {
	return github.DriftComments{Comments: []github.Comment{{ID: 1002, Body: "Explicit queue limits help operators."}}, Bytes: 100}, nil
}

type driftFixtureAnalyst struct{}

func (*driftFixtureAnalyst) Isolation(context.Context) (string, error) {
	return "controlled fixture; no live certification", nil
}
func (f *driftFixtureAnalyst) Run(ctx context.Context, inv *drift.Invocation) error {
	call := func(name string, input any, dst any) error {
		raw, e := json.Marshal(input)
		if e != nil {
			return e
		}
		output, e := inv.Call(ctx, name, raw)
		if e != nil {
			return e
		}
		if dst != nil {
			return json.Unmarshal(output, dst)
		}
		return nil
	}
	if err := call("standards_categories_list", map[string]any{}, nil); err != nil {
		return err
	}
	var selection standards.RuntimeStatus
	if err := call("standards_select", map[string]any{"expected_version": 0, "category_ids": []string{"architecture"}, "reason": "Changes affect bounded architecture work."}, &selection); err != nil {
		return err
	}
	if selection.Selection == nil {
		return errors.New("fixture did not select standards")
	}
	var guidance struct {
		Body     string `json:"body"`
		Complete bool   `json:"complete"`
	}
	if err := call("standards_read", map[string]any{"selection_version": selection.Selection.Version, "selection_digest": selection.Selection.Digest}, &guidance); err != nil {
		return err
	}
	if guidance.Body != "Declare work limits." || !guidance.Complete {
		return errors.New("fixture did not retrieve exact retained standard body")
	}
	if err := call("standards_read", map[string]any{"selection_version": selection.Selection.Version, "selection_digest": selection.Selection.Digest, "acknowledge": true}, nil); err != nil {
		return err
	}
	var manifest struct {
		Evidence []store.DriftEvidence `json:"evidence"`
	}
	if err := call("drift_evidence_list", map[string]any{"page_size": 25}, &manifest); err != nil {
		return err
	}
	citations := []map[string]any{}
	for _, e := range manifest.Evidence {
		var page struct {
			EvidenceID string `json:"evidence_id"`
			Digest     string `json:"digest"`
			Start      int    `json:"start"`
			End        int    `json:"end"`
		}
		if err := call("drift_evidence_read", map[string]any{"evidence_id": e.ID, "start": 0, "limit": 4096}, &page); err != nil {
			return err
		}
		citations = append(citations, map[string]any{"evidence_id": e.ID, "digest": e.Digest, "start": page.Start, "end": page.End})
	}
	if len(citations) == 0 {
		return errors.New("fixture received no evidence")
	}
	result := map[string]any{"request_id": "fixture-result", "report": map[string]any{"disposition": "proposals", "explanation": "The queue change warrants explicit guidance.", "uncertainty": "Fixture judgment requires human review.", "selection_digest": selection.Selection.Digest, "findings": []any{map[string]any{"observed_drift": "The architecture now queues work.", "reason": "Queue operation needs explicit bounds.", "benefit": "Operators can assess finite work.", "uncertainty": "The ideal ceiling depends on workload.", "citations": citations}}, "candidates": []any{map[string]any{"key": "queue-guidance", "finding": 0, "operation": standards.Operation{Kind: "standard.add", Standard: &standards.Standard{ID: "queue-limits", Title: "Declare queue limits", Body: "Document queue capacity and work limits.", CategoryIDs: []string{"architecture"}}}}}}}
	if err := call("communication_preflight", result, nil); err != nil {
		return err
	}
	if err := call("communication_submit", result, nil); err != nil {
		return err
	}
	return call("communication_receipt", map[string]any{"request_id": "fixture-result"}, nil)
}
