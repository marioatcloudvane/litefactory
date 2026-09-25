# Implement notes

Assumptions: set name limit 80 chars; request id format `plan-request-<instance>-<attempt>`; set-stage id `set`; role tier flagship; promotion command `/approve-plan set <64-hex revision>`; public ordinal `<parent>-NNN` reserved at promotion in displayed order.
Observations: an agent-supplied factory member second diagnostic ("changed since recorded") is misleading on first submit; revoked-authority submit reports the generic "retain request ID" message; no dependency-approval barrier exists in production (dependants may advance while a dependency awaits review); a recovered create is not given a review wait until the next owner comment; full internal/turn suite now takes ~13 minutes total.
Not verified: real-agent decomposition, authentic host delivery, daemon end-to-end dispatch mode for graph step triggers.
