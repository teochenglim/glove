package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/teochenglim/glove/internal/adapter"
	"github.com/teochenglim/glove/internal/version"
)

// Options configures `glove serve` (PRD §3.2).
type Options struct {
	Port          int
	Endpoint      string // laya-serve endpoint (flag > policy > default)
	Image         string
	ImageDigest   string
	ReviewBand    []float64
	NoSetupPrompt bool // non-interactive callers never prompt
	Stdin         io.Reader
	Stdout        io.Writer
	Stderr        io.Writer
}

// Server is the serve daemon (PRD §7 0.4.0). Loopback-only by construction:
// a non-loopback bind address is refused before listening (PRD §3.3).
type Server struct {
	laya   *Laya
	band   []float64
	port   int
	ready  atomic.Bool
	addr   atomic.Value // string, set once listening
	stdout io.Writer
}

// Addr returns the bound address once listening (empty before).
func (s *Server) Addr() string {
	v, _ := s.addr.Load().(string)
	return v
}

// New validates options and wires the runtime.
func New(opts Options) (*Server, error) {
	host := "127.0.0.1" // the only legal bind (PRD §3.3)
	_ = host
	if opts.Port <= 0 || opts.Port > 65535 {
		opts.Port = 8799
	}
	if opts.Stdout == nil {
		opts.Stdout = os.Stdout
	}
	if opts.Stderr == nil {
		opts.Stderr = os.Stderr
	}
	return &Server{laya: NewLaya(opts.Endpoint, opts.Image, opts.ImageDigest), band: opts.ReviewBand, port: opts.Port, stdout: opts.Stdout}, nil
}

// Run probes (and optionally sets up) Laya, then serves until signalled.
func (s *Server) Run(ctx context.Context, opts Options) error {
	// Optional image digest pin first: fail closed before anything else.
	if err := s.laya.VerifyDigest(); err != nil {
		return err
	}
	if err := s.laya.Probe(); err != nil {
		fmt.Fprintf(s.stdout, "laya probe: %v\n", err)
		if !opts.NoSetupPrompt {
			if err := s.laya.StartSetup(opts.Stdin, opts.Stdout); err != nil {
				fmt.Fprintf(s.stdout, "setup: %v\n", err)
			}
		}
		if err := s.laya.Probe(); err != nil {
			fmt.Fprintf(s.stdout, "continuing DEGRADED — model_available will be false and /evaluate answers 503.\n%s\n", s.laya.SetupInstructions())
		}
	} else {
		fmt.Fprintf(s.stdout, "laya ready: %s\n", s.laya.Describe())
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /evaluate", s.handleEvaluate)
	mux.HandleFunc("GET /healthz", s.handleHealth)

	addr := fmt.Sprintf("127.0.0.1:%d", s.port)
	httpSrv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("bind %s: %w", addr, err)
	}
	// Double-check the listener really is loopback (defence in depth).
	if tcp, ok := ln.Addr().(*net.TCPAddr); !ok || !tcp.IP.IsLoopback() {
		ln.Close()
		return fmt.Errorf("refusing to serve on non-loopback address %s (PRD §3.3)", ln.Addr())
	}
	s.ready.Store(true)
	s.addr.Store(addr)

	done := make(chan error, 1)
	go func() { done <- httpSrv.Serve(ln) }()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	fmt.Fprintf(s.stdout, "glove serve %s listening on %s (laya: %s)\n", version.Version, addr, s.laya.Describe())

	select {
	case err := <-done:
		return err
	case <-sig:
		fmt.Fprintln(s.stdout, "shutting down")
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	return nil
}

// handleEvaluate answers POST /evaluate (PRD §7 0.4.0): HookInput in, partial
// Verdict out. Degraded serve answers 503 — hooks treat that as the offline
// path (PRD §3.2).
func (s *Server) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	if !s.laya.Ready() {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"model_available": false,
			"reason":          s.laya.Describe(),
		})
		return
	}
	var in adapter.HookInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": fmt.Sprintf("decode HookInput: %v", err)})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), layaAskTimeout)
	defer cancel()
	v, err := s.laya.Ask(ctx, in, s.band)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"model_available": false,
			"reason":          fmt.Sprintf("laya ask failed: %v", err),
		})
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"version":  version.Version,
		"laya":     s.laya.Describe(),
		"ready":    s.laya.Ready(),
		"endpoint": s.laya.Endpoint,
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
