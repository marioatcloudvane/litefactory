package turn

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

var proposalUnitRef = naming.Ref{Forge: "github", Owner: "example", Repo: "results", Issue: 263}

func proposalUnitService() *CreationProposalService {
	return &CreationProposalService{sourceIssue: proposalUnitRef.Issue}
}

func TestCreationProposalNonce_DeterministicAndBound(t *testing.T) {
	t.Parallel()
	b := store.ResultBinding{Task: proposalUnitRef.String(), RunID: 7, Attempt: "attempt-1"}
	args := []byte(`{"drafts":[{"title":"T","body":"B"}]}`)
	base := creationProposalNonce(b, args)
	if base != creationProposalNonce(b, args) {
		t.Fatal("identical call changed nonce")
	}
	other := b
	other.Task = "github:example/results#264"
	otherRun := b
	otherRun.RunID = 8
	otherAttempt := b
	otherAttempt.Attempt = "attempt-2"
	for name, got := range map[string][16]byte{
		"task":    creationProposalNonce(other, args),
		"run":     creationProposalNonce(otherRun, args),
		"attempt": creationProposalNonce(otherAttempt, args),
		// The bytes the agent sent are hashed, not a re-encoding of them.
		"bytes": creationProposalNonce(b, []byte(`{"drafts": [{"title":"T","body":"B"}]}`)),
	} {
		if got == base {
			t.Errorf("nonce ignores %s", name)
		}
	}
}

func TestLinkSourceIssue(t *testing.T) {
	t.Parallel()
	const source = 263
	for _, tt := range []struct {
		name string
		in   []int
		want []int
	}{
		{"none", nil, []int{source}},
		{"appended after others", []int{280, 9}, []int{280, 9, source}},
		{"already present is not duplicated", []int{source, 280}, []int{source, 280}},
		{"already present in the middle", []int{280, source, 9}, []int{280, source, 9}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			in := []submission.CreationArtifact{{Title: "T", Body: "B", LinkIssues: slices.Clone(tt.in)}, {Title: "U", Body: "C", LinkIssues: slices.Clone(tt.in)}}
			got := linkSourceIssue(in, source)
			for i, a := range got {
				if !slices.Equal(a.LinkIssues, tt.want) {
					t.Errorf("artifact %d links %v, want %v", i, a.LinkIssues, tt.want)
				}
				if n := slices.Index(a.LinkIssues, source); n < 0 || slices.Contains(a.LinkIssues[n+1:], source) {
					t.Errorf("artifact %d does not list the source exactly once: %v", i, a.LinkIssues)
				}
			}
			for i := range in {
				if !slices.Equal(in[i].LinkIssues, tt.in) {
					t.Errorf("input artifact %d mutated: %v", i, in[i].LinkIssues)
				}
			}
		})
	}
	if got := linkSourceIssue(nil, source); len(got) != 0 {
		t.Fatalf("no artifacts produced %v", got)
	}
}

// The helper does not cap: the canonical validator does, after the source issue
// is appended, so 16 targets already containing the source pass and 16 that do
// not are refused.
func TestLinkSourceIssue_CapIsEnforcedAfterAppend(t *testing.T) {
	t.Parallel()
	links := func(withSource bool) []int {
		out := make([]int, 0, 16)
		for i := 1; len(out) < 15; i++ {
			out = append(out, 1000+i)
		}
		if withSource {
			return append(out, proposalUnitRef.Issue)
		}
		return append(out, 2000)
	}
	for _, tt := range []struct {
		name       string
		withSource bool
		refused    bool
	}{{"sixteen including the source", true, false}, {"sixteen without the source", false, true}} {
		artifacts := linkSourceIssue([]submission.CreationArtifact{{Title: "T", Body: "B", LinkIssues: links(tt.withSource)}}, proposalUnitRef.Issue)
		_, _, err := submission.CanonicalCreation(submission.CreationEnvelope{Version: 1, Repository: "example/results", SourceTask: proposalUnitRef.String(), Relationship: "linked", Artifacts: artifacts})
		if (err != nil) != tt.refused {
			t.Errorf("%s: err=%v, refused=%v", tt.name, err, tt.refused)
		}
	}
}

func TestCreationProposalEnvelope_FillsFactoryDefaults(t *testing.T) {
	t.Parallel()
	s := proposalUnitService()
	e, err := s.envelope(CreationProposalRequest{Drafts: []CreationProposalDraft{{Title: "T", Body: "B", LinkIssues: []int{280}}}})
	if err != nil {
		t.Fatal(err)
	}
	if e.Relationship != "linked" || e.Artifacts[0].ParentIssue != 0 || !slices.Equal(e.Artifacts[0].LinkIssues, []int{280, 263}) {
		t.Fatalf("linked default: %+v", e)
	}
	e, err = s.envelope(CreationProposalRequest{Relationship: "attach", Drafts: []CreationProposalDraft{{Title: "T", Body: "B"}, {Title: "U", Body: "C", ParentIssue: 263}}})
	if err != nil {
		t.Fatal(err)
	}
	for i, a := range e.Artifacts {
		if e.Relationship != "attach" || a.ParentIssue != 263 || !slices.Equal(a.LinkIssues, []int{263}) {
			t.Errorf("attach draft %d: %+v", i, a)
		}
	}
	if _, err := s.envelope(CreationProposalRequest{Relationship: "attach", Drafts: []CreationProposalDraft{{Title: "T", Body: "B", ParentIssue: 99}}}); err == nil {
		t.Fatal("attach to a foreign parent accepted")
	}
}

// The refusal list at the pure-validation level: the envelope a draft set turns
// into must be refused by the canonical validator before anything is retained.
func TestCreationProposalEnvelope_CanonicalRefusals(t *testing.T) {
	t.Parallel()
	draft := func() CreationProposalDraft { return CreationProposalDraft{Title: "Title", Body: "Body"} }
	many := func(n int) []CreationProposalDraft {
		out := make([]CreationProposalDraft, n)
		for i := range out {
			out[i] = draft()
		}
		return out
	}
	with := func(mutate func(*CreationProposalDraft)) []CreationProposalDraft {
		d := draft()
		mutate(&d)
		return []CreationProposalDraft{d}
	}
	seventeen := make([]int, 16)
	for i := range seventeen {
		seventeen[i] = 500 + i
	}
	for _, tt := range []struct {
		name         string
		relationship string
		drafts       []CreationProposalDraft
		refused      bool
	}{
		{"one draft", "", many(1), false},
		{"five drafts", "", many(5), false},
		{"zero drafts", "", nil, true},
		{"six drafts", "", many(6), true},
		{"blank title", "", with(func(d *CreationProposalDraft) { d.Title = " \t" }), true},
		{"blank body", "", with(func(d *CreationProposalDraft) { d.Body = "\n" }), true},
		{"title at limit", "", with(func(d *CreationProposalDraft) { d.Title = strings.Repeat("t", 256) }), false},
		{"title over limit", "", with(func(d *CreationProposalDraft) { d.Title = strings.Repeat("t", 257) }), true},
		{"body at limit", "", with(func(d *CreationProposalDraft) { d.Body = strings.Repeat("b", 65536) }), false},
		{"body over limit", "", with(func(d *CreationProposalDraft) { d.Body = strings.Repeat("b", 65537) }), true},
		{"zero link target", "", with(func(d *CreationProposalDraft) { d.LinkIssues = []int{0} }), true},
		{"negative link target", "", with(func(d *CreationProposalDraft) { d.LinkIssues = []int{-4} }), true},
		{"sixteen targets plus source", "", with(func(d *CreationProposalDraft) { d.LinkIssues = seventeen }), true},
		{"parent with linked", "linked", with(func(d *CreationProposalDraft) { d.ParentIssue = 263 }), true},
		{"wrong parent with attach", "attach", with(func(d *CreationProposalDraft) { d.ParentIssue = 99 }), true},
		{"negative parent", "attach", with(func(d *CreationProposalDraft) { d.ParentIssue = -1 }), true},
		{"unknown relationship", "adopt", many(1), true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			e, err := proposalUnitService().envelope(CreationProposalRequest{Relationship: tt.relationship, Drafts: tt.drafts})
			if err == nil {
				e.Repository, e.SourceTask, e.Version = "example/results", proposalUnitRef.String(), 1
				_, _, err = submission.CanonicalCreation(e)
			}
			if (err != nil) != tt.refused {
				t.Fatalf("err=%v, refused=%v", err, tt.refused)
			}
		})
	}
}

func TestCreationProposalRequest_RefusesFactoryFieldsAndUnknowns(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, args string
		ok         bool
	}{
		{"minimal", `{"drafts":[{"title":"T","body":"B"}]}`, true},
		{"all fields", `{"relationship":"attach","drafts":[{"title":"T","body":"B","parent_issue":263,"link_issues":[1]}]}`, true},
		{"repository", `{"repository":"example/results","drafts":[{"title":"T","body":"B"}]}`, false},
		{"source_task", `{"source_task":"github:example/results#263","drafts":[{"title":"T","body":"B"}]}`, false},
		{"version", `{"version":1,"drafts":[{"title":"T","body":"B"}]}`, false},
		{"factory field in a draft", `{"drafts":[{"title":"T","body":"B","repository":"x/y"}]}`, false},
		{"unknown draft field", `{"drafts":[{"title":"T","body":"B","labels":["x"]}]}`, false},
		{"trailing data", `{"drafts":[{"title":"T","body":"B"}]} {}`, false},
		{"duplicate key", `{"drafts":[],"drafts":[{"title":"T","body":"B"}]}`, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var q CreationProposalRequest
			err := decodeMCPArguments([]byte(tt.args), &q)
			if (err == nil) != tt.ok {
				t.Fatalf("err=%v, ok=%v", err, tt.ok)
			}
		})
	}
}

func TestCreationProposalService_PermissionMaterial(t *testing.T) {
	t.Parallel()
	s := &CreationProposalService{authorizer: &CreationAuthorizer{enabled: true, ownerID: 10, botID: 20}, sourceIssue: 263}
	data, err := s.PermissionMaterial()
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 3 || fields["Enabled"] != true || fields["BotID"] != float64(20) || fields["SourceIssue"] != float64(263) {
		t.Fatalf("material %s carries more or less than gate, bot and source", data)
	}
}

func TestCreationProposalReceiptProjection(t *testing.T) {
	t.Parallel()
	project := CreationProposalReceiptProjection()
	scope := store.FamilyScope{RunID: 7}
	for _, tt := range []struct {
		name string
		ref  store.FamilyReference
		want string
	}{
		{"linked to the run's proposal", store.FamilyReference{Kind: "creation_proposal", ID: 7}, "accepted"},
		{"crash before the reference", store.FamilyReference{}, ""},
		{"another run's row", store.FamilyReference{Kind: "creation_proposal", ID: 8}, ""},
		{"another kind", store.FamilyReference{Kind: "comment", ID: 7}, ""},
	} {
		states, publications, err := project(t.Context(), store.FamilyRequest{Scope: scope, Tool: "issues_create_prepare", Reference: tt.ref})
		if err != nil || states.Input != tt.want || states.Effects != "" || len(publications) != 0 {
			t.Errorf("%s: states=%+v publications=%v err=%v", tt.name, states, publications, err)
		}
	}
}
