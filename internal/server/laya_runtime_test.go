package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/teochenglim/glove/internal/adapter"
	"github.com/teochenglim/glove/pkg/contract"
)

// layaStub is an in-process laya-serve: /health plus a configurable
// /v1/systemone.
type layaStub struct {
	t        *testing.T
	server   *httptest.Server
	answers  map[string]layaAnswer
	healthOK bool
	notLaya  bool
}

func newLayaStub(t *testing.T) *layaStub {
	t.Helper()
	s := &layaStub{t: t, healthOK: true}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if s.notLaya {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>welcome</html>"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		status := "ok"
		if !s.healthOK {
			status = "warming"
		}
		_, _ = w.Write([]byte(`{"status":"` + status + `","loaded":["multilingual"],"device":"cpu"}`))
	})
	mux.HandleFunc("POST /v1/systemone", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(mustJSON(t, layaResponse{Model: "laya-rl-agent", Answers: s.answers}))
	})
	s.server = httptest.NewServer(mux)
	t.Cleanup(s.server.Close)
	return s
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func noul(p float64) layaAnswer {
	return layaAnswer{Type: "noul", Noul: &p, AnswerConfidence: p, Confidence: p}
}

func defaultStubAnswers() map[string]layaAnswer {
	return map[string]layaAnswer{
		"destructive":          noul(0.05),
		"exfiltration":         noul(0.05),
		"privilege_escalating": noul(0.05),
		"blast_radius":         {Type: "choice", Choice: "local", AnswerConfidence: 0.9},
	}
}

func testInput(t *testing.T) adapter.HookInput {
	t.Helper()
	return adapter.HookInput{
		Agent:     adapter.ClaudeCodeName,
		Event:     "PreToolUse",
		ToolName:  "Bash",
		ToolInput: []byte(`{"command":"ls -la"}`),
	}
}

// TestCalibrateTable pins the calibration mapping (PRD §7 0.4.0): firing
// flags block, the review band reviews, calm safety allows.
func TestCalibrateTable(t *testing.T) {
	l := NewLaya("", "", "")
	band := []float64{0.35, 0.75}
	t.Run("table", func(t *testing.T) {
		cases := []struct {
			name     string
			answers  map[string]layaAnswer
			decision contract.Decision
			risk     contract.Risk
		}{
			{"calm_benign_allows", defaultStubAnswers(), contract.DecisionAllow, contract.RiskLow},
			{"rm_rf_blocks_critical", map[string]layaAnswer{
				"destructive":          noul(1.0),
				"exfiltration":         noul(0.85),
				"privilege_escalating": noul(0.30),
				"blast_radius":         {Type: "choice", Choice: "local", AnswerConfidence: 0.4},
			}, contract.DecisionBlock, contract.RiskCritical},
			{"exfiltration_alone_blocks_high", map[string]layaAnswer{
				"destructive":          noul(0.02),
				"exfiltration":         noul(0.98),
				"privilege_escalating": noul(0.02),
				"blast_radius":         {Type: "choice", Choice: "local", AnswerConfidence: 0.9},
			}, contract.DecisionBlock, contract.RiskHigh},
			// Blast radius alone never blocks (measured on the live model:
			// even `git status` reads "system-reaching") — it reviews, and
			// escalates RISK only when a noul flag fired.
			{"remote_blast_alone_reviews", map[string]layaAnswer{
				"destructive":          noul(0.02),
				"exfiltration":         noul(0.02),
				"privilege_escalating": noul(0.02),
				"blast_radius":         {Type: "choice", Choice: "remote", AnswerConfidence: 0.9},
			}, contract.DecisionReview, contract.RiskMedium},
			{"system_blast_alone_reviews", map[string]layaAnswer{
				"destructive":          noul(0.02),
				"exfiltration":         noul(0.02),
				"privilege_escalating": noul(0.02),
				"blast_radius":         {Type: "choice", Choice: "system", AnswerConfidence: 0.9},
			}, contract.DecisionReview, contract.RiskMedium},
			{"flag_plus_remote_critical", map[string]layaAnswer{
				"destructive":          noul(0.02),
				"exfiltration":         noul(0.98),
				"privilege_escalating": noul(0.02),
				"blast_radius":         {Type: "choice", Choice: "remote", AnswerConfidence: 0.9},
			}, contract.DecisionBlock, contract.RiskCritical},
			{"flag_plus_system_high", map[string]layaAnswer{
				"destructive":          noul(0.98),
				"exfiltration":         noul(0.05),
				"privilege_escalating": noul(0.02),
				"blast_radius":         {Type: "choice", Choice: "system", AnswerConfidence: 0.9},
			}, contract.DecisionBlock, contract.RiskHigh},
			{"uncertain_noul_reviews", map[string]layaAnswer{
				"destructive":          noul(0.5),
				"exfiltration":         noul(0.05),
				"privilege_escalating": noul(0.05),
				"blast_radius":         {Type: "choice", Choice: "local", AnswerConfidence: 0.9},
			}, contract.DecisionReview, contract.RiskMedium},
			{"low_confidence_reviews", map[string]layaAnswer{
				"destructive":          {Type: "noul", Noul: floatPtr(0.1), AnswerConfidence: 0.5},
				"exfiltration":         {Type: "noul", Noul: floatPtr(0.1), AnswerConfidence: 0.5},
				"privilege_escalating": {Type: "noul", Noul: floatPtr(0.1), AnswerConfidence: 0.5},
				"blast_radius":         {Type: "choice", Choice: "local", AnswerConfidence: 0.5},
			}, contract.DecisionReview, contract.RiskMedium},
		}
		for _, tc := range cases {
			v := l.calibrate(layaResponse{Model: "laya-rl-agent", Answers: tc.answers}, band)
			if v.Decision != tc.decision || v.Risk != tc.risk {
				t.Errorf("%s: got %s/%s, want %s/%s (reasons %v)", tc.name, v.Decision, v.Risk, tc.decision, tc.risk, v.Reasons)
			}
			if v.ModelAvailable != true || v.Deterministic != false {
				t.Errorf("%s: flags = %+v", tc.name, v)
			}
			if len(v.Questions) != 4 {
				t.Errorf("%s: questions = %d, want the four typed answers", tc.name, len(v.Questions))
			}
		}
	})
}

func floatPtr(f float64) *float64 { return &f }

// TestAskAgainstStub — the /v1/systemone round trip parses the real wire
// shape and returns the four questions.
func TestAskAgainstStub(t *testing.T) {
	stub := newLayaStub(t)
	stub.answers = defaultStubAnswers()
	l := NewLaya(stub.server.URL, "", "")
	if err := l.Probe(); err != nil {
		t.Fatalf("probe: %v", err)
	}
	if !l.Ready() {
		t.Fatal("runtime must be ready after a healthy probe")
	}
	v, err := l.Ask(context.Background(), testInput(t), []float64{0.35, 0.75})
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if v.Decision != contract.DecisionAllow || len(v.Questions) != 4 {
		t.Errorf("verdict = %+v", v)
	}
}

// TestAC_0_4_5_not_a_laya_refuses — AC0.4.5: a reachable service that is
// not a laya instance is a hard probe error naming the fix.
func TestAC_0_4_5_not_a_laya_refuses(t *testing.T) {
	stub := newLayaStub(t)
	stub.notLaya = true
	l := NewLaya(stub.server.URL, "", "")
	err := l.Probe()
	if err == nil {
		t.Fatal("probe must reject a non-laya service")
	}
	if !strings.Contains(err.Error(), "not a laya instance") {
		t.Errorf("error = %v, want the actionable not-a-laya message", err)
	}
	if l.Ready() {
		t.Error("runtime must not be ready after a failed probe")
	}
}

// TestAC_0_4_5_digest_mismatch_refuses — AC0.4.5 container-era: a pinned
// [laya] image_digest that the local image does not carry refuses to start.
// Skipped when docker or the laya image is absent (CI ubuntu runner).
func TestAC_0_4_5_digest_mismatch_refuses(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not available")
	}
	if out, err := exec.Command("docker", "image", "inspect", DefaultLayaImage+":multilingual-cpu", "--format", "{{.Id}}").Output(); err != nil || len(out) == 0 {
		t.Skip("laya image not pulled")
	}
	l := NewLaya("", DefaultLayaImage, "sha256:deadbeef00000000000000000000000000000000000000000000000000000000")
	err := l.VerifyDigest()
	if err == nil {
		t.Fatal("mismatched digest pin must refuse")
	}
	if !strings.Contains(err.Error(), "pin mismatch") {
		t.Errorf("error = %v, want the pin-mismatch message", err)
	}
}
