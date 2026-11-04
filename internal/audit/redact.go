package audit

import (
	"regexp"
	"strings"
)

// Redaction rules (PRD §7 0.3.0): strip Authorization headers, token=/key=
// query-style secrets, long base64 blobs and private key blocks. Applied in
// order — the private-key pattern first, so a key's base64 body never
// trips the blob rule and leaves partial material behind.
var (
	privateKeyRE = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)
	authzRE      = regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(bearer\s+)?\S+`)
	tokenRE      = regexp.MustCompile(`(?i)\b((?:api[_-]?key|access[_-]?token|token|secret|password|passwd|bearer)\s*[:=]\s*)("[^"\s]*"|'[^'\s]*'|[^\s;&|'"]+)`)
	base64RE     = regexp.MustCompile(`\b[A-Za-z0-9+/]{40,}={0,2}\b`)
)

// Redact returns the verbose-only audit view of raw input with secret
// material replaced. It is applied only when --audit-verbose is set; the
// default record carries no input text at all.
func Redact(raw string) string {
	out := privateKeyRE.ReplaceAllString(raw, "[REDACTED-PRIVATE-KEY]")
	out = authzRE.ReplaceAllString(out, "${1}[REDACTED]")
	out = tokenRE.ReplaceAllString(out, "${1}[REDACTED]")
	out = base64RE.ReplaceAllString(out, "[REDACTED-BASE64]")
	return strings.TrimRight(out, "\n")
}
