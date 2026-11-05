package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Policy bundles (v0.8.0, AC0.8.3): a checksummed policy file with origin
// metadata, produced by `glove policy pack` and installed by
// `glove policy import --verify`. Bundles are files — the fleet story stays
// sneakernet/scm, there is no cross-machine service (PRD §3.3). Integrity
// covers both halves: the checksum is computed over the exact policy bytes
// plus the canonical origin JSON, so tampering with either is detected
// before any write happens.

// BundleVersion is the bundle container format version.
const BundleVersion = 1

// Bundle is the on-disk JSON container.
type Bundle struct {
	Version    int    `json:"glove_bundle_version"`
	PolicyTOML string `json:"policy_toml"`
	Origin     Origin `json:"origin"`
	Checksum   string `json:"checksum"` // sha256-hex over PolicyTOML + canonical Origin
}

// Origin records where a bundle came from (AC0.8.3: source paths, glove
// version, pack time).
type Origin struct {
	Sources      []string  `json:"sources"`
	GloveVersion string    `json:"glove_version"`
	PackedAt     time.Time `json:"packed_at"`
}

// Pack reads a policy file and seals it into a bundle.
func Pack(policyPath string, gloveVersion string, now time.Time) (*Bundle, error) {
	raw, err := os.ReadFile(policyPath)
	if err != nil {
		return nil, fmt.Errorf("read policy %s: %w", policyPath, err)
	}
	// Pack validates: a bundle of an invalid policy would just move the
	// failure to import time — refuse it at the source.
	if _, err := LoadFile(policyPath); err != nil {
		return nil, err
	}
	b := &Bundle{
		Version:    BundleVersion,
		PolicyTOML: string(raw),
		Origin: Origin{
			Sources:      []string{policyPath},
			GloveVersion: gloveVersion,
			PackedAt:     now.UTC(),
		},
	}
	b.Checksum = b.computeChecksum()
	return b, nil
}

// computeChecksum is sha256 over the exact policy bytes followed by the
// canonical origin JSON.
func (b *Bundle) computeChecksum() string {
	ob, err := json.Marshal(b.Origin)
	if err != nil {
		return "" // Origin contains only strings and a time; unreachable
	}
	h := sha256.New()
	h.Write([]byte(b.PolicyTOML))
	h.Write(ob)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// Verify recomputes the checksum over the bundle's own content (AC0.8.3:
// a tampered bundle is refused before any write). Callers must verify
// before trusting PolicyTOML or Origin.
func (b *Bundle) Verify() error {
	if b.Version != BundleVersion {
		return fmt.Errorf("bundle version %d is not %d — refusing", b.Version, BundleVersion)
	}
	want := b.computeChecksum()
	if want == "" {
		return fmt.Errorf("bundle checksum could not be computed")
	}
	if want != b.Checksum {
		return fmt.Errorf("bundle checksum mismatch — content was modified (declared %s, computed %s)", b.Checksum, want)
	}
	return nil
}

// SaveTo writes the bundle JSON (0600, parent dirs created).
func (b *Bundle) SaveTo(path string) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("encode bundle: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create bundle dir: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write bundle: %w", err)
	}
	return nil
}

// LoadBundle reads and verifies a bundle file; a tampered bundle never
// leaves this function.
func LoadBundle(path string) (*Bundle, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read bundle %s: %w", path, err)
	}
	var b Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("%s: not a glove policy bundle: %v", path, err)
	}
	if err := b.Verify(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &b, nil
}
