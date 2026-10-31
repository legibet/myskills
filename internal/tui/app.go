package tui

import (
	"context"
	"errors"
	"io/fs"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/legibet/myskills/internal/agent"
	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/sanitize"
	"github.com/legibet/myskills/internal/store"
)

// wideWidth is the width from which the preview sits next to the list.
const wideWidth = 100

// app is the root model: routing between views, layout, the status line,
// modals, loading and the mutation runner.
type app struct {
	ctx       context.Context
	svc       service
	opts      Options
	running   *running   // mutations in flight; Run waits for them
	checkouts *checkouts // of browsed sources; Run closes those left

	width, height int
	th            theme
	help          help.Model
	spin          spinner.Model
	spinning      bool

	keys  skillsKeys
	fkeys filterKeys
	rkeys readerKeys
	nkeys findKeys

	view      View
	reading   bool // the preview is open full screen
	modal     *modal
	status    status
	statusSeq int
	quitting  bool

	loaded  bool
	loadSeq int
	data    snapshot
	scope   core.Scope
	busy    map[string]bool // keys of mutations in flight

	sk skillsState
	fd findState
	pv previewer

	summary []string
}

// snapshot is everything loaded from core for the views.
type snapshot struct {
	lib    []core.Skill
	status map[core.Scope]core.Status
	dirs   map[core.Scope][]core.AgentDir // agent skill directories, grouped by resolved location
	usage  map[string][]core.Scope
	config store.Config
	agents []agent.Agent
	err    error
}

func newApp(ctx context.Context, svc service, opts Options) *app {
	a := &app{
		ctx:       ctx,
		svc:       svc,
		opts:      opts,
		running:   &running{},
		checkouts: &checkouts{open: map[*core.Fetched]bool{}},
		help:      help.New(),
		spin:      spinner.New(spinner.WithSpinner(spinner.MiniDot)),
		keys:      newSkillsKeys(),
		fkeys:     newFilterKeys(),
		rkeys:     newReaderKeys(),
		nkeys:     newFindKeys(),
		view:      opts.View,
		scope:     opts.Scope,
		busy:      map[string]bool{},
		sk:        newSkillsState(),
		fd:        newFindState(),
		pv:        newPreviewer(),
	}
	if opts.Project == nil {
		a.scope = core.Global
	}
	a.help.ShortSeparator = "  "
	a.applyTheme(newTheme(true))
	a.fd.input.SetValue(opts.Query)
	return a
}

// applyTheme switches the palette, e.g. when the terminal reports a light
// background.
func (a *app) applyTheme(t theme) {
	a.th = t
	a.help.Styles = t.helpStyles()
	a.spin.Style = t.accented
	a.sk.filter.SetStyles(t.inputStyles())
	a.fd.input.SetStyles(t.inputStyles())
}

func (a *app) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.RequestBackgroundColor, a.load()}
	if a.view == Find {
		cmds = append(cmds, a.enterFind(true))
	}
	return tea.Batch(cmds...)
}

// shutdown waits for mutations in flight, which may install from a checkout,
// then closes the checkouts still open. Run calls it after the program exits
// and the context is cancelled.
func (a *app) shutdown() {
	a.running.stop()
	a.checkouts.closeAll()
}

func (a *app) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := a.update(msg)
	a.syncKeys()
	return a, tea.Batch(cmd, a.sync())
}

func (a *app) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.help.SetWidth(msg.Width - 2)
	case tea.BackgroundColorMsg:
		a.applyTheme(newTheme(msg.IsDark()))
	case tea.KeyPressMsg:
		return a.key(msg)
	case tea.PasteMsg:
		return a.paste(msg)
	case spinner.TickMsg:
		if !a.active() {
			a.spinning = false
			return nil
		}
		var cmd tea.Cmd
		a.spin, cmd = a.spin.Update(msg)
		return cmd
	case statusClearMsg:
		if msg.id == a.status.id {
			a.status = status{}
		}
	case loadedMsg:
		return a.applyLoad(msg)
	case doneMsg:
		return a.finish(msg)
	case docMsg:
		a.pv.handle(msg)
	case openedMsg:
		if msg.err != nil {
			return a.setStatus(stError, "Couldn't open the browser: "+msg.err.Error())
		}
	default:
		if cmd, ok := a.updateSkills(msg); ok {
			return cmd
		}
		return a.updateFind(msg)
	}
	return nil
}

func (a *app) key(msg tea.KeyPressMsg) tea.Cmd {
	if msg.String() == "ctrl+c" {
		return a.quit()
	}
	if m := a.modal; m != nil {
		closed, cmd := m.update(msg)
		if closed && a.modal == m { // the action may have opened the next modal
			a.modal = nil
		}
		return cmd
	}
	if a.reading {
		return a.readerKey(msg)
	}
	if a.view == Find {
		return a.findKey(msg)
	}
	return a.skillsKey(msg)
}

func (a *app) paste(msg tea.PasteMsg) tea.Cmd {
	switch {
	case a.modal != nil || a.reading:
		return nil
	case a.view == Find:
		return a.findInput(msg)
	case a.sk.filtering:
		return a.filterInput(msg)
	}
	return nil
}

// quit exits, after the mutations in flight have finished.
func (a *app) quit() tea.Cmd {
	if len(a.busy) == 0 || a.quitting {
		return tea.Quit
	}
	a.quitting = true
	a.modal = nil
	return a.setStatus(stInfo, "Finishing "+plural(len(a.busy), "change", "changes")+"… (ctrl+c again to quit now)")
}

func (a *app) readerKey(msg tea.KeyPressMsg) tea.Cmd {
	k, vp := a.rkeys, &a.pv.vp
	switch {
	case key.Matches(msg, k.Back):
		a.reading = false
	case key.Matches(msg, k.Quit):
		return a.quit()
	case key.Matches(msg, k.Up):
		vp.ScrollUp(1)
	case key.Matches(msg, k.Down):
		vp.ScrollDown(1)
	case key.Matches(msg, k.HalfDown):
		vp.HalfPageDown()
	case key.Matches(msg, k.HalfUp):
		vp.HalfPageUp()
	case key.Matches(msg, k.PageDown):
		vp.PageDown()
	case key.Matches(msg, k.PageUp):
		vp.PageUp()
	case key.Matches(msg, k.Top):
		vp.GotoTop()
	case key.Matches(msg, k.Bottom):
		vp.GotoBottom()
	}
	return nil
}

// active reports whether something shows a spinner.
func (a *app) active() bool {
	return len(a.busy) > 0 || a.sk.checking || a.fd.loading || a.fd.fetching
}

// sync brings derived state in line after every update: list scrolling, the
// preview, the spinner.
func (a *app) sync() tea.Cmd {
	l := a.layout()
	a.sk.list.fit(len(a.sk.rows), l.bodyH-1)
	a.fd.list.fit(len(a.findRows()), l.bodyH-2)
	a.sk.filter.SetWidth(max(1, l.listW-10))
	a.fd.input.SetWidth(max(1, l.listW-8))
	var cmds []tea.Cmd
	cmds = append(cmds, a.syncPreview(l))
	if a.active() && !a.spinning {
		a.spinning = true
		cmds = append(cmds, a.spin.Tick)
	}
	return tea.Batch(cmds...)
}

// syncKeys enables the bindings that apply to the current state, so that
// the help bar only lists what works.
func (a *app) syncKeys() {
	a.syncSkillsKeys()
	a.syncFindKeys()
}

// Loading.

type loadedMsg struct {
	seq  int
	snap snapshot
}

// scopes returns the scopes the app shows: global and the project, if any.
func (a *app) scopes() []core.Scope {
	if a.opts.Project == nil {
		return []core.Scope{core.Global}
	}
	return []core.Scope{*a.opts.Project, core.Global}
}

// load reads the library and both scopes. Only the latest load is applied.
func (a *app) load() tea.Cmd {
	a.loadSeq++
	seq, svc, scopes := a.loadSeq, a.svc, a.scopes()
	return func() tea.Msg {
		s := snapshot{status: map[core.Scope]core.Status{}, dirs: map[core.Scope][]core.AgentDir{}}
		var errs []error
		var err error
		if s.lib, err = svc.Library(); err != nil {
			errs = append(errs, err)
		}
		for _, sc := range scopes {
			st, err := svc.Status(sc)
			if err != nil {
				errs = append(errs, err)
			}
			s.status[sc] = st
			dirs, err := svc.AgentDirs(sc)
			if err != nil {
				errs = append(errs, err)
			}
			s.dirs[sc] = dirs
		}
		if s.usage, err = svc.Usage(); err != nil {
			errs = append(errs, err)
		}
		if s.config, err = svc.Config(); err != nil {
			errs = append(errs, err)
		}
		if s.agents, err = svc.Agents(); err != nil {
			errs = append(errs, err)
		}
		if len(errs) > 0 {
			s.err = errs[0]
		}
		return loadedMsg{seq: seq, snap: s}
	}
}

func (a *app) applyLoad(msg loadedMsg) tea.Cmd {
	if msg.seq != a.loadSeq {
		return nil
	}
	a.data, a.loaded = msg.snap, true
	a.rebuildRows()
	a.refreshInLibrary()
	if a.view == Skills {
		a.revealAdded()
	}
	if msg.snap.err != nil {
		return a.setStatus(stError, msg.snap.err.Error())
	}
	return nil
}

// Mutations.

// mutation is a change to one skill (or other item) run as a command. While
// it runs, its key is busy and further changes to the same item are ignored.
type mutation struct {
	key     string
	run     func(force bool) outcome
	discard string                    // confirmation text when run fails with core.ErrModified
	done    func(out outcome) tea.Cmd // optional; runs in Update when finished
}

// outcome describes what a mutation did.
type outcome struct {
	lines    []string // changes made, for the status line and the summary
	warnings []string
	note     string // status text when nothing changed
	hint     string // appended to the status line on success
	path     string // library path of the skill an install added or found
	err      error
}

func (o *outcome) merge(p outcome) {
	o.lines = append(o.lines, p.lines...)
	o.warnings = append(o.warnings, p.warnings...)
	if p.note != "" {
		o.note = p.note
	}
	if p.err != nil {
		o.err = p.err
	}
}

type doneMsg struct {
	m     mutation
	force bool
	out   outcome
}

// start runs m unless its key is busy.
func (a *app) start(m mutation, force bool) tea.Cmd {
	if a.busy[m.key] {
		return nil
	}
	a.busy[m.key] = true
	run := a.running
	return func() tea.Msg {
		if !run.begin() {
			return nil
		}
		defer run.wg.Done()
		return doneMsg{m: m, force: force, out: m.run(force)}
	}
}

// running counts the mutations in flight. Once stopped, it lets none start:
// a command that the program dropped when it quit, or ran too late, does
// nothing, and Run never waits for it.
type running struct {
	mu      sync.Mutex
	wg      sync.WaitGroup
	stopped bool
}

func (r *running) begin() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.stopped {
		r.wg.Add(1)
	}
	return !r.stopped
}

// stop waits for the mutations in flight.
func (r *running) stop() {
	r.mu.Lock()
	r.stopped = true
	r.mu.Unlock()
	r.wg.Wait()
}

func (a *app) finish(msg doneMsg) tea.Cmd {
	delete(a.busy, msg.m.key)
	out := msg.out
	for _, l := range out.lines {
		a.summary = append(a.summary, "✓ "+sanitize.Line(l))
	}
	for _, w := range out.warnings {
		a.summary = append(a.summary, "! "+sanitize.Line(w))
	}
	cmds := []tea.Cmd{a.load()}
	if msg.m.done != nil {
		cmds = append(cmds, msg.m.done(out))
	}
	switch {
	case errors.Is(out.err, core.ErrModified) && !msg.force && a.modal == nil && !a.quitting:
		body := msg.m.discard
		if body == "" {
			body = sanitize.Line(out.err.Error())
		}
		m := msg.m
		a.modal = confirmModal("Discard local changes?", body, "Discard", true, func() tea.Cmd {
			return a.start(m, true)
		})
	case out.err != nil:
		cmds = append(cmds, a.setStatus(stError, out.err.Error()))
	case len(out.warnings) > 0:
		cmds = append(cmds, a.setStatus(stWarn, strings.Join(append(out.lines, out.warnings...), " · ")))
	case len(out.lines) > 0:
		text := strings.Join(out.lines, " · ")
		if out.hint != "" {
			text += " · " + out.hint
		}
		cmds = append(cmds, a.setStatus(stSuccess, text))
	case out.note != "":
		cmds = append(cmds, a.setStatus(stInfo, out.note))
	}
	if a.quitting && len(a.busy) == 0 {
		cmds = append(cmds, tea.Quit)
	}
	return tea.Batch(cmds...)
}

// Status line.

type statusKind int

const (
	stInfo statusKind = iota
	stSuccess
	stWarn
	stError
)

type status struct {
	kind statusKind
	text string
	id   int
}

type statusClearMsg struct{ id int }

// setStatus shows a transient message; errors stay a little longer.
func (a *app) setStatus(kind statusKind, text string) tea.Cmd {
	a.statusSeq++
	a.status = status{kind: kind, text: sanitize.Line(text), id: a.statusSeq}
	d := 4 * time.Second
	switch kind {
	case stWarn:
		d = 6 * time.Second
	case stError:
		d = 8 * time.Second
	}
	id := a.statusSeq
	return tick(d, func(time.Time) tea.Msg { return statusClearMsg{id} })
}

// Browser.

type openedMsg struct{ err error }

// openBrowser opens url in the default browser.
func openBrowser(url string) tea.Cmd {
	return func() tea.Msg {
		var c *exec.Cmd
		switch runtime.GOOS {
		case "darwin":
			c = exec.Command("open", url)
		case "windows":
			c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
		default:
			c = exec.Command("xdg-open", url)
		}
		return openedMsg{err: c.Run()}
	}
}

// Layout and rendering.

type layout struct {
	wide         bool
	listW, prevW int // pane widths; prevW is 0 when narrow
	bodyH        int
}

func (a *app) layout() layout {
	helpH := lipgloss.Height(a.helpView())
	l := layout{
		wide:  a.width >= wideWidth,
		listW: max(0, a.width-2),
		bodyH: max(1, a.height-4-helpH),
	}
	if l.wide {
		l.listW = max(44, min(a.width*2/5, 64))
		l.prevW = a.width - l.listW - 5 // margins and " │ "
	}
	return l
}

// View renders the frame. The frame shows names and paths from disk, config
// and skills.sh, so only its styling survives as escape sequences.
func (a *app) View() tea.View {
	v := tea.NewView(sanitize.Styled(a.render()))
	v.AltScreen = true
	v.WindowTitle = "myskills"
	return v
}

func (a *app) render() string {
	if a.width == 0 || a.height == 0 {
		return ""
	}
	t := a.th
	if a.width < 40 || a.height < 10 {
		return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center,
			t.dim.Render(truncate("Make the terminal a little larger", a.width)))
	}
	l := a.layout()
	rule := t.rule.Render(strings.Repeat("─", a.width))
	s := strings.Join([]string{
		a.headerView(),
		rule,
		a.bodyView(l),
		rule,
		a.statusView(),
		" " + a.helpView(),
	}, "\n")
	if a.modal != nil {
		box := a.modal.view(t, a.width)
		x := max(0, (a.width-lipgloss.Width(box))/2)
		y := max(0, (a.height-lipgloss.Height(box))/3)
		s = lipgloss.NewCompositor(lipgloss.NewLayer(s), lipgloss.NewLayer(box).X(x).Y(y).Z(1)).Render()
	}
	return s
}

func (a *app) headerView() string {
	t := a.th
	tab := func(name string, on bool) string {
		if on {
			return t.bold.Render(name)
		}
		return t.dim.Render(name)
	}
	left := " " + t.title.Render("myskills") + "   " +
		tab("Skills", a.view == Skills) + t.hint.Render(" · ") + tab("Find", a.view == Find)

	// The right side holds the update check, the active scope and a count of
	// the other scope. The scope always stays, its path shortened from the
	// left; the others go when there is no room.
	room := a.width - lipgloss.Width(left) - 4 // gap and right margin
	scope := t.accented.Render("▸ ")
	if a.scope.IsGlobal() {
		scope += t.title.Render("global")
	} else {
		label, word := sanitize.Line(a.scope.String()), "project "
		if room-2-len(word) < min(ansi.StringWidth(label), 16) {
			word = "" // a path is clearly a project; keep room for it
		}
		if w, fit := ansi.StringWidth(label), max(2, room-2-len(word)); w > fit {
			label = ansi.TruncateLeft(label, w-fit+1, "…")
		}
		scope += t.dim.Render(word) + t.title.Render(label)
	}
	var checking, other string
	if a.sk.checking {
		text := "checking updates"
		if total := a.sk.progress[1]; total > 0 {
			text += " " + strconv.Itoa(a.sk.progress[0]) + "/" + strconv.Itoa(total)
		}
		checking = a.spin.View() + " " + t.dim.Render(text)
	}
	if sc, ok := a.otherScope(); ok && a.loaded {
		name := "global"
		if !sc.IsGlobal() {
			name = "project"
		}
		other = t.dim.Render(name + ": " + strconv.Itoa(len(a.data.status[sc].Enablements)))
	}
	var right string
	for _, parts := range [][]string{{checking, scope, other}, {checking, scope}, {scope}} {
		right = strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), "   ")
		if lipgloss.Width(right) <= room {
			break
		}
	}
	gap := a.width - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		return truncate(left, a.width)
	}
	return left + strings.Repeat(" ", gap) + right + " "
}

// otherScope returns the scope that is not active, when there are two.
func (a *app) otherScope() (core.Scope, bool) {
	if a.opts.Project == nil {
		return core.Scope{}, false
	}
	if a.scope.IsGlobal() {
		return *a.opts.Project, true
	}
	return core.Global, true
}

func (a *app) bodyView(l layout) string {
	if a.reading {
		return " " + indent(a.pv.view(a.th, a.width-2, l.bodyH))
	}
	var list string
	switch a.view {
	case Find:
		list = a.findListView(l.listW, l.bodyH)
	default:
		if body, ok := a.skillsPlaceholder(l); ok {
			return body
		}
		list = a.skillsListView(l.listW, l.bodyH)
	}
	if !l.wide {
		return " " + indent(fitBlock(list, l.listW, l.bodyH))
	}
	sep := strings.TrimSuffix(strings.Repeat(a.th.rule.Render("│")+"\n", l.bodyH), "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top,
		" ", fitBlock(list, l.listW, l.bodyH), " ", sep, " ", a.pv.view(a.th, l.prevW, l.bodyH), " ")
}

// indent prefixes every line but the first with one space (the first gets
// its space from the caller).
func indent(s string) string {
	return strings.ReplaceAll(s, "\n", "\n ")
}

func (a *app) statusView() string {
	t := a.th
	if a.status.text == "" {
		return ""
	}
	var s string
	switch a.status.kind {
	case stSuccess:
		s = t.good.Render("✓ " + a.status.text)
	case stWarn:
		s = t.warn.Render("! " + a.status.text)
	case stError:
		s = t.bad.Render("✗ " + a.status.text)
	default:
		s = t.dim.Render(a.status.text)
	}
	return " " + truncate(s, a.width-2)
}

func (a *app) helpView() string {
	switch {
	case a.reading:
		return a.help.ShortHelpView(a.rkeys.ShortHelp())
	case a.view == Find:
		return a.help.ShortHelpView(a.nkeys.ShortHelp())
	case a.sk.filtering:
		return a.help.ShortHelpView(a.fkeys.ShortHelp())
	}
	return a.help.View(a.keys)
}

// centered places a block in the middle of the body area.
func centered(w, h int, lines ...string) string {
	return lipgloss.Place(w, h, lipgloss.Center, lipgloss.Center, strings.Join(lines, "\n"))
}

// syncPreview sets what the preview pane shows for the highlighted row and
// requests its markdown when needed. Remote markdown waits for the cursor to
// settle (see settle).
func (a *app) syncPreview(l layout) tea.Cmd {
	w := l.prevW
	if a.reading {
		w = a.width - 2
	}
	if w <= 0 {
		return nil
	}
	var (
		item, intro string
		header      []string
		d           *doc
	)
	if a.view == Find {
		item, header, intro, d = a.findPreview(w)
	} else {
		item, header, intro, d = a.skillPreview(w)
	}
	content := intro
	var cmd tea.Cmd
	if d != nil {
		wrap := min(w, 100)
		body, ready, err := a.pv.body(*d, wrap, a.th)
		switch {
		case ready && errors.Is(err, fs.ErrNotExist):
			content = joinBlocks(content, a.th.hint.Render("No SKILL.md to preview"))
		case ready && err != nil:
			content = joinBlocks(content, a.th.warn.Render("Preview unavailable: "+sanitize.Line(err.Error())))
		case ready:
			content = joinBlocks(content, body)
		default:
			if d.remote {
				content = joinBlocks(content, a.th.hint.Render("Loading preview…"))
			}
			if !d.remote || a.fd.settled == d.key {
				cmd = a.pv.request(a.ctx, *d, wrap, a.th)
			}
		}
	}
	a.pv.show(item, header, content, w, l.bodyH)
	return cmd
}
