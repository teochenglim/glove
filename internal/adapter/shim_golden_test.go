package adapter

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// shimGoldenPath is where the tsc-validated golden shim for an agent lives.
func shimGoldenPath(agent string) string {
	return filepath.Join("..", "..", "testdata", "conformance", agent+".ts.golden")
}

// TestAC_0_5_3_shims_match_tsc_validated_golden — AC0.5.3: the generated
// kilo/opencode shims (including the Windows platform branch, which is
// inside the template) are byte-identical to the goldens in
// testdata/conformance/ that were validated with `tsc --noEmit` at
// authoring time. Regenerate with GLOVE_UPDATE_GOLDENS=1, then re-validate
// the goldens with tsc before committing a template change.
func TestAC_0_5_3_shims_match_tsc_validated_golden(t *testing.T) {
	for _, agent := range []string{KiloName, OpenCodeName} {
		t.Run(agent, func(t *testing.T) {
			data, err := shimHeaderFor(agent)
			if err != nil {
				t.Fatalf("shim header: %v", err)
			}
			got, err := renderShim(data)
			if err != nil {
				t.Fatalf("render shim: %v", err)
			}
			golden := shimGoldenPath(agent)
			if os.Getenv("GLOVE_UPDATE_GOLDENS") == "1" {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden (generate with GLOVE_UPDATE_GOLDENS=1): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("generated shim drifted from the tsc-validated golden (%s) — if the template changed deliberately, regenerate the golden and re-run tsc over it", golden)
			}
		})
	}
}

// stubPluginTypes is the minimal structural contract the shims rely on:
// the Plugin factory type. The runtime hook semantics (tool.execute.before
// signature, mutate output.args in place, throw blocks) were verified
// against each vendor's docs and source during 0.5.0 (PRD §11) — these
// stubs exist so strict-mode tsc validation needs no network.
const stubPluginTypes = `// Minimal structural stubs of the plugin type packages the glove shims
// import. The shims rely only on the Plugin factory type; the runtime
// contract is verified against the vendors' docs (PRD §11).
declare module "@kilocode/plugin" {
  export type Plugin = (
    input?: unknown,
    options?: unknown,
  ) => Promise<Record<string, unknown>>;
}
declare module "@opencode-ai/plugin" {
  export type Plugin = (
    input?: unknown,
    options?: unknown,
  ) => Promise<Record<string, unknown>>;
}
`

// TestAC_0_5_3_shims_tsc_clean_when_available — AC0.5.3 other half: when
// `tsc` is on PATH, the freshly generated shims compile strict with no
// errors. CI machines without tsc skip this half; the golden byte-identity
// test above is the always-on gate.
func TestAC_0_5_3_shims_tsc_clean_when_available(t *testing.T) {
	tsc, err := exec.LookPath("tsc")
	if err != nil {
		t.Skip("tsc not on PATH — golden byte-identity (validated with tsc at authoring time) is the gate")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "types.d.ts"), []byte(stubPluginTypes), 0o644); err != nil {
		t.Fatalf("write types stub: %v", err)
	}
	tsconfig := `{
  "compilerOptions": {
    "strict": true,
    "noEmit": true,
    "target": "es2022",
    "module": "esnext",
    "moduleResolution": "bundler",
    "lib": ["es2022", "dom"],
    "types": [],
    "skipLibCheck": true
  },
  "include": ["*.ts"]
}`
	if err := os.WriteFile(filepath.Join(dir, "tsconfig.json"), []byte(tsconfig), 0o644); err != nil {
		t.Fatalf("write tsconfig: %v", err)
	}
	for _, agent := range []string{KiloName, OpenCodeName} {
		data, err := shimHeaderFor(agent)
		if err != nil {
			t.Fatalf("shim header: %v", err)
		}
		content, err := renderShim(data)
		if err != nil {
			t.Fatalf("render shim: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, agent+".ts"), content, 0o644); err != nil {
			t.Fatalf("write shim: %v", err)
		}
	}
	cmd := exec.Command(tsc, "-p", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("tsc rejected the generated shims:\n%s", out)
	}
}
