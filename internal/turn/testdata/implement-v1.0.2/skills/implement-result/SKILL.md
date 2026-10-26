---
name: implement-result
description: Submit the result of one Implement story as completed, incomplete or blocked, with a plain reason when it is not completed.
resources:
  - node-result.md
---
Build the one story you were given, then report it. Read [node result](node-result.md) before you submit: it carries the worked result for each outcome. Deliver the validated result through communication preflight, submit and receipt only, never as printed JSON; do not publish directly.

The factory checks your result once, when you submit it. Do not write a status or a note yourself. When you submit `completed`, the factory commits and pushes your branch and moves the story forward. Unfinished tasks are not a reason to stop: submit `incomplete` only when your time is about to run out or something outside the story stops the work for now. The factory then runs another attempt with your reason, and parks the story and asks the owner only once the attempts are spent. A gap the plan did not foresee is yours to decide within the story's intent and the approved decisions: record it in the completed result's `deviations` and keep building. Submit `blocked` only when every option would contradict an approved decision or a frozen acceptance criterion, or the work needs access or credentials you cannot get: the factory parks the story with your reason at once and asks the owner.
