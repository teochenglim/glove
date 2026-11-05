package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAC_0_7_6_sink_config_validate — AC0.7.6: malformed [audit.sink] config
// fails `policy validate` with line numbers; valid sinks pass.
func TestAC_0_7_6_sink_config_validate(t *testing.T) {
	validPath := filepath.Join(t.TempDir(), "policy.toml")
	if err := os.WriteFile(validPath, []byte(`
[general]
mode = "offline"

[audit.sink]
type = "syslog"
target = "127.0.0.1:514"
protocol = "udp"
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadFile(validPath); err != nil {
		t.Fatalf("valid syslog sink rejected: %v", err)
	}

	cases := []struct {
		name     string
		toml     string
		wantSub  []string
		wantLine int // the reported line of the offending key (0 = don't check)
	}{
		{
			name:    "unknown type",
			toml:    "[audit.sink]\ntype = \"carrier-pigeon\"\ntarget = \"x\"",
			wantSub: []string{"audit.sink.type", "carrier-pigeon", "file|syslog|webhook"},
		},
		{
			name:    "missing target",
			toml:    "[audit.sink]\ntype = \"file\"",
			wantSub: []string{"audit.sink.target", "requires"},
		},
		{
			name:    "webhook not http",
			toml:    "[audit.sink]\ntype = \"webhook\"\ntarget = \"ftp://example.com/x\"",
			wantSub: []string{"audit.sink.target", "http(s) URL"},
		},
		{
			name:    "bad syslog protocol",
			toml:    "[audit.sink]\ntype = \"syslog\"\ntarget = \"127.0.0.1:514\"\nprotocol = \"smoke\"",
			wantSub: []string{"audit.sink.protocol", "smoke"},
		},
		{
			name:    "target without type",
			toml:    "[audit.sink]\ntarget = \"out.jsonl\"",
			wantSub: []string{"audit.sink.target", "without audit.sink.type"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "policy.toml")
			if err := os.WriteFile(path, []byte(tc.toml), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadFile(path)
			if err == nil {
				t.Fatalf("malformed sink config accepted:\n%s", tc.toml)
			}
			for _, sub := range tc.wantSub {
				if !strings.Contains(err.Error(), sub) {
					t.Errorf("error missing %q:\n%s", sub, err)
				}
			}
		})
	}

	t.Run("ac_0_7_6_line_numbers_reported", func(t *testing.T) {
		// The offending `type` key sits on line 3 of the file.
		path := filepath.Join(t.TempDir(), "policy.toml")
		content := "[general]\nmode = \"offline\"\n\n[audit.sink]\ntype = \"carrier-pigeon\"\ntarget = \"x\"\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadFile(path)
		if err == nil {
			t.Fatal("malformed sink accepted")
		}
		if !strings.Contains(err.Error(), "(line 5)") {
			t.Fatalf("validate must report the source line of audit.sink.type (line 5):\n%s", err)
		}
	})

	t.Run("ac_0_7_6_technique_map_validated", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "policy.toml")
		content := "[rules.techniques]\n\"terraform\\\\s+destroy\" = \"NOT-A-TAG\"\n"
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadFile(path)
		if err == nil || !strings.Contains(err.Error(), "MITRE ATT&CK technique tag") {
			t.Fatalf("bad technique tag accepted: %v", err)
		}
	})
}
