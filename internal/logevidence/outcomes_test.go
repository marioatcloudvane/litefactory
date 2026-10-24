package logevidence

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/cloudvane/litefactory/internal/mcpadapter"
	"github.com/cloudvane/litefactory/internal/redact"
	"github.com/cloudvane/litefactory/internal/store"
	"github.com/cloudvane/litefactory/internal/submission"
)

func (f *evidenceFixture) failedRun(t *testing.T, issue int, reason string, age time.Duration) store.Run {
	t.Helper()
	session, err := f.db.GetOrCreateSession(t.Context(), f.repo, issue)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := f.db.GetOrCreateSegment(t.Context(), session.ID, 1, fmt.Sprintf("session-%d", session.ID), "codex")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().Add(-age)
	run, err := f.db.AllocateRun(t.Context(), store.AllocateRunParams{SessionID: session.ID, SegmentID: segment.ID, Mode: "implement", Agent: "codex", TriggerCommentID: time.Now().UnixNano(), StartedAt: start, LogPath: func(n int) string { return filepath.Join(f.root, fmt.Sprintf("failed-%d-%d.jsonl", issue, n)) }})
	if err != nil {
		t.Fatal(err)
	}
	run, err = f.db.FinishRun(t.Context(), run.ID, store.RunOutcome{Status: store.RunFailed, SemanticOutcome: "failed", Error: reason, FinishedAt: start.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func (f *evidenceFixture) outcomeService(t *testing.T, limits Limits) *Service {
	t.Helper()
	budget, err := NewAnalystBudget(limits)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(t.Context(), Options{Store: f.db, RunsRoot: f.root, Scope: Scope{RepositoryIDs: []int64{f.repo.ID}}, Budget: budget, Outcomes: true, Redactor: redact.New(redact.Options{Environ: []string{"EVIDENCE_API_TOKEN=fixture-secret-value"}})})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func outcomeCall(t *testing.T, b *submission.Broker, name string, args any) (Result, string) {
	t.Helper()
	result, response, err := evidenceCall(t.Context(), b, name, args)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := mcpadapter.EncodeToolResult(response)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > outcomePageMaximum {
		t.Fatalf("encoded result exceeds ceiling: %d", len(encoded))
	}
	return result, string(response.Result)
}

func TestIncidentsListPagesNewestFirstFiltersAndRedacts(t *testing.T) {
	f := newEvidenceFixture(t)
	var busy []int64
	for i := range 3 {
		busy = append(busy, f.failedRun(t, 1, "tests failed", time.Duration(5-i)*time.Hour).ID)
	}
	quiet := f.failedRun(t, 2, "token fixture-secret-value leaked", time.Hour).ID
	b := evidenceBroker(t, f.outcomeService(t, Limits{}))

	first, body := outcomeCall(t, b, "incidents_list", map[string]any{"limit": 2})
	if first.Status != "ok" || len(first.Incidents) != 2 || first.NextCursor == "" {
		t.Fatalf("first page=%s", body)
	}
	rest, body2 := outcomeCall(t, b, "incidents_list", map[string]any{"limit": 2, "cursor": first.NextCursor})
	if rest.Status != "ok" || len(rest.Incidents) != 2 || rest.NextCursor != "" || !rest.Coverage.EndOfSource {
		t.Fatalf("second page=%s", body2)
	}
	all := slices.Concat(first.Incidents, rest.Incidents)
	var runs []int64
	for i, in := range all {
		runs = append(runs, in.RunIDs...)
		if in.Kind != store.IncidentFailedRun || in.WhatWentWrong == "" || i > 0 && in.At.After(all[i-1].At) {
			t.Fatalf("incident order or shape: %+v", all)
		}
	}
	if all[0].RunIDs[0] != quiet || all[0].Counts.FailedRuns != 1 || !slices.Contains(busy, all[1].RunIDs[0]) || all[1].Counts.FailedRuns != 3 {
		t.Fatalf("newest incident did not come first: %+v", all)
	}
	if strings.Contains(body+body2, "fixture-secret-value") || all[0].Redactions == 0 {
		t.Fatalf("incident text not redacted: %s", body2)
	}
	if len(runs) != 4 {
		t.Fatalf("runs=%v", runs)
	}
	if bad, _ := outcomeCall(t, b, "incidents_list", map[string]any{"limit": 2, "kind": "bug", "cursor": first.NextCursor}); bad.Status != "expired_cursor" {
		t.Fatalf("cursor reused for another filter: %+v", bad)
	}
	if bugs, _ := outcomeCall(t, b, "incidents_list", map[string]any{"kind": "bug"}); bugs.Status != "ok" || len(bugs.Incidents) != 0 {
		t.Fatalf("kind filter=%+v", bugs)
	}
	if task, _ := outcomeCall(t, b, "incidents_list", map[string]any{"task_ref": all[0].TaskRef, "role": "implement"}); len(task.Incidents) != 1 || task.Incidents[0].RunIDs[0] != quiet {
		t.Fatalf("task and role filter=%+v", task)
	}
	if later, _ := outcomeCall(t, b, "incidents_list", map[string]any{"after": time.Now().UTC().Format(time.RFC3339)}); later.Status != "ok" || len(later.Incidents) != 0 {
		t.Fatalf("time filter=%+v", later)
	}

	// The plain reader keeps its two-tool catalog.
	plain := evidenceBroker(t, f.service(t, Scope{RepositoryIDs: []int64{f.repo.ID}}, nil))
	if r, _ := outcomeCall(t, plain, "incidents_list", map[string]any{}); r.Status != "invalid_request" {
		t.Fatalf("reader without outcome grant listed incidents: %+v", r)
	}
}

// The failed command's long output keeps only its tail.
var codexDigestFixture = `{"type":"thread.started","thread_id":"t"}
{"type":"item.completed","item":{"id":"i0","type":"agent_message","text":"I will run the tests."}}
{"type":"item.started","item":{"id":"i1","type":"command_execution","command":"go test ./...","status":"in_progress"}}
{"type":"item.completed","item":{"id":"i1","type":"command_execution","command":"go test ./...","aggregated_output":"ok  pkg/a\n` + strings.Repeat(`VERBOSE-OUTPUT-MARKER line\n`, 100) + `--- FAIL: TestRetry\nFAIL","exit_code":1,"status":"failed"}}
{"type":"item.completed","item":{"id":"i2","type":"command_execution","command":"go test ./...","aggregated_output":"SUCCESS-OUTPUT-MARKER","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"i3","type":"file_change","changes":[{"path":"/home/u/.lf-worktrees/api/task-7/internal/retry.go","kind":"update"}],"status":"completed"}}
{"type":"turn.completed","usage":{"input_tokens":1}}
`

const claudeDigestFixture = `{"type":"system","subtype":"init","session_id":"s"}
{"type":"assistant","message":{"content":[{"type":"text","text":"Checking the build."},{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"make build"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","is_error":true,"content":"compile error: undefined: Foo"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","id":"tu2","name":"Edit","input":{"file_path":"/home/u/.lf-worktrees/api/task-7/main.go","old_string":"a","new_string":"b"}}]}}
{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu2","content":"FILE-CONTENT-MARKER"}]}}
{"type":"result","subtype":"success","is_error":false,"result":"done"}
`

func digestShape(entries []DigestEntry) string {
	var parts []string
	for _, e := range entries {
		part := e.Kind
		if e.Failed {
			part += "!"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, " ")
}

func TestRunDigestCondensesCodexAndClaudeStreams(t *testing.T) {
	f := newEvidenceFixture(t)
	codex := f.run(t, f.repo, 1, codexDigestFixture, false)
	claude := f.run(t, f.repo, 2, claudeDigestFixture, false)
	b := evidenceBroker(t, f.outcomeService(t, Limits{}))

	r, body := outcomeCall(t, b, "run_digest_read", map[string]any{"run_id": codex.ID})
	if r.Status != "ok" || digestShape(r.Digest) != "message command! command edit" || !r.Coverage.EndOfSource || r.NextCursor != "" {
		t.Fatalf("codex digest=%s", body)
	}
	if strings.Contains(body, "pkg/a") || strings.Contains(body, "SUCCESS-OUTPUT-MARKER") || strings.Count(body, "VERBOSE-OUTPUT-MARKER") > 20 {
		t.Fatalf("digest carried command output: %s", body)
	}
	failed := r.Digest[1]
	if failed.Text != "go test ./..." || failed.Exit == nil || *failed.Exit != 1 || !strings.Contains(failed.Error, "FAIL: TestRetry") {
		t.Fatalf("failed command=%+v", failed)
	}
	if r.Digest[0].Text != "I will run the tests." || !slices.Equal(r.Digest[3].Files, []string{"update internal/retry.go"}) {
		t.Fatalf("messages or edits lost: %s", body)
	}
	if s := r.DigestSummary; s == nil || s.Messages != 1 || s.Commands != 2 || s.Failed != 1 || s.Edits != 1 {
		t.Fatalf("summary=%+v", r.DigestSummary)
	}

	// A digest offset reads the exact record, with a citation that recovers.
	read, _ := outcomeCall(t, b, "run_logs_read", map[string]any{"run_id": codex.ID, "offset": failed.Offset, "limit": 1})
	if read.Status != "ok" || len(read.Records) != 1 || read.Records[0].Citation.Start != failed.Offset || read.NextCursor != "" {
		t.Fatalf("offset read=%+v", read)
	}
	again, _ := outcomeCall(t, b, "run_logs_read", map[string]any{"run_id": codex.ID, "citation": read.Records[0].Citation})
	if again.Status != "ok" || len(again.Records) != 1 {
		t.Fatalf("citation from offset read=%+v", again)
	}
	if mid, _ := outcomeCall(t, b, "run_logs_read", map[string]any{"run_id": codex.ID, "offset": failed.Offset + 1}); mid.Status != "source_changed" {
		t.Fatalf("offset inside a record accepted: %+v", mid)
	}

	r, body = outcomeCall(t, b, "run_digest_read", map[string]any{"run_id": claude.ID})
	if r.Status != "ok" || digestShape(r.Digest) != "message command! edit" || strings.Contains(body, "FILE-CONTENT-MARKER") {
		t.Fatalf("claude digest=%s", body)
	}
	if r.Digest[1].Text != "make build" || !strings.Contains(r.Digest[1].Error, "undefined: Foo") || !slices.Equal(r.Digest[2].Files, []string{"main.go"}) {
		t.Fatalf("claude entries=%+v", r.Digest)
	}
}

func TestRunDigestPagesAndChargesDigestCeiling(t *testing.T) {
	f := newEvidenceFixture(t)
	first := f.run(t, f.repo, 1, codexDigestFixture, false)
	second := f.run(t, f.repo, 2, claudeDigestFixture, false)
	b := evidenceBroker(t, f.outcomeService(t, Limits{Digests: 1}))

	var shapes []string
	page, _ := outcomeCall(t, b, "run_digest_read", map[string]any{"run_id": first.ID, "limit": 3})
	for {
		if page.Status != "ok" {
			t.Fatalf("page=%+v", page)
		}
		shapes = append(shapes, digestShape(page.Digest))
		if page.NextCursor == "" {
			break
		}
		if page.Coverage.EndOfSource {
			t.Fatal("intermediate page claimed end of source")
		}
		page, _ = outcomeCall(t, b, "run_digest_read", map[string]any{"run_id": first.ID, "limit": 3, "cursor": page.NextCursor})
	}
	if strings.Join(shapes, "|") != "message command! command|edit" || !page.Coverage.EndOfSource {
		t.Fatalf("pages=%v", shapes)
	}
	if _, raw := outcomeCall(t, b, "run_digest_read", map[string]any{"run_id": second.ID}); !strings.Contains(raw, `"trigger":"digests"`) {
		t.Fatalf("second digested run exceeded the ceiling: %s", raw)
	}
	var usage struct {
		Budget Usage `json:"budget"`
	}
	_, raw := outcomeCall(t, evidenceBroker(t, f.outcomeService(t, Limits{})), "run_digest_read", map[string]any{"run_id": first.ID})
	if err := json.Unmarshal([]byte(raw), &usage); err != nil || usage.Budget.Consumed.Digests != 1 || usage.Budget.Remaining.Digests != AnalystLimits().Digests-1 {
		t.Fatalf("digest usage=%s", raw)
	}
}
