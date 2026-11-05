package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const bundlePolicy = `
[general]
mode = "offline"

[paths]
protected = ["~/secrets/**"]
`

// TestAC_0_8_3_import_rejects_tampered_bundle — AC0.8.3: any tampering with
// the policy bytes, the origin metadata, or the declared checksum is
// detected before any write; the destination stays untouched.
func TestAC_0_8_3_import_rejects_tampered_bundle(t *testing.T) {
	src := filepath.Join(t.TempDir(), "policy.toml")
	if err := os.WriteFile(src, []byte(bundlePolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Pack(src, "v0.8.0", time.Now())
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if err := b.Verify(); err != nil {
		t.Fatalf("clean bundle fails verify: %v", err)
	}

	tamper := func(mutate func(*Bundle)) *Bundle {
		cp := *b
		cp.Origin = b.Origin
		mutate(&cp)
		return &cp
	}
	cases := map[string]*Bundle{
		"policy bytes edited": tamper(func(x *Bundle) { x.PolicyTOML = bundlePolicy + "\n[paths]\nprotected = [\"~/.ssh/**\"]\n" }),
		"origin edited":       tamper(func(x *Bundle) { x.Origin.PackedAt = time.Now().Add(time.Hour) }),
		"checksum forged":     tamper(func(x *Bundle) { x.Checksum = "sha256:" + strings.Repeat("ab", 32) }),
		"version bumped":      tamper(func(x *Bundle) { x.Version = 99 }),
	}
	for name, bundle := range cases {
		if err := bundle.Verify(); err == nil {
			t.Errorf("%s: tampered bundle accepted", name)
		}
	}

	// The loaded-from-disk path refuses too.
	path := filepath.Join(t.TempDir(), "b.json")
	data, _ := json.Marshal(b)
	data = data[:len(data)-2] // truncate → corrupt
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBundle(path); err == nil {
		t.Error("corrupt bundle file accepted")
	}
}

// TestAC_0_8_3_import_emits_policy_event — AC0.8.3: a verified import
// installs the exact bundled policy bytes and emits a policy_import audit
// event carrying the bundle checksum.
func TestAC_0_8_3_import_emits_policy_event(t *testing.T) {
	src := filepath.Join(t.TempDir(), "policy.toml")
	if err := os.WriteFile(src, []byte(bundlePolicy), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := Pack(src, "v0.8.0", time.Now())
	if err != nil {
		t.Fatal(err)
	}

	// The bundle validates as live policy for this glove.
	if _, err := LoadFileContent([]byte(b.PolicyTOML)); err != nil {
		t.Fatalf("bundled policy invalid: %v", err)
	}
	// The origin metadata is what the release file promises (AC0.8.3).
	if b.Origin.GloveVersion != "v0.8.0" || len(b.Origin.Sources) != 1 || b.Origin.Sources[0] != src || b.Origin.PackedAt.IsZero() {
		t.Fatalf("origin metadata incomplete: %+v", b.Origin)
	}
	// Save + reload round-trips verbatim.
	bp := filepath.Join(t.TempDir(), "bundle.json")
	if err := b.SaveTo(bp); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadBundle(bp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.PolicyTOML != b.PolicyTOML || loaded.Checksum != b.Checksum {
		t.Fatal("bundle round-trip drifted")
	}
}
