package tui

import (
	"regexp"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
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
	th := newTheme(newTermColors())
	tinted := newTheme(termColors{fg: lipgloss.Color("#dddddd"), bg: lipgloss.Color("#202020"), profile: colorprofile.TrueColor})
	left := []seg{
		{text: " "},
		{text: padRight("a-rather-long-skill-name", 30), style: th.bold, hl: []int{0, 2, 30}, hlStyle: th.accented},
		{text: "  collection", style: th.hint},
	}
	right := []seg{{text: "global", style: th.accented}, {text: "  "}, {text: "link", style: th.dim}, {text: " "}}
	for _, w := range []int{10, 20, 30, 44, 80} {
		for _, base := range []lipgloss.Style{lipgloss.NewStyle(), th.selected, tinted.selected} {
			if got := ansi.StringWidth(layoutRow(w, base, left, right)); got != w {
				t.Errorf("width %d (base %v): row is %d wide", w, base, got)
			}
		}
	}
	// The state word (last) survives when badges are dropped.
	row := ansi.Strip(layoutRow(30, lipgloss.NewStyle(), left, right))
	if !strings.Contains(row, "link") || strings.Contains(row, "global") {
		t.Errorf("narrow row = %q", row)
	}
	// Short of room, the padding of the name column goes before the name.
	row = ansi.Strip(layoutRow(40, lipgloss.NewStyle(), left, right))
	if !strings.HasPrefix(row, " a-rather-long-skill-name ") || strings.Contains(row, "…") {
		t.Errorf("row = %q", row)
	}
}

// Neutral tones blend the terminal's colors when it reports them and shows
// 256 colors or more; with 256, they are grays of the palette.
func TestThemeFollowsTerminal(t *testing.T) {
	fg, bg := lipgloss.Color("#cdd6f4"), lipgloss.Color("#1e1e2e")
	for _, tt := range []struct {
		c      termColors
		tinted bool
	}{
		{newTermColors(), false},
		{termColors{fg: fg, bg: bg, profile: colorprofile.ANSI}, false},
		{termColors{fg: fg, bg: bg, profile: colorprofile.ANSI256}, true},
		{termColors{fg: fg, bg: bg, profile: colorprofile.TrueColor}, true},
	} {
		th := newTheme(tt.c)
		if th.tinted != tt.tinted || (th.selectionBg != nil) != tt.tinted || th.selected.GetReverse() == tt.tinted {
			t.Errorf("%v: tinted = %v, selection %v", tt.c.profile, th.tinted, th.selectionBg)
		}
		if tt.c.profile == colorprofile.ANSI256 {
			if i, ok := th.selectionBg.(lipgloss.ANSIColor); !ok || i < 232 {
				t.Errorf("256 colors: selection = %v, want a gray of the palette", th.selectionBg)
			}
		}
	}
}

func TestRenderMarkdownKeepsOnlySGR(t *testing.T) {
	md := "# Title\n\nclear &#27;[2J and title &#27;]0;x&#7; here"
	out := renderMarkdown(md, 60, newTheme(newTermColors()))
	rest := regexp.MustCompile("\x1b\\[[0-9;:]*m").ReplaceAllString(out, "")
	if strings.ContainsAny(rest, "\x1b\x07") {
		t.Fatalf("rendered markdown holds escapes other than SGR: %q", out)
	}
	if !strings.Contains(ansi.Strip(out), "here") {
		t.Fatalf("text lost: %q", out)
	}

	// An SGR with more parameters than the screen renderer's parser holds
	// would crash it.
	out = renderMarkdown("&#27;["+strings.Repeat("1;", 40)+"mhello", 60, newTheme(newTermColors()))
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
