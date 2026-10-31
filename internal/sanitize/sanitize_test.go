package sanitize

import "testing"

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
		"\x1b[1;38;2;1;2;3mbold\x1b[0m": "\x1b[1;38;2;1;2;3mbold\x1b[0m",
		"\x1b[4:3mcurly\x1b[m":          "\x1b[4:3mcurly\x1b[m",
		"\x1b[31mred\x1b[2Jcleared":     "\x1b[31mredcleared",
		"\x1b[?25lhidden cursor":        "hidden cursor",
		"\x1b]0;title\x07text":          "text",
		"\x1bPdcs\x1b\\text":            "text",
		"ok\r\nnext":                    "ok\nnext",
	}
	for in, want := range tests {
		if got := Styled(in); got != want {
			t.Errorf("Styled(%q) = %q, want %q", in, got, want)
		}
	}
}
