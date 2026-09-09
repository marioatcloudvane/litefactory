# Owner exceptions

Separated-workflow features accept an explicit control in a comment on the
parent issue. The factory authenticates the configured owner by numeric user ID.
Legacy features keep their existing behavior. Checkbox edits, labels, manually
closing an issue, third-party comments, and ordinary prose confer no authority.

Use:

```text
/waive <generation> <selector[,selector...]> -- <reason>
```

The generation must be the current frozen managed generation. The reason is
required. Commands are at most 8192 bytes, reasons at most 4096 bytes, with at most
128 unique selectors of at most 256 bytes each. The command must begin the comment
and use this exact grammar. It never launches an agent or changes modes.

| Selector | Exact scope expanded once |
| --- | --- |
| `criterion:US-001:AC-001-1` | The named frozen criterion in that story |
| `review:US-001:code` | The named review obligation (`code`, `architecture`, or `plan`) |
| `story:US-001` | All outstanding criteria and review obligations for that story |
| `acceptance` | All outstanding frozen story criteria and reviews |
| `tests:US-001` | That story's executed-check readiness obligation |
| `child:5401207602` | The currently attached child's open-state readiness obligation, selected by numeric GitHub issue ID |
| `readiness` | Every current attached open child, missing managed acceptance, story check evidence and incomplete aggregate engineering/save readiness obligation |
| `all` | Both acceptance and readiness scope |

For example:

```text
/waive 1 criterion:US-001:AC-001-1,review:US-001:architecture -- The legacy deployment does not need this behavior.
/waive 1 story:US-001 -- Accept the documented limitations for this release.
/waive 1 all -- Accept every remaining obligation described in this generation.
```

Criteria without an AC identifier can be selected using their URL-encoded exact
parser key (see the factory's criterion report); a whole-story override also
covers every criterion regardless of naming. A readiness exception alone does
not change criterion checkboxes or claim a successful test. Creating a follow-up
never implicitly waives anything; approval of its concrete content is separate.

The factory durably records comment identity, numeric actor, reason, exact
expanded keys, frozen revision/snapshot and original findings before projecting
checkboxes. Previously retained assessments are immutable. Missing assessments
produce explicitly unverified findings; no validation run or successful check is
invented. Owner-waived criteria are checked with visible reasoned exception notes,
while their original failed/unmet/unverified facts remain in the record and report.
A fully waived story follows the same ordered report, body projection and close
path as factual acceptance. Projection conflicts and uncertain forge responses
remain pending for recovery.

Duplicate delivery or restart uses the same expansion. Newly attached children
are not covered by an earlier readiness exception. Historical acceptance remains
bound to its original snapshot and is not automatically reopened by later changes.

No exception grants credentials, changes numeric identity checks, fakes a save or
integration, or authorizes a merge. Delivery still requires explicit approval of
the concrete PR/current head/main target and confirmed integration into main
before parent closure.
