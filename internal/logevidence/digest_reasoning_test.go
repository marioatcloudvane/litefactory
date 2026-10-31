package logevidence

import (
	"strings"
	"testing"
)

var longReasoning = strings.Repeat("LONG-REASONING ", 200) + "END-OF-THOUGHT"

var codexReasoningFixture = `{"type":"thread.started","thread_id":"t"}
{"type":"item.completed","item":{"id":"r0","type":"reasoning","text":"**Planning the retry fix**\n\nThe rule says to keep retries bounded; token fixture-secret-value is unrelated."}}
{"type":"item.completed","item":{"id":"r1","type":"reasoning","text":""}}
{"type":"item.completed","item":{"id":"r2","type":"reasoning","summary":[{"type":"summary_text","text":"Checking the summary-array shape."}]}}
{"type":"item.completed","item":{"id":"r3","type":"reasoning","text":"` + longReasoning + `"}}
{"type":"item.completed","item":{"id":"r4","type":"reasoning","summary":[{"type":"summary_text","text":"**First heading**"},{"type":"summary_text","text":""},{"type":"summary_text","text":"Second part with fixture-secret-value."},{"type":"summary_text","text":"Third part."}]}}
{"type":"item.completed","item":{"id":"i0","type":"agent_message","text":"I will run the tests."}}
{"type":"turn.completed","usage":{"input_tokens":1}}
`

const claudeThinkingFixture = `{"type":"system","subtype":"init","session_id":"s"}
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"","signature":"sig-empty"}]}}
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"The standard asks for table tests, so I will add one.","signature":"sig"},{"type":"text","text":"Adding a test."}]}}
{"type":"result","subtype":"success","is_error":false,"result":"done"}
`

func TestRunDigestKeepsFullRedactedReasoning(t *testing.T) {
	f := newEvidenceFixture(t)
	codex := f.run(t, f.repo, 1, codexReasoningFixture, false)
	claude := f.run(t, f.repo, 2, claudeThinkingFixture, false)
	b := evidenceBroker(t, f.outcomeService(t, Limits{}))

	r, body := outcomeCall(t, b, "run_digest_read", map[string]any{"run_id": codex.ID})
	if r.Status != "ok" || digestShape(r.Digest) != "reasoning reasoning reasoning reasoning message" {
		t.Fatalf("codex digest=%s", body)
	}
	if !strings.Contains(r.Digest[0].Text, "Planning the retry fix") || strings.Contains(body, "fixture-secret-value") {
		t.Fatalf("codex reasoning lost or unredacted: %s", body)
	}
	if r.Digest[1].Text != "Checking the summary-array shape." {
		t.Fatalf("codex summary array=%+v", r.Digest[1])
	}
	if long := r.Digest[2].Text; long != strings.TrimSpace(longReasoning) {
		t.Fatalf("long reasoning not kept in full: %d bytes, ends %q", len(long), long[max(0, len(long)-20):])
	}
	parts := r.Digest[3].Text
	first, second, third := strings.Index(parts, "**First heading**"), strings.Index(parts, "Second part with"), strings.Index(parts, "Third part.")
	if first < 0 || second <= first || third <= second {
		t.Fatalf("summary parts lost or out of order: %q", parts)
	}
	if s := r.DigestSummary; s == nil || s.Reasoning != 4 || s.Messages != 1 {
		t.Fatalf("summary=%+v", r.DigestSummary)
	}

	r, body = outcomeCall(t, b, "run_digest_read", map[string]any{"run_id": claude.ID})
	if r.Status != "ok" || digestShape(r.Digest) != "reasoning message" {
		t.Fatalf("claude digest=%s", body)
	}
	if r.Digest[0].Text != "The standard asks for table tests, so I will add one." || strings.Contains(body, "sig") {
		t.Fatalf("claude thinking=%s", body)
	}
}

const codexParallelFixture = `{"type":"thread.started","thread_id":"root"}
{"type":"item.completed","item":{"id":"c0","type":"collab_tool_call","tool":"spawn_agent","sender_thread_id":"root","receiver_thread_ids":["a","b"],"status":"completed"}}
{"type":"item.completed","item":{"id":"c1","type":"collab_tool_call","tool":"wait","sender_thread_id":"root","receiver_thread_ids":["a","b"],"status":"completed"}}
{"type":"turn.completed","usage":{"input_tokens":1}}
`

const claudeParallelFixture = `{"type":"system","subtype":"init","session_id":"s"}
{"type":"assistant","message":{"id":"msg-1","content":[{"type":"tool_use","id":"t1","name":"Task","input":{"description":"fixtures A"}}]}}
{"type":"assistant","message":{"id":"msg-1","content":[{"type":"tool_use","id":"t2","name":"Task","input":{"description":"fixtures B"}}]}}
{"type":"assistant","message":{"id":"msg-2","content":[{"type":"tool_use","id":"t3","name":"Read","input":{"file_path":"a.go"}},{"type":"tool_use","id":"t4","name":"Read","input":{"file_path":"b.go"}}]}}
{"type":"assistant","message":{"id":"msg-3","content":[{"type":"tool_use","id":"t5","name":"Bash","input":{"command":"go test ./..."}}]}}
{"type":"result","subtype":"success","is_error":false,"result":"done"}
`

func TestRunDigestCountsParallelWork(t *testing.T) {
	f := newEvidenceFixture(t)
	codex := f.run(t, f.repo, 1, codexParallelFixture, false)
	claude := f.run(t, f.repo, 2, claudeParallelFixture, false)
	b := evidenceBroker(t, f.outcomeService(t, Limits{}))

	r, body := outcomeCall(t, b, "run_digest_read", map[string]any{"run_id": codex.ID})
	if s := r.DigestSummary; r.Status != "ok" || s == nil || s.Subagents != 2 || s.Withheld != 0 {
		t.Fatalf("codex subagents not counted: %s", body)
	}

	r, body = outcomeCall(t, b, "run_digest_read", map[string]any{"run_id": claude.ID})
	if s := r.DigestSummary; r.Status != "ok" || s == nil || s.Subagents != 2 || s.ParallelBatches != 2 {
		t.Fatalf("claude parallel work not counted: %s", body)
	}
}
