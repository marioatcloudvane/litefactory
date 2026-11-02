# Characterization before selection replacement

Starting commit: `5aed96075867c2c9dda6d88f1aacab2be107326d`.

Before production selection changes, the following focused command passed:

```
go test -timeout=10m ./internal/daemon ./internal/config ./internal/turn -run 'TestResolveAgent_|TestNoAgentLabelRequest|TestAgentRefusal|TestMalformedAgentLabel|TestStoryIssueInherits|TestStoryInherits|TestAutomaticAdmission|TestBugRuntimePermissions|TestJudgeSettingsAndFallback|TestJudgeOtherUnconfigured|TestJudgeAvailabilityFallback|TestJudgeNonAvailability|TestJudgeChallengesBug|TestGraphBugConversationPublishes|TestBugSyncGateCannot|TestGraphBugDeliveryConcrete'
```

Output:

```
ok github.com/cloudvane/litefactory/internal/daemon 2.757s
ok github.com/cloudvane/litefactory/internal/config 0.656s
ok github.com/cloudvane/litefactory/internal/turn 12.958s
```

The existing tests cover strict label refusal, default selection, unchanged Story inheritance, provider-change recovery cancellation, Bug analyst/fixer grants, judge override and availability fallback, analysis approval and delivery gates.

Added baseline behavioral tables for Bug bootstrap selection and the settled judge precedence, then ran:

```
go test -timeout=5m ./internal/daemon ./internal/config -run 'TestBugAgentDispatchCharacterization|TestBugAgentJudgePrecedenceCharacterization'
```

Output:

```
ok github.com/cloudvane/litefactory/internal/daemon 0.919s
ok github.com/cloudvane/litefactory/internal/config 0.962s
```

The new Bug table demonstrates unlabelled first-agent default, explicit Claude/Codex selection, unknown/ambiguous/malformed refusal. The judge table demonstrates recorded Codex without override, explicit Codex second opinion despite shared Claude, shared preference over recorded provider, and refusal to fallback from these named selections. No external provider credentials were used.
