// Package sanitize makes untrusted text safe to write to a terminal: skill
// names, descriptions and markdown, skills.sh results, file names found on
// disk, and errors that quote them.
package sanitize

import (
	"strings"
	"unicode"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/parser"
)

// Text removes escape sequences, control characters other than newline and
// tab, and bidirectional overrides.
func Text(s string) string {
	return filter(s, false)
}

// Line is Text on a single line: newlines and tabs become spaces, and the
// ends are trimmed.
func Line(s string) string {
	return strings.TrimSpace(strings.NewReplacer("\n", " ", "\t", " ").Replace(Text(s)))
}

// Styled is Text that keeps SGR sequences (colors and text attributes). It is
// for output that is already styled but may hold untrusted text decoded after
// sanitizing, such as Glamour turning "&#27;" in markdown into ESC.
func Styled(s string) string {
	return filter(s, true)
}

func filter(s string, keepSGR bool) string {
	var b strings.Builder
	var state byte
	for len(s) > 0 {
		// No parser: its fixed parameter buffer overflows on long untrusted
		// sequences, and isSGR reads the bytes instead.
		seq, _, n, next := ansi.DecodeSequence(s, state, nil)
		state, s = next, s[n:]
		switch {
		case keepSGR && isSGR(seq):
			b.WriteString(seq)
		case seq[0] == ansi.ESC || isC1(seq[0]):
			// Any other sequence: cursor movement, OSC titles and links, …
		default:
			for _, r := range seq {
				if r == '\n' || r == '\t' || !unicode.IsControl(r) && !isBidi(r) {
					b.WriteRune(r)
				}
			}
		}
	}
	return b.String()
}

// isSGR reports whether seq is ESC [ params m, with params only digits and
// separators: colors and text attributes. Longer parameter lists than
// terminal parsers hold (the TUI renderer's panics) are not kept.
func isSGR(seq string) bool {
	params, ok := strings.CutPrefix(seq, "\x1b[")
	if !ok {
		return false
	}
	params, ok = strings.CutSuffix(params, "m")
	return ok && strings.Trim(params, "0123456789;:") == "" &&
		strings.Count(params, ";")+strings.Count(params, ":") < parser.MaxParamsSize
}

// isC1 reports whether b starts a C1 control as a raw byte (not UTF-8).
func isC1(b byte) bool {
	return b >= 0x80 && b <= 0x9f
}

// isBidi reports whether r overrides the direction of the text around it.
func isBidi(r rune) bool {
	return r >= '\u202a' && r <= '\u202e' || r >= '\u2066' && r <= '\u2069'
}
