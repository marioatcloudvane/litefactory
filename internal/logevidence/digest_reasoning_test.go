package logevidence

import (
	"strings"
	"testing"
)

var codexReasoningFixture = `{"type":"thread.started","thread_id":"t"}
{"type":"item.completed","item":{"id":"r0","type":"reasoning","text":"**Planning the retry fix**\n\nThe rule says to keep retries bounded; token fixture-secret-value is unrelated."}}
{"type":"item.completed","item":{"id":"r1","type":"reasoning","text":""}}
{"type":"item.completed","item":{"id":"r2","type":"reasoning","summary":[{"type":"summary_text","text":"Checking the summary-array shape."}]}}
{"type":"item.completed","item":{"id":"r3","type":"reasoning","text":"` + strings.Repeat("LONG-REASONING ", 200) + `"}}
{"type":"item.completed","item":{"id":"i0","type":"agent_message","text":"I will run the tests."}}
{"type":"turn.completed","usage":{"input_tokens":1}}
`

const claudeThinkingFixture = `{"type":"system","subtype":"init","session_id":"s"}
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"","signature":"sig-empty"}]}}
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"The standard asks for table tests, so I will add one.","signature":"sig"},{"type":"text","text":"Adding a test."}]}}
{"type":"result","subtype":"success","is_error":false,"result":"done"}
`

func TestRunDigestKeepsBoundedRedactedReasoning(t *testing.T) {
	f := newEvidenceFixture(t)
	codex := f.run(t, f.repo, 1, codexReasoningFixture, false)
	claude := f.run(t, f.repo, 2, claudeThinkingFixture, false)
	b := evidenceBroker(t, f.outcomeService(t, Limits{}))

	r, body := outcomeCall(t, b, "run_digest_read", map[string]any{"run_id": codex.ID})
	if r.Status != "ok" || digestShape(r.Digest) != "reasoning reasoning reasoning message" {
		t.Fatalf("codex digest=%s", body)
	}
	if !strings.Contains(r.Digest[0].Text, "Planning the retry fix") || strings.Contains(body, "fixture-secret-value") {
		t.Fatalf("codex reasoning lost or unredacted: %s", body)
	}
	if r.Digest[1].Text != "Checking the summary-array shape." {
		t.Fatalf("codex summary array=%+v", r.Digest[1])
	}
	if long := r.Digest[2].Text; len(long) > reasoningBytes+len("…") || !strings.HasSuffix(long, "…") {
		t.Fatalf("reasoning not bounded: %d bytes", len(long))
	}
	if s := r.DigestSummary; s == nil || s.Reasoning != 3 || s.Messages != 1 {
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
