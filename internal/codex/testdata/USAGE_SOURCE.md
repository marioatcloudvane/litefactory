# Usage decoder source bounds

The usage tests are synthetic, not authentic captures or supported-version proof.

Inspected upstream source during implementation:
- https://github.com/openai/codex/blob/main/codex-rs/exec/src/exec_events.rs
- https://github.com/openai/codex/blob/main/codex-rs/exec/src/event_processor_with_jsonl_output.rs

The inspected exec event schema defines collab_tool_call with sender_thread_id,
receiver_thread_ids and tool=spawn_agent. These fields establish direct observed
ancestry only; send_input/wait/close_agent do not establish new parentage.

The inspected processor's usage_from_last_total copies total counters from
ThreadTokenUsage, although the public event name is turn.completed. Consequently
this adapter does not charge that value as a fresh increment on resume. It emits
non-additive inclusive reconciliation evidence until a verified source establishes
scope and baseline. Missing actual-model evidence remains unknown. No model is
inferred from configuration or generated text. cache/reasoning subsets are
normalized without double counting; absent dimensions stay unknown.

The source inspection does not establish installed-version compatibility, full
nested event forwarding, child usage, failure consumption or actual model linkage.
Those remain manual capture obligations. Associated session files are not scanned
without a validated association/source contract. Root and known child identities
remain visible as incomplete coverage, so missing measurements cannot look free.
