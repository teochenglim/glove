package decision

import "strings"

// Segment is one piece of a command chain. Via is the operator that started
// it ("" for the first, then "&&", "||", ";", "|", "&", "\n"). A balanced
// command substitution appears as its own inner segment right after the
// segment that contains it; an UNBALANCED one stays inside its outer segment
// and marks it Compound "subshell-unbalanced" — Layer 0 cannot see what an
// opaque substitution will run, so it reviews instead of claiming.
type Segment struct {
	Text     string
	Via      string
	Compound string // "", "subshell", "subshell-unbalanced"
	inner    bool   // command-substitution body (evaluated, excluded from pipe pairing)
}

const (
	compoundNone       = ""
	compoundSubshell   = "subshell"
	compoundUnbalanced = "subshell-unbalanced"
)

// Split splits a command into segments on the top-level operators `&&`, `||`,
// `;`, `|`, `&` and newline. Operators inside quotes do not split. `$(` opens
// a command substitution even inside double quotes — real shells expand it
// there — and its body is extracted and evaluated too; inside single quotes
// it stays literal. Segment text is preserved verbatim (outer whitespace
// trimmed).
func Split(cmd string) []Segment {
	var segs []Segment
	var buf strings.Builder
	var inners []string
	via := ""
	depth := 0 // open command substitutions

	flush := func() {
		text := strings.TrimSpace(buf.String())
		buf.Reset()
		if text != "" {
			compound := compoundNone
			if depth > 0 {
				compound = compoundUnbalanced
			}
			segs = append(segs, Segment{Text: text, Via: via, Compound: compound})
		}
		for _, in := range inners {
			segs = append(segs, Segment{Text: in, Via: "$()", Compound: compoundSubshell, inner: true})
		}
		inners = nil
		via = ""
	}

	runes := []rune(cmd)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '\'' && depth == 0:
			// Single quotes are literal: operators and $( inside do not act.
			buf.WriteRune(r)
			for i+1 < len(runes) {
				i++
				buf.WriteRune(runes[i])
				if runes[i] == '\'' {
					break
				}
			}
		case r == '"' && depth == 0:
			// Double quotes stop operators but NOT expansion.
			buf.WriteRune(r)
			for i+1 < len(runes) {
				i++
				if runes[i] == '$' && i+1 < len(runes) && runes[i+1] == '(' {
					var closed bool
					i, closed = scanSubshell(runes, i, &buf, &inners)
					if !closed {
						depth++
					}
					continue
				}
				buf.WriteRune(runes[i])
				if runes[i] == '"' {
					break
				}
			}
		case r == '$' && i+1 < len(runes) && runes[i+1] == '(':
			var closed bool
			i, closed = scanSubshell(runes, i, &buf, &inners)
			if !closed {
				depth++
			}
		case r == '&' && i+1 < len(runes) && runes[i+1] == '&':
			flush()
			via = "&&"
			i++
		case r == '|' && i+1 < len(runes) && runes[i+1] == '|':
			flush()
			via = "||"
			i++
		case r == ';' || r == '|':
			flush()
			via = string(r)
		case r == '&':
			flush()
			via = "&"
		case r == '\n':
			flush()
			via = "\n"
		default:
			buf.WriteRune(r)
		}
	}
	flush()
	return segs
}

// scanSubshell consumes a $(...) starting at runes[i] ('$'), appending the
// literal "$(...)" to buf and its BODY to inners. closed reports whether the
// closing ')' was found; when false, the caller marks the segment
// subshell-unbalanced (the substitution swallows the rest of the command).
func scanSubshell(runes []rune, i int, buf *strings.Builder, inners *[]string) (int, bool) {
	var body strings.Builder
	depth := 0
	for ; i < len(runes); i++ {
		r := runes[i]
		buf.WriteRune(r)
		if r == '$' && i+1 < len(runes) && runes[i+1] == '(' {
			buf.WriteRune('(')
			depth++
			if depth >= 2 {
				body.WriteString("$(") // nested substitution stays literal in the body
			}
			i++
			continue
		}
		switch r {
		case '(':
			depth++ // plain paren inside the body
			body.WriteRune(r)
		case ')':
			depth--
			if depth == 0 {
				*inners = append(*inners, strings.TrimSpace(body.String()))
				return i, true
			}
			body.WriteRune(r)
		default:
			if depth >= 1 {
				body.WriteRune(r)
			}
		}
	}
	return i - 1, false
}

// isShellInvocation reports whether a segment's first word names a shell
// interpreter — the receiving end of a pipe into a shell.
func isShellInvocation(text string) bool {
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return false
	}
	name := fields[0]
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	return shellNames[name]
}

// shellNames are the interpreters a pipe into which is compound risk (PRD
// §7 0.2.0): `curl ... | sh` is only visible as a pair, not as a segment.
var shellNames = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "ash": true,
}
