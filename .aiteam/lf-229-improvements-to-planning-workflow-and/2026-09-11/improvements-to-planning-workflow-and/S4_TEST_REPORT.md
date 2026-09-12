# S-4 executed engineering checks

Story: S-4, issue #250, numeric issue ID 5427442485.
Frozen revision: 029f46d4e9d07d64686388b0c59b1cdc7bbcccc8a891483b51171d48dc42d963.
Dependencies: none. Workflow: separated engineering, no acceptance assessment.

Two instruction assertions passed and the selected existing Go test passed; no failures. No tests or production files were added or changed by the test engineer.

## s4-claude-completion-rules

Executed `python3 -` with the following script (exit 0):

```python
from pathlib import Path
import re
for mode in ('plan', 'implement'):
    path = Path(f'.claude/agents/{mode}-orchestrator.md')
    text = path.read_text()
    assert text.count('## Completion message\n') == 1
    section = text.split('## Completion message\n', 1)[1].split('\n## ', 1)[0]
    normal = ' '.join(section.split())
    for term in ("run's last message", 'plain business language', 'no Go identifiers', '`path:line` citations', 'no pasted file contents'):
        assert term in normal, (mode, term)
    assert re.findall(r'^([1-3])\. ', section, re.M) == ['1', '2', '3']
    if mode == 'plan':
        claims = ['1. Leads with links — the parent plan issue and each published story issue, one per line.', '2. Says in plain language what the plan does and how many stories it has.', '3. Names what is unresolved — open questions or assumptions the owner should check, or says there are none — followed by the existing `/implement <instruction>` recommendation.', 'requirement to return the unresolved questions in the final report still applies and wins']
        assert 'Return one final report recommending `/implement <instruction>`.' in text
    else:
        claims = ['1. Leads with the story links and, for each, whether it is done.', '2. Says in plain language what was built.', '3. Names what was not delivered — dropped, deferred or blocked — or says there is nothing.', 'When a run stops early, the escalation report shape above — Where / What triggered it / What is already done / The decision needed / The options — is not replaced by these three rules and wins.']
    for claim in claims:
        assert claim in normal, (mode, claim)
    assert '.codex/' not in section
    print(f'PASS {path}: shared language, three ordered rules, early-stop precedence, no cross-tree pointer')
print('2 instruction checks passed; 0 failed. Only .claude files read.')
```

Actual output:

```text
PASS .claude/agents/plan-orchestrator.md: shared language, three ordered rules, early-stop precedence, no cross-tree pointer
PASS .claude/agents/implement-orchestrator.md: shared language, three ordered rules, early-stop precedence, no cross-tree pointer
2 instruction checks passed; 0 failed. Only .claude files read.
```

## s4-story-index-regression

Command (exit 0):

```sh
go test ./internal/planbrief -run '^TestParseStoryIndex$' -short -race -cover -count=1 -timeout=8m
```

Actual output:

```text
ok  	github.com/cloudvane/litefactory/internal/planbrief	1.532s	coverage: 5.5% of statements
```

This existing contract test covers story index parsing; it does not establish the prose requirements. The owned-Claude assertions above check those directly without requiring the Codex tree. Package coverage is 5.5% for this narrow selection, not full package coverage. No generator, aggregate suite, or make check ran in this specialist. The orchestrator owns the final aggregate gate.

Only file written by this specialist: this report. No implementation, review, forge effects or acceptance performed.
