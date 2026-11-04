# ADR 2: `pkg/contract` is the API spec; no OpenAPI

**Status:** accepted

**Context.** The house pattern for Go services is OpenAPI-first (spec → `make api-codegen` → implement the generated interface). Glove's PRD (§4.3) defines no `api/openapi.yaml`; its public surface is a stdin/stdout hook protocol and one internal HTTP endpoint (`POST /evaluate`, PRD §3.2), both described by the same Go types.

**Decision.** `pkg/contract` (PRD §5.3) is the canonical API definition. It ships at scaffold with shape-freeze tests (`TestVerdictJSONShapeIsFrozen`) that fail on any drift in JSON keys, enum strings or optionality. Freezing rules (PRD §10.4): no field change without a documented schema-version bump and owner sign-off. Adapters, `serve` and external tooling depend only on this package.

**Consequences.** No codegen drift gate (`api-check` absent from the Makefile); the freeze tests + contract package review are the drift gate instead. If `serve` grows beyond one endpoint, revisit this ADR before adding a second route — not after.

**Alternatives considered.** OpenAPI-first (rejected: one endpoint, the contract types already exist as product truth in PRD §5.3, and generated stubs would duplicate them); documenting the JSON shapes only in prose (rejected: prose drifts; a failing test does not).
