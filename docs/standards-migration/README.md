# Shared guidance migration account

Reference material only. The role and skill edits this migration made were
reverted; roles keep their own guidance and nothing loads these files.

This directory contains review artifacts, not runtime instructions. The factory does
not load these files as a book, publish them during upgrade, or use them as fallback
advice. Publication and adoption are separate owner operations; see [rollout](rollout.md).
No production book, owner approval or provider behavior was observed here.

The source baseline is the immutable revision recorded in `inventory.json`. Each
section records its original text, SHA-256, source path/line, stable item ID,
consumer stack/workflow, disposition and reason. Source lines refer to that baseline,
not the cleaned working tree. Every duplicate occurrence remains an individual item;
`duplicate_occurrences` links identical source bytes. Equal advice wrapped in
different sections still maps to the same stable standard ID. Role/skill source paths
identify the exact consumers; graph entries account for their routing and selection.

`proposal.json` is the existing CLI proposal-create/edit input format. Its zero base
is suitable only for an empty test book. Operators must compare the existing book,
choose its actual base revision, resolve stable-ID collisions explicitly, then review
the complete exact version before approval and publication. No proposal or publication
identity is invented here: these references and the resulting digest remain pending
until the owner records observed output. This proposal contains complete standard
bodies and category applicability, rather than excerpts that need local evidence files.

The migration maps these behavioral groups:

| Source advice | Category | Stable standards |
| --- | --- | --- |
| Each conditional role Voice variant | shared-working | factory-voice, factory-voice-options, factory-voice-plan-set, factory-voice-investigation, factory-voice-execution, factory-voice-plain-options |
| Investigation advice only in runs permitted to ask | shared-working | factory-investigation |
| Research observation, inference and assumption reasoning | shared-working | factory-research-evidence |
| Optional agent/Markdown/workflow design advice | agent-setup | factory-agent-setup |
| Optional Go advice for actual Go scope in Plan | stack-go | factory-go-planning |
| Python/FastAPI API engineering and optional React/TypeScript/Vite frontend | stack-python-fastapi | factory-python-api, factory-react-vite |
| Blazor application practices with existing hosting/render-mode conditions | stack-dotnet | factory-blazor-practices |

Different Voice variants remain distinct standards, preserving their original advice
about options, story simplicity, source inference or observed execution. This migration
does not settle differences between them. Shared working categories apply to the
participating factory roles, while stack and artifact categories apply only to their
stated work. Category choice remains agent-driven; an explicit empty selection is valid.

Mixed sections are split where there is a clear boundary: Go Plan advice leaves its
no-code/stage gate local; the asking rule leaves permission, ask-before-choosing and
asynchronous reporting procedures local; Research keeps authentic-observation and owner-decision requirements local while moving its separate reasoning advice; Blazor leaves its evidence and approval
paragraph local. Python book bodies replace the original relative version-resource
reference with a reference to the retained stack supported-version resource. Pointer
stubs preserve profile/skill closure without delivering the migrated advice again.

Other conditional engineering text remains where reusable advice is coupled to
factory controls, executable verification, repository ownership, or pinned host and
stack contracts. In particular, Swift compatibility and engineering sections retain
compiler/SDK requirements, explicit simulator inputs, admitted permissions and offline
versus live evidence boundaries. These sections are accounted as retained, rather than
silently generalized into new stack policy. Repository-specific litefactory rules keep
SQL ownership, package checks, build procedures and prohibitions local. Result schemas,
receipt recovery, tool ceilings, workflow ownership, graph loops and human gates remain
factory authority. Book text cannot change them.

The inventory covers all canonical Markdown, graph and profile sections under the
four source roots and the embedded instruction family, plus substantial production Go
literals at the harness, turn, doctor and messaging delivery boundaries. Literal items
include diagnostics and protocol strings as well as agent instructions to avoid
mistaking a Markdown search for complete prompt coverage. An original asking-rule
concatenation fragment marked removed is replaced by the separately listed behavioral
and procedural items; it is not an independently retained fallback copy.

Generated releases and legacy source history are listed separately as immutable
historical assets with hashes. Their old advice is retained history, not new migrated
run delivery. Existing run captures and owner overlays keep their original identities
and bytes. Embedded helper/conversation guidance is retained because those consumers
have no standards-capable bound-run context; metadata stays at 1.9.0 because its shipped
bytes do not change. This story adds no second retrieval interface to those helpers.

Missing, corrupt, unsupported-schema and incomplete book reads remain visible degraded
states under unchanged local controls. They do not mean an empty selection and do not
load this proposal or an old behavioral copy. Recovery retains the same book revision
and additive selections. A conflict with local authority is reported for review without
changing permissions, gates or result protocols.
