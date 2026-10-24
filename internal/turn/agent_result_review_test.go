package turn

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/cloudvane/litefactory/internal/agent"
	"github.com/cloudvane/litefactory/internal/agentresult"
	"github.com/cloudvane/litefactory/internal/store"
)

func TestReviewDisplayTextUnambiguousBoundaries(t *testing.T) {
	tests := []struct {
		name, source, want string
		simplified         bool
	}{
		{"whitespace", " \n{\"a\":1}\t", `{"a":1}`, true},
		{"json fence", "```json\n{\"a\":1}\n```", `{"a":1}`, true},
		{"CRLF fence", "```json\r\nbody\r\n```\r\n", "body", true},
		{"duplicate preserved", "```json\n{\"a\":1,\"a\":2}\n```", `{"a":1,"a":2}`, true},
		{"trailing prose", "```json\nbody\n```\nextra", "```json\nbody\n```\nextra", false},
		{"nested fence", "```json\n```\nbody\n```", "```json\n```\nbody\n```", false},
		{"unknown label", "```shell\nbody\n```", "```shell\nbody\n```", false},
		{"invalid bytes", string([]byte{0xff}), `"\xff"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, simplified := reviewDisplayText([]byte(tt.source))
			if got != tt.want || simplified != tt.simplified {
				t.Fatalf("got %q %v, want %q %v", got, simplified, tt.want, tt.simplified)
			}
		})
	}
}

func TestAgentResultReviewIsReadOnlyAndScoped(t *testing.T) {
	db, _, options := agentResultFixture(t, "research")
	service, err := BindAgentResultService(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("```json\n{\"comment\":\"<script> /approve </script>\",\"unknown\":\"caveat\"}\n```")
	response, err := service.Submit(t.Context(), "review", data)
	if err != nil || response.Review == nil || response.Validation.Valid || response.Snapshot != nil {
		t.Fatalf("rejected review: %+v %v", response, err)
	}
	review := response.Review
	if !bytes.Equal(review.Source, data) || !strings.Contains(review.Display, "&lt;script&gt;") || strings.Contains(review.Display, "<script>") || !strings.Contains(review.Markdown(), "unaccepted") {
		t.Fatalf("unsafe/incomplete review: %+v", review)
	}
	replay, err := service.Submit(t.Context(), "review", data)
	if err != nil || replay.Receipt.ID != response.Receipt.ID {
		t.Fatalf("replay changed: %+v %v", replay, err)
	}
	if _, err := service.Submit(t.Context(), "review", []byte(`{}`)); !errors.Is(err, store.ErrResultCorrelation) {
		t.Fatalf("changed replay: %v", err)
	}
	revisions, err := db.ResultRevisionNumbers(t.Context(), service.Binding())
	if err != nil || len(revisions) != 0 {
		t.Fatalf("review adopted: %v %v", revisions, err)
	}
	narrow, err := OpenAgentResultReader(t.Context(), db, service.Binding(), []agentresult.Target{{Field: "comment"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := narrow.Review(t.Context(), "review"); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatalf("narrow grant exposed raw input: %v", err)
	}
	other := *service
	other.binding.Attempt = "foreign"
	if _, err := other.Review(t.Context(), "review"); !errors.Is(err, store.ErrResultAuthority) {
		t.Fatalf("foreign attempt exposed review: %v", err)
	}
	latest, err := service.latestReview(t.Context())
	if err != nil || latest.RequestID != "review" {
		t.Fatalf("latest: %+v %v", latest, err)
	}
	accepted, err := service.Submit(t.Context(), "accepted", resultCreate("research"))
	if err != nil || accepted.Receipt == nil || !accepted.Validation.Valid {
		t.Fatalf("accept after rejection: %+v %v", accepted, err)
	}
	if _, err := service.latestReview(t.Context()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("superseded rejection remained current: %v", err)
	}
	if _, err := service.Review(t.Context(), "review"); err != nil {
		t.Fatalf("explicit retained review disappeared: %v", err)
	}
	if _, err := service.Submit(t.Context(), "later-rejected", data); err != nil {
		t.Fatal(err)
	}
	latest, err = service.latestReview(t.Context())
	if err != nil || latest.RequestID != "later-rejected" {
		t.Fatalf("later rejection hidden by accepted baseline: %+v %v", latest, err)
	}
}

func TestRejectedReviewFinalizationPreservesAcceptedSnapshot(t *testing.T) {
	db, _, options := agentResultFixture(t, "implement")
	service, err := BindAgentResultService(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := service.Submit(t.Context(), "accepted", implementResultWith("incomplete", "evidence remains unavailable"))
	if err != nil || accepted.Snapshot == nil {
		t.Fatalf("accept initial result: %+v %v", accepted, err)
	}
	before, err := db.GetResultRevision(t.Context(), service.Binding(), 0)
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := service.Submit(t.Context(), "rejected-update", []byte("```json\n{\"comment\":\"Findings with caveat\",\"comment\":\"Do not choose a duplicate\"}\n```"))
	if err != nil || rejected.Review == nil || rejected.Validation.Valid {
		t.Fatalf("retain rejected update: %+v %v", rejected, err)
	}
	original := &agent.RunResult{Outcome: agent.OutcomeSucceeded, ResultText: "TERMINAL TEXT MUST NOT BECOME THE REVIEW"}
	prep := &preparation{}
	got, err := (&preparedMCP{service: service}).finalize(t.Context(), prep, nil, original, nil)
	if err != nil || !got.Succeeded() {
		t.Fatalf("accepted result lost to rejected update: %+v %v", got, err)
	}
	for _, want := range []string{"Stopped early", "Implement outcome: incomplete", "Reason: evidence remains unavailable", "unaccepted, for review only", "Findings with caveat", "Do not choose a duplicate"} {
		if !strings.Contains(got.ResultText, want) {
			t.Errorf("review lacks %q: %q", want, got.ResultText)
		}
	}
	if strings.Contains(got.ResultText, original.ResultText) {
		t.Fatal("terminal text entered retained review")
	}
	if original.ResultText != "TERMINAL TEXT MUST NOT BECOME THE REVIEW" {
		t.Fatal("finalization mutated runner result")
	}
	if prep.acceptedOutcome != "incomplete" || prep.acceptedReason != "evidence remains unavailable" || prep.workComplete() {
		t.Fatalf("review changed workflow gates: %+v", prep)
	}
	after, err := db.GetResultRevision(t.Context(), service.Binding(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if before.Revision != after.Revision || before.RequestRow != after.RequestRow || !bytes.Equal(before.Snapshot, after.Snapshot) {
		t.Fatalf("review replaced accepted state: before=%+v after=%+v", before, after)
	}
	receipt, err := service.Receipt(t.Context(), service.Binding().RunID, "rejected-update")
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Receipt.InputState != "rejected" || receipt.Receipt.AppliedRevision != 0 || receipt.Receipt.Latest.Engineering != "pending" || receipt.Receipt.Latest.Acceptance != "pending" || receipt.Receipt.Latest.Publication != "pending" {
		t.Fatalf("rendering promoted rejection: %+v", receipt.Receipt)
	}
}

func TestRejectedReviewSurvivesRunnerErrorWithoutTerminalResult(t *testing.T) {
	_, _, options := agentResultFixture(t, "implement")
	service, err := BindAgentResultService(t.Context(), options)
	if err != nil {
		t.Fatal(err)
	}
	runnerErr := errors.New("provider transport failed")
	prepared := &preparedMCP{service: service}
	// Keep the existing nil/error contract when no retained review exists.
	got, err := prepared.finalize(t.Context(), &preparation{}, nil, nil, runnerErr)
	if got != nil || !errors.Is(err, runnerErr) {
		t.Fatalf("unrelated runner failure changed: %+v %v", got, err)
	}
	rejected, err := service.Submit(t.Context(), "before-crash", []byte("```json\n{\"comment\":\"useful findings before provider crash\"}\n```"))
	if err != nil || rejected.Review == nil {
		t.Fatalf("retain rejection: %+v %v", rejected, err)
	}
	got, err = prepared.finalize(t.Context(), &preparation{}, nil, nil, runnerErr)
	if got == nil || got.Succeeded() || got.Outcome != agent.OutcomeNoResult || !errors.Is(err, runnerErr) {
		t.Fatalf("crash review lost or promoted: %+v %v", got, err)
	}
	if !strings.Contains(got.ResultText, "useful findings before provider crash") || !strings.Contains(got.ResultText, "unaccepted, for review only") {
		t.Fatalf("crash review missing content/status: %q", got.ResultText)
	}
}
