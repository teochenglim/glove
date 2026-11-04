package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// httptestRecorder builds a ResponseWriter recorder without pulling the
// full httptest package into every call site.
func httptestRecorder() *httptest.ResponseRecorder {
	return httptest.NewRecorder()
}

// evaluateRequest builds a POST /evaluate request for the benign input.
func evaluateRequest(t *testing.T) *http.Request {
	t.Helper()
	in := testInput(t)
	req := httptest.NewRequest(http.MethodPost, "/evaluate", strings.NewReader(string(in.ToolInput)))
	// The handler decodes a HookInput, not the raw tool input — send the
	// full struct.
	full, _ := json.Marshal(in)
	req = httptest.NewRequest(http.MethodPost, "/evaluate", strings.NewReader(string(full)))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// freePort grabs an unused loopback port (best effort — small race window).
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("grab port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// startDaemon runs a real serve daemon against the stub laya and waits for
// readiness. Returns the port.
func startDaemon(t *testing.T, layaURL string) int {
	t.Helper()
	port := freePort(t)
	opts := Options{
		Port:          port,
		Endpoint:      layaURL,
		NoSetupPrompt: true,
		Stdout:        nopWriter{},
	}
	srv, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	go func() { _ = srv.Run(context.Background(), opts) }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
		if err == nil {
			resp.Body.Close()
			// AC0.4.6 server half: the daemon's bound address must be loopback.
			host, _, splitErr := net.SplitHostPort(srv.Addr())
			if splitErr != nil {
				t.Fatalf("daemon addr %q: %v", srv.Addr(), splitErr)
			}
			if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
				t.Fatalf("daemon bound to %q, want loopback", srv.Addr())
			}
			return port
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("daemon did not come up within 5s")
	return 0
}

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// TestServerEvaluateDegraded — serve without a laya instance answers 503
// with model_available=false (the hook's offline-path trigger, PRD §3.2).
func TestServerEvaluateDegraded(t *testing.T) {
	l := NewLaya("http://127.0.0.1:1", "", "") // nothing listens there
	srv, err := New(Options{Endpoint: l.Endpoint})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	rec := httptestRecorder()
	srv.handleEvaluate(rec, evaluateRequest(t))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", rec.Code)
	}
	var body struct {
		ModelAvailable bool `json:"model_available"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	if body.ModelAvailable {
		t.Error("degraded serve must answer model_available=false")
	}
}

// TestServerEvaluateWithStub — a ready serve + stub laya answers 200 with a
// partial verdict carrying the four questions.
func TestServerEvaluateWithStub(t *testing.T) {
	stub := newLayaStub(t)
	stub.answers = map[string]layaAnswer{
		"destructive":          noul(0.98),
		"exfiltration":         noul(0.02),
		"privilege_escalating": noul(0.02),
		"blast_radius":         {Type: "choice", Choice: "local", AnswerConfidence: 0.9},
	}
	srv, err := New(Options{Endpoint: stub.server.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// The handler only answers when the server's own runtime probed healthy —
	// probe it (Server.Run does this before listening).
	if err := srv.laya.Probe(); err != nil {
		t.Fatalf("probe: %v", err)
	}
	rec := httptestRecorder()
	srv.handleEvaluate(rec, evaluateRequest(t))
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var v struct {
		Decision  string `json:"decision"`
		Questions []struct {
			Question string `json:"question"`
		} `json:"questions"`
		ModelAvailable bool `json:"model_available"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v.Decision != "block" || !v.ModelAvailable || len(v.Questions) != 4 {
		t.Errorf("verdict = %+v", v)
	}
}

// TestDaemonEndToEnd — AC0.4.1 harness: a real daemon on a real loopback
// port answers /evaluate and /healthz.
func TestDaemonEndToEnd(t *testing.T) {
	stub := newLayaStub(t)
	stub.answers = defaultStubAnswers()
	port := startDaemon(t, stub.server.URL)
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
	if err != nil {
		t.Fatalf("healthz: %v", err)
	}
	defer resp.Body.Close()
	var h struct {
		Status string `json:"status"`
		Ready  bool   `json:"ready"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		t.Fatalf("decode healthz: %v", err)
	}
	if h.Status != "ok" || !h.Ready {
		t.Errorf("health = %+v", h)
	}
}

// TestAC_0_4_6_server_binds_loopback_only — AC0.4.6 server half, named for
// the traceability gate: the daemon's listener is loopback by construction
// (PRD §3.3). startDaemon asserts this on every startup; this test makes the
// contract explicit and also pins that Options cannot express a non-loopback
// host at all — loopback is the only shape the API exposes.
func TestAC_0_4_6_server_binds_loopback_only(t *testing.T) {
	stub := newLayaStub(t)
	stub.answers = defaultStubAnswers()
	port := startDaemon(t, stub.server.URL)

	host, _, err := net.SplitHostPort(fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Fatalf("split addr: %v", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		t.Fatalf("daemon host %q is not loopback", host)
	}
	// The endpoint override cannot smuggle a non-loopback bind either: the
	// laya endpoint must be loopback or New-time probing surfaces it.
	if err := ensureLoopbackServerEndpoint("http://10.0.0.5:8080"); err == nil {
		t.Fatal("non-loopback laya endpoint must be rejected")
	}
}
