---
name: implement-result
description: Submit the result of one Implement story as completed, incomplete or blocked, with a plain reason when it is not completed.
resources:
  - node-result.md
---
Build the one story you were given, then report it. Read [node result](node-result.md) before you submit: it carries the worked result for each outcome. Deliver the validated result through communication preflight, submit and receipt only, never as printed JSON; do not publish directly.

The factory checks your result once, when you submit it. Do not write a status or a note yourself. When you submit `completed`, the factory commits and pushes your branch and moves the story forward. When you submit `incomplete` or `blocked`, the factory parks the story with your reason and asks the owner.
