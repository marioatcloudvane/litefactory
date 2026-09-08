# Plan Brief — commit-staging-fails-when-the-repo

- Branch: `lf/157-commit-staging-fails-when-the-repo`
- Date: 2026-09-08
- Parent: [#157](https://github.com/marioatcloudvane/litefactory/issues/157)
- Citation base: `ca9d101b217695b94f16a03b5688fc7f5c2c5754`
- Research brief: none; explicit managed Plan handoff supplies the problem and scope.
- Proposal generation: 1

## Shared Decisions

One story; no cross-story design decisions.

## Workflow

Publish and verify the single story through the factory, then execute its frozen approved revision in a separately requested Implement run. There are no sibling dependencies.

## Cross-Story Constraints

Factory-only forge access and publication apply. Agents work in the supplied task worktree and leave commits and pushes to litefactory. The root JSON proposal is transient transport and must never be committed.

## Story Index

| ID | Title | Sub-issue |
| --- | --- | --- |
| US-1 | Commit task changes when factory transport is gitignored | [#158](https://github.com/marioatcloudvane/litefactory/issues/158) |

## Out of Scope

Implementation during this Plan run.

## Provenance

The supplied parent issue and triggering comment define scope. No existing child-story identities or base revisions were supplied; this proposal creates one story. The factory replaces the pending Story Index cell with a verified GitHub issue link after publication. Full story content is carried only in the managed proposal for publication, with no local per-story Markdown fallback. Revalidate cited code against the implementation revision before editing.
