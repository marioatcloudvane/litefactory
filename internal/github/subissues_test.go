package github_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/github"
	"github.com/cloudvane/litefactory/internal/naming"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/report"
)

// Everything in this file runs against the package's httptest fixture, so the
// assertions are about what actually went on the wire rather than about what
// this package meant to send. That matters more here than anywhere else in the
// package: the sub-issue API takes two different identifiers for one issue —
// the parent's *number* in the path and the child's database *id* in the body —
// and a swap is a 201 that links the wrong issue. Only a request whose id and
// number differ can tell the two apart, so every fixture below uses issues
// whose id is nothing like its number.

const (
	// testParent is the issue a plan was asked for on: the parent every
	// sub-issue here is attached to. It is addressed by *number*.
	testParent = 7

	// testMarkerC1 and testMarkerC2 are what a story's body carries. They are
	// written out rather than built with report.StoryMarker so that these
	// tests fail on a change to this package alone; the two are proved to
	// agree by TestARenderedSubIssueIsAcceptedAsASpec below, which uses the
	// real renderer.
	testMarkerC1 = "<!-- lf:story task=github:acme/acme-api#123 story=C1 -->"
	testMarkerC2 = "<!-- lf:story task=github:acme/acme-api#123 story=C2 -->"
	testMarkerC3 = "<!-- lf:story task=github:acme/acme-api#123 story=C3 -->"
)

// forgeReply is one canned HTTP response.
type forgeReply struct {
	status int
	body   string
}

// forge is a scripted GitHub for the three sub-issue calls: a listing, a queue
// of create responses and a queue of add responses. A queue that runs out
// repeats its last entry, so a test that expects every call to behave the same
// way writes one.
type forge struct {
	list    string
	creates []forgeReply
	adds    []forgeReply

	mu      sync.Mutex
	nCreate int
	nAdd    int
}

// issueJSON is a create response whose id and number deliberately differ.
func issueJSON(id int64, number int) string {
	return fmt.Sprintf(`{"id": %d, "number": %d, "title": "a story", "state": "open",
		"html_url": "https://github.com/acme/acme-api/issues/%d"}`, id, number, number)
}

// subIssueJSON is one entry of the parent's sub-issue listing.
func subIssueJSON(id int64, number int, body string) string {
	encoded, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf(`{"id": %d, "number": %d, "title": "an existing story", "state": "open",
		"html_url": "https://github.com/acme/acme-api/issues/%d", "body": %s}`,
		id, number, number, encoded)
}

func (f *forge) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/sub_issues"):
			list := f.list
			if list == "" {
				list = `[]`
			}
			writeReply(w, forgeReply{status: http.StatusOK, body: list})

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sub_issues"):
			writeReply(w, f.take(f.adds, &f.nAdd, forgeReply{
				status: http.StatusCreated, body: issueJSON(9001, 42),
			}))

		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issues"):
			writeReply(w, f.take(f.creates, &f.nCreate, forgeReply{
				status: http.StatusCreated, body: issueJSON(9001, 42),
			}))

		default:
			// Not Fatalf: this runs on the server's goroutine.
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			writeReply(w, forgeReply{status: http.StatusInternalServerError, body: `{"message": "unexpected"}`})
		}
	}
}

// take pops the next scripted reply, repeating the last one once the queue is
// spent and falling back to fallback for an empty queue.
func (f *forge) take(queue []forgeReply, n *int, fallback forgeReply) forgeReply {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(queue) == 0 {
		return fallback
	}
	i := *n
	*n++
	if i >= len(queue) {
		i = len(queue) - 1
	}
	return queue[i]
}

func writeReply(w http.ResponseWriter, reply forgeReply) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(reply.status)
	_, _ = io.WriteString(w, reply.body)
}

// spec builds a well-formed SubIssueSpec whose body carries its marker.
func spec(t *testing.T, storyID, marker string) github.SubIssueSpec {
	t.Helper()
	return github.SubIssueSpec{
		StoryID: storyID,
		Title:   body(t, storyID+" — a story"),
		Body:    body(t, marker+"\n\nWhat this story is for.\n"),
		Marker:  marker,
	}
}

// requestsTo counts the requests the server saw for one method and path suffix.
func requestsTo(f *fixture, method, suffix string) []capturedRequest {
	var matched []capturedRequest
	for _, req := range f.Requests() {
		if req.Method == method && strings.HasSuffix(req.Path, suffix) {
			matched = append(matched, req)
		}
	}
	return matched
}

// TestTheSubIssueRequestCarriesTheChildsDatabaseID is AC-C1.1a, and it is the
// only real protection there is.
//
// The parent is addressed by its *number* in the path; the child by its
// database *id* in the body. Both are int64 by the time they reach the SDK and
// GitHub answers 201 either way, so a swap links a real issue to the wrong
// parent and nothing anywhere says so. The fixture therefore creates an issue
// whose id (9001) is nothing like its number (42), and whose parent's number
// (7) is neither: every one of the three values is distinguishable, so no
// mix-up can pass by coincidence.
//
// A fixture whose id equalled its number would let the bug through in silence,
// which is why one is not written here.
func TestTheSubIssueRequestCarriesTheChildsDatabaseID(t *testing.T) {
	t.Parallel()

	f := newFixture(t, (&forge{creates: []forgeReply{{
		status: http.StatusCreated, body: issueJSON(9001, 42),
	}}}).handler(t))

	results, err := f.Client.CreateSubIssues(t.Context(), testRepo, testParent,
		[]github.SubIssueSpec{spec(t, "C1", testMarkerC1)})
	if err != nil {
		t.Fatalf("CreateSubIssues: %v", err)
	}
	if got := results[0].Outcome; got != github.SubIssueCreated {
		t.Fatalf("outcome = %s (%v), want created", got, results[0].Err)
	}

	adds := requestsTo(f, http.MethodPost, "/sub_issues")
	if len(adds) != 1 {
		t.Fatalf("the server saw %d add requests, want 1", len(adds))
	}
	add := adds[0]

	// The path names the parent, by number.
	if want := fmt.Sprintf("/repos/acme/acme-api/issues/%d/sub_issues", testParent); add.Path != want {
		t.Errorf("add path = %q, want %q — the path addresses the parent by its NUMBER", add.Path, want)
	}

	// The body names the child, by database id.
	var payload struct {
		SubIssueID    int64 `json:"sub_issue_id"`
		ReplaceParent *bool `json:"replace_parent"`
	}
	add.JSON(t, &payload)
	switch payload.SubIssueID {
	case 9001:
		// Correct: the created issue's database id.
	case 42:
		t.Errorf("sub_issue_id = 42, which is the created issue's NUMBER. " +
			"sub_issue_id is the issue's database id (9001); sending the number links whichever " +
			"issue happens to have 42 as its id, and GitHub answers 201 either way.")
	case testParent:
		t.Errorf("sub_issue_id = %d, which is the PARENT's number", testParent)
	default:
		t.Errorf("sub_issue_id = %d, want the created issue's database id 9001", payload.SubIssueID)
	}
	if payload.ReplaceParent != nil {
		t.Errorf("replace_parent = %v; litefactory does not move an issue that already has a parent",
			*payload.ReplaceParent)
	}

	// And the result reports both identifiers, so a story index row can link
	// the issue while a resumed run can still attach it.
	if results[0].ID != 9001 || results[0].Number != 42 {
		t.Errorf("result = {ID: %d, Number: %d}, want {9001, 42}", results[0].ID, results[0].Number)
	}
}

// TestAddSubIssueSendsTheIDItWasGiven is the same guarantee for the single
// call, which is the one a caller reaches for when it resumes an orphan by
// hand.
func TestAddSubIssueSendsTheIDItWasGiven(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusCreated, issueJSON(9001, 42)))

	if err := f.Client.AddSubIssue(t.Context(), testRepo, testParent, 9001); err != nil {
		t.Fatalf("AddSubIssue: %v", err)
	}

	req := f.Only(t)
	if req.Method != http.MethodPost {
		t.Errorf("method = %s, want POST", req.Method)
	}
	if want := "/repos/acme/acme-api/issues/7/sub_issues"; req.Path != want {
		t.Errorf("path = %q, want %q", req.Path, want)
	}
	if got := strings.TrimSpace(req.Body); got != `{"sub_issue_id":9001}` {
		t.Errorf("body = %s, want {\"sub_issue_id\":9001} and nothing else", got)
	}
}

// TestAddSubIssueRefusesIdentifiersNoForgeMints keeps a zero or negative
// identifier from being interpolated into a path or a body. A parent of 0 is
// how the SDK addresses a repository-level endpoint, and an id of 0 addresses
// no issue at all.
func TestAddSubIssueRefusesIdentifiersNoForgeMints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		parent     int
		subIssueID int64
	}{
		{name: "parent zero", parent: 0, subIssueID: 9001},
		{name: "parent negative", parent: -1, subIssueID: 9001},
		{name: "id zero", parent: testParent, subIssueID: 0},
		{name: "id negative", parent: testParent, subIssueID: -9001},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
			})

			err := f.Client.AddSubIssue(t.Context(), testRepo, tc.parent, tc.subIssueID)
			if !errors.Is(err, github.ErrInvalidArgument) {
				t.Fatalf("AddSubIssue error = %v, want ErrInvalidArgument", err)
			}
			if got := len(f.Requests()); got != 0 {
				t.Errorf("the server saw %d requests; a refusal must not reach the network", got)
			}
		})
	}
}

// TestCreateIssueSendsTitleAndBodyAndNothingElse pins the request. The absence
// of labels and assignees is the assertion that matters: an issue litefactory
// files carries no decision nobody asked it to take.
func TestCreateIssueSendsTitleAndBodyAndNothingElse(t *testing.T) {
	t.Parallel()

	f := newFixture(t, respondJSON(http.StatusCreated, issueJSON(9001, 42)))

	issue, err := f.Client.CreateIssue(t.Context(), testRepo,
		body(t, "C1 — the plan ships as sub-issues"), body(t, testMarkerC1+"\n\nthe story\n"))
	if err != nil {
		t.Fatalf("CreateIssue: %v", err)
	}

	req := f.Only(t)
	if req.Method != http.MethodPost || req.Path != "/repos/acme/acme-api/issues" {
		t.Fatalf("request = %s %s, want POST /repos/acme/acme-api/issues", req.Method, req.Path)
	}

	var payload map[string]any
	req.JSON(t, &payload)
	if got, want := payload["title"], "C1 — the plan ships as sub-issues"; got != want {
		t.Errorf("title = %v, want %q", got, want)
	}
	if got, ok := payload["body"].(string); !ok || !strings.HasPrefix(got, testMarkerC1) {
		t.Errorf("body = %v, want the rendered body beginning with its marker", payload["body"])
	}
	for _, unwanted := range []string{"labels", "assignees", "assignee", "milestone", "type"} {
		if _, ok := payload[unwanted]; ok {
			t.Errorf("the request carries %q; litefactory files an issue with a title and a body "+
				"and takes no other decision about it", unwanted)
		}
	}

	// Both identifiers come back, because the attach step needs the id and a
	// link needs the number.
	if issue.ID != 9001 || issue.Number != 42 {
		t.Errorf("issue = {ID: %d, Number: %d}, want {9001, 42}", issue.ID, issue.Number)
	}
	if !strings.Contains(f.Logs(), `"issue_id":9001`) {
		t.Errorf("the created issue's id is not in the log:\n%s", f.Logs())
	}
}

// TestCreateIssueRefusesAResponseWithoutBothIdentifiers covers the case that
// would otherwise flow straight into the attach step. An id of 0 addresses no
// issue, and "the story was linked to nothing" must not be something a caller
// discovers by looking at the forge.
func TestCreateIssueRefusesAResponseWithoutBothIdentifiers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response string
	}{
		{name: "no id", response: `{"number": 42}`},
		{name: "no number", response: `{"id": 9001}`},
		{name: "null", response: `null`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, respondJSON(http.StatusCreated, tc.response))
			_, err := f.Client.CreateIssue(t.Context(), testRepo, body(t, "a title"), body(t, "a body"))
			if !errors.Is(err, github.ErrUnexpectedStatus) {
				t.Fatalf("CreateIssue error = %v, want ErrUnexpectedStatus", err)
			}
		})
	}
}

// TestCreateIssueRefusesUnsendableText keeps a bad title or body from reaching
// the network at all. The redact.Body zero value is the interesting one: it is
// what a caller has if it skipped the redaction pass, and it is refused by the
// same check that refuses an empty one.
func TestCreateIssueRefusesUnsendableText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		title redact.Body
		body  redact.Body
	}{
		{name: "unredacted title", title: redact.Body{}, body: body(t, "a body")},
		{name: "empty title", title: body(t, ""), body: body(t, "a body")},
		{name: "title with a line break", title: body(t, "two\nlines"), body: body(t, "a body")},
		{
			name:  "oversize title",
			title: body(t, strings.Repeat("t", github.IssueTitleSizeLimit+1)),
			body:  body(t, "a body"),
		},
		{name: "unredacted body", title: body(t, "a title"), body: redact.Body{}},
		{name: "empty body", title: body(t, "a title"), body: body(t, "")},
		{
			name:  "oversize body",
			title: body(t, "a title"),
			body:  bodyWithLimit(t, strings.Repeat("b", github.CommentSizeLimit+1_000), github.CommentSizeLimit*4),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
			})

			_, err := f.Client.CreateIssue(t.Context(), testRepo, tc.title, tc.body)
			if !errors.Is(err, github.ErrInvalidArgument) {
				t.Fatalf("CreateIssue error = %v, want ErrInvalidArgument", err)
			}
			if got := len(f.Requests()); got != 0 {
				t.Errorf("the server saw %d requests; a refusal must not reach the network", got)
			}
		})
	}
}

// TestCreateSubIssuesSkipsAStoryThatIsAlreadyFiled is AC-C1.4: re-running Plan
// on the same session creates no duplicates. The check is the marker in the
// body of the parent's existing sub-issues — no local state, so it holds after
// a database loss and after the process was killed.
func TestCreateSubIssuesSkipsAStoryThatIsAlreadyFiled(t *testing.T) {
	t.Parallel()

	f := newFixture(t, (&forge{
		list: `[` + subIssueJSON(8001, 41, testMarkerC1+"\n\nfiled last time\n") + `]`,
		creates: []forgeReply{
			{status: http.StatusCreated, body: issueJSON(9002, 43)},
		},
	}).handler(t))

	results, err := f.Client.CreateSubIssues(t.Context(), testRepo, testParent, []github.SubIssueSpec{
		spec(t, "C1", testMarkerC1),
		spec(t, "C2", testMarkerC2),
	})
	if err != nil {
		t.Fatalf("CreateSubIssues: %v", err)
	}

	if got := results[0].Outcome; got != github.SubIssueExisted {
		t.Errorf("C1 outcome = %s, want existed", got)
	}
	if results[0].Number != 41 || results[0].ID != 8001 {
		t.Errorf("C1 = {ID: %d, Number: %d}, want the existing issue {8001, 41}",
			results[0].ID, results[0].Number)
	}
	if results[0].HTMLURL == "" {
		t.Error("C1 carries no URL; a story index row without a link is a failing outcome (AC-C1.3a)")
	}
	if got := results[1].Outcome; got != github.SubIssueCreated {
		t.Errorf("C2 outcome = %s (%v), want created", got, results[1].Err)
	}

	if got := len(requestsTo(f, http.MethodPost, "/issues")); got != 1 {
		t.Errorf("%d issues were created, want 1 — the already-filed story must not be filed twice", got)
	}
	if got := len(requestsTo(f, http.MethodPost, "/sub_issues")); got != 1 {
		t.Errorf("%d attach calls, want 1 — attaching an already-attached issue is a write for nothing, "+
			"against the endpoint GitHub secondary-rate-limits", got)
	}
}

// TestAQuotedMarkerIsNotTheIssuesOwn covers the line anchoring. A sub-issue
// whose body merely *quotes* another story's marker — GitHub's quote-reply puts
// "> " in front of every line — is not that story's issue, and treating it as
// one would leave a story silently never filed.
func TestAQuotedMarkerIsNotTheIssuesOwn(t *testing.T) {
	t.Parallel()

	f := newFixture(t, (&forge{
		list: `[` + subIssueJSON(8001, 41, "someone wrote:\n\n> "+testMarkerC1+"\n> a story\n") + `]`,
	}).handler(t))

	results, err := f.Client.CreateSubIssues(t.Context(), testRepo, testParent,
		[]github.SubIssueSpec{spec(t, "C1", testMarkerC1)})
	if err != nil {
		t.Fatalf("CreateSubIssues: %v", err)
	}
	if got := results[0].Outcome; got != github.SubIssueCreated {
		t.Fatalf("outcome = %s, want created: a quoted marker is not the issue's own marker", got)
	}
}

// TestCreateSubIssuesStopsAtTheFirstFailureAndSaysWhatItFiled is AC-C1.5.
//
// The stories before the failure are real issues on the forge and the result
// has to say so; the ones after it were never attempted, which is a different
// fact from having failed. The error return stays nil because the batch *was*
// attempted — a bad outcome is a value here.
func TestCreateSubIssuesStopsAtTheFirstFailureAndSaysWhatItFiled(t *testing.T) {
	t.Parallel()

	f := newFixture(t, (&forge{
		creates: []forgeReply{
			{status: http.StatusCreated, body: issueJSON(9001, 42)},
			{status: http.StatusUnprocessableEntity, body: `{"message": "Validation Failed"}`},
		},
	}).handler(t))

	results, err := f.Client.CreateSubIssues(t.Context(), testRepo, testParent, []github.SubIssueSpec{
		spec(t, "C1", testMarkerC1),
		spec(t, "C2", testMarkerC2),
		spec(t, "C3", testMarkerC3),
	})
	if err != nil {
		t.Fatalf("CreateSubIssues returned an error for a batch it did attempt: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want one per story", len(results))
	}

	if results[0].Outcome != github.SubIssueCreated || results[0].Number != 42 {
		t.Errorf("C1 = %s #%d, want created #42", results[0].Outcome, results[0].Number)
	}
	if results[1].Outcome != github.SubIssueFailed {
		t.Errorf("C2 outcome = %s, want failed", results[1].Outcome)
	}
	if !errors.Is(results[1].Err, github.ErrUnprocessable) {
		t.Errorf("C2 error = %v, want ErrUnprocessable — the caller reads the same typed error "+
			"it would have had from a single call", results[1].Err)
	}
	if results[2].Outcome != github.SubIssueNotAttempted {
		t.Errorf("C3 outcome = %s, want not_attempted: the run stops at the first failure rather "+
			"than pushing on into the endpoint GitHub secondary-rate-limits", results[2].Outcome)
	}
	if results[2].Err != nil {
		t.Errorf("C3 carries the error %v; nothing was tried for it", results[2].Err)
	}

	if got := len(requestsTo(f, http.MethodPost, "/issues")); got != 2 {
		t.Errorf("%d create requests, want 2 — the third story must not be attempted", got)
	}
	for _, want := range []string{`"created":1`, `"failed":1`, `"stopped_early":true`} {
		if !strings.Contains(f.Logs(), want) {
			t.Errorf("the summary line does not report %s:\n%s", want, f.Logs())
		}
	}
}

// TestCreateSubIssuesReportsAnIssueItCouldNotAttach covers the sharpest partial
// failure: the issue exists and its parent does not know about it. It has an
// outcome of its own because reporting it as a plain failure would send the
// next run to create the very issue that is already there.
func TestCreateSubIssuesReportsAnIssueItCouldNotAttach(t *testing.T) {
	t.Parallel()

	f := newFixture(t, (&forge{
		creates: []forgeReply{{status: http.StatusCreated, body: issueJSON(9001, 42)}},
		adds:    []forgeReply{{status: http.StatusNotFound, body: `{"message": "Not Found"}`}},
	}).handler(t))

	results, err := f.Client.CreateSubIssues(t.Context(), testRepo, testParent,
		[]github.SubIssueSpec{spec(t, "C1", testMarkerC1)})
	if err != nil {
		t.Fatalf("CreateSubIssues: %v", err)
	}

	if got := results[0].Outcome; got != github.SubIssueUnattached {
		t.Fatalf("outcome = %s, want unattached", got)
	}
	if results[0].ID != 9001 || results[0].Number != 42 {
		t.Errorf("result = {ID: %d, Number: %d}, want the orphan's own {9001, 42} — without the id "+
			"a later run cannot finish attaching it", results[0].ID, results[0].Number)
	}
	if !errors.Is(results[0].Err, github.ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", results[0].Err)
	}
}

// TestCreateSubIssuesResumesAnUnattachedIssue is the other half of the above:
// handed the orphan's id, a later run attaches it instead of filing the story a
// second time (AC-C1.4, AC-C1.5).
func TestCreateSubIssuesResumesAnUnattachedIssue(t *testing.T) {
	t.Parallel()

	f := newFixture(t, (&forge{}).handler(t))

	resume := spec(t, "C1", testMarkerC1)
	resume.AttachID = 9001

	results, err := f.Client.CreateSubIssues(t.Context(), testRepo, testParent,
		[]github.SubIssueSpec{resume, spec(t, "C2", testMarkerC2)})
	if err != nil {
		t.Fatalf("CreateSubIssues: %v", err)
	}

	if got := results[0].Outcome; got != github.SubIssueAttached {
		t.Fatalf("C1 outcome = %s (%v), want attached", got, results[0].Err)
	}
	if results[0].ID != 9001 {
		t.Errorf("C1 ID = %d, want the id it was given, 9001", results[0].ID)
	}
	if got := len(requestsTo(f, http.MethodPost, "/issues")); got != 1 {
		t.Errorf("%d issues were created, want 1 — the resumed story already has one", got)
	}

	adds := requestsTo(f, http.MethodPost, "/sub_issues")
	if len(adds) != 2 {
		t.Fatalf("%d attach calls, want 2", len(adds))
	}
	var payload struct {
		SubIssueID int64 `json:"sub_issue_id"`
	}
	adds[0].JSON(t, &payload)
	if payload.SubIssueID != 9001 {
		t.Errorf("the resumed attach sent sub_issue_id %d, want 9001", payload.SubIssueID)
	}
}

// TestARerunAfterAPartialFailureFilesOnlyTheRest is the whole of AC-C1.5 in one
// test: a run that stops halfway, then a second run against a forge that now
// holds what the first one filed. Nothing is filed twice and nothing is left
// out, and the second run needed no memory of the first — only the markers.
func TestARerunAfterAPartialFailureFilesOnlyTheRest(t *testing.T) {
	t.Parallel()

	specs := []github.SubIssueSpec{
		spec(t, "C1", testMarkerC1),
		spec(t, "C2", testMarkerC2),
		spec(t, "C3", testMarkerC3),
	}

	first := newFixture(t, (&forge{
		creates: []forgeReply{
			{status: http.StatusCreated, body: issueJSON(9001, 42)},
			{status: http.StatusTooManyRequests, body: `{"message": "slow down"}`},
		},
	}).handler(t))

	firstResults, err := first.Client.CreateSubIssues(t.Context(), testRepo, testParent, specs)
	if err != nil {
		t.Fatalf("the first run: %v", err)
	}
	if firstResults[0].Outcome != github.SubIssueCreated || firstResults[1].Outcome != github.SubIssueFailed {
		t.Fatalf("the first run went %s, %s; want created, failed",
			firstResults[0].Outcome, firstResults[1].Outcome)
	}

	// The forge now holds what the first run filed, attached to the parent.
	second := newFixture(t, (&forge{
		list: `[` + subIssueJSON(9001, 42, testMarkerC1+"\n\nWhat this story is for.\n") + `]`,
		creates: []forgeReply{
			{status: http.StatusCreated, body: issueJSON(9002, 43)},
			{status: http.StatusCreated, body: issueJSON(9003, 44)},
		},
	}).handler(t))

	secondResults, err := second.Client.CreateSubIssues(t.Context(), testRepo, testParent, specs)
	if err != nil {
		t.Fatalf("the second run: %v", err)
	}

	want := []github.SubIssueOutcome{github.SubIssueExisted, github.SubIssueCreated, github.SubIssueCreated}
	for i, outcome := range want {
		if got := secondResults[i].Outcome; got != outcome {
			t.Errorf("the second run's story %d went %s (%v), want %s",
				i, got, secondResults[i].Err, outcome)
		}
	}
	if got := len(requestsTo(second, http.MethodPost, "/issues")); got != 2 {
		t.Errorf("the second run created %d issues, want the 2 the first run did not", got)
	}
}

// TestCreateSubIssuesSurfacesARateLimitAndDoesNotWait pins the resolution of
// T-C1.7 against D1's rate-limit decision.
//
// GitHub documents a secondary rate limit on the add endpoint, and the plan
// asked for backoff. This package does not back off — that is the daemon's job,
// because only the daemon knows what else is queued — so what it does instead
// is hand the delay GitHub asked for straight back, in the typed error the
// daemon already reads, and stop. Resuming is the answer to the pause, and it
// survives the process being killed in a way a sleep does not.
func TestCreateSubIssuesSurfacesARateLimitAndDoesNotWait(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeReply(w, forgeReply{status: http.StatusOK, body: `[]`})
			return
		}
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message": "You have exceeded a secondary rate limit",
			"documentation_url": "https://docs.github.com/rest/using-the-rest-api/rate-limits-for-the-rest-api#about-secondary-rate-limits"}`)
	})

	started := time.Now()
	results, err := f.Client.CreateSubIssues(t.Context(), testRepo, testParent, []github.SubIssueSpec{
		spec(t, "C1", testMarkerC1),
		spec(t, "C2", testMarkerC2),
	})
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("CreateSubIssues: %v", err)
	}

	var limited *github.RateLimitError
	if !errors.As(results[0].Err, &limited) {
		t.Fatalf("C1 error = %v, want a *github.RateLimitError", results[0].Err)
	}
	if !limited.Secondary {
		t.Error("Secondary = false; a secondary limit clears in seconds and a primary one does not")
	}
	if limited.RetryAfter != 42*time.Second {
		t.Errorf("RetryAfter = %s, want 42s — the daemon's backoff reads this field", limited.RetryAfter)
	}
	if results[1].Outcome != github.SubIssueNotAttempted {
		t.Errorf("C2 outcome = %s, want not_attempted", results[1].Outcome)
	}
	if elapsed > 30*time.Second {
		t.Errorf("CreateSubIssues took %s; this package sleeps nowhere", elapsed)
	}
	if got := len(f.Requests()); got != 2 {
		t.Errorf("the server saw %d requests, want 2: the listing and the one create that was refused", got)
	}
}

// TestCreateSubIssuesRefusesABatchItWillNotSend covers the inputs that mean the
// batch cannot be attempted at all — the one case that is an error return
// rather than an outcome. Every one of them is caught before the first request,
// because finding the eighth bad spec after seven issues have been filed turns
// a programming mistake into forge state somebody has to clean up.
func TestCreateSubIssuesRefusesABatchItWillNotSend(t *testing.T) {
	t.Parallel()

	duplicateMarker := spec(t, "C2", testMarkerC1)
	mismatched := spec(t, "C1", testMarkerC1)
	mismatched.Body = body(t, testMarkerC2+"\n\na story\n")
	noMarker := spec(t, "C1", testMarkerC1)
	noMarker.Marker = ""
	unredacted := spec(t, "C1", testMarkerC1)
	unredacted.Title = redact.Body{}
	negativeResume := spec(t, "C1", testMarkerC1)
	negativeResume.AttachID = -1
	anonymous := spec(t, "", testMarkerC1)

	tests := []struct {
		name  string
		specs []github.SubIssueSpec
	}{
		{name: "no stories", specs: nil},
		{name: "a story with no id", specs: []github.SubIssueSpec{anonymous}},
		{name: "the same story twice", specs: []github.SubIssueSpec{
			spec(t, "C1", testMarkerC1), spec(t, "C1", testMarkerC1),
		}},
		{name: "two stories sharing a marker", specs: []github.SubIssueSpec{
			spec(t, "C1", testMarkerC1), duplicateMarker,
		}},
		{name: "a body that does not carry its marker", specs: []github.SubIssueSpec{mismatched}},
		{name: "no marker at all", specs: []github.SubIssueSpec{noMarker}},
		{name: "an unredacted title", specs: []github.SubIssueSpec{unredacted}},
		{name: "a resume id no forge would mint", specs: []github.SubIssueSpec{negativeResume}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				w.WriteHeader(http.StatusInternalServerError)
			})

			results, err := f.Client.CreateSubIssues(t.Context(), testRepo, testParent, tc.specs)
			if !errors.Is(err, github.ErrInvalidArgument) {
				t.Fatalf("CreateSubIssues error = %v, want ErrInvalidArgument", err)
			}
			if results != nil {
				t.Errorf("results = %+v, want none: nothing was attempted", results)
			}
			if got := len(f.Requests()); got != 0 {
				t.Errorf("the server saw %d requests; a batch that will not be sent must not "+
					"reach the network at all", got)
			}
		})
	}
}

// TestCreateSubIssuesWillNotFileWithoutTheListing is the other error return.
// Without knowing what is already there, "create" and "create again" are the
// same instruction, and guessing is how a plan is filed twice.
func TestCreateSubIssuesWillNotFileWithoutTheListing(t *testing.T) {
	t.Parallel()

	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected write %s %s after a failed listing", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"message": "oops"}`)
	})

	results, err := f.Client.CreateSubIssues(t.Context(), testRepo, testParent,
		[]github.SubIssueSpec{spec(t, "C1", testMarkerC1)})
	if err == nil {
		t.Fatal("CreateSubIssues returned no error when the sub-issue listing failed")
	}
	if results != nil {
		t.Errorf("results = %+v, want none", results)
	}
	if got := len(requestsTo(f, http.MethodPost, "/issues")); got != 0 {
		t.Errorf("%d issues were created after the listing failed", got)
	}
}

// TestListSubIssuesReadsEveryPage. A plan with more than one page of stories is
// unlikely, but a listing that stopped at the first page would report the rest
// as unfiled and file them again — the exact duplication the listing exists to
// prevent.
func TestListSubIssuesReadsEveryPage(t *testing.T) {
	t.Parallel()

	f := newFixture(t, pagedHandler([]string{
		`[` + subIssueJSON(8001, 41, testMarkerC1) + `]`,
		`[` + subIssueJSON(8002, 43, testMarkerC2) + `]`,
	}))

	issues, err := f.Client.ListSubIssues(t.Context(), testRepo, testParent)
	if err != nil {
		t.Fatalf("ListSubIssues: %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("got %d sub-issues, want 2: %+v", len(issues), issues)
	}
	if issues[0].ID != 8001 || issues[1].ID != 8002 {
		t.Errorf("ids = %d, %d; want 8001, 8002", issues[0].ID, issues[1].ID)
	}
	if !strings.Contains(issues[0].Body, testMarkerC1) {
		t.Errorf("the first sub-issue's body did not survive the listing: %q", issues[0].Body)
	}
	if got := f.Requests()[0].Path; got != "/repos/acme/acme-api/issues/7/sub_issues" {
		t.Errorf("path = %q, want the parent's sub_issues collection", got)
	}
}

// TestARenderedSubIssueIsAcceptedAsASpec is the seam between the two packages,
// asserted rather than assumed: internal/report renders and internal/github
// posts, they do not import one another, and the only thing holding them
// together is that a rendered SubIssue is exactly what a SubIssueSpec is made
// of — marker included.
//
// It is also the test that would fail if StoryMarker's shape and this package's
// idempotency check ever stopped agreeing, which the hand-written markers
// elsewhere in this file cannot catch.
func TestARenderedSubIssueIsAcceptedAsASpec(t *testing.T) {
	t.Parallel()

	reporter, err := report.New(report.Options{
		Redactor: redact.New(redact.Options{Environ: []string{}}),
		Limit:    github.CommentSizeLimit,
	})
	if err != nil {
		t.Fatalf("report.New: %v", err)
	}
	ref := naming.Ref{Forge: naming.ForgeGitHub, Owner: "acme", Repo: "acme-api", Issue: 123}
	rendered, err := reporter.RenderCompleteStory(t.Context(), report.Story{
		Ref: ref, ID: "C1",
		Title:   "The plan ships as one sub-issue per user story",
		Excerpt: "Each story arrives as its own issue, so it can be tracked and closed on its own.",
	})
	if err != nil {
		t.Fatalf("RenderCompleteStory: %v", err)
	}

	f := newFixture(t, (&forge{
		creates: []forgeReply{{status: http.StatusCreated, body: issueJSON(9001, 42)}},
	}).handler(t))

	results, err := f.Client.CreateSubIssues(t.Context(), testRepo, testParent, []github.SubIssueSpec{{
		StoryID: "C1",
		Title:   rendered.Title,
		Body:    rendered.Body,
		Marker:  rendered.Marker,
	}})
	if err != nil {
		t.Fatalf("CreateSubIssues on a rendered story: %v", err)
	}
	if got := results[0].Outcome; got != github.SubIssueCreated {
		t.Fatalf("outcome = %s (%v), want created", got, results[0].Err)
	}

	// And the same rendering, found on the forge, is recognised as already
	// filed. This is the round trip the idempotency guarantee rests on.
	again := newFixture(t, (&forge{
		list: `[` + subIssueJSON(9001, 42, rendered.Body.String()) + `]`,
	}).handler(t))

	results, err = again.Client.CreateSubIssues(t.Context(), testRepo, testParent, []github.SubIssueSpec{{
		StoryID: "C1",
		Title:   rendered.Title,
		Body:    rendered.Body,
		Marker:  rendered.Marker,
	}})
	if err != nil {
		t.Fatalf("the second CreateSubIssues: %v", err)
	}
	if got := results[0].Outcome; got != github.SubIssueExisted {
		t.Errorf("outcome = %s, want existed: the rendered marker must match itself", got)
	}
}
