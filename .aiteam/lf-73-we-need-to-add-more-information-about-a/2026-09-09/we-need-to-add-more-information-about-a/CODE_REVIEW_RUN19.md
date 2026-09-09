# Run 19 production review

The owner expressly requested completion of remaining implementation without tests. This review does not claim test execution or authentic provider certification.

## US-1 existing shared implementation

Identity: US-1, issue5396812119, revision5f9601901d81707a291188df53dbab44dc139f6d81e4d92c51e7659331a0e794; no dependencies.

Statically reviewed internal/agent/usage.go and internal/meter/tokens.go, reconcile.go, compatibility.go. The shared boundary expresses attempt/source/event/invocation/parent identities, source model provenance, nullable token dimensions and collection coverage. Clone methods copy mutable counters and diagnostics. Normalization and checked arithmetic separate primary dimensions from subsets, reject invalid/regressing/overflowing values, and preserve unknown values. Encoded tuple identities bind replay and cumulative checkpoints to attempt/source/invocation/model/epoch/scope. Capability diagnostics distinguish unsupported source versions, missing authentic evidence, incomplete dimensions and fully evidenced capability.

No shared production change is currently required for the remaining adapters. Each adapter can compare source-proven prelaunch baselines and emit incremental exclusive observations through the existing sink. Authentic fixture evidence and test criteria are intentionally not certified by this static review.

## Provider implementation review

Reviewed the first stable provider snapshots and routed findings to their package owners before final approval:

- Codex: avoid inherited lifetime-counter charging by using per-response records; seed historical response identities to reject appended replay; reject duplicate session sources; use nonblocking descriptor-validated reads; page directory discovery; check deadlines during ancestry resolution; retry delayed finalization within a bounded drain.
- Claude: retain descriptor identities across offset reads; use nonblocking directory opens; drain associated records after cancellation within a separate bounded finalization context; retry delayed linked children; preserve message identity deduplication across stream and transcript sources.

Positive findings: both adapters preserve inclusive summaries as non-additive evidence and use observed model provenance rather than configured defaults. Source reads remain within the child environment, and provider execution outcome classification remains in the existing runner. Missing certification keeps collection qualified.

## US-3 scoped review result

Static production review passed after the reported fixes. Discovery uses bounded directory pages and nonblocking descriptor-validated reads. Historical response identities seed replay suppression; forked parent records cannot enter child totals. Only exclusive per-response usage is additive, with model attribution tied to the matching runtime turn. Finalization polls under a bounded drain after cancellation. Existing runner outcomes remain independent of metering.

Reviewed stable SHA256 content:

```text
7687f4d731968ef0b9dd0d7a40eb45d4470ce0fda1282ea1e026c0378038b034  internal/codex/runner.go
2f0c52d71dfd47fd4d23678ccb41661b667c9b82210bebb8c2e43c19f8d54872  internal/codex/usage.go
8604fdff768edbdcc52bac32d0b776100b375265531d0222a5867e9688a844d4  internal/codex/usage_rollout.go
```

## US-2 scoped review result

Static production review passed after the reported fixes. Child finalization requires both completion evidence and measured usage, allowing a delayed first usage record or final child work to arrive within the bounded drain. Cancellation does not prevent this bounded associated-file drain. Directory and file opens validate descriptor types without blocking on substituted FIFOs; file identity and offsets protect resumed reads. Message counter identities deduplicate stream/transcript overlap and prelaunch history. Parent links originate in observed Agent/Task calls and structured source associations. Model quantities remain exclusive while provider summaries remain non-additive.

Reviewed stable SHA256 content:

```text
2a90047a936c3a1333beff6d1df881f8d99678545c2f3f7e24cf1d5b58f8e4b3  internal/harness/runner.go
4aa413beb8aae087e42f094496b4591c6075303aa0d855a177bba4fb8a7b996a  internal/harness/usage.go
35a298580d1e208535a630681a0b2301cf9066a1eef26822f354322516b02d62  internal/harness/usage_transcripts.go
```

## Aggregate review

The providers share the existing usage contract and store orchestration; US-4, US-5 and US-6 production code is unchanged. Reviewed docs/meter-provider-sources.md: inspected source provenance is explicitly separated from authentic execution qualification. No test or provider certification result is claimed. Tests were intentionally deferred by the owner. Static gates are recorded in the run implementation report.
