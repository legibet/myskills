package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"

	"github.com/legibet/myskills/internal/sanitize"
	"github.com/legibet/myskills/internal/skill"
)

// doc is markdown to preview: a skill directory on disk or a skill on skills.sh.
type doc struct {
	key    string
	remote bool
	load   func(ctx context.Context) (string, error)
}

func dirDoc(dir string) doc {
	return doc{key: "dir:" + dir, load: func(context.Context) (string, error) {
		return readBody(dir)
	}}
}

func remoteDoc(svc service, src, skillID string) doc {
	return doc{key: "sh:" + src + "/" + skillID, remote: true, load: func(ctx context.Context) (string, error) {
		md, err := svc.SkillMarkdown(ctx, src, skillID)
		return skill.StripFrontmatter(md), err
	}}
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
			return docMsg{key: d.key, md: l.md, rk: rk, out: renderMarkdown(l.md, width, t)}
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
			msg.out = renderMarkdown(md, width, t)
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
