---
name: plan-go-guidance
description: Optional guidance for actual Go code scope.
---
# Go guidance
Apply only to Go work identified in the current story/artifacts. Reuse existing
packages and interfaces; follow repository dependency direction and designated
ownership. Discuss context cancellation, explicit errors, invariants and failure
boundaries where relevant. Model data and service access only where the change has
those concerns. Do not apply Go package/service assumptions to Markdown-only work.
Record applicable choices in the shared typed architecture rather than a separate
Go-specific report. No code, implementation tasks or owner approval at this stage.
