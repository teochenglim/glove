package decision

import (
	"testing"
)

// TestLayer2NeverDowngrades is the PRD §4.2 invariant test; it goes live in
// v0.4.0 when the Layer 2 client exists (PRD §7 0.4.0): a Layer 0 `block`
// must survive a mocked Laya response of `allow` and the final decision must
// be `block` (PRD §10.1 step 9 keeps the stub from 0.1.0). The arithmetic it
// will assert is Combine, already covered by verdict_test.go.
func TestLayer2NeverDowngrades(t *testing.T) {
	t.Skip("goes live with v0.4.0 (PRD §7 0.4.0): the Laya client does not exist in 0.1.0")
}
