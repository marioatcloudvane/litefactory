# Owner-approved repair release check

Use a disposable instance and issue with no active worker. Keep the issue link
and the instance identity for the acceptance record.

1. Record the stuck instance's running state, the absence of pending/running
   attempts, running or run-allocated dispatches, running runs and held session
   locks, and any waiting or claimed owner waits and pending or reserved
   unallocated dispatches. `release_instance` cancels those waits and dispatches
   in the same transaction and records them in the step backup.
2. As the configured owner, send `/doctor` with a request to diagnose the stuck
   instance. Review the presented proposal: it must contain `release_instance`
   for that instance, with running before-state and failed after-state.
3. Send `/doctor apply` on that proposal.
4. Confirm the posted outcome lists the verified step, its backup reference and
   successful recheck. Confirm the instance is failed, its recorded owner waits
   and dispatches are cancelled, and the target hold released.
5. Observe the next factory tick offer replacement work for the Implement story.
   Confirm the factory stayed running throughout; record its instance identity.
6. Save the proposal, approval and outcome comment links, the before/after
   instance state and the offered-work evidence for Validate.

This checklist prepares the manual check. It does not claim that a live repair
has been performed or that manual acceptance has passed.

## Provider isolation during repair

Both providers give the same guarantee: nothing changes except through the
eight owner-approved repair tools of the broker. Each enforces it differently.

- Claude: `--tools ""` with `--allowedTools` limited to the eight repair tools
  and the communication tools; no native tool is available.
- Codex: Codex cannot hide every native tool. The repair run uses
  `--sandbox read-only`, `features.shell_tool`, `features.unified_exec`,
  `features.view_image`, `features.image_generation` and
  `features.multi_agent` off, and `web_search="disabled"`. The tools Codex
  keeps (such as `exec` and `apply_patch`) cannot write through the read-only
  sandbox, so every change still goes through the broker tools.
