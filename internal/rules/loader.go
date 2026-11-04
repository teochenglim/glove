// Package rules holds Glove's Layer 0 rule set: hard-block and force-confirm
// regexes plus protected-path globs, shipped as embedded builtin.yaml
// (PRD Appendix B). Users extend via policy.toml from v0.7.0; until then the
// builtin set is the only source, and it must always load (a load failure in
// production is a build bug — the CLI treats it as fail-closed).
package rules

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed builtin.yaml
var builtinFS embed.FS

// RegexRule is a compiled {id, pattern, reason} rule (PRD Appendix B format).
type RegexRule struct {
	ID     string
	Reason string
	RE     *regexp.Regexp
}

// PathRule is a compiled protected-path glob; `**` spans directories, `*`
// and `?` stay within one.
type PathRule struct {
	Pattern string
	Reason  string
	RE      *regexp.Regexp
}

// RuleSet is a fully compiled rule set, safe for concurrent reads.
type RuleSet struct {
	HardBlock    []RegexRule
	ForceConfirm []RegexRule
	Composition  []RegexRule // v0.2.0: matched per chain segment and per pipe pair
	Protected    []PathRule
}

type regexSpec struct {
	ID      string `yaml:"id"`
	Pattern string `yaml:"pattern"`
	Reason  string `yaml:"reason"`
}

type pathSpec struct {
	Pattern string `yaml:"pattern"`
	Reason  string `yaml:"reason"`
}

type fileSpec struct {
	HardBlock      []regexSpec `yaml:"hard_block"`
	ForceConfirm   []regexSpec `yaml:"force_confirm"`
	Composition    []regexSpec `yaml:"composition"`
	ProtectedPaths []pathSpec  `yaml:"protected_paths"`
}

// LoadBuiltin parses the embedded builtin.yaml.
func LoadBuiltin() (*RuleSet, error) {
	data, err := builtinFS.ReadFile("builtin.yaml")
	if err != nil {
		return nil, fmt.Errorf("read embedded builtin rules: %w", err)
	}
	return Load(data)
}

// Load parses a rule set in builtin.yaml format.
func Load(data []byte) (*RuleSet, error) {
	var f fileSpec
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse rules: %w", err)
	}
	rs := &RuleSet{}
	for _, s := range f.HardBlock {
		re, err := regexp.Compile(s.Pattern)
		if err != nil {
			return nil, fmt.Errorf("hard_block %s: bad pattern: %w", s.ID, err)
		}
		rs.HardBlock = append(rs.HardBlock, RegexRule{ID: s.ID, Reason: s.Reason, RE: re})
	}
	for _, s := range f.ForceConfirm {
		re, err := regexp.Compile(s.Pattern)
		if err != nil {
			return nil, fmt.Errorf("force_confirm %s: bad pattern: %w", s.ID, err)
		}
		rs.ForceConfirm = append(rs.ForceConfirm, RegexRule{ID: s.ID, Reason: s.Reason, RE: re})
	}
	for _, s := range f.Composition {
		re, err := regexp.Compile(s.Pattern)
		if err != nil {
			return nil, fmt.Errorf("composition %s: bad pattern: %w", s.ID, err)
		}
		rs.Composition = append(rs.Composition, RegexRule{ID: s.ID, Reason: s.Reason, RE: re})
	}
	for _, s := range f.ProtectedPaths {
		re, err := globToRegexp(s.Pattern)
		if err != nil {
			return nil, fmt.Errorf("protected_paths %s: bad glob: %w", s.Pattern, err)
		}
		rs.Protected = append(rs.Protected, PathRule{Pattern: s.Pattern, Reason: s.Reason, RE: re})
	}
	if len(rs.HardBlock) == 0 && len(rs.ForceConfirm) == 0 && len(rs.Protected) == 0 {
		return nil, fmt.Errorf("rule set is empty")
	}
	return rs, nil
}

// globToRegexp translates a protected-path glob into a full-match regexp:
// `**/` becomes `(.*/)?`, bare `**` becomes `.*`, `*` -> `[^/]*`, `?` -> `[^/]`.
func globToRegexp(glob string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(glob); {
		switch c := glob[i]; c {
		case '*':
			if i+1 < len(glob) && glob[i+1] == '*' {
				if i+2 < len(glob) && glob[i+2] == '/' {
					b.WriteString("(.*/)?")
					i += 3
				} else {
					b.WriteString(".*")
					i += 2
				}
				continue
			}
			b.WriteString("[^/]*")
			i++
		case '?':
			b.WriteString("[^/]")
			i++
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
			i++
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// Extra carries the policy-provided pattern extensions (PRD §5.5
// rules.hard_block / rules.force_confirm / paths.protected).
type Extra struct {
	HardBlock    []string
	ForceConfirm []string
	Protected    []string
}

// Compose extends a base rule set (usually the builtin one) with policy
// patterns (PRD §7 0.3.0: user extensions are enforced without restarting
// anything — every hook process re-composes at startup).
func Compose(base *RuleSet, extra Extra) (*RuleSet, error) {
	rs := &RuleSet{
		HardBlock:    append([]RegexRule(nil), base.HardBlock...),
		ForceConfirm: append([]RegexRule(nil), base.ForceConfirm...),
		Composition:  append([]RegexRule(nil), base.Composition...),
		Protected:    append([]PathRule(nil), base.Protected...),
	}
	for i, pat := range extra.HardBlock {
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("policy hard_block[%d]: %w", i, err)
		}
		rs.HardBlock = append(rs.HardBlock, RegexRule{ID: fmt.Sprintf("policy_hard_%d", i), Reason: "user policy pattern", RE: re})
	}
	for i, pat := range extra.ForceConfirm {
		re, err := regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("policy force_confirm[%d]: %w", i, err)
		}
		rs.ForceConfirm = append(rs.ForceConfirm, RegexRule{ID: fmt.Sprintf("policy_force_%d", i), Reason: "user policy pattern", RE: re})
	}
	for i, pat := range extra.Protected {
		re, err := globToRegexp(pat)
		if err != nil {
			return nil, fmt.Errorf("policy protected[%d]: %w", i, err)
		}
		rs.Protected = append(rs.Protected, PathRule{Pattern: pat, Reason: "user policy protected path", RE: re})
	}
	return rs, nil
}

// FirstHardHit returns the first hard-block rule matching s, or nil.
func (rs *RuleSet) FirstHardHit(s string) *RegexRule {
	for i := range rs.HardBlock {
		if rs.HardBlock[i].RE.MatchString(s) {
			return &rs.HardBlock[i]
		}
	}
	return nil
}

// FirstForceHit returns the first force-confirm rule matching s, or nil.
func (rs *RuleSet) FirstForceHit(s string) *RegexRule {
	for i := range rs.ForceConfirm {
		if rs.ForceConfirm[i].RE.MatchString(s) {
			return &rs.ForceConfirm[i]
		}
	}
	return nil
}

// CompositionHits returns every composition rule matching s (a chain segment
// or a joined pipe pair). Unlike the single-hit rules above, all hits are
// reported so a construct that breaks several rules shows every reason.
func (rs *RuleSet) CompositionHits(s string) []RegexRule {
	var hits []RegexRule
	for _, r := range rs.Composition {
		if r.RE.MatchString(s) && !containsRegexRule(hits, r) {
			hits = append(hits, r)
		}
	}
	return hits
}

func containsRegexRule(hits []RegexRule, r RegexRule) bool {
	for _, h := range hits {
		if h.ID == r.ID {
			return true
		}
	}
	return false
}

// PathHits extracts path-looking tokens from s (a command or an input value),
// expands `~`, `$HOME`, `%USERPROFILE%` and `%LOCALAPPDATA%` (Windows hardening,
// §8), and returns every protected rule that matches. It is deliberately
// literal: composition tricks are Layer 1/2 territory (v0.2.0/v0.4.0); Layer 0
// only claims plain, visible paths.
func (rs *RuleSet) PathHits(s string) []PathRule {
	home := func() string {
		h, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return h
	}()
	localAppData := os.Getenv("LOCALAPPDATA")
	var hits []PathRule
	seen := map[string]bool{}
	try := func(p string) {
		if p == "" {
			return
		}
		for _, cand := range []string{p, filepath.Clean(p)} {
			if seen[cand] {
				continue
			}
			seen[cand] = true
			for _, r := range rs.Protected {
				if r.RE.MatchString(cand) && !containsRule(hits, r) {
					hits = append(hits, r)
				}
			}
		}
	}
	for _, tok := range candidatePaths(s) {
		t := tok
		if home != "" {
			if strings.HasPrefix(t, "~/") || t == "~" {
				t = filepath.Join(home, strings.TrimPrefix(t, "~"))
			}
			t = strings.ReplaceAll(t, "$HOME", home)
			t = strings.ReplaceAll(t, `%USERPROFILE%`, home)
		}
		if localAppData != "" {
			t = strings.ReplaceAll(t, `%LOCALAPPDATA%`, localAppData)
		}
		try(tok)
		try(t)
		// Windows tokens arrive with backslash separators; protected globs
		// are slash-shaped. Normalize separators for an extra candidate —
		// string-level, so the behavior is identical on every OS, and it
		// only ever adds candidates (tighten-only).
		try(strings.ReplaceAll(t, "\\", "/"))
	}
	return hits
}

func containsRule(hits []PathRule, r PathRule) bool {
	for _, h := range hits {
		if h.Pattern == r.Pattern {
			return true
		}
	}
	return false
}

// candidatePaths splits s into path-looking tokens. Separators are whitespace
// and shell punctuation that rarely appears inside a plain path; quoted
// strings therefore come out intact.
func candidatePaths(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', ';', '|', '&', '(', ')', '<', '>', '"', '\'', '=', ':', '`':
			return true
		}
		return false
	})
	var out []string
	for _, f := range fields {
		// Path-like: contains a slash (either separator), is home-relative
		// in POSIX or Windows env-var form, or is a dotfile reference such
		// as `.env` (protected globs match bare names too).
		dotfile := strings.HasPrefix(f, ".") && f != "." && f != ".."
		if strings.Contains(f, "/") || strings.Contains(f, "\\") ||
			strings.HasPrefix(f, "~") || strings.HasPrefix(f, "$HOME") ||
			strings.HasPrefix(f, "%") || dotfile {
			out = append(out, f)
		}
	}
	return out
}

// StringValues walks a decoded JSON document and returns every string value
// found in it (used to scan non-shell tool inputs for protected paths).
func StringValues(v any) []string {
	var out []string
	var walk func(x any)
	walk = func(x any) {
		switch t := x.(type) {
		case string:
			out = append(out, t)
		case map[string]any:
			for _, vv := range t {
				walk(vv)
			}
		case []any:
			for _, vv := range t {
				walk(vv)
			}
		}
	}
	walk(v)
	return out
}
