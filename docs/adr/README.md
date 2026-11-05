# Architecture decision records

One decision per file: `NNNN-<slug>.md`. A decision is recorded when it is made, and cited from the PRD section, release file or code comment that depends on it.

| ADR | Decision | Status |
|---|---|---|
| [0001](0001-deterministic-first-fail-closed.md) | Deterministic-first, fail-closed decision path; the guard only ever tightens | accepted (PRD-given) |
| [0002](0002-contract-package-is-the-spec.md) | `pkg/contract` is the API spec; no OpenAPI | accepted |
| [0003](0003-laya-runtime-is-the-laya-container.md) | The Laya runtime is the laya-serve container; glove ships zero inference code | accepted |
| [0004](0004-audit-forwarding-is-post-decision-and-off-by-default.md) | Audit forwarding is post-decision, opt-in, off by default; delivery never affects a verdict | accepted |
