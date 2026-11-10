# Proposed deny-group interface

The named-group design is owner-directed; exact nesting and syntax below are agent assumptions pending the remaining owner decisions.

```yaml
settings:
  denyrules:
    full_checks:
      deny:
        - 'Bash(scripts/lf-check.sh:*)'
        - 'Bash(scripts/ci-test.sh:*)'
    no_delegation:
      deny:
        - 'Agent'
```

```dot
worker [lf_action=lf_agent, lf_deny="full_checks,no_delegation"];
```

The example does not assert that these scripts exist in shipped stacks. The exact cockpit browser command has not been supplied; do not invent it.

## Data contract

| Surface | Proposed meaning |
| --- | --- |
| settings.denyrules | Optional map of group names to objects with a required nonempty deny string list; absent means empty catalogue. |
| Group names | Case-sensitive letters/digits/underscore/hyphen, starting with a letter; no nested group references. |
| lf_deny | Comma-separated group names with surrounding whitespace trimmed; omitted or empty selects no additional groups. Raw Tool(pattern) entries are rejected here. |
| Graph and node combination | Pending q3; recommend additive union. An empty node value then cannot clear defaults. |
| Duplicate selections/entries | Deduplicate without changing their meaning; duplicate YAML keys are invalid. |
| Entry grammar | Explicit Bash(pattern), Read(path), Edit(path), and pending q5 Agent or Agent(name); no bare script shorthand, arbitrary tool names or nested groups. |
| Unknown group | Reject configured graph admission before launch; name group and graph/node reference. |
| Unsupported runtime entry | Refuse before launch; name runtime, group and entry with the unsupported feature. Never silently discard it. |
| Paths | Resolve relative to launch worktree; translate Claude anchors explicitly. Reject path forms whose intended semantics cannot be preserved. |
| Attempt lifecycle | Resolve and freeze selected entries per attempt; new/resumed attempts resolve current configuration, preventing stale rule files from retaining removed/changed selections. |

## Runtime comparison

| Intent | Claude | Codex candidate and limitation |
| --- | --- | --- |
| Deny command prefix | Append Bash rule to per-run permissions.deny. | Translate supported token prefixes to forbidden exec-policy rules in a private active configuration layer. Runtime evidence must establish enforcement inside the actual sandbox/tool paths; checker-only results are insufficient. |
| Deny file reads or edits | Native Read/Edit permission rules with documented path semantics. | Filesystem profile restrictions; a deny may block both reads and writes, so this is not automatically an exact translation. Refuse unsupported semantics; do not promise parity. |
| Deny all native subagents | Native Agent deny. | features.multi_agent=false is an existing adapter control; establish it covers the relevant native tools. |
| Deny a named native subagent | Native Agent(name) deny. | No established selective equivalent; refuse rather than disable all agents silently. |
| Deny agent CLI launched in shell | Ordinary Bash rule for that invocation. | Ordinary command-prefix rule for that invocation; indirect execution remains a limit. |
| Change factory graph routing | Not a CLI permission rule. | Not a CLI permission rule; would require different scope if selected in q5. |

Shared credential/state restrictions remain additive and unchanged. Rule delivery cannot import unrelated ambient allow rules. Descendants must receive the same effective restrictions; launching a fresh independent CLI through a shell is not established native-subagent inheritance.

Sources: internal/harness/settings.go; internal/harness/sandbox.go; internal/codex/runner.go; pkg/execution/providers/codex/runner.go; https://code.claude.com/docs/en/permissions; https://developers.openai.com/codex/rules. This table specifies candidate behavior and refusal boundaries, not measured end-to-end success.
