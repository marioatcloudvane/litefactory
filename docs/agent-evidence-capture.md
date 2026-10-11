# Capturing agent evidence for test fixtures

How to collect a real sample of a coding agent's output, clean it, and keep it in the repository as a test fixture, with a record of where it came from. Back to the [documentation index](README.md).

Use this when a behavior (for example a quota message or a startup error from Claude or Codex) must be tested against real output. It is a task for a person, done outside litefactory's managed runs. Tests and CI never start a live coding agent to get a fixture. Nothing here adds automatic collection, a new permission or a change to the agent's sandbox, sign-in or environment. For a concrete example of a startup error, see [Codex initialization troubleshooting](codex-initialization-troubleshooting.md).

## Collect and transfer

1. **List what you need to prove.** For each behavior write down the proof you need, the source that could give it, the agent version and context it applies to, and what is missing. If no source exists, say so and leave the item unproven.
2. **Capture outside litefactory.** A person with the right access captures in a scratch folder outside the repository, using a short, read-only check and the normal security setup. Write down the actual command settings, the environment and the agent version if you know it. Do not copy credentials, widen permissions, force quota exhaustion, or read or edit the daemon's runtime files to get a shortcut. Unknown facts stay marked unknown.
3. **Keep the raw material private.** Prepare a cleaned candidate from it. Keep the order of the records and keep standard output, standard error, provider events and text the model generated apart. Write down what you selected, what you left out, exit codes and any clock assumptions. Text the model wrote about an action is never proof that the action happened.
4. **Remove sensitive data.** Take out tokens, authorization headers, cookies, private prompts, generated work, personal data, private paths and sensitive IDs. Where a relationship matters (two IDs equal, one ID the parent of another, order, which model produced which event), replace each value with a consistent, distinct stand-in so the relationship survives. Keep the mapping outside the repository. If cleaning destroys a relationship you need, mark the evidence as not sufficient. Do not invent events to fill the gap.
5. **Review.** Read every selected line as JSON and look at both the fields and the free text for sensitive data. Compare the kept relationships with the raw source (through the person who has it). Record who reviewed it and what could not be checked. Compute the SHA-256 of the final cleaned file and record its byte and record counts. A checksum shows later changes; it does not prove the capture is real.
6. **Add it to the repository.** Put only the reviewed, cleaned file and its provenance file in the owning package's `testdata/` folder. Replay it in tests with a fake executable or parser, and record the exact assertions, the commands you ran and their limits in the provenance. A reviewer checks the checksum, the source, the cleaning and the assertions. Raw captures, credentials and the stand-in mapping must stay out of the repository.

Cleaning a fixture does not permit editing `$LF_HOME/logs` or other runtime files. Outgoing issue comments still go through the normal redaction. Redaction keeps secrets out of comments; it is not permission to show secrets to an agent.

## Provenance template

Copy this next to a fixture. A line of placeholder text is a field to fill in, not evidence. Write **unknown**, **unavailable** or **not applicable** with a reason where that is true, and keep assumptions apart from observed facts.

```markdown
# Provenance: <fixture filename>

- Evidence kind: authentic selected capture / synthetic example (choose one).
- Source and collector: where the original stream was, how much was selected,
  and who or what collected it. Say so if the source or author is unknown.
- Capture date and time: observed times, time zone or offset, clock limits.
  Mark assumptions separately.
- Environment: operating system, shell, kind of working folder, top-level or
  nested start, and sandbox and environment facts, without private paths or
  secret values. Separate what was observed from what was inferred.
- Agent: program name and checked version. For old captures write unknown if it
  is not established. A version inspected elsewhere is not this version.
- Invocation: relevant flags, input shape, transport and settings, with values
  cleaned and omissions listed. No secrets.
- Selection: which channels (stdout, stderr, events), which records, in what
  order, what was left out, exit observations, and whether it is complete for
  each claim.
- Cleaning: which fields were removed or replaced, and how the stand-ins keep
  references working without publishing the mapping.
- Asserted relationships: parent and child IDs, model links, order or other
  exact relationships you rely on, with event and field references.
- Integrity: cleaned filename, SHA-256, byte and record counts, JSONL check
  result, and what was compared with the raw source and by whom.
- Review: reviewer, date, sensitive-data check, relationship check, open gaps.
- Offline checks: the exact test commands and results that use this fixture.
  Do not mark a check as passed if it was not run.
- Claim limits: what the fixture supports, for which version and context, and
  what it does not.
- Re-verification: what new capture and review is needed when the version or
  context changes.
```

## What a fixture proves

An **authentic** capture shows selected past events in the context it records. A **synthetic** example is built to test parsing, classification or relationship handling. Label it synthetic even when it reproduces a real error message. Never invent a stream or its provenance and call it authentic.

A claim must stay within what was kept. A fixture alone does not prove that something works today, that a sandbox is compatible, how a conversation behaves over time, or end-to-end behavior. Those need their own evidence. Missing evidence stays missing; replaying a fixture does not close the gap.

## Existing example: Codex quota message

[`internal/codex/testdata/quota-authentic.jsonl`](../internal/codex/testdata/quota-authentic.jsonl) and [`QUOTA_PROVENANCE.md`](../internal/codex/testdata/QUOTA_PROVENANCE.md) hold two selected diagnostic lines and their provenance. They come from September 5, 2026 (stream `100/0006.jsonl`, lines 85 to 86). The Codex version is **unknown**. The local time zone (Europe/Vienna) is an **assumption**, and the reset time derived from it depends on that assumption. The fixture supports only classifying that quota message. It has no conversation handle, prompt, generated work or credential. Other quota cases in the tests are synthetic. After a Codex upgrade, record the new version separately and collect a refusal that already happened; do not cause one.

For usage records the tests are synthetic, and [`USAGE_SOURCE.md`](../internal/codex/testdata/USAGE_SOURCE.md) records which upstream source was inspected. That is not an authentic capture.
