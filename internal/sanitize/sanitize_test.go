package sanitize

import (
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

func TestText(t *testing.T) {
	tests := map[string]string{
		"plain":                       "plain",
		"two\nlines\tand tab":         "two\nlines\tand tab",
		"\x1b[31mred\x1b[0m":          "red",
		"clear\x1b[2J screen":         "clear screen",
		"\x1b]0;title\x07after":       "after",
		"\x1b]8;;http://x\x1b\\link":  "link",
		"bell\x07 cr\r del\x7f":       "bell cr del",
		"bidi\u202eevil\u2066x\u2069": "bidievilx",
		"c1 \u009b2J":                 "c1 2J",
		"lone esc\x1b":                "lone esc",
		"中文 ✓":                        "中文 ✓",
	}
	for in, want := range tests {
		if got := Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLine(t *testing.T) {
	if got, want := Line("  a\nb\tc\x1b[1m  "), "a b c"; got != want {
		t.Errorf("Line = %q, want %q", got, want)
	}
}

func TestStyled(t *testing.T) {
	tests := map[string]string{
		"\x1b[1;38;2;1;2;3mbold\x1b[0m":               "\x1b[1;38;2;1;2;3mbold\x1b[0m",
		"\x1b[4:3mcurly\x1b[m":                        "\x1b[4:3mcurly\x1b[m",
		"\x1b[31mred\x1b[2Jcleared":                   "\x1b[31mredcleared",
		"\x1b[?25lhidden cursor":                      "hidden cursor",
		"\x1b]0;title\x07text":                        "text",
		"\x1bPdcs\x1b\\text":                          "text",
		"ok\r\nnext":                                  "ok\nnext",
		"\x1b[" + strings.Repeat("1;", 40) + "mlong":  "long",
		"\x1b[" + strings.Repeat("1;", 31) + "1mmax":  "\x1b[" + strings.Repeat("1;", 31) + "1mmax",
		"\x1b[" + strings.Repeat("1:", 32) + "1mover": "over",
		"\x1b[>4;1mkeys":                              "keys",
	}
	for in, want := range tests {
		if got := Styled(in); got != want {
			t.Errorf("Styled(%q) = %q, want %q", in, got, want)
		}
	}
}

var sgr = regexp.MustCompile(`\x1b\[[0-9;:]*m`)

// FuzzFilter checks that no input panics and that nothing but SGR sequences
// and printable text (plus newline and tab) comes out.
func FuzzFilter(f *testing.F) {
	for _, s := range []string{
		"plain", "\x1b[31mred\x1b[0m", "\x1b]8;;http://x\x1b\\link", "\x1bPdcs\x1b\\",
		"\x1b[" + strings.Repeat("1;", 40) + "m", "\x1b[" + strings.Repeat("1;", 31) + "1m", "\x1b[" + strings.Repeat("9", 100) + "m",
		"\x1b]" + strings.Repeat("x", 5000), "\x1b[", "\x9b31m", "\xff\xfe", "a\x1b\x1b[2J[31m",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		check := func(name, out string) {
			for _, r := range out {
				if r != '\n' && r != '\t' && (unicode.IsControl(r) || isBidi(r)) {
					t.Fatalf("%s(%q) = %q keeps %U", name, s, out, r)
				}
			}
		}
		check("Text", Text(s))
		styled := Styled(s)
		check("Styled without SGR", sgr.ReplaceAllString(styled, ""))
		// What is kept must not overflow a terminal parser downstream.
		p := ansi.NewParser()
		var state byte
		for rest := styled; len(rest) > 0; {
			_, _, n, next := ansi.DecodeSequence(rest, state, p)
			state, rest = next, rest[n:]
		}
		if again := Styled(styled); again != styled {
			t.Fatalf("Styled is not idempotent on %q: %q then %q", s, styled, again)
		}
	})
}
