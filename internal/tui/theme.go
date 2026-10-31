package tui

import (
	"fmt"
	"image/color"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/textinput"
	gansi "charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
	"charm.land/lipgloss/v2"
)

// theme is the single palette of the app, resolved for a light or dark
// terminal background. Bubble Tea downsamples the colors to the terminal's
// color profile, so NO_COLOR and 16-color terminals are handled there.
type theme struct {
	dark bool

	accent, muted, faint, border, selection, success, warning, danger color.Color

	text     lipgloss.Style // default foreground
	bold     lipgloss.Style
	title    lipgloss.Style // app name
	accented lipgloss.Style
	dim      lipgloss.Style // secondary text
	hint     lipgloss.Style // placeholders, separators, inactive glyphs
	rule     lipgloss.Style
	good     lipgloss.Style
	warn     lipgloss.Style
	bad      lipgloss.Style
	modal    lipgloss.Style // modal box
}

func newTheme(dark bool) theme {
	ld := lipgloss.LightDark(dark)
	c := lipgloss.Color
	t := theme{
		dark:      dark,
		accent:    ld(c("#5A55D6"), c("#9D9BFF")),
		muted:     ld(c("#62626E"), c("#A0A0AE")),
		faint:     ld(c("#9A9AA6"), c("#686876")),
		border:    ld(c("#DCDCE4"), c("#363642")),
		selection: ld(c("#ECECF7"), c("#2A2A3B")),
		success:   ld(c("#2F8A4C"), c("#7BCB8F")),
		warning:   ld(c("#A86A00"), c("#E6BC6E")),
		danger:    ld(c("#C2403A"), c("#F2857E")),
	}
	s := lipgloss.NewStyle()
	t.text = s
	t.bold = s.Bold(true)
	t.title = s.Bold(true).Foreground(t.accent)
	t.accented = s.Foreground(t.accent)
	t.dim = s.Foreground(t.muted)
	t.hint = s.Foreground(t.faint)
	t.rule = s.Foreground(t.border)
	t.good = s.Foreground(t.success)
	t.warn = s.Foreground(t.warning)
	t.bad = s.Foreground(t.danger)
	t.modal = s.Border(lipgloss.RoundedBorder()).BorderForeground(t.accent).Padding(1, 2).Margin(0, 1) // the margin keeps background text off the border
	return t
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

// markdown is the glamour style for SKILL.md previews: the stock light or
// dark style without document margins, with headings in the accent color.
func (t theme) markdown() gansi.StyleConfig {
	cfg := styles.DarkStyleConfig
	if !t.dark {
		cfg = styles.LightStyleConfig
	}
	zero := uint(0)
	accent := hex(t.accent)
	onAccent := hex(lipgloss.LightDark(t.dark)(lipgloss.Color("#FFFFFF"), lipgloss.Color("#16161E")))
	cfg.Document.Margin = &zero
	cfg.Document.BlockPrefix = ""
	cfg.Document.BlockSuffix = ""
	cfg.Document.Color = nil
	cfg.Heading.Color = &accent
	cfg.H1.Color = &onAccent
	cfg.H1.BackgroundColor = &accent
	return cfg
}

// hex formats a color as #rrggbb for glamour.
func hex(c color.Color) string {
	r, g, b, _ := c.RGBA()
	return fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
}
