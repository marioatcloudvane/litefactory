package turn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// proposalForge fails every call: drafting must never reach the forge.
type proposalForge struct{ calls atomic.Int32 }

var errProposalForge = errors.New("the forge must not be reached")

func (f *proposalForge) CreateIssue(context.Context, github.Repo, redact.Body, redact.Body) (github.Issue, error) {
	f.calls.Add(1)
	return github.Issue{}, errProposalForge
}
func (f *proposalForge) AddSubIssue(context.Context, github.Repo, int, int64) error {
	f.calls.Add(1)
	return errProposalForge
}
func (f *proposalForge) GetIssue(context.Context, github.Repo, int) (github.Issue, error) {
	f.calls.Add(1)
	return github.Issue{}, errProposalForge
}
func (f *proposalForge) GetParentIssue(context.Context, github.Repo, int) (github.Issue, error) {
	f.calls.Add(1)
	return github.Issue{}, errProposalForge
}
func (f *proposalForge) ListIssuesPage(context.Context, github.Repo, string, int, int) (github.IssuePage, error) {
	f.calls.Add(1)
	return github.IssuePage{}, errProposalForge
}
func (f *proposalForge) PostComment(context.Context, github.Repo, int, redact.Body) (github.Comment, error) {
	f.calls.Add(1)
	return github.Comment{}, errProposalForge
}
func (f *proposalForge) ListCommentsPage(context.Context, github.Repo, int, int, int) (github.CommentPage, error) {
	f.calls.Add(1)
	return github.CommentPage{}, errProposalForge
}

type proposalRuntime struct {
	prep  *preparation
	db    *store.DB
	cs    *store.CreationStore
	auth  *CreationAuthorizer
	forge *proposalForge
	now   time.Time
}

// newProposalRuntime composes the production launch inputs around a fake
// native-MCP runner. Nothing here starts a coding-agent process.
func newProposalRuntime(t *testing.T, mode string, enabled bool) *proposalRuntime {
	t.Helper()
	prep, db := lifecycleMCPPreparation(t, mode)
	r := &proposalRuntime{prep: prep, db: db, forge: &proposalForge{}, now: time.Now().UTC()}
	var err error
	r.cs, err = store.NewCreationStore(db, func() time.Time { return r.now })
	mustProgress(t, err)
	r.auth, err = NewCreationAuthorizer(CreationAuthorizerOptions{Store: r.cs, Enabled: enabled, OwnerID: 10, BotID: 20})
	mustProgress(t, err)
	rt := prep.mcpRuntime
	rt.CreationAuthorizer, rt.Creation, rt.Redactor, rt.BotID = r.auth, r.forge, redact.New(redact.Options{}), 20
	prep.runner = &mcpGateRunner{supported: true}
	return r
}

func (r *proposalRuntime) launch(t *testing.T, req agent.Request) (*preparedMCP, []string) {
	t.Helper()
	p, spec, err := prepareMCP(t.Context(), r.prep, req, nil)
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, p.close(context.WithoutCancel(t.Context()))) })
	names := spec.PermittedTools()
	// The provider-facing catalog is what the broker lists, not only what the
	// launch spec says.
	listed, err := submission.Client{DescriptorPath: spec.DescriptorPath()}.List(t.Context())
	mustProgress(t, err)
	var catalog struct {
		Tools []submission.ToolSpec `json:"tools"`
	}
	mustProgress(t, json.Unmarshal(listed.Result, &catalog))
	brokered := make([]string, len(catalog.Tools))
	for i, tool := range catalog.Tools {
		brokered[i] = tool.Name
	}
	if !slices.Equal(names, brokered) {
		t.Fatalf("launch spec %v differs from installed catalog %v", names, brokered)
	}
	return p, names
}

func (r *proposalRuntime) client(t *testing.T, req agent.Request) (submission.Client, []string) {
	t.Helper()
	p, spec, err := prepareMCP(t.Context(), r.prep, req, nil)
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, p.close(context.WithoutCancel(t.Context()))) })
	return submission.Client{DescriptorPath: spec.DescriptorPath()}, spec.PermittedTools()
}

// One table, invocation as a column: Research is offered the tool on the legacy
// path and on the graph Research-node path alike, and only then.
func TestCreationProposalCatalog_Installation(t *testing.T) {
	const tool = "issues_create_prepare"
	for _, tt := range []struct {
		name       string
		mode       string
		invocation agent.InvocationKind
		enabled    bool
		native     bool
		disabled   bool
		noForge    bool
		want       bool
	}{
		{"legacy research", "research", "", true, true, false, false, true},
		{"graph research node", "research", agent.InvocationResearchNode, true, true, false, false, true},
		{"legacy research, creation disabled", "research", "", false, true, false, false, false},
		{"graph research node, creation disabled", "research", agent.InvocationResearchNode, false, true, false, false, false},
		{"legacy research, host without native MCP", "research", "", true, false, false, false, false},
		{"graph research node, host without native MCP", "research", agent.InvocationResearchNode, true, false, false, false, false},
		{"legacy research, MCP disabled", "research", "", true, true, true, false, false},
		{"graph research node, MCP disabled", "research", agent.InvocationResearchNode, true, true, true, false, false},
		{"legacy research, no creation forge", "research", "", true, true, false, true, false},
		{"graph research node, no creation forge", "research", agent.InvocationResearchNode, true, true, false, true, false},
		{"plan", "plan", "", true, true, false, false, false},
		{"spike", "spike", "", true, true, false, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := newProposalRuntime(t, tt.mode, tt.enabled)
			r.prep.runner = &mcpGateRunner{supported: tt.native}
			r.prep.mcpRuntime.DisableMCP = tt.disabled
			if tt.noForge {
				r.prep.mcpRuntime.Creation = nil
			}
			_, names := r.launch(t, agent.Request{Mode: tt.mode, Invocation: tt.invocation})
			if got := slices.Contains(names, tool); got != tt.want {
				t.Fatalf("%s installed=%t want=%t in %v", tool, got, tt.want, names)
			}
			if slices.Contains(names, "issues_create_batch") {
				t.Fatalf("installing the drafting tool registered publication: %v", names)
			}
		})
	}
}

// With the gate off the catalog is exactly what it would be without the feature.
func TestCreationProposalCatalog_OnlyAddsTheTool(t *testing.T) {
	for _, invocation := range []agent.InvocationKind{"", agent.InvocationResearchNode} {
		t.Run(fmt.Sprintf("invocation=%q", invocation), func(t *testing.T) {
			req := agent.Request{Mode: "research", Invocation: invocation}
			_, enabled := newProposalRuntime(t, "research", true).launch(t, req)
			_, disabled := newProposalRuntime(t, "research", false).launch(t, req)
			if slices.Contains(disabled, "issues_create_prepare") {
				t.Fatalf("disabled catalog %v", disabled)
			}
			withoutTool := slices.DeleteFunc(slices.Clone(enabled), func(name string) bool { return name == "issues_create_prepare" })
			if !slices.Equal(withoutTool, disabled) || len(enabled) != len(disabled)+1 {
				t.Fatalf("enabled %v disabled %v", enabled, disabled)
			}
		})
	}
}

// Installing the tool is not a grant. The graph path keeps its exclusions for
// the comment service and the publication grant even with an approved intent,
// and never consumes that intent; the legacy path still binds it.
func TestCreationProposalCatalog_InstallationGrantsNothingAndKeepsGraphExclusions(t *testing.T) {
	r := newProposalRuntime(t, "research", true)
	rt := r.prep.mcpRuntime
	rt.Comments = r.forge
	task := r.prep.session.TaskRef

	wire := `{"version":1,"artifacts":[{"title":"Exact title","body":"Exact body","link_issues":[280]}]}`
	control := OwnerGrantControl{Repository: "example/results", SourceTask: task, Mode: "research", ActorID: 10, CommentID: 700, CreatedAt: r.now, Body: "/mcp-create prepare " + wire}
	prepared, err := rt.PrepareCreationProposal(t.Context(), control)
	mustProgress(t, err)

	// AC-025: the hand-written owner path is byte-identical to an envelope
	// assembled here without the shared helper.
	nonce := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", task, control.CommentID, control.Body)))
	marker := fmt.Sprintf("<!-- lf:mcp-create v=1 key=%s artifact=0 -->", hex.EncodeToString(nonce[:16]))
	want := submission.CreationEnvelope{Version: 1, Repository: "example/results", SourceTask: task, Relationship: "linked", Artifacts: []submission.CreationArtifact{{Title: "Exact title", Body: "Exact body\n\nRelated: https://github.com/example/results/issues/280\n\nRelated: https://github.com/example/results/issues/263\n\n" + marker, LinkIssues: []int{280, 263}}}}
	wantBytes, wantHash, err := submission.CanonicalCreation(want)
	mustProgress(t, err)
	gotBytes, gotHash, err := submission.CanonicalCreation(prepared.Envelope)
	mustProgress(t, err)
	if string(gotBytes) != string(wantBytes) || gotHash != wantHash || prepared.Hash != wantHash {
		t.Fatalf("owner path changed:\n got %s\nwant %s", gotBytes, wantBytes)
	}
	retained, err := r.cs.RetainedCreationProposal(t.Context(), wantHash)
	mustProgress(t, err)
	if string(retained.Envelope) != string(wantBytes) {
		t.Fatal("owner path retained different bytes")
	}
	if _, err := r.cs.PendingCreationPresentation(t.Context(), r.prep.run.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("owner path wrote agent bookkeeping: %v", err)
	}

	control.CommentID++
	control.Body = "/mcp-create " + prepared.Hash + " linked"
	intent, err := rt.AdmitCreationApproval(t.Context(), control)
	mustProgress(t, err)

	graph, names := r.launch(t, agent.Request{Mode: "research", Invocation: agent.InvocationResearchNode})
	if !slices.Contains(names, "issues_create_prepare") || slices.Contains(names, "issues_create_batch") || slices.Contains(names, "issues_comment") || len(graph.creationProposal) != 0 {
		t.Fatalf("graph catalog %v", names)
	}
	got, _, err := r.cs.EligibleCreationIntent(t.Context(), task)
	mustProgress(t, err)
	if got != intent {
		t.Fatal("graph launch consumed the owner intent")
	}

	legacy, names := r.launch(t, agent.Request{Mode: "research"})
	if !slices.Contains(names, "issues_create_prepare") || !slices.Contains(names, "issues_create_batch") || !slices.Contains(names, "issues_comment") || len(legacy.creationProposal) == 0 {
		t.Fatalf("legacy catalog %v", names)
	}
	if _, _, err := r.cs.EligibleCreationIntent(t.Context(), task); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("legacy launch left the intent eligible: %v", err)
	}
	if r.forge.calls.Load() != 0 {
		t.Fatal("launch reached the forge")
	}
}

// The drafting tool works from a graph Research node, where there is no
// submission session, and publishes nothing.
func TestCreationProposalCatalog_ToolWorksOnBothInvocations(t *testing.T) {
	for _, invocation := range []agent.InvocationKind{"", agent.InvocationResearchNode} {
		t.Run(fmt.Sprintf("invocation=%q", invocation), func(t *testing.T) {
			r := newProposalRuntime(t, "research", true)
			client, names := r.client(t, agent.Request{Mode: "research", Invocation: invocation})
			if !slices.Contains(names, "issues_create_prepare") {
				t.Fatalf("catalog %v", names)
			}
			response, err := client.Call(t.Context(), "issues_create_prepare", []byte(`{"drafts":[{"title":"From a Research run","body":"Body"}]}`))
			mustProgress(t, err)
			if response.Error != "" || strings.Contains(string(response.Result), "/mcp-create") {
				t.Fatalf("response %+v", response)
			}
			var result CreationProposalResult
			mustProgress(t, json.Unmarshal(response.Result, &result))
			pending, err := r.cs.PendingCreationPresentation(t.Context(), r.prep.run.ID)
			mustProgress(t, err)
			if pending.ProposalHash != result.Hash || result.Drafts != 1 {
				t.Fatalf("pending %+v result %+v", pending, result)
			}
			if r.forge.calls.Load() != 0 {
				t.Fatal("drafting reached the forge")
			}
			// A malformed call is refused and leaves the earlier proposal alone.
			bad, err := client.Call(t.Context(), "issues_create_prepare", []byte(`{"repository":"example/results","drafts":[{"title":"X","body":"Y"}]}`))
			mustProgress(t, err)
			if bad.Error == "" {
				t.Fatal("factory field accepted")
			}
			after, err := r.cs.PendingCreationPresentation(t.Context(), r.prep.run.ID)
			mustProgress(t, err)
			if after.ProposalHash != result.Hash {
				t.Fatal("refusal replaced the proposal")
			}
		})
	}
}

func TestCreationProposalCatalog_DigestChangesWhenInstalled(t *testing.T) {
	db, _, opts := agentResultFixture(t, "research")
	service, err := BindAgentResultService(t.Context(), opts)
	mustProgress(t, err)
	base := familyResearchOptions(t, db, service)
	without, err := FamilyPermissionDigest(base)
	mustProgress(t, err)
	digestFor := func(enabled bool) string {
		cs, err := store.NewCreationStore(db, time.Now)
		mustProgress(t, err)
		auth, err := NewCreationAuthorizer(CreationAuthorizerOptions{Store: cs, Enabled: enabled, OwnerID: 10, BotID: 20})
		mustProgress(t, err)
		proposals, err := NewCreationProposalService(db, base.Receipts, auth, redact.New(redact.Options{}))
		mustProgress(t, err)
		with := base
		with.Registrations = []FamilyRegistration{proposals.Registration()}
		digest, err := FamilyPermissionDigest(with)
		mustProgress(t, err)
		return digest
	}
	installed := digestFor(true)
	if installed == without || installed == digestFor(false) {
		t.Fatal("the permission digest does not bind the drafting tool and its gate")
	}
	if installed != digestFor(true) {
		t.Fatal("digest is not deterministic")
	}
	if submission.FamilyCatalogVersion != "families-v3" {
		t.Fatalf("catalog version %q", submission.FamilyCatalogVersion)
	}
}

func TestMCPGuidance_CreationProposal(t *testing.T) {
	t.Parallel()
	research := &AgentResultService{binding: store.ResultBinding{Mode: "research"}}
	render := func(tools ...string) string {
		t.Helper()
		got, err := mcpGuidance(t.Context(), guidanceSpec(t, tools...), research, nil)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := render("issues_create_prepare", "receipts_get")
	for _, want := range []string{"issues_create_prepare", "only when the owner's request asks for issues or stories", "1 to 5 drafts", "nothing is published", "grants no approval and no publication", "cannot approve, publish or create", "never write or quote an approval command", "Advice, not a rule", "splitting", "never blocks", "48000"} {
		if !strings.Contains(got, want) {
			t.Errorf("guidance lacks %q", want)
		}
	}
	for _, forbidden := range []string{"/mcp-create", "Creation:", "issues_create_batch"} {
		if strings.Contains(got, forbidden) {
			t.Errorf("guidance mentions %q", forbidden)
		}
	}
	for _, other := range [][]string{{"receipts_get"}, {"issues_list"}, {"issues_create_batch", "feedback_preflight", "receipts_get"}, {"activity_update", "activity_get"}} {
		if strings.Contains(render(other...), "issues_create_prepare") {
			t.Errorf("guidance for %v mentions the uninstalled tool", other)
		}
	}
	// With both creation tools the grant paragraph stays separate and unchanged.
	both := render("issues_create_prepare", "issues_create_batch", "feedback_preflight", "receipts_get")
	if !strings.Contains(both, "Tool visibility alone is not creation approval") || !strings.Contains(both, "Creation proposal:") {
		t.Error("both paragraphs must render when both tools are installed")
	}
}
