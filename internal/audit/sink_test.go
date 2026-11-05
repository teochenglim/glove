package audit

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestAC_0_7_6_sinks_deliver_rfc5424_and_webhook — AC0.7.6: the file, syslog
// (RFC 5424) and webhook sinks deliver a record end-to-end at the dispatcher
// level.
func TestAC_0_7_6_sinks_deliver_rfc5424_and_webhook(t *testing.T) {
	r := rec("sink-test", "block")
	r.Host, r.User, r.MachineID = "host-1", "ci", "0123456789abcdef"
	r.Techniques = []string{"T1485"}

	t.Run("ac_0_7_6_file_sink", func(t *testing.T) {
		target := filepath.Join(t.TempDir(), "forward.jsonl")
		d := NewDispatcher(SinkConfig{Type: "file", Target: target})
		d.Enqueue(r)
		dropped, failed := d.Drain(time.Second)
		if dropped != 0 || failed != 0 {
			t.Fatalf("dropped=%d failed=%d, want 0/0", dropped, failed)
		}
		b, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("sink file missing: %v", err)
		}
		var got Record
		if err := json.Unmarshal([]byte(strings.TrimSpace(string(b))), &got); err != nil {
			t.Fatalf("sink line not JSON: %v", err)
		}
		if got.AuditID != r.AuditID || got.Decision != "block" {
			t.Fatalf("sink record mismatch: %+v", got)
		}
	})

	t.Run("ac_0_7_6_syslog_rfc5424_udp", func(t *testing.T) {
		pc, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Skipf("no udp loopback: %v", err)
		}
		defer pc.Close()
		target := pc.LocalAddr().String()

		d := NewDispatcher(SinkConfig{Type: "syslog", Target: target, Protocol: "udp"})
		d.Enqueue(r)
		dropped, failed := d.Drain(2 * time.Second)
		if dropped != 0 || failed != 0 {
			t.Fatalf("dropped=%d failed=%d", dropped, failed)
		}
		buf := make([]byte, 4096)
		_ = pc.SetReadDeadline(time.Now().Add(2 * time.Second))
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			t.Fatalf("no syslog datagram: %v", err)
		}
		line := string(buf[:n])
		if !strings.HasPrefix(line, "<11>1 ") { // facility 1 + severity 3 (block=err)
			t.Fatalf("want block PRI <11>1, got: %q", line)
		}
		for _, want := range []string{` host-1 glove`, ` [glove@1 decision="block" risk="high"`, `T1485`} {
			if !strings.Contains(line, want) {
				t.Fatalf("syslog line missing %q: %q", want, line)
			}
		}
	})

	t.Run("ac_0_7_6_webhook_post", func(t *testing.T) {
		type recv struct {
			ContentType string
			Body        string
		}
		ch := make(chan recv, 1)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			body := make([]byte, 8192)
			n, _ := req.Body.Read(body)
			ch <- recv{req.Header.Get("Content-Type"), string(body[:n])}
			w.WriteHeader(202)
		}))
		defer srv.Close()

		d := NewDispatcher(SinkConfig{Type: "webhook", Target: srv.URL})
		d.Enqueue(r)
		dropped, failed := d.Drain(2 * time.Second)
		if dropped != 0 || failed != 0 {
			t.Fatalf("dropped=%d failed=%d", dropped, failed)
		}
		select {
		case got := <-ch:
			if !strings.HasPrefix(got.ContentType, "application/json") {
				t.Fatalf("content type %q", got.ContentType)
			}
			var rec Record
			if err := json.Unmarshal([]byte(got.Body), &rec); err != nil {
				t.Fatalf("webhook body not record JSON: %v", err)
			}
			if rec.AuditID != r.AuditID {
				t.Fatalf("webhook record mismatch: %+v", rec)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("webhook never received the record")
		}
	})

	t.Run("ac_0_7_6_queue_overflow_drops_and_counts", func(t *testing.T) {
		// A webhook to an unreachable port keeps the worker busy while the
		// queue overflows; drops must be counted, never block.
		d := NewDispatcher(SinkConfig{Type: "webhook", Target: "http://127.0.0.1:1/nope", QueueSize: 1})
		for i := 0; i < 32; i++ {
			d.Enqueue(r)
		}
		dropped, _ := d.Drain(3 * time.Second)
		if dropped == 0 {
			t.Fatal("overflow produced no drop count")
		}
	})
}
