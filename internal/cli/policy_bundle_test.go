package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teochenglim/glove/internal/audit"
	"github.com/teochenglim/glove/internal/policy"
)

// runPolicyCmd executes a policy subcommand in-process.
func runPolicyCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newPolicyCmd()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

const bundleTestPolicy = "[general]\nmode = \"offline\"\n\n[paths]\nprotected = [\"~/secrets/**\"]\n"

// TestAC_0_8_3_import_rejects_tampered_bundle — the CLI end-to-end half:
// `policy pack` → tamper → `policy import` refuses BEFORE any write and the
// destination stays untouched.
func TestAC_0_8_3_import_rejects_tampered_bundle(t *testing.T) {
	_, dataDir := hookTestEnv(t)
	src := filepath.Join(t.TempDir(), "policy.toml")
	writeFile(t, src, bundleTestPolicy)

	out, err := runPolicyCmd(t, "pack", src)
	if err != nil {
		t.Fatalf("pack: %v (%s)", err, out)
	}
	bundlePath := src + ".bundle.json"
	bRaw, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatalf("bundle not written: %v", err)
	}
	var b policy.Bundle
	if err := json.Unmarshal(bRaw, &b); err != nil {
		t.Fatalf("bundle not valid JSON: %v", err)
	}

	dest := filepath.Join(t.TempDir(), "installed-policy.toml")
	// Tamper with the policy bytes inside the bundle.
	b.PolicyTOML += "\n[paths]\nprotected = [\"~/.ssh/**\"]\n"
	tampered, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	tamperedPath := filepath.Join(t.TempDir(), "tampered.json")
	writeFile(t, tamperedPath, string(tampered))

	if _, err := runPolicyCmd(t, "import", tamperedPath, "--out", dest); err == nil {
		t.Fatal("tampered bundle imported")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatalf("destination written despite tampered bundle: %v", err)
	}
	// No policy_import event for a refused import.
	recs, err := audit.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range recs {
		if r.Event == audit.EventPolicyImport {
			t.Fatalf("policy_import event emitted for a refused bundle: %+v", r)
		}
	}
}

// TestAC_0_8_3_import_emits_policy_event — the CLI end-to-end half: a
// verified import installs the exact bundled bytes, shows the diff, and
// emits a policy_import audit event carrying the bundle checksum.
func TestAC_0_8_3_import_emits_policy_event(t *testing.T) {
	_, dataDir := hookTestEnv(t)
	src := filepath.Join(t.TempDir(), "policy.toml")
	writeFile(t, src, bundleTestPolicy)
	if _, err := runPolicyCmd(t, "pack", src); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "installed-policy.toml")
	out, err := runPolicyCmd(t, "import", src+".bundle.json", "--out", dest)
	if err != nil {
		t.Fatalf("import: %v (%s)", err, out)
	}
	if !strings.Contains(out, "bundle verified (checksum sha256:") || !strings.Contains(out, "+ [paths]") {
		t.Fatalf("import output missing verification or diff:\n%s", out)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("destination not written: %v", err)
	}
	if string(got) != bundleTestPolicy {
		t.Fatalf("installed bytes drifted:\n%q", got)
	}
	// The audit event carries the checksum (AC0.8.3).
	raw, err := os.ReadFile(src + ".bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	var b policy.Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	recs, err := audit.Load(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range recs {
		if r.Event == audit.EventPolicyImport && strings.Contains(strings.Join(r.Reasons, " "), b.Checksum) {
			found = true
		}
	}
	if !found {
		t.Fatalf("no policy_import event with checksum %s:\n%+v", b.Checksum, recs)
	}

	// --dry-run verifies and shows the diff but writes nothing.
	dryDest := filepath.Join(t.TempDir(), "dry.toml")
	out, err = runPolicyCmd(t, "import", src+".bundle.json", "--out", dryDest, "--dry-run")
	if err != nil {
		t.Fatalf("dry-run import: %v", err)
	}
	if !strings.Contains(out, "dry-run: nothing written") {
		t.Fatalf("dry-run marker missing:\n%s", out)
	}
	if _, err := os.Stat(dryDest); !os.IsNotExist(err) {
		t.Fatalf("dry-run wrote the destination: %v", err)
	}
	_ = time.Now
}
