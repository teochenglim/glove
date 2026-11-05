package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/user"
	"sync"
)

// MachineIdentity is the per-machine join key set (PRD §7 0.7.0 AC0.7.3):
// hostname, username, and a stable derived machine_id. It is stamped by the
// Writer on every record so a SIEM can aggregate a fleet (docs/siem.md).
type MachineIdentity struct {
	Host      string
	User      string
	MachineID string
}

var (
	idOnce sync.Once
	idVal  MachineIdentity
)

// Identity resolves the machine identity once per process. Failures degrade
// to empty strings — identity is metadata, never a reason to lose a record.
// MachineID is sha256(host + "/" + user), first 16 hex: a stable single
// join key derived from the other two fields, so it leaks nothing new.
func Identity() MachineIdentity {
	idOnce.Do(func() {
		host, _ := os.Hostname()
		var name string
		if u, err := user.Current(); err == nil {
			name = u.Username
		}
		sum := sha256.Sum256([]byte(host + "/" + name))
		idVal = MachineIdentity{
			Host:      host,
			User:      name,
			MachineID: hex.EncodeToString(sum[:])[:16],
		}
	})
	return idVal
}
