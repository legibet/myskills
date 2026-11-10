package tui

import (
	"fmt"
	"image/color"
	"math"
	"strconv"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	gansi "charm.land/glamour/v2/ansi"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
)

// termColors is what the terminal reports about its colors. The theme is
// derived from it, so that the app looks like part of the terminal's own
// color scheme.
type termColors struct {
	fg, bg  color.Color // nil until the terminal answers, and when it never does
	dark    bool
	profile colorprofile.Profile
}

func newTermColors() termColors {
	return termColors{dark: true, profile: colorprofile.TrueColor}
}

// requestTermColors asks the terminal for its default colors; the answers
// arrive as messages for termColors.update.
func requestTermColors() tea.Cmd {
	return tea.Batch(tea.RequestForegroundColor, tea.RequestBackgroundColor)
}

// update records a report about the terminal's colors and says whether msg
// was one.
func (c *termColors) update(msg tea.Msg) bool {
	switch msg := msg.(type) {
	case tea.ForegroundColorMsg:
		c.fg = msg.Color
	case tea.BackgroundColorMsg:
		c.bg, c.dark = msg.Color, msg.IsDark()
	case tea.ColorProfileMsg:
		c.profile = msg.Profile
	default:
		return false
	}
	return true
}

// theme is the palette of the app. Colors that carry meaning (the accent,
// success, warning, danger) are the terminal's own ANSI colors, so they follow
// its color scheme. Neutral tones (secondary text, rules, the selection) are
// blends of the terminal's foreground and background; when the terminal does
// not report those, or shows 16 colors or fewer, they fall back to text
// attributes: faint text and a reversed selection.
type theme struct {
	key string // identifies the palette, for caching what it renders

	dark   bool
	tinted bool // neutral tones are blended colors, not attributes

	accent                 color.Color
	muted, faint, border   color.Color // nil when not tinted
	selectionBg, codeBg    color.Color // nil when not tinted
	selected               lipgloss.Style
	text, bold, title      lipgloss.Style
	accented, dim, hint    lipgloss.Style // hint: placeholders, inactive marks
	rule                   lipgloss.Style
	good, warn, bad, modal lipgloss.Style
}

func newTheme(c termColors) theme {
	t := theme{
		dark:   c.dark,
		tinted: c.fg != nil && c.bg != nil && c.profile >= colorprofile.ANSI256,
		accent: lipgloss.Blue,
	}
	s := lipgloss.NewStyle()
	if t.tinted {
		mix := func(pct int) color.Color {
			m := lipgloss.Blend1D(101, c.bg, c.fg)[pct] // pct percent of the way from the background to the text color, in CIELAB
			if c.profile == colorprofile.ANSI256 {
				return gray256(m)
			}
			return m
		}
		t.muted = mix(62)
		t.faint = mix(40)
		t.border = mix(20)
		t.selectionBg = mix(14)
		t.codeBg = mix(9)
		t.dim = s.Foreground(t.muted)
		t.hint = s.Foreground(t.faint)
		t.rule = s.Foreground(t.border)
		t.selected = s.Background(t.selectionBg)
		t.key = fmt.Sprintf("%s/%s/%d", hex(c.fg), hex(c.bg), c.profile)
	} else {
		t.dim = s.Faint(true)
		t.hint = s.Faint(true)
		t.rule = s.Faint(true)
		t.selected = s.Reverse(true)
		t.key = "attrs"
	}
	t.text = s
	t.bold = s.Bold(true)
	t.title = s.Bold(true).Foreground(t.accent)
	t.accented = s.Foreground(t.accent)
	t.good = s.Foreground(lipgloss.Green)
	t.warn = s.Foreground(lipgloss.Yellow)
	t.bad = s.Foreground(lipgloss.Red)
	t.modal = s.Border(lipgloss.RoundedBorder()).BorderForeground(t.accent).Padding(1, 2).Margin(0, 1) // the margin keeps background text off the border
	return t
}

// gray256 returns the gray of the 256-color palette closest in lightness to
// c. Left to the downsampling, a gray with a tint of the terminal's
// background can turn into a saturated color of the palette's color cube.
func gray256(c color.Color) color.Color {
	r, g, b, _ := c.RGBA()
	y := (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 0x101
	switch {
	case y < 4:
		return lipgloss.ANSIColor(16)
	case y > 250:
		return lipgloss.ANSIColor(231)
	}
	return lipgloss.ANSIColor(232 + min(23, max(0, int(math.Round((y-8)/10))))) // the ramp runs from 8 to 238 in steps of 10
}

// hex formats a color as #rrggbb.
func hex(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}

// colorString formats a color for glamour: a palette index as is, so that it
// stays one, anything else as #rrggbb.
func colorString(c color.Color) string {
	if i, ok := c.(lipgloss.ANSIColor); ok {
		return strconv.Itoa(int(i))
	}
	return hex(c)
}

// helpStyles styles the help bar: keys in the default color, descriptions muted.
func (t theme) helpStyles() help.Styles {
	return help.Styles{
		ShortKey:       t.bold,
		ShortDesc:      t.dim,
		ShortSeparator: t.hint,
		Ellipsis:       t.hint,
		FullKey:        t.bold,
		FullDesc:       t.dim,
		FullSeparator:  t.hint,
	}
}

// inputStyles styles text inputs: accent prompt, faint placeholder.
func (t theme) inputStyles() textinput.Styles {
	st := textinput.DefaultStyles(t.dark)
	for _, s := range []*textinput.StyleState{&st.Focused, &st.Blurred} {
		s.Prompt = t.accented
		s.Text = t.text
		s.Placeholder = t.hint
	}
	st.Blurred.Prompt = t.dim
	st.Cursor.Color = t.accent
	st.Cursor.Blink = false
	return st
}

// markdown is the glamour style for SKILL.md previews, in the theme's
// colors: no document margins, headings in the accent color without
// markers, inline code on a light tint, code blocks indented and not
// highlighted (a highlighter would bring its own colors).
func (t theme) markdown() gansi.StyleConfig {
	str := func(s string) *string { return &s }
	yes, no, zero, two := new(true), new(false), new(uint(0)), new(uint(2))
	accent := str("4")
	var muted, faint, codeBg *string
	if t.tinted {
		muted, faint, codeBg = str(colorString(t.muted)), str(colorString(t.faint)), str(colorString(t.codeBg))
	}
	dim := gansi.StylePrimitive{Color: muted, Faint: boolIf(!t.tinted)}
	hint := gansi.StylePrimitive{Color: faint, Faint: boolIf(!t.tinted)}

	link := hint
	link.Underline = yes
	quote := dim
	code := gansi.StylePrimitive{BackgroundColor: codeBg}
	if !t.tinted {
		code = gansi.StylePrimitive{Bold: yes}
	}
	rule := hint
	rule.Format = "\n────────\n"
	return gansi.StyleConfig{
		Document:              gansi.StyleBlock{Margin: zero},
		Paragraph:             gansi.StyleBlock{},
		BlockQuote:            gansi.StyleBlock{StylePrimitive: quote, Indent: new(uint(1)), IndentToken: str("│ ")},
		List:                  gansi.StyleList{LevelIndent: 2},
		Heading:               gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{BlockSuffix: "\n", Color: accent, Bold: yes}},
		H1:                    gansi.StyleBlock{},
		H2:                    gansi.StyleBlock{},
		H3:                    gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Color: str("")}},
		H4:                    gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Color: str("")}},
		H5:                    gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Color: str("")}},
		H6:                    gansi.StyleBlock{StylePrimitive: gansi.StylePrimitive{Color: str(""), Bold: no}},
		Strikethrough:         gansi.StylePrimitive{CrossedOut: yes},
		Emph:                  gansi.StylePrimitive{Italic: yes},
		Strong:                gansi.StylePrimitive{Bold: yes},
		HorizontalRule:        rule,
		Item:                  gansi.StylePrimitive{BlockPrefix: "• "},
		Enumeration:           gansi.StylePrimitive{BlockPrefix: ". "},
		Task:                  gansi.StyleTask{Ticked: "[✓] ", Unticked: "[ ] "},
		Link:                  link,
		LinkText:              gansi.StylePrimitive{Color: accent},
		Image:                 link,
		ImageText:             gansi.StylePrimitive{Color: faint, Faint: boolIf(!t.tinted), Format: "Image: {{.text}}"},
		Code:                  gansi.StyleBlock{StylePrimitive: code},
		CodeBlock:             gansi.StyleCodeBlock{StyleBlock: gansi.StyleBlock{Margin: two}},
		Table:                 gansi.StyleTable{},
		DefinitionList:        gansi.StyleBlock{},
		DefinitionTerm:        gansi.StylePrimitive{Bold: yes},
		DefinitionDescription: gansi.StylePrimitive{BlockPrefix: "\n  "},
		HTMLBlock:             gansi.StyleBlock{StylePrimitive: hint},
		HTMLSpan:              gansi.StyleBlock{StylePrimitive: hint},
	}
}

// boolIf returns a pointer to true when b is set, nil otherwise.
func boolIf(b bool) *bool {
	if b {
		return new(true)
	}
	return nil
}
