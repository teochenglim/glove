// Package policy loads and validates glove's policy.toml (PRD §5.5, §7
// 0.3.0). Precedence: project .glove.toml (discovered by walking up from the
// working directory) > user ~/.config/glove/policy.toml > built-in defaults.
// Scalars override by precedence; list-valued keys concatenate (builtin →
// user → project) because users extend patterns rather than replace them —
// a scalar key in a higher-precedence file is what "overrides" means, and
// that is the tested contract.
package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/teochenglim/glove/internal/paths"
)

// General modes (PRD §5.5).
const (
	ModeStandard = "standard"
	ModeFast     = "fast"
	ModeOffline  = "offline"
)

// Policy is the resolved policy (PRD §5.5).
type Policy struct {
	General     General          `toml:"general"`
	Rules       Rules            `toml:"rules"`
	Paths       PathsConf        `toml:"paths"`
	Laya        Laya             `toml:"laya"`
	Audit       AuditConf        `toml:"audit"`
	Correlation Correlation      `toml:"correlation"`
	Agents      map[string]Agent `toml:"agents"`
}

// Correlation configures session escalation (v0.8.0, AC0.8.1): when a
// session accumulates SessionThreshold block/review decisions, subsequent
// *undecidable* calls (no deterministic rule fired, final decision allow)
// tighten to review. Correlation is an additive layer — it can never loosen
// a deterministic verdict. SessionThreshold: 0 = the default (3), -1 =
// disabled (an explicit 0 cannot be distinguished from unset in TOML
// merging, so disable is spelled -1).
type Correlation struct {
	SessionThreshold int `toml:"session_threshold"`
}

// DefaultSessionThreshold applies when the key is absent.
const DefaultSessionThreshold = 3

// ResolvedThreshold maps the config value onto the effective threshold
// (0 → DefaultSessionThreshold, -1 → 0 = disabled).
func (c Correlation) ResolvedThreshold() int {
	switch c.SessionThreshold {
	case 0:
		return DefaultSessionThreshold
	case -1:
		return 0
	default:
		return c.SessionThreshold
	}
}

type General struct {
	Mode               string `toml:"mode"`
	OnModelUnavailable string `toml:"on_model_unavailable"`
}

type Rules struct {
	HardBlock    []string          `toml:"hard_block"`
	ForceConfirm []string          `toml:"force_confirm"`
	Techniques   map[string]string `toml:"techniques"` // v0.7.0: user pattern → MITRE ATT&CK tag
}

// AuditConf carries the audit-forwarding config (v0.7.0, ADR 4): outbound,
// post-decision, off by default — an empty Sink.Type disables everything.
type AuditConf struct {
	Sink Sink `toml:"sink"`
}

// Sink is the [audit.sink] block. Type: file | syslog | webhook; Target:
// path | host:port or socket path | http(s) URL; Protocol (syslog only):
// udp (default) | tcp | tcp+tls | unix. TimeoutMs is the post-decision
// drain window; QueueSize bounds the delivery queue. TypeLine/TargetLine
// carry the source line numbers for `policy validate` errors (AC0.7.6) and
// are excluded from the TOML round-trip (policy.Hash() stays stable).
type Sink struct {
	Type       string `toml:"type"`
	Target     string `toml:"target"`
	Protocol   string `toml:"protocol"`
	QueueSize  int    `toml:"queue_size"`
	TimeoutMs  int    `toml:"timeout_ms"`
	TypeLine   int    `toml:"-"`
	TargetLine int    `toml:"-"`
}

type PathsConf struct {
	Protected []string `toml:"protected"`
}

// Laya configures the Layer 2 runtime (ADR 3 — the laya-serve container).
type Laya struct {
	Endpoint             string    `toml:"endpoint"`        // default http://127.0.0.1:8080
	Image                string    `toml:"image"`           // default ghcr.io/teochenglim/laya
	ImageDigest          string    `toml:"image_digest"`    // optional pin; mismatch refuses to start
	HookTimeoutMs        int       `toml:"hook_timeout_ms"` // hook's wait for serve; default 5000
	ModelPath            string    `toml:"model_path"`      // deprecated (GGUF era); ignored
	ConfidenceReviewBand []float64 `toml:"confidence_review_band"`
}

// DefaultHookTimeoutMs bounds how long the hook waits for serve.
const DefaultHookTimeoutMs = 5000

type Agent struct {
	Enabled bool `toml:"enabled"`
}

// Defaults returns the built-in baseline (PRD §5.5 comments).
func Defaults() *Policy {
	return &Policy{
		General: General{Mode: ModeStandard, OnModelUnavailable: "review"},
		Laya:    Laya{ConfidenceReviewBand: []float64{0.35, 0.75}},
		Agents: map[string]Agent{
			"claude-code": {Enabled: true},
		},
	}
}

// Sources records which files contributed to a resolved policy (audit and
// doctor output).
type Sources struct {
	User    string
	Project string
}

// Describe renders the sources for logs and audit reasons.
func (s Sources) Describe() string {
	switch {
	case s.User != "" && s.Project != "":
		return fmt.Sprintf("user=%s project=%s", s.User, s.Project)
	case s.Project != "":
		return "project=" + s.Project
	case s.User != "":
		return "user=" + s.User
	default:
		return "builtin defaults"
	}
}

// Load resolves the effective policy: defaults ← user file ← project
// .glove.toml found by walking up from the working directory. Invalid
// files are a hard error (AC0.3.7): the guard never silently falls back
// to defaults.
func Load() (*Policy, Sources, error) {
	p := Defaults()
	var src Sources

	if up := paths.PolicyUserPath(); up != "" {
		if _, err := os.Stat(up); err == nil {
			if err := mergeFile(p, up); err != nil {
				return nil, src, err
			}
			src.User = up
		}
	}
	if pp := findProjectPolicy(); pp != "" {
		if err := mergeFile(p, pp); err != nil {
			return nil, src, err
		}
		src.Project = pp
	}
	if err := p.Validate(); err != nil {
		return nil, src, fmt.Errorf("resolved policy invalid: %w", err)
	}
	return p, src, nil
}

// LoadFile decodes and validates a single policy file (user-, project- or
// explicitly-named via `glove check --policy` / `glove policy validate`).
func LoadFile(path string) (*Policy, error) {
	p := Defaults()
	if err := mergeFile(p, path); err != nil {
		return nil, err
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

// LoadFileContent validates a policy given as raw bytes — the bundle import
// path (`glove policy import` checks the bundled TOML before installing it).
func LoadFileContent(raw []byte) (*Policy, error) {
	p := Defaults()
	var over Policy
	if err := toml.Unmarshal(raw, &over); err != nil {
		return nil, fmt.Errorf("%s", syntaxError(err))
	}
	over.Audit.Sink.TypeLine, over.Audit.Sink.TargetLine = sinkKeyLines(string(raw))
	mergeInto(p, &over)
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return p, nil
}

func mergeFile(p *Policy, path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read policy %s: %w", path, err)
	}
	var over Policy
	if err := toml.Unmarshal(raw, &over); err != nil {
		return fmt.Errorf("%s: %s", path, syntaxError(err))
	}
	// Line numbers for sink-config validation errors (AC0.7.6): the TOML
	// decoder does not expose them per key, so the raw text is scanned for
	// the [audit.sink] section's type/target keys.
	typeLine, targetLine := sinkKeyLines(string(raw))
	over.Audit.Sink.TypeLine, over.Audit.Sink.TargetLine = typeLine, targetLine
	mergeInto(p, &over)
	return nil
}

// mergeInto applies over on top of p: non-empty scalars and non-nil slices
// override, list-valued keys append (see the package comment). The
// rules.techniques map (v0.7.0) merges key-wise — adding a technique never
// wipes another file's entries.
func mergeInto(p, over *Policy) {
	if over.General.Mode != "" {
		p.General.Mode = over.General.Mode
	}
	if over.General.OnModelUnavailable != "" {
		p.General.OnModelUnavailable = over.General.OnModelUnavailable
	}
	p.Rules.HardBlock = append(p.Rules.HardBlock, over.Rules.HardBlock...)
	p.Rules.ForceConfirm = append(p.Rules.ForceConfirm, over.Rules.ForceConfirm...)
	if len(over.Rules.Techniques) > 0 {
		if p.Rules.Techniques == nil {
			p.Rules.Techniques = map[string]string{}
		}
		for k, v := range over.Rules.Techniques {
			p.Rules.Techniques[k] = v
		}
	}
	p.Paths.Protected = append(p.Paths.Protected, over.Paths.Protected...)
	if over.Laya.ModelPath != "" {
		p.Laya.ModelPath = over.Laya.ModelPath
	}
	if over.Laya.Endpoint != "" {
		p.Laya.Endpoint = over.Laya.Endpoint
	}
	if over.Laya.Image != "" {
		p.Laya.Image = over.Laya.Image
	}
	if over.Laya.ImageDigest != "" {
		p.Laya.ImageDigest = over.Laya.ImageDigest
	}
	if over.Laya.HookTimeoutMs > 0 {
		p.Laya.HookTimeoutMs = over.Laya.HookTimeoutMs
	}
	if len(over.Laya.ConfidenceReviewBand) > 0 {
		p.Laya.ConfidenceReviewBand = over.Laya.ConfidenceReviewBand
	}
	if over.Audit.Sink.Type != "" {
		p.Audit.Sink.Type = over.Audit.Sink.Type
	}
	if over.Audit.Sink.Target != "" {
		p.Audit.Sink.Target = over.Audit.Sink.Target
	}
	if over.Audit.Sink.Protocol != "" {
		p.Audit.Sink.Protocol = over.Audit.Sink.Protocol
	}
	if over.Audit.Sink.QueueSize > 0 {
		p.Audit.Sink.QueueSize = over.Audit.Sink.QueueSize
	}
	if over.Audit.Sink.TimeoutMs > 0 {
		p.Audit.Sink.TimeoutMs = over.Audit.Sink.TimeoutMs
	}
	if over.Audit.Sink.TypeLine > 0 {
		p.Audit.Sink.TypeLine = over.Audit.Sink.TypeLine
	}
	if over.Audit.Sink.TargetLine > 0 {
		p.Audit.Sink.TargetLine = over.Audit.Sink.TargetLine
	}
	if over.Correlation.SessionThreshold != 0 {
		p.Correlation.SessionThreshold = over.Correlation.SessionThreshold
	}
	for name, a := range over.Agents {
		if p.Agents == nil {
			p.Agents = map[string]Agent{}
		}
		p.Agents[name] = a
	}
}

// sinkKeyLines finds the source lines of `type =` and `target =` inside the
// `[audit.sink]` section of a policy file (0 = absent).
func sinkKeyLines(raw string) (typeLine, targetLine int) {
	inSink := false
	for i, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inSink = trimmed == "[audit.sink]"
			continue
		}
		if !inSink {
			continue
		}
		key := strings.SplitN(trimmed, "=", 2)[0]
		key = strings.TrimSpace(key)
		if key == "type" && typeLine == 0 {
			typeLine = i + 1
		}
		if key == "target" && targetLine == 0 {
			targetLine = i + 1
		}
	}
	return typeLine, targetLine
}

// findProjectPolicy walks up from the working directory looking for
// .glove.toml (PRD §5.5). Returns "" when none is found.
func findProjectPolicy() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	dir := wd
	for {
		candidate := filepath.Join(dir, ".glove.toml")
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// Validate checks enums, the confidence band and every pattern as a valid
// RE2 regexp. Errors name the offending key path.
func (p *Policy) Validate() error {
	var errs []string
	switch p.General.Mode {
	case ModeStandard, ModeFast, ModeOffline:
	default:
		errs = append(errs, fmt.Sprintf("general.mode: %q is not one of standard|fast|offline", p.General.Mode))
	}
	switch p.General.OnModelUnavailable {
	case "review", "block":
	default:
		errs = append(errs, fmt.Sprintf("general.on_model_unavailable: %q is not one of review|block", p.General.OnModelUnavailable))
	}
	band := p.Laya.ConfidenceReviewBand
	if len(band) != 2 || band[0] < 0 || band[1] > 1 || band[0] >= band[1] {
		errs = append(errs, fmt.Sprintf("laya.confidence_review_band: %v must be two ascending floats within [0,1]", band))
	}
	if p.Laya.HookTimeoutMs < 0 || p.Laya.HookTimeoutMs > 60000 {
		errs = append(errs, fmt.Sprintf("laya.hook_timeout_ms: %d must be within [0, 60000]", p.Laya.HookTimeoutMs))
	}
	for i, pat := range p.Rules.HardBlock {
		if _, err := regexp.Compile(pat); err != nil {
			errs = append(errs, fmt.Sprintf("rules.hard_block[%d]: invalid regexp: %v", i, err))
		}
	}
	for i, pat := range p.Rules.ForceConfirm {
		if _, err := regexp.Compile(pat); err != nil {
			errs = append(errs, fmt.Sprintf("rules.force_confirm[%d]: invalid regexp: %v", i, err))
		}
	}
	for i, pat := range p.Paths.Protected {
		if _, err := regexp.Compile(globToRegexpSource(pat)); err != nil {
			errs = append(errs, fmt.Sprintf("paths.protected[%d]: invalid glob: %v", i, err))
		}
	}
	errs = append(errs, p.validateSink()...)
	errs = append(errs, p.validateTechniques()...)
	if t := p.Correlation.SessionThreshold; t < -1 || t > 10000 {
		errs = append(errs, fmt.Sprintf("correlation.session_threshold: %d must be -1 (disabled) or within [1, 10000]", t))
	}
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%d policy error(s):\n  %s", len(errs), joinLines(errs))
}

// validateSink checks the [audit.sink] block (v0.7.0, ADR 4). Errors carry
// the source line number when the key came from a file (AC0.7.6).
func (p *Policy) validateSink() []string {
	var errs []string
	s := p.Audit.Sink
	line := func(n int) string {
		if n > 0 {
			return fmt.Sprintf(" (line %d)", n)
		}
		return ""
	}
	switch s.Type {
	case "":
		if s.Target != "" {
			errs = append(errs, fmt.Sprintf("audit.sink.target%s: set without audit.sink.type", line(s.TargetLine)))
		}
		return errs
	case "file":
		if s.Target == "" {
			errs = append(errs, fmt.Sprintf("audit.sink.target%s: file sink requires a path", line(s.TargetLine)))
		}
	case "syslog":
		if s.Target == "" {
			errs = append(errs, fmt.Sprintf("audit.sink.target%s: syslog sink requires host:port or a socket path", line(s.TargetLine)))
		}
		switch s.Protocol {
		case "", "udp", "tcp", "tcp+tls", "unix":
		default:
			errs = append(errs, fmt.Sprintf("audit.sink.protocol%s: %q is not udp|tcp|tcp+tls|unix", line(s.TypeLine), s.Protocol))
		}
	case "webhook":
		u, err := url.Parse(s.Target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			errs = append(errs, fmt.Sprintf("audit.sink.target%s: webhook sink requires an http(s) URL, got %q", line(s.TargetLine), s.Target))
		}
	default:
		errs = append(errs, fmt.Sprintf("audit.sink.type%s: %q is not file|syslog|webhook", line(s.TypeLine), s.Type))
	}
	if s.QueueSize < 0 || s.QueueSize > 1_000_000 {
		errs = append(errs, fmt.Sprintf("audit.sink.queue_size: %d must be within [0, 1000000]", s.QueueSize))
	}
	if s.TimeoutMs < 0 || s.TimeoutMs > 60000 {
		errs = append(errs, fmt.Sprintf("audit.sink.timeout_ms: %d must be within [0, 60000]", s.TimeoutMs))
	}
	return errs
}

// validateTechniques checks the [rules.techniques] map (v0.7.0): keys are
// user patterns (must compile), values are MITRE ATT&CK tags
// (T\\d{4}(.\\d{3})?).
func (p *Policy) validateTechniques() []string {
	var errs []string
	tagShape := regexp.MustCompile(`^T\d{4}(\.\d{3})?$`)
	for pat, tag := range p.Rules.Techniques {
		if _, err := regexp.Compile(pat); err != nil {
			errs = append(errs, fmt.Sprintf("rules.techniques[%q]: invalid pattern regexp: %v", pat, err))
		}
		if !tagShape.MatchString(tag) {
			errs = append(errs, fmt.Sprintf("rules.techniques[%q]: %q is not a MITRE ATT&CK technique tag (T\\d{4} or T\\d{4}.\\d{3})", pat, tag))
		}
	}
	return errs
}

// globToRegexpSource mirrors internal/rules.globToRegexp's translation for
// validation only (the actual matcher lives in rules).
func globToRegexpSource(glob string) string {
	out := make([]byte, 0, len(glob)+8)
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch {
		case c == '*':
			out = append(out, '[', '^', '/', ']', '*')
		case c == '?':
			out = append(out, '[', '^', '/', ']')
		default:
			out = append(out, regexp.QuoteMeta(string(c))...)
		}
	}
	return string(out)
}

// syntaxError extracts a line number from a TOML parse error when available
// (AC0.3.7: validate reports line numbers for schema errors).
func syntaxError(err error) string {
	var pe toml.ParseError
	if errors.As(err, &pe) {
		return pe.ErrorWithPosition()
	}
	return err.Error()
}

// Hash returns a stable digest of the resolved policy — the reload
// detector compares this across hook invocations (PRD §7 0.3.0
// policy_reload audit events).
func (p *Policy) Hash() (string, error) {
	b, err := toml.Marshal(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func joinLines(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += "\n  "
		}
		out += s
	}
	return out
}
