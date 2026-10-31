package tui

import (
	"context"
	"errors"
	"image/color"
	"io/fs"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/sanitize"
)

// picker is the add picker: a checklist of the candidates of a fetched
// source with a preview of each SKILL.md.
type picker struct {
	f       *core.Fetched
	from    string
	th      theme
	keys    pickerKeys
	help    help.Model
	width   int
	height  int
	list    listCursor
	checked []bool
	pv      previewer
	problem string // why the last key did nothing

	picked []core.Candidate // result; nil when cancelled
}

func newPicker(f *core.Fetched) *picker {
	p := &picker{
		f:       f,
		from:    fetchedLabel(f),
		keys:    newPickerKeys(),
		help:    help.New(),
		checked: make([]bool, len(f.Candidates)),
		pv:      newPreviewer(),
	}
	p.help.ShortSeparator = "  "
	p.applyTheme(newTheme(true))
	return p
}

func (p *picker) applyTheme(t theme) {
	p.th = t
	p.help.Styles = t.helpStyles()
}

// selectable reports whether a candidate can be added; the reason when not.
func selectable(c core.Candidate) (bool, string) {
	switch {
	case c.Installed != "":
		return false, sanitize.Line(c.Name) + " is already in your library"
	case c.Conflict != "":
		return false, "The library already has " + sanitize.Line(c.Conflict) + " under the name " + sanitize.Line(c.Name)
	case c.Issue != "":
		return false, sanitize.Line(c.Name) + " is invalid: " + sanitize.Line(c.Issue)
	}
	return true, ""
}

func (p *picker) Init() tea.Cmd {
	return tea.RequestBackgroundColor
}

func (p *picker) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		p.width, p.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		p.applyTheme(newTheme(msg.IsDark()))
	case tea.KeyPressMsg:
		cmd = p.key(msg)
	case docMsg:
		p.pv.handle(msg)
	}
	return p, tea.Batch(cmd, p.sync())
}

func (p *picker) key(msg tea.KeyPressMsg) tea.Cmd {
	k, n := p.keys, len(p.f.Candidates)
	p.problem = ""
	switch {
	case key.Matches(msg, k.Cancel):
		p.picked = nil
		return tea.Quit
	case key.Matches(msg, k.Up):
		p.list.move(-1, n)
	case key.Matches(msg, k.Down):
		p.list.move(1, n)
	case key.Matches(msg, k.Toggle):
		if n == 0 {
			return nil
		}
		i := p.list.cursor
		if ok, why := selectable(p.f.Candidates[i]); !ok {
			p.problem = why
			return nil
		}
		p.checked[i] = !p.checked[i]
		p.list.move(1, n)
	case key.Matches(msg, k.All):
		all := true
		for i, c := range p.f.Candidates {
			if ok, _ := selectable(c); ok && !p.checked[i] {
				all = false
			}
		}
		for i, c := range p.f.Candidates {
			if ok, _ := selectable(c); ok {
				p.checked[i] = !all
			}
		}
	case key.Matches(msg, k.Install):
		var picks []core.Candidate
		for i, c := range p.f.Candidates {
			if p.checked[i] {
				picks = append(picks, c)
			}
		}
		if len(picks) == 0 && n > 0 {
			c := p.f.Candidates[p.list.cursor]
			if ok, why := selectable(c); !ok {
				p.problem = why + "; select skills with space"
				return nil
			}
			picks = []core.Candidate{c}
		}
		if len(picks) == 0 {
			return nil
		}
		p.picked = picks
		return tea.Quit
	case key.Matches(msg, k.ScrollDown):
		p.pv.vp.HalfPageDown()
	case key.Matches(msg, k.ScrollUp):
		p.pv.vp.HalfPageUp()
	}
	return nil
}

type pickerLayout struct {
	wide         bool
	listW, prevW int
	bodyH        int
}

func (p *picker) layout() pickerLayout {
	l := pickerLayout{wide: p.width >= wideWidth, listW: max(0, p.width-2), bodyH: max(1, p.height-5)}
	if l.wide {
		l.listW = max(44, min(p.width*2/5, 64))
		l.prevW = p.width - l.listW - 5
	}
	return l
}

func (p *picker) sync() tea.Cmd {
	l := p.layout()
	p.list.fit(len(p.f.Candidates), l.bodyH)
	if !l.wide || len(p.f.Candidates) == 0 {
		return nil
	}
	c := p.f.Candidates[p.list.cursor]
	header, intro := candidatePreview(p.th, c, p.from, false, l.prevW)
	d := dirDoc(c.Dir)
	wrap := min(l.prevW, 100)
	content := intro
	var cmd tea.Cmd
	body, ready, err := p.pv.body(d, wrap, p.th)
	switch {
	case ready && errors.Is(err, fs.ErrNotExist):
		content = joinBlocks(content, p.th.hint.Render("No SKILL.md to preview"))
	case ready && err != nil:
		content = joinBlocks(content, p.th.warn.Render("Preview unavailable: "+sanitize.Line(err.Error())))
	case ready:
		content = joinBlocks(content, body)
	default:
		cmd = p.pv.request(context.Background(), d, wrap, p.th)
	}
	p.pv.show(c.Dir, header, content, l.prevW, l.bodyH)
	return cmd
}

// View renders the frame, keeping only its styling as escape sequences, like
// the app's.
func (p *picker) View() tea.View {
	v := tea.NewView(sanitize.Styled(p.render()))
	v.AltScreen = true
	v.WindowTitle = "myskills add"
	return v
}

func (p *picker) render() string {
	if p.width == 0 || p.height == 0 {
		return ""
	}
	t := p.th
	if p.width < 40 || p.height < 10 {
		return lipgloss.Place(p.width, p.height, lipgloss.Center, lipgloss.Center,
			t.dim.Render(truncate("Make the terminal a little larger", p.width)))
	}
	l := p.layout()
	n := 0
	for _, on := range p.checked {
		if on {
			n++
		}
	}
	right := t.dim.Render(plural(len(p.f.Candidates), "skill", "skills")+" · ") + t.accented.Render(selectedLabel(n))
	from := p.from                                                                               // shortened from the left so that the counts stay
	if w, fit := ansi.StringWidth(from), max(8, p.width-2-20-lipgloss.Width(right)-2); w > fit { // 20: "myskills   Add from "
		from = ansi.TruncateLeft(from, w-fit+1, "…")
	}
	header := " " + layoutLine(p.width-2, t.title.Render("myskills")+"   "+t.dim.Render("Add from ")+t.bold.Render(from), right)
	rule := t.rule.Render(strings.Repeat("─", p.width))

	list := fitBlock(p.listView(l.listW, l.bodyH), l.listW, l.bodyH)
	var body string
	if l.wide {
		sep := strings.TrimSuffix(strings.Repeat(t.rule.Render("│")+"\n", l.bodyH), "\n")
		body = lipgloss.JoinHorizontal(lipgloss.Top, " ", list, " ", sep, " ", p.pv.view(t, l.prevW, l.bodyH), " ")
	} else {
		body = " " + indent(list)
	}

	status := ""
	switch {
	case p.problem != "":
		status = t.warn.Render("! " + p.problem)
	case !l.wide && len(p.f.Candidates) > 0:
		status = t.dim.Render(sanitize.Line(p.f.Candidates[p.list.cursor].Description))
	}
	return strings.Join([]string{
		header,
		rule,
		body,
		rule,
		" " + truncate(status, p.width-2),
		" " + fitHelp(p.help, p.width-2, p.keys.ShortHelp()),
	}, "\n")
}

func selectedLabel(n int) string {
	if n == 0 {
		return "none selected"
	}
	return plural(n, "selected", "selected")
}

func (p *picker) listView(w, h int) string {
	t := p.th
	if len(p.f.Candidates) == 0 {
		return " " + t.dim.Render("No skills found in this source")
	}
	var lines []string
	start, end := p.list.window(len(p.f.Candidates), h)
	for i := start; i < end; i++ {
		c := p.f.Candidates[i]
		var bg color.Color
		nameStyle := t.text
		if i == p.list.cursor {
			bg, nameStyle = t.selection, t.bold
		}
		ok, _ := selectable(c)
		box := seg{text: "[ ]", style: t.hint}
		var right []seg
		switch {
		case p.checked[i]:
			box = seg{text: "[✓]", style: t.accented}
		case c.Installed != "":
			box = seg{text: " ✓ ", style: t.good}
			right = []seg{{text: "in library", style: t.good}}
		case !ok:
			box = seg{text: " ✗ ", style: t.bad}
			word := "conflict"
			if c.Issue != "" {
				word = "invalid"
			}
			right = []seg{{text: word, style: t.bad}}
		}
		if !ok {
			nameStyle = nameStyle.Foreground(t.muted)
		}
		left := []seg{cursorSeg(t, i == p.list.cursor), box, {text: " "}, {text: sanitize.Line(c.Name), style: nameStyle}}
		if c.RelPath != "" {
			left = append(left, seg{text: "  " + sanitize.Line(c.RelPath), style: t.hint})
		}
		right = append(right, seg{text: " "})
		lines = append(lines, layoutRow(w, bg, left, right))
	}
	return strings.Join(lines, "\n")
}
