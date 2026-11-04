package decision

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/teochenglim/glove/internal/adapter"
	"github.com/teochenglim/glove/pkg/contract"
)

// TestAC_0_4_6_client_refuses_nonloopback — AC0.4.6: the hook's client
// refuses non-loopback endpoints before dialing anything (PRD §3.3 — zero
// non-loopback network on the decision path).
func TestAC_0_4_6_client_refuses_nonloopback(t *testing.T) {
	t.Run("table", func(t *testing.T) {
		for _, ep := range []string{"http://8.8.8.8:8799", "http://laya.internal.example:8080", "https://example.com"} {
			c := NewLayaClient(ep, time.Second)
			_, err := c.Ask(context.Background(), bashInput("ls"))
			if !errors.Is(err, ErrModelUnavailable) {
				t.Errorf("%s: err = %v, want ErrModelUnavailable", ep, err)
			}
			if !strings.Contains(err.Error(), "loopback") {
				t.Errorf("%s: err = %v, want the loopback refusal", ep, err)
			}
		}
	})
	t.Run("loopback_accepted", func(t *testing.T) {
		c := NewLayaClient("http://127.0.0.1:1", 200*time.Millisecond)
		_, err := c.Ask(context.Background(), bashInput("ls"))
		if !errors.Is(err, ErrModelUnavailable) {
			t.Fatalf("err = %v, want ErrModelUnavailable", err)
		}
		// A loopback endpoint gets dialed (and refused) — the error is a
		// connection failure, not the loopback refusal.
		if strings.Contains(err.Error(), "must be loopback") {
			t.Errorf("loopback endpoint was refused: %v", err)
		}
	})
}

// TestLayaClientContract — the client decodes a partial Verdict from
// serve's /evaluate and flags it as model-consulted.
func TestLayaClientContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/evaluate" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		var in adapter.HookInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Errorf("serve received undecodable HookInput: %v", err)
		}
		if in.ToolName != "Bash" {
			t.Errorf("tool_name = %q", in.ToolName)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"decision":"review","risk":"medium","confidence":0.6,"reasons":["laya: calibrated uncertainty"],"questions":[{"question":"destructive","answer":false,"confidence":0.1}],"deterministic":false,"model_available":true}`))
	}))
	defer srv.Close()

	c := NewLayaClient(srv.URL, time.Second)
	v, err := c.Ask(context.Background(), bashInput("ls"))
	if err != nil {
		t.Fatalf("ask: %v", err)
	}
	if v.Decision != contract.DecisionReview || !v.ModelAvailable || v.Deterministic {
		t.Errorf("verdict = %+v", v)
	}
	if len(v.Questions) != 1 || v.Questions[0].Question != "destructive" {
		t.Errorf("questions = %+v", v.Questions)
	}
}

// TestLayaClientServeDown — a refused connection surfaces as
// ErrModelUnavailable (the offline-path trigger).
func TestLayaClientServeDown(t *testing.T) {
	c := NewLayaClient("http://127.0.0.1:1", 300*time.Millisecond)
	_, err := c.Ask(context.Background(), bashInput("ls"))
	if !errors.Is(err, ErrModelUnavailable) {
		t.Fatalf("err = %v, want ErrModelUnavailable", err)
	}
}
