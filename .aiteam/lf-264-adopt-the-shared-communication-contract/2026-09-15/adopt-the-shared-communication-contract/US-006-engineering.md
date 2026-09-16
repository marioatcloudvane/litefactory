# US-006 — Update installed Claude instructions

## Status: DONE (scoped engineering; no acceptance decision)

Frozen #303, issue_id 5463754196, parent_id 5439211198, revision
`cff6397e2b39e141b55fc15a26ad62ef3b7dce35c87d980ca600fd51d823c1ce`,
workflow 2, dependency US-005 engineering-recorded as supplied by orchestrator.
Executed under Codex. T-001 integration and T-002 scoped assertions completed.

Manually edited exactly the fourteen installed Claude targets below. Preserved all
original frontmatter/tool/model/color fields, specialist wrappers and role content.
Common application/finalization semantics live in the existing shared workflow file;
all applicable role entrypoints explicitly consume them. Research retains the neutral
research-evidence contract and explicit self-contained finalization. Plan keeps typed
publication ownership and separate checkpoint/final output. Implement requires retained
aggregate/checkpoint evidence against final bytes. Validate now returns complete
reference-only terminal findings, reconciles optional live contributions and retains
Read/Glob/Grep only, without shell, edits, capture, new checks or delegation.

No .codex changes, canonical-source substitution, installed-tree generator, new skill,
test file or immutable release change. No provider invocation, forge mutation, daemon
state/dist write, contribution submission, review, commit or push. Session report is
engineering evidence, outside the owned production instruction targets.

## Executed checks

Base HEAD `16634ed33075ea3acfc449bcb6316163bdc0b807` plus the current uncommitted US-001 through US-006 state;
HEAD alone does not identify tested bytes. SHA256 inventory below identifies current
owned/shared inputs. Environment: macOS-26.5.1-arm64-arm-64bit; Python 3 stdlib and repository Go toolchain.

- `python3 /tmp/us006-assert.py` — PASS, exit 0. Eight checks cover exact manifest
  ownership, frontmatter/tools and literal header preservation, effective common
  semantics, Validate restrictions/reconciliation, mode-specific evidence/publication,
  canonical semantic correspondence, unchanged sibling handoff, SHA256 inventory.
- `go test -race -timeout=8m ./internal/teamsource -run 'TestSharedWorkflowRendering/claude|TestAdoptedInstructionManifestExactRoleTargets' -count=1`
  — PASS: `ok github.com/cloudvane/litefactory/internal/teamsource 2.667s`, exit 0.
  Existing tests render Claude in memory from copied sources in a temporary root,
  exercise all supported native-render modes and verify no installed output. The
  manifest test checks the exact declared target mapping; it does not require sibling
  content parity. These tests read canonical sources, not installed execution behavior.
- `git diff --check -- .claude` — PASS, exit 0, no output.

The first temporary assertion run failed because Research lacked explicit legacy
compatibility wording. Added the declared-route/owner-upgrade condition and reran all
assertions successfully. An edit patch had a context mismatch; its remaining edits
were applied explicitly. Neither is represented as a passing first attempt.
No full-package/repository suite or make check: the aggregate gate belongs to the
orchestrator. Tests are offline source assertions, not authentic Claude invocation,
same-run correction, observed authoritative receipts or present-day host permissions.
The four authentic captures remain unsatisfied on #274/#296.

### Actual assertion output

```text
PASS owned-only edits: 14 Claude targets; no Codex/shared/release writes or created files
PASS exact manifest targets, original frontmatter/tools/model/color and literal mode-header preservation
PASS all 14 effective contracts: prior state, revisions, retired IDs, correction/replay, receipt dimensions, compatibility and external evidence bounds
PASS Validate terminal/live reconciliation, complete reference-only findings and unchanged Read/Glob/Grep boundary
PASS Plan publication ownership, Implement aggregate evidence, neutral Research and common application/final-enforcement semantics
PASS canonical source assertions establish matching semantic obligations, not byte-generation parity
PASS US-007 exact 12-path handoff unchanged; known sibling drift retained for Claude; US-010 parity pending
PASS SHA256 inventory retained for owned, canonical and unchanged handoff bytes
```

## Exact paths and byte provenance

`owned` enumerates the exact fourteen changed targets. `canonical` records the shared
serialization sources/examples read or inherited through the manifest. `codex_handoff`
is the exact unchanged twelve-path US-007 handoff, requiring the Claude harness.

```json
{
  "owned": {
    ".claude/agents/requirements-engineer.md": "69b9771553fb446b354e8c7bcb7cc3231bc7f339cdbc792c12b37c45879db957",
    ".claude/agents/proxy-product-owner.md": "ea60a7ad91e2192e5b7aa3af546a25c9ab68ebea7bff53e008b6ff49ca9bb4ee",
    ".claude/agents/research-orchestrator.md": "1e1c62d9cac15b1c86baaca3f04f664b93e0acc95125a8b6e3cdba01600886e6",
    ".claude/agents/plan-orchestrator.md": "d907bad531a9511f34729205da5bf7cc5ff7677d156b09e9f971ec7fa779d9f9",
    ".claude/agents/implement-orchestrator.md": "143ee4c4afce535ab95bdff240674241f9a098ce35022e026808a4741c34bac6",
    ".claude/agents/go-architect.md": "b96c5053139ec74ca5d8ce574f5f571e284595671ac6246ce2507cb01029ad50",
    ".claude/agents/go-implementation-engineer.md": "17f4917930f7df3f452aba111f03c358103ce25320a98d6669ab53620f9e294a",
    ".claude/agents/go-unit-test-engineer.md": "f8dbf612042735bc91c11cfaf16182b94e53ae886d15a06b90d603d0fdc42926",
    ".claude/agents/go-integration-test-engineer.md": "ff0c4137fed453238f2eadb881ff05ee6f708deee82de839c35ef2b4e43e9246",
    ".claude/agents/go-code-reviewer.md": "f7efe8aa88900b16d9959c01b4d8e64d7f30bb5269d45bf01c5c205c8c9f730e",
    ".claude/skills/shared/workflow.md": "66508166b2fc43e697050add8c62bc3ee5d66b163da56f956b505c964d420846",
    ".claude/skills/shared/review-evidence.md": "363dcda15668b2f2904dcf27798a988c1e77d5b3dbab31372d4404f45e5e07ca",
    ".claude/modes/session.md": "915ec5f7d4e659502aedd956f7574ff293aa647265108065a48f0df9b218a44f",
    ".claude/modes/validate.md": "f8e9afa4dd0484c768db6980f462e96aa69fdc89706b4ba41ad8480f952e4231"
  },
  "canonical": {
    "teams/source/skills/agent-finalization/SKILL.md": "8a15b927009599f879baf583a7743b700b7619d022cbb9e475d5f3c16a13282c",
    "teams/source/skills/agent-finalization/references/common.md": "8b30e6c80612509511afd21936608fb43650c7e21a9465e47b8769163714ac11",
    "teams/source/skills/agent-finalization/references/examples/implement-blocked.json": "82551f8fd02ab9cc613481d7b3a003f10713038eeaac8eb789fb4681c5731bb0",
    "teams/source/skills/agent-finalization/references/examples/implement-completed.json": "e293bfb7d34c31410895fb38581db7215661c41e3867d53e77121e77146b14e9",
    "teams/source/skills/agent-finalization/references/examples/implement-incomplete.json": "3b5c011763242332d66407e6937f8954bea63d0c83c09cc3dcae5fa841286232",
    "teams/source/skills/agent-finalization/references/examples/implement-update.json": "65cc8e08beb14d15e55b97306ab147f8e58e93548540083df3fbabd0b85628d1",
    "teams/source/skills/agent-finalization/references/examples/invalid-spike-question.json": "d9c684a6eceb044e4e0a34ef658719689082070fe5a9ad7001aeee8ed90a93fc",
    "teams/source/skills/agent-finalization/references/examples/plan-blocked.json": "ee7650d2c377904a13daee5871e3b3293cc925f8cbc0ccb22cf8a05e1e20580c",
    "teams/source/skills/agent-finalization/references/examples/plan-completed.json": "8bc822b47d5c752cc46a1e6568131ce214e5ff81f6dd9d5f974ad73487af8c27",
    "teams/source/skills/agent-finalization/references/examples/plan-incomplete.json": "e77ff539d2db840b29ef0dafa2836abc49e828cc00cf1f6b966bfc9c02d47d3f",
    "teams/source/skills/agent-finalization/references/examples/plan-update.json": "0e022b5ed5f10364420d4d153a54abd128a4ab6e8199e946c8af2c904a857dff",
    "teams/source/skills/agent-finalization/references/examples/research-blocked.json": "8f43db974d91d82636ebea892aef154c67998e8b1b0a59b655a190bc7caa1c64",
    "teams/source/skills/agent-finalization/references/examples/research-completed.json": "50f06dd3a34d2dd946776b9eee295cc67833bc384ed060f07cf71950b7633488",
    "teams/source/skills/agent-finalization/references/examples/research-incomplete.json": "51fb5a01d1898424c75bf584e7d78547368e0ed32dd96b27c67cc2ba2da7ed78",
    "teams/source/skills/agent-finalization/references/examples/research-question.json": "8005889e9135e40829d2e412885f837e4f77ad777bcc4f146eb91b445b4a350b",
    "teams/source/skills/agent-finalization/references/examples/research-update.json": "bf83149bf2c1e78701e24bbef55be9ce874cb655ff5973463b8ea7352c5bac97",
    "teams/source/skills/agent-finalization/references/examples/spike-blocked.json": "dde26a5543c8bec8e0f89c694b9f71db18a67b4c56a833c909e766a8ccf8033c",
    "teams/source/skills/agent-finalization/references/examples/spike-completed.json": "83e16a439d787ecde61dc123cd09e4ed18bef756e7b6dc90b1b75c6163a3f12e",
    "teams/source/skills/agent-finalization/references/examples/spike-incomplete.json": "6721a18cfd12ccd37690d13429e67e7cb7eac63260fa2274e081adcfc3edee7d",
    "teams/source/skills/agent-finalization/references/examples/spike-update.json": "386def2e8bab7b4ec9812a865674bc5d7c60e986c344ade852788ed04270e6ce",
    "teams/source/skills/agent-finalization/references/examples/validate-blocked.json": "23a4677d320b7c9fe57a4b594aa0793edc70af820a38b4148687db276bbe1794",
    "teams/source/skills/agent-finalization/references/examples/validate-completed.json": "b9d245d7579ba2cab8a2ea07b049ba32a50b952c46534124dcd4cdad5a57e4d9",
    "teams/source/skills/agent-finalization/references/examples/validate-incomplete.json": "0517b59b42c89487833d33e5c6f643483b76b996604c7dccaa4b1980a5a10ed5",
    "teams/source/skills/agent-finalization/references/examples/validate-update.json": "903c859cc82000caa4005993d62b68c84f22882627b38cbd3ebde9b8d90d33e8",
    "teams/source/skills/agent-finalization/references/implement.md": "f027d0771113c97c8c9e1f9959093e42364e6926f8cf8ff97ed8d117daccda14",
    "teams/source/skills/agent-finalization/references/plan.md": "bb53b025acfb3f50738ade2d7e74be463ab08c3690bec799c59f6e81f60ee190",
    "teams/source/skills/agent-finalization/references/research.md": "ff0f7f972f3d66c2a0483ec1a21760c6f7e40675ae5dadbd57576482232da116",
    "teams/source/skills/agent-finalization/references/spike.md": "5a75d104ed1f8c17a3888b0a67436ee6cd48cadc540ac032880f8939a8d0a49c",
    "teams/source/skills/agent-finalization/references/validate.md": "82e6697475a9b5aaea39427c5d63ccd6c58385c883617af0f520420bcd6b07f8",
    "teams/source/skills/planning-contracts/SKILL.md": "ff96968a6039b0b40b1885fbd8d0e208915ea854adfa8f5f071000ce89356ac4",
    "teams/source/skills/research-evidence/SKILL.md": "d896d5b18cde936f4bef629f36c82bb3165714c32f52a8246d15ff1f95602a81",
    "teams/source/skills/review-evidence/SKILL.md": "aa9488d86d5936ba33593f64fe96ccb37e187b5fceb9e12b44d5b454caa1c0f7",
    "teams/source/skills/workflow/SKILL.md": "a47e2fd284b697086bd55f937d50a51ede7af3d08cb3e31c14748a907047284d"
  },
  "codex_handoff": {
    ".codex/skills/requirements-engineer/SKILL.md": "d68515b776ee7f263f68433832ef1d4feb8f8ece1ae95a8b9dfa31ccfe9ab74b",
    ".codex/skills/proxy-product-owner/SKILL.md": "5eeb4573fd210c30fcd8efedaac23d88bce8d6f0c189d9b7d9c13bad51c322d9",
    ".codex/skills/research-orchestrator/SKILL.md": "78c0d1a287aa177fe62603aa9f03ca7a1b6b3ecadcaf72affb8e25917638fcee",
    ".codex/skills/plan-orchestrator/SKILL.md": "109affc299951dac7ea48c532ba1d7caead7ca704fde3c4c62585fa8069921e4",
    ".codex/skills/implement-orchestrator/SKILL.md": "125cc6e4e90817f06dfa86ba67d185d118e2175278031caf8470fb3e763d735c",
    ".codex/skills/go-architect/SKILL.md": "88f41c6d9088195a24fba7463c500591548a99686cec0fdb1f843f5ab0c93298",
    ".codex/skills/go-implementation-engineer/SKILL.md": "5677c748499eb9e83a7aa91ce49c7ea7a953937879953e1b09ed57f64c356caf",
    ".codex/skills/go-unit-test-engineer/SKILL.md": "c7485fe92799009966d2c5991b093901157a5d867f5fb0814902f2ad9ab7fe76",
    ".codex/skills/go-integration-test-engineer/SKILL.md": "192150a6d9d29e880ea418f5f44da86016feac976e6d378ec15a9bc80446a6e9",
    ".codex/skills/go-code-reviewer/SKILL.md": "db2f8078d5d4c7801a524dcc68d5445e6cbf296c8972447042dee956e37a833f",
    ".codex/skills/workflow/SKILL.md": "4caa414ff14ae4daf41688243903a0336c00ea248dfed8eb40c77e80e17cdb63",
    ".codex/skills/review-evidence/SKILL.md": "69ef0e031f61ee09c24ace27c3a16cdf6e9dbaf5badae357bb91caff1da742e6"
  }
}
```

## Sibling drift and remaining handoff

US-007 must run under Claude and manually integrate the canonical contract into only
the twelve `codex_handoff` targets above, preserving Codex wrappers and role policies.
The Codex workflow still lacks Shared final routes and continuation; its installed
Validate route is also unchanged. Do not use a generator, write the sibling tree or
replace protected edits with shared-source changes. Temporary drift does not block
US-006; US-010 owns aggregate parity after both independent stories complete.
This report grants neither US-007 completion nor adoption/retirement completion.

## Retained read-only assertion program

The exact command was `python3 /tmp/us006-assert.py`. The script and baseline were
created in /tmp; the baseline was captured immediately before US-006 edits using
Path.rglob/read_bytes over .claude, .codex, teams/source and teams/releases, recording
SHA256 for every file and original text for Claude Markdown only. It was never a
source generator and wrote only /tmp/us006-digests.json. For a repeat comparison,
use the corresponding before-edit snapshot; this post-edit worktree cannot substitute
for that baseline. Original owned-file hashes are retained below.

```json
{
  ".claude/agents/requirements-engineer.md": "84d2f2af8d87d1d03510255b1a4bf5ef51578ab6d8fa1a226430a990df2abc74",
  ".claude/agents/proxy-product-owner.md": "ba47df5eea9daba588887a11815f93cc79cb7e1be2ebdf4ec46f039f21a5647a",
  ".claude/agents/research-orchestrator.md": "7368dc1401ceb41b02fdc86aff201a91ee90af28bec47edf82bb655f1dbe5479",
  ".claude/agents/plan-orchestrator.md": "04b3eb76644fdf19a9824361cad67febaa7ed2bef705feedc0636199b2371d26",
  ".claude/agents/implement-orchestrator.md": "854f9323ef535700e7f0be3c0e84c161aa91d67fca0aab1ab2b6b442de7f2389",
  ".claude/agents/go-architect.md": "bd3cbcc4128d8b47ff3e4336eae6375b5860e3890058633f11a9ac64878e8c79",
  ".claude/agents/go-implementation-engineer.md": "f19c8238fac28c42d317b9cdc311b8f56f4c2b9a6f84d6db3c3bcff1f54785c9",
  ".claude/agents/go-unit-test-engineer.md": "9c8fe04390e93bd798857f43a6379bcfda8bacd5c78606f98ea0f69bcc2a0f5d",
  ".claude/agents/go-integration-test-engineer.md": "d142fd633001a0cdd9513db1788d50b99a372383e7dde1e72af654005fb2daea",
  ".claude/agents/go-code-reviewer.md": "7a255dcc99412a60baae115b4f45cc61b5828688037bdc9bc80e6c87e821c7ca",
  ".claude/skills/shared/workflow.md": "6527d76d42c3de1c69a0d035c52fad0529fcf40aa07a1d365a27d1e8df52f856",
  ".claude/skills/shared/review-evidence.md": "d3c09a752a17dbeabbfe69a5c1ac3ee4e89310b96a3623338330396703cd4d50",
  ".claude/modes/session.md": "1b14c0eddf3e6b5c8e51deddb8ddb7ef27e241c5cd7044d186c71f30f099b0ec",
  ".claude/modes/validate.md": "786584a31909fc34d1932e310bd10ce28575a01c3a4d784dbdb9b66ff0497430"
}
```

```python
from pathlib import Path
import hashlib,json,re
before=json.loads(Path('/tmp/us006-before.json').read_text())
manifest=Path('docs/story-contract-instruction-manifest.md').read_text()
roles=['requirements-engineer','proxy-product-owner','research-orchestrator','plan-orchestrator','implement-orchestrator','go-architect','go-implementation-engineer','go-unit-test-engineer','go-integration-test-engineer','go-code-reviewer']
owned=['.claude/agents/'+r+'.md' for r in roles]+['.claude/skills/shared/workflow.md','.claude/skills/shared/review-evidence.md','.claude/modes/session.md','.claude/modes/validate.md']
sha=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest()
changed=[p for p,v in before.items() if sha(p)!=v['sha256']]
assert set(changed)==set(owned),changed
for root in ('.claude','.codex','teams/source','teams/releases'):
 assert {str(p) for p in Path(root).rglob('*') if p.is_file()}=={p for p in before if p.startswith(root+'/')}
print('PASS owned-only edits: 14 Claude targets; no Codex/shared/release writes or created files')
workflow=Path('.claude/skills/shared/workflow.md').read_text()
for p in owned:
 s=Path(p).read_text();old=before[p]['text']
 assert p in manifest,p
 if old.startswith('---\n'):assert s.split('---',2)[1]==old.split('---',2)[1],p
 for line in old.splitlines():
  if '- Mode: X' in line:assert line in s,p
 assert '- Mode: X' in s,p
 assert s.endswith('\n') and not any(line.rstrip()!=line for line in s.splitlines()),p
print('PASS exact manifest targets, original frontmatter/tools/model/color and literal mode-header preservation')
for p in owned:
 s=Path(p).read_text()
 effective=s if p.endswith('research-orchestrator.md') else s+'\n'+workflow
 for term in ('baseline','revision','retired IDs','preflight','request identity','engineering','acceptance','remote','compatibility','retirement','#274/#296'):
  assert term.lower() in effective.lower(),(p,term)
print('PASS all 14 effective contracts: prior state, revisions, retired IDs, correction/replay, receipt dimensions, compatibility and external evidence bounds')
for p in ('.claude/agents/go-code-reviewer.md','.claude/skills/shared/review-evidence.md','.claude/modes/validate.md'):
 s=Path(p).read_text()
 for term in ('reference-only','terminal','list-B','identical','conflict','delegation','capture','unverified'):
  assert term in s.lower() if term!='list-B' else term in s,(p,term)
 assert 'orchestrator/helper handoff' not in s and 'through\nthe supplied attempt helper' not in s,p
assert 'tools: Read, Glob, Grep\n' in Path('.claude/agents/go-code-reviewer.md').read_text()
print('PASS Validate terminal/live reconciliation, complete reference-only findings and unchanged Read/Glob/Grep boundary')
for r in ('requirements-engineer','proxy-product-owner','plan-orchestrator','go-architect'):
 s=Path('.claude/agents/'+r+'.md').read_text();assert 'payload.plan' in s and 'checkpoint alone' in s and 'success-only publication' in s
s=Path('.claude/agents/implement-orchestrator.md').read_text()
for term in ('payload.implement.aggregate_checks','current worktree bytes','Aggregate-only recovery','Questions are disallowed'):assert term in s
s=Path('.claude/agents/research-orchestrator.md').read_text();assert 'without reading the full story workflow' in s and 'payload.research' in s
for term in ('Application schema integer 1','expected_revision','full final merge','256 operations','post-exit','Missing or malformed','advisory','pending effects'):
 assert term in workflow,term
print('PASS Plan publication ownership, Implement aggregate evidence, neutral Research and common application/final-enforcement semantics')
for p in ('teams/source/skills/agent-finalization/references/common.md','teams/source/skills/workflow/SKILL.md','teams/source/skills/research-evidence/SKILL.md'):
 s=Path(p).read_text();assert 'baseline' in s and 'revision' in s and 'retired IDs' in s
for p in ('teams/source/skills/review-evidence/SKILL.md','teams/source/skills/agent-finalization/references/validate.md'):
 s=Path(p).read_text();assert 'reference' in s and 'terminal' in s and 'conflict' in s and 'identical' in s
print('PASS canonical source assertions establish matching semantic obligations, not byte-generation parity')
sibling=['.codex/skills/'+r+'/SKILL.md' for r in roles]+['.codex/skills/workflow/SKILL.md','.codex/skills/review-evidence/SKILL.md']
assert all(sha(p)==before[p]['sha256'] for p in sibling)
assert '## Shared final routes and continuation' not in Path('.codex/skills/workflow/SKILL.md').read_text()
print('PASS US-007 exact 12-path handoff unchanged; known sibling drift retained for Claude; US-010 parity pending')
sources=sorted(p for p in before if p.startswith('teams/source/skills/') and any('/'+x+'/' in p for x in ('agent-finalization','workflow','research-evidence','planning-contracts','review-evidence')))
Path('/tmp/us006-digests.json').write_text(json.dumps({'owned':{p:sha(p) for p in owned},'canonical':{p:sha(p) for p in sources},'codex_handoff':{p:sha(p) for p in sibling}},indent=2)+'\n')
print('PASS SHA256 inventory retained for owned, canonical and unchanged handoff bytes')
```
