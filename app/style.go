package main

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The app's look: its colors and type, and the controls the pages are made
// of. Every page builds from these, never from the stock widgets' look, so
// that the pages read as one app.

// palette is the colors of one appearance. Text and fills are the label's
// color at an opacity, as macOS draws them, so they sit on any background.
type palette struct {
	text, text2, text3 ui.Color // primary, secondary and tertiary text

	content, sidebar ui.Color // the pane backgrounds
	separator        ui.Color // hairlines
	hover, pressed   ui.Color // the fill of a borderless control under the pointer
	fill             ui.Color // a push button's, a pop-up button's and a field's fill
	card             ui.Color // a group of rows
	navSelected      ui.Color // the page chosen in the sidebar
	rowInactive      ui.Color // the row chosen in a list without the focus

	green, orange, red ui.Color
}

var lightPalette = palette{
	text:        ui.RGBA(0, 0, 0, 0.86),
	text2:       ui.RGBA(0, 0, 0, 0.52),
	text3:       ui.RGBA(0, 0, 0, 0.3),
	content:     ui.Hex("#ffffff"),
	sidebar:     ui.Hex("#f3f3f4"),
	separator:   ui.RGBA(0, 0, 0, 0.08),
	hover:       ui.RGBA(0, 0, 0, 0.05),
	pressed:     ui.RGBA(0, 0, 0, 0.1),
	fill:        ui.RGBA(0, 0, 0, 0.055),
	card:        ui.RGBA(0, 0, 0, 0.028),
	navSelected: ui.RGBA(0, 0, 0, 0.07),
	rowInactive: ui.RGBA(0, 0, 0, 0.07),
	green:       ui.Hex("#28a745"),
	orange:      ui.Hex("#e08600"),
	red:         ui.Hex("#e5383b"),
}

var darkPalette = palette{
	text:        ui.RGBA(255, 255, 255, 0.88),
	text2:       ui.RGBA(255, 255, 255, 0.55),
	text3:       ui.RGBA(255, 255, 255, 0.3),
	content:     ui.Hex("#1e1e20"),
	sidebar:     ui.Hex("#252527"),
	separator:   ui.RGBA(255, 255, 255, 0.08),
	hover:       ui.RGBA(255, 255, 255, 0.06),
	pressed:     ui.RGBA(255, 255, 255, 0.12),
	fill:        ui.RGBA(255, 255, 255, 0.09),
	card:        ui.RGBA(255, 255, 255, 0.035),
	navSelected: ui.RGBA(255, 255, 255, 0.1),
	rowInactive: ui.RGBA(255, 255, 255, 0.1),
	green:       ui.Hex("#32d74b"),
	orange:      ui.Hex("#ff9f0a"),
	red:         ui.Hex("#ff453a"),
}

// The type scale, in DIPs.
const (
	textTitle   = 22 // an item's title in a detail pane
	textBody    = 13
	textSmall   = 12
	textCaption = 11
)

// barHeight is the height of the bars at the top of the panes, which the
// window controls sit in.
const barHeight = 52

// hoverAnimation is how fast a row's or a button's fill follows the pointer.
var hoverAnimation = ui.Animation{Duration: 110 * time.Millisecond}

func colors(c *ui.Context) *palette {
	if c.Theme().Dark {
		return &darkPalette
	}
	return &lightPalette
}

// accent is the desktop's accent color, which the default theme follows.
func accent(c *ui.Context) ui.Color { return c.Theme().Accent }

// applyTheme gives the stock widgets left (dialogs, menus, check boxes,
// switches, the markdown view) the app's colors and type.
func applyTheme(c *ui.Context) {
	p := colors(c)
	t := *c.Theme()
	t.Background = p.content
	t.Text = p.text
	t.TextMuted = p.text2
	t.Border = p.separator
	t.Surface = p.fill
	t.SurfaceHover = p.hover
	t.SurfacePressed = p.pressed
	t.AccentHover = t.Accent.Mix(ui.RGB(0, 0, 0), 0.12)
	t.AccentPressed = t.Accent.Mix(ui.RGB(0, 0, 0), 0.2)
	t.Danger, t.Warning, t.Success = p.red, p.orange, p.green
	t.Radius = 6
	scale := c.Preferences().TextScale
	if scale <= 0 {
		scale = 1
	}
	t.FontSize = textBody * scale
	c.SetTheme(&t)
}

// bar is the top of a pane: it drags the window and holds the pane's title
// and its actions, centered on the window controls.
func bar(c *ui.Context) ui.Element {
	return ui.Row(c).Height(barHeight).Shrink(0).AlignItems(ui.Center).Gap(6).DragWindow()
}

// paneTitle is a pane's title in its bar, with a detail under it.
func paneTitle(c *ui.Context, title, detail string) {
	p := colors(c)
	ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
		ui.Text(c, title).FontSize(textBody).FontWeight(700).SingleLine()
		if detail != "" {
			ui.Text(c, detail).FontSize(textCaption).TextColor(p.text2).SingleLine()
		}
	})
}

// buttonKind is a push button's weight.
type buttonKind int

const (
	plain buttonKind = iota
	primary
	destructive
)

// button is a push button: a flat rounded fill, the accent's for the
// primary action of a pane.
func button(c *ui.Context, title string, kind buttonKind) ui.Element {
	return iconTextButton(c, nil, title, kind)
}

// iconTextButton is a push button with an icon before its title.
func iconTextButton(c *ui.Context, icon *ui.SVG, title string, kind buttonKind) ui.Element {
	p := colors(c)
	b := ui.ButtonBase(c).Height(26).Padding(0, 12).Radius(6).Gap(6).Justify(ui.Center).Shrink(0).
		FontSize(textBody).FontWeight(500).TextColor(p.text).Label(title)
	fill := p.fill
	switch kind {
	case primary:
		fill = accent(c)
		b.TextColor(c.Theme().AccentText)
	case destructive:
		b.TextColor(p.red)
	}
	switch {
	case b.Pressed():
		fill = fill.Mix(p.text, 0.12)
	case b.Hovered():
		fill = fill.Mix(p.text, 0.06)
	}
	b.Background(fill).Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(14, 14)
		}
		ui.Text(c, title).SingleLine()
	})
	return b
}

// toolButton is a borderless icon button, named for assistive technology
// and in its tooltip; it fills under the pointer.
func toolButton(c *ui.Context, icon *ui.SVG, label string) ui.Element {
	p := colors(c)
	b := ui.ButtonBase(c).Size(28, 28).Shrink(0).Radius(6).Center().TextColor(p.text2).Label(label).Tooltip(label)
	bg := ui.Transparent
	switch {
	case b.Pressed():
		bg = p.pressed
	case b.Hovered():
		bg = p.hover
	}
	b.Background(b.AnimateColor("bg", bg, hoverAnimation)).Children(func() {
		ui.Icon(c, icon).Size(16, 16)
	})
	return b
}

// linkButton is an action written in the accent color, as in a row.
func linkButton(c *ui.Context, icon *ui.SVG, title string) ui.Element {
	b := ui.ButtonBase(c).Gap(5).AlignItems(ui.Center).FontSize(textBody).FontWeight(500).Label(title).Cursor(ui.CursorPointer)
	color := accent(c)
	if b.Hovered() {
		color = color.Mix(ui.RGB(0, 0, 0), 0.15)
	}
	b.TextColor(color).Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(14, 14)
		}
		ui.Text(c, title).SingleLine()
	})
	return b
}

// popUp is a button showing a value, which opens a menu or a popover to
// change it. label names what it chooses; its value is spoken with it.
func popUp(c *ui.Context, label, value string) ui.Element {
	p := colors(c)
	b := ui.ButtonBase(c).Height(24).Padding(0, 6, 0, 9).Radius(6).Gap(6).AlignItems(ui.Center).MinWidth(0).
		FontSize(textSmall).TextColor(p.text).Label(label).Value(value).Role(ui.RolePopUpButton)
	fill := p.fill
	switch {
	case b.Pressed():
		fill = fill.Mix(p.text, 0.1)
	case b.Hovered():
		fill = fill.Mix(p.text, 0.05)
	}
	b.Background(fill).Children(func() {
		ui.Text(c, value).SingleLine().Shrink(1).MinWidth(0).Ellipsis("…")
		ui.Icon(c, iconChevrons).Size(11, 11).Shrink(0).TextColor(p.text2)
	})
	return b
}

// tag is a small colored label for a state, as "Outdated".
func tag(c *ui.Context, text string, color ui.Color) ui.Element {
	return ui.Text(c, text).FontSize(textCaption).FontWeight(600).TextColor(color).Background(color.Alpha(0.12)).
		Padding(1, 6).Radius(4).SingleLine().Shrink(0)
}

// searchField is a field for a query: Escape and its clear button clear
// it. It returns the input, and whether the query changed, as typing or
// clearing changes it.
func searchField(c *ui.Context, query *string, placeholder string, h *ui.Handle) (ui.Element, bool) {
	p := colors(c)
	var input ui.Element
	cleared := false
	box := ui.Row(c).Height(28).Gap(6).Padding(0, 6, 0, 8).Radius(7).Background(p.fill).TextColor(p.text2).Cursor(ui.CursorText)
	box.Children(func() {
		ui.Icon(c, iconFind).Size(14, 14).Shrink(0)
		input = ui.TextInputBase(c, query).Grow(1).Shrink(1).MinWidth(0).FontSize(textBody).TextColor(p.text).
			Placeholder(placeholder).Label(placeholder).FocusRing(false)
		if h != nil {
			input.Bind(h)
		}
		if *query == "" {
			return
		}
		clearButton := ui.ButtonBase(c).Size(16, 16).Shrink(0).Radius(8).Center().Background(p.text3).TextColor(p.content).Label("Clear").FocusRing(false)
		clearButton.Children(func() { ui.Icon(c, iconDisable).Size(10, 10) })
		if input.Shortcut(0, ui.KeyEscape) || clearButton.Clicked() {
			*query, cleared = "", true
		}
	})
	if input.Focused() {
		box.Shadow(0, 0, 0, 3, accent(c).Alpha(0.25))
	}
	if box.Clicked() {
		input.Focus()
	}
	return input, input.Changed() || cleared
}

// segmented is one choice of a few, each segment as wide as the others.
func segmented(c *ui.Context, selected *int, label string, labels ...string) ui.Element {
	p := colors(c)
	parts := ui.SegmentedBase(c, selected, len(labels))
	parts.Track.Height(26).Padding(2).Radius(7).Background(p.fill).Label(label)
	parts.Track.Children(func() {
		for i, text := range labels {
			seg := parts.Segment(i).Grow(1).Basis(0).Radius(5).Center().FontSize(textSmall).FontWeight(500).TextColor(p.text2)
			if i == *selected {
				seg.Background(c.Theme().Background.Mix(p.text, 0.02)).TextColor(p.text).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.12))
			}
			seg.Children(func() { ui.Text(c, text).SingleLine() })
		}
	})
	return parts.Track
}

// group is a titled card of rows with hairlines between them. rows builds
// them, each through g.row.
func group(c *ui.Context, title string, rows func(g *rowGroup)) {
	p := colors(c)
	ui.Column(c).MinWidth(0).Gap(8).Label(title).Children(func() {
		if title != "" {
			ui.Text(c, title).FontSize(textSmall).FontWeight(600).TextColor(p.text2).Padding(0, 2)
		}
		g := &rowGroup{line: p.separator}
		ui.Column(c).Radius(10).Background(p.card).Border(1, p.separator).Clip().Children(func() { rows(g) })
	})
}

// rowGroup is a group's card while its rows are built: each row after the
// first draws the hairline above it.
type rowGroup struct {
	line ui.Color
	n    int
}

// row is a row of the group: its children in a line.
func (g *rowGroup) row(c *ui.Context) ui.Element {
	e := ui.Row(c).MinHeight(48).Padding(8, 14).Gap(10).AlignItems(ui.Center).MinWidth(0)
	if g.n > 0 {
		line := g.line
		e.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X + 14, Y: r.Y, W: r.W - 14, H: 1}, line, 0)
		})
	}
	g.n++
	return e
}

// rowText is a row's title over its detail, which take the room left.
func rowText(c *ui.Context, title, detail string) {
	p := colors(c)
	ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
		ui.Text(c, title).FontWeight(600).SingleLine()
		if detail != "" {
			ui.Text(c, detail).FontSize(textSmall).TextColor(p.text2).SingleLine()
		}
	})
}

// rowIcon is the icon a row leads with.
func rowIcon(c *ui.Context, icon *ui.SVG) {
	ui.Icon(c, icon).Size(16, 16).Shrink(0).TextColor(colors(c).text2)
}

// note is a callout over a detail pane: what needs attention.
func note(c *ui.Context, text string) {
	p := colors(c)
	ui.Row(c).Gap(10).Padding(10, 12).Radius(8).AlignItems(ui.Start).Background(p.orange.Alpha(0.1)).Children(func() {
		ui.Icon(c, iconWarning).Size(15, 15).Shrink(0).TextColor(p.orange).Margin(1, 0, 0, 0)
		ui.Text(c, text).FontSize(textSmall).LineHeight(1.4).Grow(1).Selectable()
	})
}

// emptyState fills a pane that has nothing to show yet.
func emptyState(c *ui.Context, title, detail string, actions func()) {
	p := colors(c)
	ui.Column(c).Grow(1).Center().Gap(6).Padding(24).Children(func() {
		ui.Text(c, title).FontSize(15).FontWeight(600)
		if detail != "" {
			ui.Text(c, detail).FontSize(textSmall).TextColor(p.text2).TextAlign(ui.Center).LineHeight(1.4).MaxWidth(340)
		}
		if actions != nil {
			ui.Row(c).Gap(8).Margin(10, 0, 0, 0).Children(actions)
		}
	})
}

// loading fills a pane while its first read runs.
func loading(c *ui.Context) {
	ui.Column(c).Grow(1).Center().Children(func() { ui.Spinner(c).Size(16, 16).Label("Loading") })
}

// spinner is a small spinner for work in a row or a bar.
func spinner(c *ui.Context, label string) ui.Element {
	return ui.Spinner(c).Size(14, 14).Shrink(0).Label(label)
}

// sourceList is a list whose rows the user chooses from, as macOS draws
// one: the row chosen in the accent color while the list has the focus,
// else in gray. row gets whether its text is on the accent.
func sourceList(c *ui.Context, s *ui.ListState, n int, label string, row func(i int, onAccent bool)) ui.Element {
	p := colors(c)
	focused := s.FocusWithin(c)
	t := *c.Theme()
	if !focused {
		t.Accent, t.AccentText = p.rowInactive, p.text
	}
	t.SurfaceHover = p.hover
	var list ui.Element
	ui.Themed(c, &t).Grow(1).MinHeight(0).Children(func() {
		list = ui.List(c, s, n).Grow(1).Padding(0, 10, 10, 10).Gap(1).Label(label)
		list.Rows(func(r ui.ListRow) { row(r.Index, focused && r.Selected()) })
	})
	return list
}

// listRow is a row of a source list: a title line and a detail line, the
// detail in white on the accent.
func listRow(c *ui.Context, onAccent bool, title func(), detail string) {
	p := colors(c)
	sub := p.text2
	if onAccent {
		sub = c.Theme().AccentText.Alpha(0.8)
	}
	ui.Column(c).Height(50).Justify(ui.Center).Padding(0, 10).Gap(3).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).MinWidth(0).Children(title)
		ui.Text(c, detail).FontSize(textSmall).TextColor(sub).SingleLine()
	})
}

// rowTag is a tag in a list row, white on the accent.
func rowTag(c *ui.Context, text string, color ui.Color, onAccent bool) ui.Element {
	if onAccent {
		color = c.Theme().AccentText
	}
	return tag(c, text, color)
}
