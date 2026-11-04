# Adapter authoring (PRD §7 0.5.0)

How to add an agent to glove's coverage matrix. The conformance suite is
the gate: an adapter that doesn't pass it doesn't land.

## The contract

Implement the §5.1 `Adapter` interface **unchanged** — it froze at 0.1.0
(`TestAC_0_5_1_adapter_interface_frozen` reflection-checks the method set;
the compile-time `var _ Adapter = …` block in `internal/adapter/claude_test.go`
pins signatures). Health reporting joins through the **optional**
`StatusReporter` interface (`Status() AgentStatus`) — never by widening the
frozen one.

## The rules every adapter inherits

- **Diff before write, backup, idempotent, refuse foreign files** (PRD
  §10.4). JSON-config adapters: `installJSONHook`/`removeJSONHook`/
  `statusJSONHook` in `internal/adapter/configjson.go`. Shim adapters:
  `installShim`/`uninstallShim`/`shimStatus` in `internal/adapter/shim.go`.
- **Fail closed**: the shim (and every envelope) treats a glove crash,
  exit 2, or unreadable output as block; `glove hook <agent>` denies on
  internal errors (§6, §8).
- **Tighten only**: on surfaces without an enforced `ask` (cursor
  `preToolUse`, gemini `BeforeTool`), render review as **deny** with a
  review-prefixed message. Never render review as something an agent may
  treat as allow.
- **One protocol change touches one file** (PRD §11): wire shapes live in
  `hookwire.go`, per-agent shim parameters in `shimHeaderFor`, config
  paths in `internal/paths/paths.go`.

## Steps

1. **Verify the agent's current hook surface** against its docs/source —
   event name, stdin/stdout shapes, blocking semantics, fail-open or
   fail-closed defaults, config path. Record the access date in the
   adapter's doc comment. The hook-surface research is part of the
   release, not a nice-to-have.
2. **Add the adapter file** (`internal/adapter/<agent>.go`) using the
   shared machinery; register it in `For()`/`Names()` in
   `internal/adapter/adapter.go`; add its path helpers to
   `internal/paths/paths.go` (with `At(path)` constructors for tests).
3. **Add a `conformSpec` row** in `internal/adapter/claude_test.go`
   (`conformSpecs()`) and a native stdin fixture at
   `testdata/conformance/<agent>.stdin.json`. Pick the flavor: `json`
   (broken-JSON corruption case), `shim` (foreign-file refusal case), or
   `audit` (no install phases). The suite covers install diff/backup/
   idempotency, dry-run, corruption refusal, uninstall idempotency,
   parse/render/registry/status.
4. **Plugin shim agents** (OpenCode lineage): parameterize
   `shimHeaderFor` (import line + export shape), regenerate the goldens
   (`GLOVE_UPDATE_GOLDENS=1 go test ./internal/adapter/ -run
   TestAC_0_5_3_shims_match`), and re-validate them with `tsc -p` —
   strictly against the vendor's real type package, at minimum against
   the stub types in `shim_golden_test.go`. Record what you validated.
5. **CLI-level AC tests** in `internal/cli/matrix_test.go`: the
   docs-dependent flow test picks up the new agent automatically from
   `adapter.Names()`.
6. **Docs**: `docs/install/<agent>.md` (install + verify + uninstall +
   troubleshooting + honest limits; screenshot pending the live-session
   pass) and a row in the coverage matrix.
7. **Matrix**: add the row to `matrixRows()` in `internal/cli/list.go`,
   then regenerate the README block — `glove list --matrix` and paste it
   between the `coverage-matrix` markers — and let `glove list --matrix
   --check` prove it (`TestAC_0_5_6_matrix_matches_readme`).
8. **Gates**: `make check`, `make race`, `make audit`. New dependency →
   locked list row in `RELEASE.md` §1 + a line in the release file.
9. **Live-session smoke check** (the release file's checklist): the
   maintainer runs the agent once with glove installed — `rm -rf /`
   denied, `ls` allowed — and records it in `RELEASE.md` §5. The
   automatable tests cannot speak for the live agent.
