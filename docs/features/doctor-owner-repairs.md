# Owner-approved repair release check

Use a disposable instance and issue with no active worker. Keep the issue link
and the instance identity for the acceptance record.

1. Record the stuck instance's running state and the absence of pending/running
   attempts, dispatches, owner waits and worker sessions.
2. As the configured owner, send `/doctor` with a request to diagnose the stuck
   instance. Review the presented proposal: it must contain `release_instance`
   for that instance, with running before-state and failed after-state.
3. Send `/doctor apply` on that proposal.
4. Confirm the posted outcome lists the verified step, its backup reference and
   successful recheck. Confirm the instance is failed and the target hold released.
5. Observe the next factory tick offer replacement work for the Implement story.
   Confirm the factory stayed running throughout; record its instance identity.
6. Save the proposal, approval and outcome comment links, the before/after
   instance state and the offered-work evidence for Validate.

This checklist prepares the manual check. It does not claim that a live repair
has been performed or that manual acceptance has passed.
