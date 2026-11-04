// Package server is glove serve (PRD §3.2, §7 0.4.0): the daemon that owns
// the Layer 2 judgment path. Per ADR 3 the Laya runtime is the laya-serve
// container (ghcr.io/teochenglim/laya) — glove ships zero inference code and
// adapts its /v1/systemone protocol to the four typed questions.
package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/teochenglim/glove/internal/adapter"
	"github.com/teochenglim/glove/pkg/contract"
)

// DefaultLayaImage is the container the setup prompt offers (english
// checkpoint; the multilingual-cpu tag adds automatic language routing).
const DefaultLayaImage = "ghcr.io/teochenglim/laya"

// DefaultLayaEndpoint is where laya-serve listens (loopback only).
const DefaultLayaEndpoint = "http://127.0.0.1:8080"

// noul thresholds: ≥ fireThreshold is a confident YES, ≤ uncertainThreshold
// is a confident NO; between them is uncertainty → review territory.
const (
	layaFireThreshold      = 0.6
	layaUncertainThreshold = 0.4
	layaAskTimeout         = 15 * time.Second
)

// Laya is the Layer 2 runtime manager (ADR 3): probe, optional first-run
// setup, ask, calibrate. Safe for concurrent use.
type Laya struct {
	Endpoint    string
	Image       string
	ImageDigest string // optional [laya] image_digest pin; verified on start
	HTTP        *http.Client

	ready atomic.Bool
	model string
}

// NewLaya builds the runtime manager. Empty endpoint/image fall back to the
// defaults.
func NewLaya(endpoint, image, digest string) *Laya {
	if endpoint == "" {
		endpoint = DefaultLayaEndpoint
	}
	if image == "" {
		image = DefaultLayaImage
	}
	return &Laya{
		Endpoint:    endpoint,
		Image:       image,
		ImageDigest: digest,
		HTTP:        &http.Client{Timeout: layaAskTimeout},
	}
}

// Ready reports whether the runtime answered a recent health probe.
func (l *Laya) Ready() bool { return l.ready.Load() }

// Describe returns the runtime state for /healthz and setup output.
func (l *Laya) Describe() string {
	if l.ready.Load() {
		return "ready (" + l.model + " at " + l.Endpoint + ")"
	}
	return "degraded — no laya instance at " + l.Endpoint
}

// SetupInstructions is the actionable line the PRD's first-run flow prints.
func (l *Laya) SetupInstructions() string {
	return fmt.Sprintf(
		"Laya is not reachable at %s.\nStart it (multilingual checkpoint):\n"+
			"  docker run -d --name glove-laya -p 127.0.0.1:8080:8080 %s:multilingual-cpu\n"+
			"or the english default:\n"+
			"  docker run -p 8080:8080 %s\n"+
			"Then re-run `glove serve`. Until then serve answers 503 and hooks stay on the deterministic layers.",
		l.Endpoint, l.Image, l.Image)
}

// Probe checks the endpoint health and that it answers like a laya instance.
// A reachable service that is NOT laya is a hard configuration error (the
// decision path would silently lose its semantic layer).
func (l *Laya) Probe() error {
	body, err := l.get(l.Endpoint + "/health")
	if err != nil {
		l.ready.Store(false)
		return err
	}
	var health struct {
		Status string   `json:"status"`
		Loaded []string `json:"loaded"`
	}
	if err := json.Unmarshal(body, &health); err != nil || health.Status != "ok" || len(health.Loaded) == 0 {
		l.ready.Store(false)
		return fmt.Errorf("%s/health answered but is not a laya instance (status %q, loaded %v) — point [laya].endpoint at a laya-serve container", l.Endpoint, health.Status, health.Loaded)
	}
	l.model = strings.Join(health.Loaded, "+")
	l.ready.Store(true)
	return nil
}

// VerifyDigest enforces the optional [laya] image_digest pin (AC0.4.5,
// container-era): the local image must carry exactly the pinned digest.
// Docker must be available when a pin is set — fail closed otherwise.
func (l *Laya) VerifyDigest() error {
	if l.ImageDigest == "" {
		return nil
	}
	out, err := exec.Command("docker", "image", "inspect", l.Image, "--format", "{{join .RepoDigests \" \"}}").Output()
	if err != nil {
		return fmt.Errorf("[laya] image_digest is pinned but docker inspect failed for %s: %v — refusing to start (AC0.4.5)", l.Image, err)
	}
	for _, d := range strings.Fields(string(out)) {
		if d == l.ImageDigest || strings.HasSuffix(d, "@"+l.ImageDigest) {
			return nil
		}
	}
	return fmt.Errorf("[laya] image_digest pin mismatch: local %s is %s, policy pins %s — refusing to start; update the image or the pin", l.Image, strings.TrimSpace(string(out)), l.ImageDigest)
}

// StartSetup offers the first-run flow (PRD §7 0.4.0, container-era): print
// instructions; with docker present and an interactive y, start the
// container. Never pulls silently — no TTY, no docker, or "n" degrades to
// instructions only.
func (l *Laya) StartSetup(stdin io.Reader, stdout io.Writer) error {
	fmt.Fprintln(stdout, l.SetupInstructions())
	if _, err := exec.LookPath("docker"); err != nil {
		fmt.Fprintln(stdout, "docker not found — install docker, run the command above, then re-run `glove serve`.")
		return nil
	}
	if stdin == nil {
		return nil
	}
	fmt.Fprintf(stdout, "Start %s:multilingual-cpu now? [y/N] ", l.Image)
	var answer string
	if _, err := fmt.Fscanln(stdin, &answer); err != nil || (answer != "y" && answer != "Y") {
		fmt.Fprintln(stdout, "Skipped — serve continues degraded (model_available: false).")
		return nil
	}
	fmt.Fprintf(stdout, "Starting %s:multilingual-cpu ...\n", l.Image)
	start := exec.Command("docker", "run", "-d", "--name", "glove-laya", "-p", "127.0.0.1:8080:8080", l.Image+":multilingual-cpu")
	start.Stdout, start.Stderr = stdout, stdout
	if err := start.Run(); err != nil {
		return fmt.Errorf("docker run failed: %w — see the instructions above", err)
	}
	fmt.Fprintln(stdout, "Waiting for laya-serve to come up ...")
	for i := 0; i < 30; i++ {
		time.Sleep(2 * time.Second)
		if err := l.Probe(); err == nil {
			fmt.Fprintf(stdout, "Laya ready: %s\n", l.Describe())
			return nil
		}
	}
	return fmt.Errorf("laya did not become healthy within 60s — check docker logs glove-laya")
}

// layaAnswer mirrors one /v1/systemone answer (TypeSafe Jev protocol).
type layaAnswer struct {
	Type             string             `json:"type"`
	Noul             *float64           `json:"noul"`
	Choice           string             `json:"choice"`
	Probabilities    map[string]float64 `json:"probabilities"`
	Confidence       float64            `json:"confidence"`
	AnswerConfidence float64            `json:"answer_confidence"`
}

type layaResponse struct {
	Model   string                `json:"model"`
	Answers map[string]layaAnswer `json:"answers"`
}

// layaQuestionWire is the wire shape of one question.
type layaQuestionWire struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
}

// layaRequest is the /v1/systemone payload.
type layaRequest struct {
	State     map[string]string           `json:"state"`
	Questions map[string]layaQuestionWire `json:"questions"`
}

// The four typed questions (PRD §7 0.4.0) in their /v1/systemone shapes.
func buildQuestions() map[string]layaQuestionWire {
	return map[string]layaQuestionWire{
		"destructive": {
			Type:         "noul",
			Instructions: "Could this command destroy, format, or irreversibly delete data or systems?",
		},
		"exfiltration": {
			Type:         "noul",
			Instructions: "Could this command send private data, credentials, or files off this machine?",
		},
		"privilege_escalating": {
			Type:         "noul",
			Instructions: "Does this command escalate privileges (sudo, su, setuid, admin elevation)?",
		},
		"blast_radius": {
			Type:         "choice",
			Instructions: "How far does the damage reach if this runs?",
			Criteria: map[string]string{
				"local":  "one file or the current directory only",
				"system": "system state, many files, or installed software",
				"remote": "sends data or control off this machine",
			},
		},
	}
}

// Ask sends the tool call to laya and calibrates the answers into a partial
// Layer 2 verdict (never block-on-absence: errors are the caller's
// fail-closed fallback).
func (l *Laya) Ask(ctx context.Context, in adapter.HookInput, band []float64) (contract.Verdict, error) {
	cmd := commandText(in.ToolInput)
	payload := layaRequest{
		State:     map[string]string{"message": cmd},
		Questions: buildQuestions(),
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return contract.Verdict{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.Endpoint+"/v1/systemone", bytes.NewReader(body))
	if err != nil {
		return contract.Verdict{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.HTTP.Do(req)
	if err != nil {
		return contract.Verdict{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return contract.Verdict{}, fmt.Errorf("laya answered %d", resp.StatusCode)
	}
	var lr layaResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return contract.Verdict{}, err
	}
	return l.calibrate(lr, band), nil
}

// commandText extracts the shell command from a HookInput (best effort —
// non-shell tools send their whole input JSON).
func commandText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var in struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(raw, &in); err == nil && in.Command != "" {
		return in.Command
	}
	return string(raw)
}

// calibrate maps the four answers onto a partial verdict using the policy
// review band (PRD §7 0.4.0). Only tighten-capable verdicts leave this
// function: the §4.2 merge happens in decision.Combine.
func (l *Laya) calibrate(lr layaResponse, band []float64) contract.Verdict {
	v := contract.Verdict{
		Decision:       contract.DecisionAllow,
		Risk:           contract.RiskLow,
		Confidence:     1,
		Deterministic:  false,
		ModelAvailable: true,
		Reasons:        []string{},
		AuditID:        uuid.NewString(),
	}
	if lr.Model != "" {
		v.Reasons = append(v.Reasons, "laya model: "+lr.Model)
	}

	destructive := answerOf(lr, "destructive")
	exfiltration := answerOf(lr, "exfiltration")
	privilege := answerOf(lr, "privilege_escalating")
	blast := choiceOf(lr, "blast_radius")

	v.Questions = []contract.QA{
		{Question: "destructive", Answer: destructive.yes, Confidence: destructive.conf},
		{Question: "exfiltration", Answer: exfiltration.yes, Confidence: exfiltration.conf},
		{Question: "privilege_escalating", Answer: privilege.yes, Confidence: privilege.conf},
		{Question: "blast_radius", Answer: blast.choice != "" && blast.choice != "local", Confidence: blast.conf},
	}

	uncertain := false
	minConf := 1.0
	track := func(a layaAnswerOf) {
		if a.conf < minConf {
			minConf = a.conf
		}
		if a.uncertain {
			uncertain = true
		}
	}
	track(destructive)
	track(exfiltration)
	track(privilege)

	fire := []layaAnswerOf{}
	if destructive.yes && destructive.noul >= layaFireThreshold {
		fire = append(fire, destructive)
	}
	if exfiltration.yes && exfiltration.noul >= layaFireThreshold {
		fire = append(fire, exfiltration)
	}
	if privilege.yes && privilege.noul >= layaFireThreshold {
		fire = append(fire, privilege)
	}

	// Blast radius is severity metadata, not a block trigger: the live model
	// calls even `git status` "system-reaching" — only the noul flags block.
	// A reach verdict without flags marks the call worth a human look.
	reach := (blast.choice == "remote" || blast.choice == "system") && blast.conf >= layaFireThreshold

	if len(fire) > 0 {
		v.Decision = contract.DecisionBlock
		names := make([]string, 0, len(fire))
		for _, f := range fire {
			names = append(names, f.name)
			if f.conf < v.Confidence {
				v.Confidence = f.conf
			}
		}
		switch {
		case destructive.yes && exfiltration.yes:
			v.Risk = contract.RiskCritical
			names = append(names, "blast_radius=remote (destructive+exfiltration)")
		case anyFireRemote(fire, blast):
			v.Risk = contract.RiskCritical
			names = append(names, "blast_radius=remote")
		default:
			v.Risk = contract.RiskHigh
			if blast.choice == "system" {
				names = append(names, "blast_radius=system")
			}
		}
		sort.Strings(names)
		v.Reasons = append(v.Reasons, "laya: "+strings.Join(names, ", "))
		return v
	}

	inBand := len(band) == 2 && minConf >= band[0] && minConf <= band[1]
	if uncertain || inBand || reach {
		v.Decision = contract.DecisionReview
		v.Risk = contract.RiskMedium
		v.Confidence = minConf
		if reach {
			v.Reasons = append(v.Reasons, fmt.Sprintf("laya: blast_radius=%s — worth a human look", blast.choice))
		} else {
			v.Reasons = append(v.Reasons, fmt.Sprintf("laya: calibrated uncertainty (confidence %.2f)", minConf))
		}
		return v
	}
	v.Confidence = minConf
	return v
}

// anyFireRemote reports whether a firing flag coincides with a remote blast
// radius (data leaves the machine while a flag fired → critical).
func anyFireRemote(fire []layaAnswerOf, blast layaAnswerOf) bool {
	return blast.choice == "remote" && len(fire) > 0
}

// layaAnswerOf is the calibrated view of one answer.
type layaAnswerOf struct {
	name      string
	yes       bool
	noul      float64
	conf      float64
	choice    string
	uncertain bool
}

func answerOf(lr layaResponse, name string) layaAnswerOf {
	a, ok := lr.Answers[name]
	if !ok || a.Noul == nil {
		return layaAnswerOf{name: name}
	}
	p := *a.Noul
	conf := a.AnswerConfidence
	if conf == 0 {
		conf = a.Confidence
	}
	return layaAnswerOf{
		name:      name,
		yes:       p >= 0.5,
		noul:      p,
		conf:      conf,
		uncertain: p > layaUncertainThreshold && p < layaFireThreshold,
	}
}

func choiceOf(lr layaResponse, name string) layaAnswerOf {
	a, ok := lr.Answers[name]
	if !ok {
		return layaAnswerOf{name: name}
	}
	conf := a.AnswerConfidence
	if conf == 0 {
		conf = a.Confidence
	}
	uncertain := conf > 0 && conf < layaFireThreshold
	return layaAnswerOf{name: name, choice: a.Choice, conf: conf, uncertain: uncertain}
}

// get performs a GET and returns the body.
func (l *Laya) get(url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := l.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// ensureLoopbackServerEndpoint refuses non-loopback laya endpoints — the
// decision path makes zero non-loopback network calls (PRD §3.3); the model
// hop is loopback or it does not exist.
func ensureLoopbackServerEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("laya endpoint %q: %w", endpoint, err)
	}
	host := u.Hostname()
	if host == "localhost" {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("laya endpoint %q must be loopback (127.0.0.1, ::1 or localhost) — PRD §3.3", endpoint)
}
