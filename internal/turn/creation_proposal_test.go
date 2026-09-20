package turn

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

// proposalFixture drives the issues_create_prepare tool through the real family
// dispatcher, broker socket and receipt ledger over a migrated SQLite database.
// The host is simulated by the test calling the broker directly; nothing here
// observes a real Claude or Codex process.
type proposalFixture struct {
	db       *store.DB
	cs       *store.CreationStore
	auth     *CreationAuthorizer
	service  *CreationProposalService
	receipts *ReceiptService
	guard    *AttemptGuard
	client   submission.Client
	probe    *sql.DB
	runID    int64
	task     string
	now      time.Time
}

func newProposalFixture(t *testing.T, enabled bool) *proposalFixture {
	t.Helper()
	f := &proposalFixture{now: time.Now().UTC().Add(-time.Minute)}
	db, session, opts := agentResultFixture(t, "research")
	f.db, f.task, f.runID = db, session.TaskRef, opts.Attempt.RunID
	app, err := BindAgentResultService(t.Context(), opts)
	mustProgress(t, err)
	f.cs, err = store.NewCreationStore(db, func() time.Time { return f.now })
	mustProgress(t, err)
	f.auth, err = NewCreationAuthorizer(CreationAuthorizerOptions{Store: f.cs, Enabled: enabled, OwnerID: 10, BotID: 20})
	mustProgress(t, err)
	projection := CreationProposalReceiptProjection()
	f.receipts, err = NewReceiptService(db, app.Binding(), nil, map[string]FamilyProjection{creationProposalReferenceKind: projection, "tool:issues_create_prepare": projection})
	mustProgress(t, err)
	f.service, err = NewCreationProposalService(db, f.receipts, f.auth, redact.New(redact.Options{}))
	mustProgress(t, err)
	feedback, err := NewFeedbackService(app, nil)
	mustProgress(t, err)
	f.client, f.guard, _ = familySocketFixture(t, db, FamilyDispatcherOptions{Service: app, Receipts: f.receipts, Feedback: feedback, Registrations: []FamilyRegistration{f.service.Registration()}})
	f.probe, err = sql.Open("sqlite", store.DSN(db.Path()))
	mustProgress(t, err)
	t.Cleanup(func() { mustProgress(t, f.probe.Close()) })
	return f
}

func (f *proposalFixture) call(t *testing.T, args string) submission.Response {
	t.Helper()
	r, err := f.client.Call(t.Context(), "issues_create_prepare", json.RawMessage(args))
	mustProgress(t, err)
	return r
}

func (f *proposalFixture) retain(t *testing.T, args string) CreationProposalResult {
	t.Helper()
	r := f.call(t, args)
	if r.Error != "" {
		t.Fatalf("call refused: %s", r.Error)
	}
	var result CreationProposalResult
	mustProgress(t, json.Unmarshal(r.Result, &result))
	return result
}

func (f *proposalFixture) count(t *testing.T, table string) int {
	t.Helper()
	if !slices.Contains([]string{"mcp_creation_proposals", "mcp_creation_presentations", "mcp_creation_current"}, table) {
		t.Fatalf("unexpected table %s", table)
	}
	var n int
	mustProgress(t, f.probe.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&n))
	return n
}

// requireNothingRetained asserts the half of a refusal a happy path cannot show:
// no proposal, no current pointer and no bookkeeping row exist.
func (f *proposalFixture) requireNothingRetained(t *testing.T, context string) {
	t.Helper()
	for _, table := range []string{"mcp_creation_proposals", "mcp_creation_current", "mcp_creation_presentations"} {
		if n := f.count(t, table); n != 0 {
			t.Fatalf("%s: %d rows in %s", context, n, table)
		}
	}
}

func (f *proposalFixture) revision(t *testing.T) int {
	t.Helper()
	var revision int
	err := f.probe.QueryRowContext(t.Context(), `SELECT revision FROM mcp_creation_current WHERE source_task=?`, f.task).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0
	}
	mustProgress(t, err)
	return revision
}

func (f *proposalFixture) presented(t *testing.T) string {
	t.Helper()
	var hash string
	err := f.probe.QueryRowContext(t.Context(), `SELECT proposal_hash FROM mcp_creation_presentations WHERE run_id=?`, f.runID).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return ""
	}
	mustProgress(t, err)
	return hash
}

func (f *proposalFixture) envelope(t *testing.T, hash string) submission.CreationEnvelope {
	t.Helper()
	p, err := f.cs.RetainedCreationProposal(t.Context(), hash)
	mustProgress(t, err)
	var e submission.CreationEnvelope
	mustProgress(t, json.Unmarshal(p.Envelope, &e))
	return e
}

func (f *proposalFixture) approve(hash, relationship string, comment int64) error {
	_, err := f.auth.AdmitOwnerGrantIntent(context.Background(), OwnerGrantControl{Repository: "example/results", SourceTask: f.task, Mode: "research", Body: "/mcp-create " + hash + " " + relationship, CommentID: comment, ActorID: 10, CreatedAt: f.now})
	return err
}

func TestCreationProposalTool_RetainsAndPresentsWithoutApproval(t *testing.T) {
	f := newProposalFixture(t, true)
	r := f.call(t, `{"drafts":[{"title":"Add cache","body":"Cache the lookups.","link_issues":[280]},{"title":"Second","body":"Other body","link_issues":[263,281]}]}`)
	if r.Error != "" {
		t.Fatal(r.Error)
	}
	if strings.Contains(string(r.Result), "/mcp-create") {
		t.Fatalf("result carries an approval command: %s", r.Result)
	}
	var result CreationProposalResult
	mustProgress(t, json.Unmarshal(r.Result, &result))
	if len(result.Hash) != 64 || result.Relationship != "linked" || result.Drafts != 2 || result.Disposition != "confirmed" || !strings.Contains(result.Message, "factory will present") {
		t.Fatalf("result %+v", result)
	}
	e := f.envelope(t, result.Hash)
	if e.Version != 1 || e.Repository != "example/results" || e.SourceTask != f.task || e.Relationship != "linked" || len(e.Artifacts) != 2 {
		t.Fatalf("factory fields %+v", e)
	}
	related := "Related: https://github.com/example/results/issues/263"
	for i, a := range e.Artifacts {
		if n := strings.Count(a.Body, related); n != 1 {
			t.Errorf("draft %d renders the source link %d times", i, n)
		}
		if n := len(slices.DeleteFunc(slices.Clone(a.LinkIssues), func(l int) bool { return l != 263 })); n != 1 {
			t.Errorf("draft %d lists the source %d times: %v", i, n, a.LinkIssues)
		}
	}
	if !slices.Equal(e.Artifacts[0].LinkIssues, []int{280, 263}) || !slices.Equal(e.Artifacts[1].LinkIssues, []int{263, 281}) {
		t.Errorf("links %v / %v", e.Artifacts[0].LinkIssues, e.Artifacts[1].LinkIssues)
	}
	if !strings.Contains(e.Artifacts[0].Body, "issues/280") {
		t.Error("supplied link target not rendered")
	}
	if f.presented(t) != result.Hash || f.revision(t) != 1 || f.count(t, "mcp_creation_presentations") != 1 {
		t.Fatalf("bookkeeping: presented=%q revision=%d", f.presented(t), f.revision(t))
	}
	pending, err := f.cs.PendingCreationPresentation(t.Context(), f.runID)
	mustProgress(t, err)
	if pending.ProposalHash != result.Hash || pending.Presented || pending.Artifacts != 2 || pending.Relationship != "linked" {
		t.Fatalf("pending presentation %+v", pending)
	}
	// Drafting is not approval: no intent is eligible and nothing was published.
	if _, _, err := f.cs.EligibleCreationIntent(t.Context(), f.task); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("drafting created an eligible intent: %v", err)
	}
}

func TestCreationProposalTool_AttachFillsSourceParent(t *testing.T) {
	f := newProposalFixture(t, true)
	result := f.retain(t, `{"relationship":"attach","drafts":[{"title":"Child","body":"Body"},{"title":"Explicit","body":"Body","parent_issue":263}]}`)
	if result.Relationship != "attach" || result.Drafts != 2 {
		t.Fatalf("result %+v", result)
	}
	for i, a := range f.envelope(t, result.Hash).Artifacts {
		if a.ParentIssue != 263 || !slices.Equal(a.LinkIssues, []int{263}) {
			t.Errorf("draft %d: %+v", i, a)
		}
	}
}

func TestCreationProposalTool_RefusesFactoryOwnedFields(t *testing.T) {
	f := newProposalFixture(t, true)
	for _, args := range []string{
		`{"repository":"example/results","drafts":[{"title":"T","body":"B"}]}`,
		`{"source_task":"` + f.task + `","drafts":[{"title":"T","body":"B"}]}`,
		`{"version":1,"drafts":[{"title":"T","body":"B"}]}`,
		`{"drafts":[{"title":"T","body":"B","repository":"other/repo"}]}`,
	} {
		if r := f.call(t, args); r.Error == "" {
			t.Errorf("accepted %s", args)
		}
		f.requireNothingRetained(t, args)
	}
}

func TestCreationProposalTool_RefusalsLeaveNothing(t *testing.T) {
	f := newProposalFixture(t, true)
	draft := func(extra string) string { return `{"title":"Title","body":"Body"` + extra + `}` }
	drafts := func(n int) string {
		out := make([]string, n)
		for i := range out {
			out[i] = draft("")
		}
		return `{"drafts":[` + strings.Join(out, ",") + `]}`
	}
	links := make([]string, 16)
	for i := range links {
		links[i] = "5" + string(rune('0'+i/10)) + string(rune('0'+i%10))
	}
	for _, tt := range []struct{ name, args string }{
		{"zero drafts", `{"drafts":[]}`},
		{"missing drafts", `{}`},
		{"six drafts", drafts(6)},
		{"blank title", `{"drafts":[{"title":"  ","body":"Body"}]}`},
		{"blank body", `{"drafts":[{"title":"Title","body":"\n"}]}`},
		{"title over 256 bytes", `{"drafts":[{"title":"` + strings.Repeat("t", 257) + `","body":"Body"}]}`},
		{"body over 65536 bytes", `{"drafts":[{"title":"Title","body":"` + strings.Repeat("b", 65537) + `"}]}`},
		{"link target below one", `{"drafts":[` + draft(`,"link_issues":[0]`) + `]}`},
		{"negative link target", `{"drafts":[` + draft(`,"link_issues":[-3]`) + `]}`},
		{"sixteen targets plus the source", `{"drafts":[` + draft(`,"link_issues":[`+strings.Join(links, ",")+`]`) + `]}`},
		{"parent issue with linked", `{"drafts":[` + draft(`,"parent_issue":263`) + `]}`},
		{"parent issue with explicit linked", `{"relationship":"linked","drafts":[` + draft(`,"parent_issue":263`) + `]}`},
		{"wrong parent with attach", `{"relationship":"attach","drafts":[` + draft(`,"parent_issue":99`) + `]}`},
		{"unknown relationship", `{"relationship":"adopt","drafts":[` + draft("") + `]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := f.call(t, tt.args)
			if r.Error == "" {
				t.Fatalf("accepted: %s", r.Result)
			}
			f.requireNothingRetained(t, tt.name)
		})
	}
}

func TestCreationProposalTool_RefusesWhenStoryCreationIsDisabled(t *testing.T) {
	f := newProposalFixture(t, false)
	if r := f.call(t, `{"drafts":[{"title":"T","body":"B"}]}`); r.Error == "" {
		t.Fatal("disabled gate retained a proposal")
	}
	f.requireNothingRetained(t, "disabled")
}

// A proposal the owner could never be shown is refused at call time, in full,
// rather than truncated to fit.
func TestCreationProposalTool_OversizeEnvelopeIsRefusedNotTruncated(t *testing.T) {
	f := newProposalFixture(t, true)
	body := strings.Repeat("lorem ipsum ", 1000)
	args := `{"drafts":[` + strings.TrimSuffix(strings.Repeat(`{"title":"Big","body":"`+body+`"},`, 5), ",") + `]}`
	// The broker reports every handler failure with one fixed sentence, so the
	// message itself is asserted on the handler and the outcome on the receipt.
	r := f.call(t, args)
	if r.Error == "" {
		t.Fatalf("oversize accepted: %s", r.Result)
	}
	var refused FamilyReceipt
	lookup, err := f.client.Call(t.Context(), "receipts_get", []byte(`{"request_id":"`+r.RequestID+`"}`))
	mustProgress(t, err)
	mustProgress(t, json.Unmarshal(lookup.Result, &refused))
	if refused.Disposition != "rejected" || refused.Reference.Kind != "" {
		t.Fatalf("receipt of a refusal %+v", refused)
	}
	live, release, err := f.guard.Context(t.Context())
	mustProgress(t, err)
	_, _, err = f.receipts.Begin(live, "direct", "issues_create_prepare", []byte(args))
	mustProgress(t, err)
	_, err = f.service.HandlePrepare(live, "direct", []byte(args))
	release()
	if err == nil || !strings.Contains(err.Error(), "shorten") || !strings.Contains(err.Error(), "split") || !errors.Is(err, errCreationPresentationLimit) {
		t.Fatalf("refusal message: %v", err)
	}
	f.requireNothingRetained(t, "oversize")
	// The same drafts split across proposals fit.
	if result := f.retain(t, `{"drafts":[{"title":"Big","body":"`+body+`"},{"title":"Big two","body":"`+body+`"}]}`); result.Drafts != 2 {
		t.Fatalf("smaller proposal: %+v", result)
	}
}

func TestCreationProposalTool_IdenticalInputIsIdempotent(t *testing.T) {
	f := newProposalFixture(t, true)
	args := []byte(`{"drafts":[{"title":"Same","body":"Same body"}]}`)
	var hashes []string
	for _, id := range []string{"request-a", "request-b"} {
		live, release, err := f.guard.Context(t.Context())
		mustProgress(t, err)
		_, _, err = f.receipts.Begin(live, id, "issues_create_prepare", args)
		mustProgress(t, err)
		out, err := f.service.HandlePrepare(live, id, args)
		release()
		mustProgress(t, err)
		var result CreationProposalResult
		mustProgress(t, json.Unmarshal(out, &result))
		hashes = append(hashes, result.Hash)
	}
	if hashes[0] != hashes[1] {
		t.Fatalf("identical input on one attempt changed the hash: %v", hashes)
	}
	if f.revision(t) != 1 || f.count(t, "mcp_creation_presentations") != 1 || f.count(t, "mcp_creation_proposals") != 1 || f.presented(t) != hashes[0] {
		t.Fatalf("revision=%d presentations=%d proposals=%d", f.revision(t), f.count(t, "mcp_creation_presentations"), f.count(t, "mcp_creation_proposals"))
	}
}

func TestCreationProposalTool_ChangedInputSupersedesEarlierHash(t *testing.T) {
	f := newProposalFixture(t, true)
	first := f.retain(t, `{"drafts":[{"title":"First","body":"First body"}]}`)
	second := f.retain(t, `{"drafts":[{"title":"Second","body":"Second body"},{"title":"Third","body":"Third body"}]}`)
	if first.Hash == second.Hash {
		t.Fatal("changed input kept the hash")
	}
	if f.revision(t) != 2 || f.count(t, "mcp_creation_presentations") != 1 || f.presented(t) != second.Hash {
		t.Fatalf("revision=%d presentations=%d presented=%q", f.revision(t), f.count(t, "mcp_creation_presentations"), f.presented(t))
	}
	pending, err := f.cs.PendingCreationPresentation(t.Context(), f.runID)
	mustProgress(t, err)
	if pending.Artifacts != 2 {
		t.Fatalf("row was not replaced: %+v", pending)
	}
	if err := f.approve(first.Hash, "linked", 1); err == nil {
		t.Fatal("superseded proposal is still approvable")
	}
	if err := f.approve(second.Hash, "linked", 2); err != nil {
		t.Fatalf("current proposal is not approvable: %v", err)
	}
}

// A replayed request returns the retained receipt. Had the handler run again it
// would have made the first proposal current again and bumped the revision.
func TestCreationProposalTool_ReplayReturnsReceiptWithoutRerunning(t *testing.T) {
	f := newProposalFixture(t, true)
	firstArgs := `{"drafts":[{"title":"First","body":"First body"}]}`
	first := f.retain(t, firstArgs)
	second := f.retain(t, `{"drafts":[{"title":"Second","body":"Second body"}]}`)
	replay := f.call(t, firstArgs)
	if replay.Error != "" || replay.RequestID != first.RequestID {
		t.Fatalf("replay %+v", replay)
	}
	var receipt FamilyReceipt
	mustProgress(t, json.Unmarshal(replay.Result, &receipt))
	if receipt.Tool != "issues_create_prepare" || receipt.Disposition != "confirmed" || receipt.States.Input != "accepted" || receipt.Reference != (store.FamilyReference{Kind: "creation_proposal", ID: f.runID}) {
		t.Fatalf("receipt %+v", receipt)
	}
	if f.presented(t) != second.Hash || f.revision(t) != 2 {
		t.Fatalf("replay re-ran the handler: presented=%q revision=%d", f.presented(t), f.revision(t))
	}
	lookup, err := f.client.Call(t.Context(), "receipts_get", []byte(`{"request_id":"`+first.RequestID+`"}`))
	mustProgress(t, err)
	var observed FamilyReceipt
	mustProgress(t, json.Unmarshal(lookup.Result, &observed))
	if observed.Reference != receipt.Reference || observed.States.Input != "accepted" {
		t.Fatalf("receipts_get %+v", observed)
	}
}
