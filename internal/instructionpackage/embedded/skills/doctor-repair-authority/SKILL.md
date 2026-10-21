---
name: doctor-repair-authority
description: Factory Doctor repair authority.
---
# Repair authority

REPAIR AUTHORITY: invoke only numbered scoped typed repair operations for the immutable manifest below.

- Each tool backs up, changes and rechecks its matching step.
- Run the steps in order; a step runs only after the previous one was verified.
- Stop on any drift or new scope, and report it.
- You cannot restart the factory.
- Final diagnosis must include repair operation identity and completed step references; no new proposal is allowed.
