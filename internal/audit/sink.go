package audit

import (
	"bufio"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SinkConfig is the resolved [audit.sink] policy (PRD §5.5, ADR 4): an
// outbound, post-decision, opt-in forwarding target. Empty Type = off —
// no dispatcher is ever constructed and there are zero side effects
// (AC0.7.5).
type SinkConfig struct {
	Type      string // "" (off) | "file" | "syslog" | "webhook"
	Target    string // file path | host:port or socket path (syslog) | URL (webhook)
	Protocol  string // syslog only: udp (default) | tcp | tcp+tls | unix
	QueueSize int    // bounded queue; default 256 (0 → default)
	TimeoutMs int    // drain window after the decision; default 500 (0 → default)
}

// Enabled reports whether a sink is configured.
func (c SinkConfig) Enabled() bool { return c.Type != "" }

// Sink delivery timeouts (ADR 4: one attempt per record, bounded work).
const (
	syslogDialTimeout = 1 * time.Second
	syslogWriteTarget = time.Second
	webhookTimeout    = 2 * time.Second
)

// Dispatcher forwards records to the configured sink from a bounded queue.
// Enqueue never blocks (overflow drops and counts); delivery is one attempt
// per record with no retry (ADR 4). Failure/drop counts are returned by
// Drain so the caller can write the local-only sink_error / sink_dropped
// audit events — those events are never themselves forwarded (no loop).
// `failed` counts confirmed failures plus, when the drain window elapses,
// records whose outcome is unknowable (still queued or in flight at process
// exit — not confirmed delivered).
type Dispatcher struct {
	cfg       SinkConfig
	ch        chan Record
	done      chan struct{}
	closeOnce sync.Once
	drops     atomic.Int64
	failures  atomic.Int64
	enqueued  atomic.Int64
	delivered atomic.Int64
}

// NewDispatcher starts the delivery worker. Call only when cfg.Enabled().
func NewDispatcher(cfg SinkConfig) *Dispatcher {
	q := cfg.QueueSize
	if q <= 0 {
		q = 256
	}
	d := &Dispatcher{cfg: cfg, ch: make(chan Record, q), done: make(chan struct{})}
	go d.worker()
	return d
}

// Enqueue hands one record to the sink worker; never blocks (AC0.7.5).
func (d *Dispatcher) Enqueue(r Record) {
	select {
	case d.ch <- r:
		d.enqueued.Add(1)
	default:
		d.drops.Add(1)
	}
}

// Drain closes the queue and waits up to timeout for the worker to finish
// the backlog. Returns the drop and failure counts for the caller to audit.
func (d *Dispatcher) Drain(timeout time.Duration) (dropped, failed int64) {
	d.closeOnce.Do(func() { close(d.ch) })
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}
	timedOut := false
	select {
	case <-d.done:
	case <-time.After(timeout):
		timedOut = true
	}
	failed = d.failures.Load()
	if timedOut {
		// Unconfirmed: queued or in flight when the window closed. Counted
		// conservatively as not delivered (the honest local-audit posture).
		if unknown := d.enqueued.Load() - d.delivered.Load() - d.failures.Load(); unknown > 0 {
			failed += unknown
		}
	}
	return d.drops.Load(), failed
}

func (d *Dispatcher) worker() {
	for r := range d.ch {
		if err := d.deliver(r); err != nil {
			d.failures.Add(1)
		} else {
			d.delivered.Add(1)
		}
	}
	close(d.done)
}

func (d *Dispatcher) deliver(r Record) error {
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	switch d.cfg.Type {
	case "file":
		return deliverFile(d.cfg.Target, line)
	case "syslog":
		return deliverSyslog(d.cfg, r)
	case "webhook":
		return deliverWebhook(d.cfg.Target, line)
	default:
		return fmt.Errorf("unknown sink type %q", d.cfg.Type)
	}
}

func deliverFile(path string, line []byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("sink file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("sink file write: %w", err)
	}
	return nil
}

// RFC5424 severity per decision: block=3 (err), review=5 (notice),
// allow=6 (info); facility 1 (user). PRI = facility*8 + severity.
func syslogPRI(r Record) int {
	sev := 6
	switch r.Decision {
	case "review":
		sev = 5
	case "block":
		sev = 3
	}
	return 8 + sev
}

// RFC5424 line: <PRI>1 TIMESTAMP HOST APP PROCID MSGID - SD MSG. The
// structured data carries the machine-readable decision fields; MSG is the
// first human-readable reason. Copes with multilingual reasons (the laya
// checkpoint answers in the payload's language) — RFC 5424 MSG is UTF-8.
func syslogLine(r Record) string {
	msgid := "glove"
	if r.Tool != "" {
		msgid = sanitizeSyslogToken(r.Tool)
	}
	procid := "-"
	if r.SessionID != "" {
		procid = sanitizeSyslogToken(r.SessionID)
	}
	ts := r.Ts.Format("2006-01-02T15:04:05.000Z07:00")
	sd := fmt.Sprintf(`[glove@1 decision="%s" risk="%s" machine_id="%s" techniques="%s"]`,
		sdEscape(r.Decision), sdEscape(r.Risk), sdEscape(r.MachineID), sdEscape(strings.Join(r.Techniques, ",")))
	msg := ""
	if len(r.Reasons) > 0 {
		msg = r.Reasons[0]
	}
	return fmt.Sprintf("<%d>1 %s %s glove %s %s - %s %s",
		syslogPRI(r), ts, sdValue(r.Host), procid, msgid, sd, msg)
}

// sanitizeSyslogToken keeps PROCID/MSGID to PRINTUSASCII without spaces.
func sanitizeSyslogToken(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r > 32 && r < 127 {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	if b.Len() == 0 {
		return "-"
	}
	return b.String()
}

// sdEscape escapes an RFC 5424 SD-PARAM value: backslash, quote, bracket.
func sdEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return strings.ReplaceAll(s, "]", "\\]")
}

// sdValue keeps NILVALUE for an empty HOSTNAME.
func sdValue(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func deliverSyslog(cfg SinkConfig, r Record) error {
	proto := cfg.Protocol
	if proto == "" {
		proto = "udp"
	}
	var (
		conn net.Conn
		err  error
	)
	switch proto {
	case "udp", "tcp", "unix":
		conn, err = net.DialTimeout(proto, cfg.Target, syslogDialTimeout)
	case "tcp+tls":
		conn, err = tls.DialWithDialer(&net.Dialer{Timeout: syslogDialTimeout}, "tcp", cfg.Target, &tls.Config{})
	default:
		return fmt.Errorf("syslog protocol %q is not udp|tcp|tcp+tls|unix", proto)
	}
	if err != nil {
		return fmt.Errorf("sink syslog dial: %w", err)
	}
	defer conn.Close()
	_ = conn.SetWriteDeadline(time.Now().Add(syslogWriteTarget))
	if _, err := fmt.Fprint(conn, syslogLine(r)+"\n"); err != nil {
		return fmt.Errorf("sink syslog write: %w", err)
	}
	// UDP: give the kernel a moment to surface a local ICMP port-unreachable.
	if proto == "udp" {
		_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		var buf [1]byte
		if n, _ := conn.Read(buf[:]); n > 0 {
			return fmt.Errorf("sink syslog udp: target unreachable")
		}
	}
	return nil
}

func deliverWebhook(target string, line []byte) error {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("sink webhook target must be an http(s) URL, got %q", target)
	}
	client := &http.Client{Timeout: webhookTimeout}
	resp, err := client.Post(u.String(), "application/json", strings.NewReader(string(line)))
	if err != nil {
		return fmt.Errorf("sink webhook post: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("sink webhook: HTTP %d", resp.StatusCode)
	}
	// Drain a bounded amount so keep-alives close cleanly.
	_, _ = bufio.NewReader(resp.Body).Read(make([]byte, 512))
	return nil
}
