package tui

import (
	"image/color"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestCompactCount(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1K", 12345: "12.3K", 999_949: "999.9K", 1_200_000: "1.2M"} {
		if got := compactCount(n); got != want {
			t.Errorf("compactCount(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestLayoutRowIsExactWidth(t *testing.T) {
	th := newTheme(true)
	left := []seg{
		{text: " "},
		{text: "a-rather-long-skill-name", style: th.bold, hl: []int{0, 2, 30}, hlStyle: th.accented},
		{text: "  collection", style: th.hint},
	}
	right := []seg{{text: "global", style: th.accented}, {text: "  "}, {text: "link", style: th.dim}, {text: " "}}
	for _, w := range []int{10, 20, 30, 44, 80} {
		for _, bg := range []color.Color{nil, th.selection} {
			if got := ansi.StringWidth(layoutRow(w, bg, left, right)); got != w {
				t.Errorf("width %d (bg %v): row is %d wide", w, bg, got)
			}
		}
	}
	// The state word (last) survives when badges are dropped.
	row := ansi.Strip(layoutRow(30, nil, left, right))
	if !strings.Contains(row, "link") || strings.Contains(row, "global") {
		t.Errorf("narrow row = %q", row)
	}
}

func TestRenderMarkdownKeepsOnlySGR(t *testing.T) {
	md := "# Title\n\nclear &#27;[2J and title &#27;]0;x&#7; here"
	out := renderMarkdown(md, 60, newTheme(true))
	rest := regexp.MustCompile("\x1b\\[[0-9;:]*m").ReplaceAllString(out, "")
	if strings.ContainsAny(rest, "\x1b\x07") {
		t.Fatalf("rendered markdown holds escapes other than SGR: %q", out)
	}
	if !strings.Contains(ansi.Strip(out), "here") {
		t.Fatalf("text lost: %q", out)
	}

	// An SGR with more parameters than the screen renderer's parser holds
	// would crash it.
	out = renderMarkdown("&#27;["+strings.Repeat("1;", 40)+"mhello", 60, newTheme(true))
	p := ansi.NewParser()
	var state byte
	for rest := out; len(rest) > 0; {
		_, _, n, next := ansi.DecodeSequence(rest, state, p)
		state, rest = next, rest[n:]
	}
	if !strings.Contains(ansi.Strip(out), "hello") {
		t.Fatalf("text lost: %q", out)
	}
}
