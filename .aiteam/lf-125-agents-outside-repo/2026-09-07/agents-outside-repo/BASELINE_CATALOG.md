# Original full-catalog baseline

Captured before renderer changes in implementation run 0011. Immutable source commit: `10b5bb26031b0e007a1c574fcd8e774128c5a2b7`.

This Git object inventory preserves exact original roots, specialists, source definitions, resolver and renderers. Use `git show <commit>:<path>` to recover individual bytes without touching the worktree. To reproduce old full rendering, use the full commit in an isolated scratch checkout and its original tests/rendering pipeline; rendering today's rewritten index with today's renderer is not a valid baseline. Original Claude root markers duplicate specialist bodies, so count root sections as well as standalone discovery. Codex rendering of the embedded Claude-shaped source rejects model/tools; report that original combination as unsupported, never remove those fields to manufacture a passing baseline.

These byte sizes are reproducibility evidence, not tokenizer counts or measured billable tokens. No model call or latency benchmark was run.

```
100644 blob fe1dd9d342183dac68ced4853d8b52d463d9e306    6695	.claude/agents/codebase-tour.md
100644 blob ff066fcbc4cf35777122129477bc4369c646ccf8   25961	.claude/agents/go-architect.md
100644 blob 41388f8e9b0c9718c4fa59501926e84b2ed9249f   14911	.claude/agents/go-code-reviewer.md
100644 blob 00f1f900ccf05104126a8e5a39cf6dee0f498e40   23074	.claude/agents/go-implementation-engineer.md
100644 blob e407668226b3f807573cba13b4637cba5437c23c   10532	.claude/agents/go-integration-test-engineer.md
100644 blob 02709ce2432d63fcc4a17233317cf6e65dcafb3a    9535	.claude/agents/go-unit-test-engineer.md
100644 blob 7ee41c8b30114d95dcf48e4efcbe4e9db8073ab7   23621	.claude/agents/implement-orchestrator.md
100644 blob e8e39255cb927e0d6747875dc2b55a4d7e64cd4a   17875	.claude/agents/plan-orchestrator.md
100644 blob cb75c0cf297de8599c68c16a7dd071a9f7ee5c8b    6889	.claude/agents/proxy-product-owner.md
100644 blob b1b95062840a7249562a6996534c19a1612dee1b    6324	.claude/agents/requirements-engineer.md
100644 blob ae1ea701423323cf0784bd64aa757cbf7da6bc1d   17391	.claude/agents/research-orchestrator.md
100644 blob 29e96a9d350c3cee515609f76c0b41023cf51c72    6238	.claude/agents/web-researcher.md
100644 blob 4e99f061a9482da0ff030f6421d1d76041411cf5    5159	.claude/modes/session.md
100644 blob 7de90241affbc4f142e88127e9a9b06f0d64adca    2973	.claude/modes/spike.md
100644 blob b85da864d38845c5d3c70ec6c93924084c419f43   17177	.codex/skills/app-context-updater/SKILL.md
100644 blob 592e43758d66bbfe6107215a9a007f442532468b    7563	.codex/skills/codebase-tour/SKILL.md
100644 blob 795e1307b1cb7a142ca42744f71623b2d87cc04d   22795	.codex/skills/go-architect/SKILL.md
100644 blob cb18f821aab022c2937fb2f6fb9d8430586805df   14902	.codex/skills/go-code-reviewer/SKILL.md
100644 blob 316162fcd774f1768ed3fc76853a0b145a4f8f62   23151	.codex/skills/go-implementation-engineer/SKILL.md
100644 blob f71c0195be205f35dbc5e449b881809913f84fff   10212	.codex/skills/go-integration-test-engineer/SKILL.md
100644 blob c059cac676fd048af7eb0aaca10ca819953b9525    9584	.codex/skills/go-unit-test-engineer/SKILL.md
100644 blob 60928a1a854c6bfc76d4eaa1981daa8236b3f470   24825	.codex/skills/implement-orchestrator/SKILL.md
100644 blob 68353b6fccee8683f81f7088e2edf8c8e02fc106   18746	.codex/skills/plan-orchestrator/SKILL.md
100644 blob f3496b8f01ce57dd05ac0abb97461b31d26ea784    7324	.codex/skills/proxy-product-owner/SKILL.md
100644 blob 3ca435b96f21ddf7a6817abe29ffda355bd4970e    7200	.codex/skills/requirements-engineer/SKILL.md
100644 blob 214ec9b65ddec2ea3c6f7bfbf040778c9c3d8ba5   17993	.codex/skills/research-orchestrator/SKILL.md
100644 blob 799481999b56c8911db67497cebc7f83cf2a4a49    6707	.codex/skills/web-researcher/SKILL.md
100644 blob 8a592df8d5270729e9bd77a78c36df1a7b976c07    8472	AGENTS.md
100644 blob ad8ee1c6668619254219c63ccc865be5178376bb    6300	CLAUDE.md
100644 blob c82ee09c7e7c0e986ddecb26ab448ad12d1d46c0    3707	internal/render/claude.go
100644 blob 0b61117405ad74540b369b9792d92c551bb40ba5    7750	internal/render/claude_test.go
100644 blob 154fd7be56ce4fa321ee5335576c5434fb30cf9c    3757	internal/render/codex.go
100644 blob b975ee3fbcb0aaf883d3824bdd332c0632165cf1    6736	internal/render/codex_test.go
100644 blob 3f1e418c835b740f2cd37f403dd2601f96cf9551    2516	internal/render/doc.go
100644 blob 11eacd60960cf686d6646403e2fb1845ebc2203f    2667	internal/render/errors.go
100644 blob e93dacde6148eea757efee7dd2cabce4280fbc28    4853	internal/render/opencode.go
100644 blob f01e24e9a8180b9999a59b64e185b8cae8ed7009    7252	internal/render/opencode_test.go
100644 blob 0d639b46f50c20f1533f09b5c4ab72c5091bc78b    2765	internal/render/registry.go
100644 blob 9e67d962b35ee52fabbc9e461d8fd6b74f9157df    1192	internal/render/render.go
100644 blob 2579cb17dfdf94bf15fe928fa6591c253dddfe63    2944	internal/render/render_smoke_test.go
100644 blob c6285ac72fbf611c459e4f923b7f13247d29bf74    3849	internal/render/types.go
100644 blob ff066fcbc4cf35777122129477bc4369c646ccf8   25961	internal/team/embedded/teams/go-team/go-architect.md
100644 blob 41388f8e9b0c9718c4fa59501926e84b2ed9249f   14911	internal/team/embedded/teams/go-team/go-code-reviewer.md
100644 blob 00f1f900ccf05104126a8e5a39cf6dee0f498e40   23074	internal/team/embedded/teams/go-team/go-implementation-engineer.md
100644 blob e407668226b3f807573cba13b4637cba5437c23c   10532	internal/team/embedded/teams/go-team/go-integration-test-engineer.md
100644 blob 02709ce2432d63fcc4a17233317cf6e65dcafb3a    9535	internal/team/embedded/teams/go-team/go-unit-test-engineer.md
100644 blob 6b0b0114e7d9ed1e5fccadb7c444e074191493c2    1702	internal/team/embedded/teams/index.json
100644 blob fe1dd9d342183dac68ced4853d8b52d463d9e306    6695	internal/team/embedded/teams/shared/codebase-tour.md
100644 blob 7ee41c8b30114d95dcf48e4efcbe4e9db8073ab7   23621	internal/team/embedded/teams/shared/implement-orchestrator.md
100644 blob e8e39255cb927e0d6747875dc2b55a4d7e64cd4a   17875	internal/team/embedded/teams/shared/plan-orchestrator.md
100644 blob cb75c0cf297de8599c68c16a7dd071a9f7ee5c8b    6889	internal/team/embedded/teams/shared/proxy-product-owner.md
100644 blob b1b95062840a7249562a6996534c19a1612dee1b    6324	internal/team/embedded/teams/shared/requirements-engineer.md
100644 blob 45fc0d22fa543fcee97dfa8a3fd4a94750d1c7d1   17117	internal/team/embedded/teams/shared/research-orchestrator.md
100644 blob 277c4a6e1acbf7a18b7f4bf998f5f582f81532ef    5932	internal/team/embedded/teams/shared/web-researcher.md
100644 blob 7496b3d0dc39caf89ae1fef686658593a758e172    1251	internal/teamsource/doc.go
100644 blob 269346311b011a4da4f3534db3d83200500a794c    2236	internal/teamsource/errors.go
100644 blob 56d7938287e76258149450e9fb3f72c4075cdbf8    8617	internal/teamsource/render.go
100644 blob c6609ddf0b2a736615e134c3e17c354c172ef675    7198	internal/teamsource/render_forbidden_flag_test.go
100644 blob 5ab04394c15f672a542ee17600dae0424d94de46   15292	internal/teamsource/render_test.go
100644 blob 2812427973c28f25c36885b8456c9a50f4b8a21d    7255	internal/teamsource/resolve.go
```
