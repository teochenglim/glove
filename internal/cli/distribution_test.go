package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teochenglim/glove/internal/audit"
	"github.com/teochenglim/glove/internal/policy"
)

// fakeRelease serves the release layout install.sh consumes: checksums.txt
// plus per-platform tarballs whose contents match the release workflow's
// packaging (glove-v<ver>-<goos>-<goarch>/<same-name> binary).
func fakeRelease(t *testing.T, bin string, version, goos, goarch string, corruptChecksums bool) *httptest.Server {
	t.Helper()
	asset := fmt.Sprintf("glove-v%s-%s-%s", version, goos, goarch)
	dist := t.TempDir()
	dir := filepath.Join(dist, asset)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	binBytes, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, asset), binBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	tarPath := filepath.Join(dist, asset+".tar.gz")
	if out, err := exec.Command("tar", "-czf", tarPath, "-C", dist, asset).CombinedOutput(); err != nil {
		t.Fatalf("tar: %v\n%s", err, out)
	}
	tarBytes, err := os.ReadFile(tarPath)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256Of(tarBytes)
	checksums := fmt.Sprintf("%s  %s.tar.gz\n", sum, asset)
	if corruptChecksums {
		checksums = strings.Repeat("ab", 32) + "  " + asset + ".tar.gz\n"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v"+version+"/checksums.txt", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, checksums)
	})
	mux.HandleFunc("/v"+version+"/"+asset+".tar.gz", func(w http.ResponseWriter, r *http.Request) {
		w.Write(tarBytes)
	})
	return httptest.NewServer(mux)
}

// TestAC_0_9_1_install_script_checksum_verified — AC0.9.1: the real
// scripts/install.sh installs a checksum-verified artifact end-to-end from
// a served release layout, and refuses a tampered checksums.txt before
// anything lands in the install dir.
func TestAC_0_9_1_install_script_checksum_verified(t *testing.T) {
	version := currentVersionForScript(t)
	bin := buildGlove(t) // built BEFORE any env override (0.6.1 lesson)

	t.Run("ac_0_9_1_clean_install_runs_the_real_script", func(t *testing.T) {
		srv := fakeRelease(t, bin, version, "darwin", "arm64", false)
		defer srv.Close()
		installDir := t.TempDir()
		cmd := exec.Command("sh", "../../scripts/install.sh", version)
		cmd.Env = append(os.Environ(),
			"GLOVE_INSTALL_BASE_URL="+srv.URL,
			"GLOVE_INSTALL_DIR="+installDir,
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("install.sh failed:\n%s\n%v", out, err)
		}
		if !strings.Contains(string(out), "checksum ok:") || !strings.Contains(string(out), "installed v"+version) {
			t.Fatalf("unexpected script output:\n%s", out)
		}
		// The installed binary is the real glove and reports the version.
		got, err := exec.Command(filepath.Join(installDir, "glove"), "version").Output()
		if err != nil {
			t.Fatalf("installed binary did not run: %v", err)
		}
		if strings.TrimSpace(string(got)) != version {
			t.Fatalf("glove version = %q, want %q", got, version)
		}
	})

	t.Run("ac_0_9_1_tampered_checksums_refused_before_install", func(t *testing.T) {
		srv := fakeRelease(t, bin, version, "darwin", "arm64", true)
		defer srv.Close()
		installDir := t.TempDir()
		cmd := exec.Command("sh", "../../scripts/install.sh", version)
		cmd.Env = append(os.Environ(),
			"GLOVE_INSTALL_BASE_URL="+srv.URL,
			"GLOVE_INSTALL_DIR="+installDir,
		)
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Fatalf("tampered checksums accepted:\n%s", out)
		}
		if !strings.Contains(string(out), "CHECKSUM MISMATCH") {
			t.Fatalf("refusal not loud:\n%s", out)
		}
		if _, statErr := os.Stat(filepath.Join(installDir, "glove")); !os.IsNotExist(statErr) {
			t.Fatalf("binary installed despite checksum failure: %v", statErr)
		}
	})

	t.Run("ac_0_9_1_unlisted_asset_refused", func(t *testing.T) {
		// checksums.txt without the asset line → refuse, no install.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "checksums.txt") {
				fmt.Fprint(w, strings.Repeat("cd", 32)+"  glove-v"+version+"-darwin-arm64.tar.gz\n")
				return
			}
			w.Write([]byte("not-the-real-archive"))
		}))
		defer srv.Close()
		installDir := t.TempDir()
		cmd := exec.Command("sh", "../../scripts/install.sh", version)
		cmd.Env = append(os.Environ(),
			"GLOVE_INSTALL_BASE_URL="+srv.URL,
			"GLOVE_INSTALL_DIR="+installDir,
		)
		// The tarball "downloaded" is bogus → its hash won't be listed → refuse.
		if out, err := cmd.CombinedOutput(); err == nil {
			t.Fatalf("unlisted asset accepted:\n%s", out)
		} else if !strings.Contains(string(out), "not listed") && !strings.Contains(string(out), "MISMATCH") {
			t.Fatalf("wrong refusal reason:\n%s", out)
		}
	})
}

func currentVersionForScript(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

func sha256Of(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestAC_0_9_2_new_paths_fail_closed — AC0.9.2: the 0.7/0.8 surfaces keep
// the failure contract when hammered together: malformed sink config fails
// the hook closed, a corrupt chain names its break, a tampered bundle is
// refused before any write, and an unreadable correlation trail degrades to
// "no escalation" (additive layer).
func TestAC_0_9_2_new_paths_fail_closed(t *testing.T) {
	t.Run("ac_0_9_2_malformed_sink_config_fails_hook_closed", func(t *testing.T) {
		cfgDir, _ := hookTestEnv(t)
		writeFile(t, filepath.Join(cfgDir, "policy.toml"),
			"[general]\nmode = \"offline\"\n\n[audit.sink]\ntype = \"smoke-signals\"\ntarget = \"x\"\n")
		var out, errBuf strings.Builder
		code := RunHookWith(&out, &errBuf, "claude-code",
			strings.NewReader(`{"tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`), HookOptions{})
		if code != 2 || !strings.Contains(out.String(), "deny") {
			t.Fatalf("invalid sink config must fail the hook closed: code=%d out=%q", code, out.String())
		}
		if !strings.Contains(errBuf.String(), "audit.sink.type") {
			t.Fatalf("no actionable policy error on stderr: %q", errBuf.String())
		}
	})

	t.Run("ac_0_9_2_verify_names_a_corrupt_chain", func(t *testing.T) {
		_, dataDir := hookTestEnv(t)
		w := audit.NewWriter(dataDir)
		if _, err := w.Write(audit.Record{AuditID: "a", Agent: "claude-code", Decision: "allow", GloveVersion: "t"}); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(audit.Record{AuditID: "b", Agent: "claude-code", Decision: "allow", GloveVersion: "t"}); err != nil {
			t.Fatal(err)
		}
		// Corrupt the live log in place.
		p := filepath.Join(dataDir, "audit.jsonl")
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
		lines[0] = strings.Replace(lines[0], `"decision":"allow"`, `"decision":"block"`, 1)
		if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cmd := newAuditVerifyCmd()
		var out strings.Builder
		cmd.SetOut(&out)
		cmd.SetArgs(nil)
		// Deliberately not `if err := cmd.Execute()` — the outer err is in
		// scope and shadowing it here is the AGENTS.md pitfall, paid for
		// once already.
		execErr := cmd.Execute()
		if execErr == nil {
			t.Fatal("verify accepted an edited chain")
		}
		if !strings.Contains(execErr.Error(), "TAMPERED") || !strings.Contains(execErr.Error(), "edited") {
			t.Fatalf("verify error does not name the break: %v", execErr)
		}
	})

	t.Run("ac_0_9_2_tampered_bundle_refused_before_write", func(t *testing.T) {
		_, dataDir := hookTestEnv(t)
		src := filepath.Join(t.TempDir(), "policy.toml")
		writeFile(t, src, "[general]\nmode = \"offline\"\n")
		if _, err := runPolicyCmd(t, "pack", src); err != nil {
			t.Fatal(err)
		}
		bRaw, err := os.ReadFile(src + ".bundle.json")
		if err != nil {
			t.Fatal(err)
		}
		var b policy.Bundle
		if err := json.Unmarshal(bRaw, &b); err != nil {
			t.Fatal(err)
		}
		b.Origin.PackedAt = time.Now().Add(time.Hour)
		tampered, _ := json.MarshalIndent(b, "", "  ")
		tp := filepath.Join(t.TempDir(), "tampered.json")
		writeFile(t, tp, string(tampered))
		dest := filepath.Join(t.TempDir(), "p.toml")
		if _, err := runPolicyCmd(t, "import", tp, "--out", dest); err == nil {
			t.Fatal("tampered bundle imported")
		}
		if _, err := os.Stat(dest); !os.IsNotExist(err) {
			t.Fatalf("destination written despite refusal: %v", err)
		}
		recs, err := audit.Load(dataDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range recs {
			if r.Event == audit.EventPolicyImport {
				t.Fatalf("policy_import emitted for a refused bundle: %+v", r)
			}
		}
	})

	t.Run("ac_0_9_2_unreadable_trail_degrades_to_no_escalation", func(t *testing.T) {
		cfgDir, dataDir := hookTestEnv(t)
		// Heat the session, then break the trail: DataDir becomes a FILE, so
		// the correlation scan cannot read it.
		for i := 0; i < 4; i++ {
			if code, _ := runSession(t, "broken-trail", "rm -rf /"); code != 2 {
				t.Fatalf("setup block %d exit = %d", i, code)
			}
		}
		if err := os.RemoveAll(dataDir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dataDir, []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(dataDir) })
		code, out := runSession(t, "broken-trail", "ls -la")
		if code != 0 || strings.Contains(out, `"ask"`) {
			t.Fatalf("unreadable trail changed the undecidable call: code=%d out=%q", code, out)
		}
		if strings.Contains(out, "correlation:") {
			t.Fatalf("correlation reason attached despite unreadable trail: %q", out)
		}
		_ = cfgDir
	})
}

// TestAC_0_9_3_sink_verify_overhead_budgeted — AC0.9.3: the re-measured
// bench proves sink + verify live off the decision path. Strict budgets are
// asserted in `make stress` (release hardware); CI stays deterministic per
// the 0.6.2 split. Numbers are recorded in benchmarks/README.md.
func TestAC_0_9_3_sink_verify_overhead_budgeted(t *testing.T) {
	requireStress(t)

	t.Run("ac_0_9_3_verify_10k_chain_under_one_second", func(t *testing.T) {
		dir := t.TempDir()
		w := audit.NewWriter(dir)
		for i := 0; i < 10_000; i++ {
			if _, err := w.Write(audit.Record{
				AuditID: fmt.Sprintf("bench-%d", i), Agent: "claude-code", Tool: "Bash",
				Decision: "allow", Risk: "low", GloveVersion: "t",
			}); err != nil {
				t.Fatal(err)
			}
		}
		start := time.Now()
		rep, err := audit.Verify(dir)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatal(err)
		}
		if !rep.Ok() || rep.Verified != 10_000 {
			t.Fatalf("chain verify failed: %+v", rep)
		}
		if elapsed > time.Second {
			t.Fatalf("verify over 10k chained records took %v, budget 1s", elapsed)
		}
	})

	t.Run("ac_0_9_3_file_sink_drain_under_fifty_ms", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "forward.jsonl")
		d := audit.NewDispatcher(audit.SinkConfig{Type: "file", Target: target})
		start := time.Now()
		for i := 0; i < 50; i++ {
			d.Enqueue(audit.Record{AuditID: fmt.Sprintf("sink-%d", i), Decision: "allow", GloveVersion: "t"})
		}
		dropped, failed := d.Drain(2 * time.Second)
		elapsed := time.Since(start)
		if dropped != 0 || failed != 0 {
			t.Fatalf("happy path dropped=%d failed=%d", dropped, failed)
		}
		if elapsed > 50*time.Millisecond {
			t.Fatalf("50 file-sink deliveries took %v — the post-decision window must stay near-free", elapsed)
		}
	})

	t.Run("ac_0_9_3_syslog_udp_delivery_under_hundred_ms", func(t *testing.T) {
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Skipf("no udp loopback: %v", err)
		}
		defer pc.Close()
		d := audit.NewDispatcher(audit.SinkConfig{Type: "syslog", Target: pc.LocalAddr().String(), Protocol: "udp"})
		start := time.Now()
		d.Enqueue(audit.Record{AuditID: "syslog-bench", Decision: "block", GloveVersion: "t", Host: "h"})
		dropped, failed := d.Drain(2 * time.Second)
		elapsed := time.Since(start)
		if dropped != 0 || failed != 0 {
			t.Fatalf("syslog delivery dropped=%d failed=%d", dropped, failed)
		}
		if elapsed > 100*time.Millisecond {
			t.Fatalf("udp syslog delivery took %v (includes the 50ms ICMP read window)", elapsed)
		}
	})
}
