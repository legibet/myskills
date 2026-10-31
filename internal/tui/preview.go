package tui

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	"github.com/charmbracelet/x/ansi"

	"github.com/legibet/myskills/internal/sanitize"
	"github.com/legibet/myskills/internal/skill"
)

// doc is markdown to preview: a skill directory on disk or a skill on skills.sh.
type doc struct {
	key    string
	remote bool
	load   func(ctx context.Context) (string, error)
}

// dirDocPrefix starts the key of a doc read from disk.
const dirDocPrefix = "dir:"

func dirDoc(dir string) doc {
	return doc{key: dirDocPrefix + dir, load: func(context.Context) (string, error) {
		return readSkill(dir)
	}}
}

func remoteDoc(svc service, src, skillID string) doc {
	return doc{key: "sh:" + src + "/" + skillID, remote: true, load: func(ctx context.Context) (string, error) {
		return svc.SkillMarkdown(ctx, src, skillID)
	}}
}

// readSkillFile reads dir/SKILL.md.
func readSkillFile(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, skill.FileName))
	return string(data), err
}

// renderKey identifies a rendering of a doc.
type renderKey struct {
	doc   string
	gen   int
	width int
	dark  bool
}

type docResult struct {
	md  string
	err error
}

// docMsg delivers loaded and rendered markdown.
type docMsg struct {
	key string
	md  string
	err error
	rk  renderKey
	out string
}

// previewer is the preview pane: a fixed header above a scrollable viewport,
// and the markdown it shows, loaded and rendered off the UI goroutine and
// cached by doc, width and background.
type previewer struct {
	vp      viewport.Model
	item    string // id of the shown item; the scroll resets when it changes
	header  []string
	content string

	gen     map[string]int // bumped by invalidate to drop stale work
	md      map[string]docResult
	loading map[string]bool
	out     map[renderKey]string
	pending map[renderKey]bool
}

func newPreviewer() previewer {
	return previewer{
		vp:      viewport.New(),
		gen:     map[string]int{},
		md:      map[string]docResult{},
		loading: map[string]bool{},
		out:     map[renderKey]string{},
		pending: map[renderKey]bool{},
	}
}

// body returns the rendered doc if it is ready, or the error that prevents it.
func (p *previewer) body(d doc, width int, t theme) (out string, ready bool, err error) {
	if out, ok := p.out[renderKey{d.key, p.gen[d.key], width, t.dark}]; ok {
		return out, true, nil
	}
	if l, ok := p.md[d.key]; ok && l.err != nil {
		return "", true, l.err
	}
	return "", false, nil
}

// request returns a command that loads and renders d, unless that is done or
// in flight.
func (p *previewer) request(ctx context.Context, d doc, width int, t theme) tea.Cmd {
	rk := renderKey{d.key, p.gen[d.key], width, t.dark}
	if _, ok := p.out[rk]; ok || p.pending[rk] {
		return nil
	}
	if l, ok := p.md[d.key]; ok {
		if l.err != nil {
			return nil
		}
		p.pending[rk] = true
		return func() tea.Msg {
			return docMsg{key: d.key, md: l.md, rk: rk, out: renderSkill(l.md, width, t)}
		}
	}
	if p.loading[d.key] {
		return nil
	}
	p.loading[d.key] = true
	p.pending[rk] = true
	return func() tea.Msg {
		md, err := d.load(ctx)
		msg := docMsg{key: d.key, md: md, err: err, rk: rk}
		if err == nil {
			msg.out = renderSkill(md, width, t)
		}
		return msg
	}
}

func (p *previewer) handle(msg docMsg) {
	delete(p.pending, msg.rk)
	if msg.rk.gen != p.gen[msg.key] {
		return
	}
	delete(p.loading, msg.key)
	switch {
	case errors.Is(msg.err, context.Canceled):
	case msg.err != nil:
		p.md[msg.key] = docResult{err: msg.err}
	default:
		p.md[msg.key] = docResult{md: msg.md}
		p.out[msg.rk] = msg.out
	}
}

// invalidate forgets a doc so that it is loaded again.
func (p *previewer) invalidate(key string) {
	p.gen[key]++
	delete(p.md, key)
	delete(p.loading, key)
	for rk := range p.out {
		if rk.doc == key {
			delete(p.out, rk)
		}
	}
}

// docsChangedMsg lists the docs read from disk whose file changed.
type docsChangedMsg struct{ keys []string }

// recheckLocal reads the loaded docs from disk again, off the UI goroutine,
// and reports those that changed, so that only they load again and the
// others stay on screen. Loads in flight start over, since they may have
// read a file before it changed.
func (p *previewer) recheckLocal() tea.Cmd {
	loaded := map[string]docResult{}
	for key, l := range p.md {
		if strings.HasPrefix(key, dirDocPrefix) {
			loaded[key] = l
		}
	}
	for key := range p.loading {
		if strings.HasPrefix(key, dirDocPrefix) {
			p.invalidate(key)
		}
	}
	return func() tea.Msg {
		var changed []string
		for key, l := range loaded {
			md, err := readSkill(strings.TrimPrefix(key, dirDocPrefix))
			if md != l.md || (err == nil) != (l.err == nil) {
				changed = append(changed, key)
			}
		}
		return docsChangedMsg{changed}
	}
}

// show sets what the pane displays in a w×h area; the header lines stay
// fixed above a rule and the scrollable content.
func (p *previewer) show(item string, header []string, content string, w, h int) {
	p.header = header
	p.vp.SetWidth(w)
	p.vp.SetHeight(max(1, h-len(header)-1))
	if item != p.item {
		p.item, p.content = item, content
		p.vp.SetContent(content)
		p.vp.GotoTop()
		return
	}
	if content != p.content {
		p.content = content
		p.vp.SetContent(content)
	}
}

func (p *previewer) view(t theme, w, h int) string {
	lines := append([]string{}, p.header...)
	rule := t.rule.Render(strings.Repeat("─", w))
	if p.vp.TotalLineCount() > p.vp.Height() {
		pct := fmt.Sprintf(" %d%%", int(p.vp.ScrollPercent()*100))
		rule = t.rule.Render(strings.Repeat("─", max(0, w-len(pct)))) + t.hint.Render(pct)
	}
	lines = append(lines, rule, p.vp.View())
	return fitBlock(strings.Join(lines, "\n"), w, h)
}

// renderSkill renders SKILL.md for the preview, wrapped to width: the
// frontmatter as text, then the markdown body.
func renderSkill(md string, width int, t theme) string {
	var head string
	if m, err := skill.Parse([]byte(md)); err == nil {
		head = frontmatterView(m, width, t)
	}
	return joinBlocks(head, renderMarkdown(skill.StripFrontmatter(md), width, t))
}

// frontmatterView shows the description, then the optional fields that are
// set, one per line, the metadata entries last.
func frontmatterView(m skill.Metadata, w int, t theme) string {
	fields := [][2]string{{"license", m.License}, {"compatibility", m.Compatibility}, {"allowed-tools", m.AllowedTools}}
	for _, k := range slices.Sorted(maps.Keys(m.Metadata)) {
		fields = append(fields, [2]string{sanitize.Line(k), m.Metadata[k]})
	}
	fields = slices.DeleteFunc(fields, func(f [2]string) bool { return f[1] == "" })
	keyW := 0
	for _, f := range fields {
		keyW = max(keyW, ansi.StringWidth(f[0]))
	}
	keyW = min(keyW, w/3)
	lines := make([]string, len(fields))
	for i, f := range fields {
		lines[i] = lipgloss.JoinHorizontal(lipgloss.Top,
			t.hint.Width(keyW+2).Render(truncate(f[0], keyW)),
			t.dim.Width(max(1, w-keyW-2)).Render(sanitize.Line(f[1])))
	}
	return joinBlocks(wrapText(sanitize.Line(m.Description), w), strings.Join(lines, "\n"))
}

// renderMarkdown renders sanitized markdown wrapped to width.
func renderMarkdown(md string, width int, t theme) string {
	md = sanitize.Text(md)
	r, err := glamour.NewTermRenderer(glamour.WithStyles(t.markdown()), glamour.WithWordWrap(width))
	if err != nil {
		return md
	}
	out, err := r.Render(md)
	if err != nil {
		return md
	}
	// Glamour decodes entities such as "&#27;" after the input was sanitized.
	return strings.Trim(sanitize.Styled(out), "\n")
}
