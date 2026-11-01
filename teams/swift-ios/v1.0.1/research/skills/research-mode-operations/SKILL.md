---
name: research-mode-operations
description: Research mode for infrastructure, configuration, deployment and secrets. Load when S-01 is or may be mode=operations — servers, daemons, CI, containers, proxies, config files, keys or deploy steps.
---
# Mode: operations

**Signals:** "deploy", "server", "CI", "Docker", "nginx", "config", "token", "certificate", "restart", "disk"; the change is to how the system runs rather than what it does.

**Gather first:**
- The current setup as recorded in the repository: config files, deploy scripts, CI workflows, runbooks, docs.
- Recent incidents, issues and commits about the same machine, service or pipeline.
- Which parts live outside the repository and can only be described, not read.

**Open points worth asking:**
- The target environment and who may change it.
- Where a secret lives and who rotates it — by name or reference only. Never ask for, copy or record a secret value.
- The acceptable downtime and the rollback route.

**The spec must make clear:**
- Current state → target state per component, with the evidence for the current state.
- Order of steps, which are manual, and who performs them.
- Rollback: how to return to the previous state, and how to tell a step worked (health check, log line, command).
- Secrets and credentials by reference only, and what must keep running throughout (`K-`).

Judgment, not a template: skip what does not apply and say why in one line.
