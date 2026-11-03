package main

import (
	"cmp"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/legibet/myskills/internal/agent"
	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/skill"
)

// The pages of the sidebar.
const (
	pageSkills   = "skills"
	pageProjects = "projects"
	pageFind     = "find"
	pageAgents   = "agents"
)

const (
	titleBarHeight = 52 // the hidden title bar, which the window controls sit in
	sidebarWidth   = 196
)

// desktop is what the app asks of the system, faked in tests.
type desktop struct {
	chooseFolder func(title string) string // blocks; "" when canceled
	reveal       func(path string)         // shows the item in the file manager
	open         func(path string)         // opens the file in its default app
}

// app is the window's state: the last read of the library and the places,
// the state of each page, and the work in flight.
type app struct {
	svc  *core.Service
	desk desktop
	post func(func()) // runs a function on the main thread and redraws

	ctx    context.Context // canceled on quit: network work stops
	cancel context.CancelFunc
	work   sync.WaitGroup // changes in flight, which quitting waits for

	data        data
	loading     bool
	loadAgain   bool
	busy        map[string]bool      // keys of the work running
	busySince   map[string]time.Time // when a frame first showed each, by the UI clock
	intents     map[string]bool      // what switches and check boxes were set to, by control
	waiting     []func()             // to run once the next read completes
	page        string
	alerts      []alert
	alertOpen   bool
	toasts      []ui.Toast
	skills      skillsPage
	projects    projectsPage
	find        findPage
	agents      agentsPage
	updates     updatesState
	searchField ui.Handle // the search field of the page shown, for ⌘F
	agentsOpen  bool      // the agents popover of a placement row is open
	agentsFor   string    // the row it belongs to
}

func newApp(svc *core.Service, desk desktop, post func(func())) *app {
	a := &app{svc: svc, desk: desk, post: post, busy: map[string]bool{}, busySince: map[string]time.Time{}, intents: map[string]bool{}, page: pageSkills}
	a.ctx, a.cancel = context.WithCancel(context.Background())
	a.skills.split = 300
	a.projects.split = 260
	a.find.split = 360
	return a
}

// close stops network work, waits for the changes in flight and removes the
// temporary checkouts: the app is quitting.
func (a *app) close() {
	a.cancel()
	a.work.Wait()
	a.find.closeAll()
	a.updates.close()
}

// data is one read of everything the pages show.
type data struct {
	read     bool // a read has completed
	err      error
	lib      []core.Skill
	dup      map[string]bool // names several skills share
	docs     map[string]doc  // SKILL.md of the valid skills, by library path
	places   []place         // global first, then the projects by name
	agents   []agent.Agent
	defaults []string // default agent ids
}

// place is where skills are enabled: global or a known project.
type place struct {
	scope  core.Scope
	status core.Status
	dirs   []core.AgentDir
	err    error
}

// doc is a skill's SKILL.md: its frontmatter and its body.
type doc struct {
	meta skill.Metadata
	body string
}

// read reads everything the pages show. It runs off the main thread.
func read(svc *core.Service) data {
	d := data{read: true, docs: map[string]doc{}}
	if d.lib, d.err = svc.Library(); d.err != nil {
		return d
	}
	d.dup = core.DuplicateNames(d.lib)
	for _, sk := range d.lib {
		if !sk.Valid() {
			continue
		}
		meta, err := skill.Read(sk.Dir)
		if err != nil {
			continue
		}
		body, _ := skill.Body(sk.Dir)
		d.docs[sk.Path] = doc{meta: meta, body: body}
	}
	scopes, err := svc.Scopes()
	if err != nil {
		d.err = err
		return d
	}
	for _, sc := range scopes {
		p := place{scope: sc}
		if p.status, p.err = svc.Status(sc); p.err == nil {
			p.dirs, p.err = svc.AgentDirs(sc)
		}
		d.places = append(d.places, p)
	}
	slices.SortStableFunc(d.places[1:], func(x, y place) int {
		return cmp.Or(strings.Compare(placeName(x.scope), placeName(y.scope)), strings.Compare(x.scope.Root, y.scope.Root))
	})
	if d.agents, d.err = svc.Agents(); d.err != nil {
		return d
	}
	cfg, err := svc.Config()
	if err != nil {
		d.err = err
		return d
	}
	d.defaults = cfg.DefaultAgents()
	return d
}

// reload reads everything again in the background; the pages show the last
// read meanwhile. A reload asked for while one runs follows it. then runs
// once a read started after the call completes.
func (a *app) reload(then ...func()) {
	a.waiting = append(a.waiting, then...)
	if a.loading {
		a.loadAgain = true
		return
	}
	a.loading = true
	done := a.waiting
	a.waiting = nil
	go func() {
		d := read(a.svc)
		a.post(func() {
			a.loading = false
			a.data = d
			if d.err != nil {
				a.fail("Couldn't read the library", d.err)
			}
			for _, fn := range done {
				fn()
			}
			if a.loadAgain {
				a.loadAgain = false
				a.reload()
			}
		})
	}()
}

// skill returns the library skill at path.
func (d *data) skill(path string) (core.Skill, bool) {
	i := slices.IndexFunc(d.lib, func(sk core.Skill) bool { return sk.Path == path })
	if i < 0 {
		return core.Skill{}, false
	}
	return d.lib[i], true
}

// place returns the place of a scope.
func (d *data) place(sc core.Scope) (*place, bool) {
	i := slices.IndexFunc(d.places, func(p place) bool { return p.scope == sc })
	if i < 0 {
		return nil, false
	}
	return &d.places[i], true
}

// enablement returns the enablement of a library skill in a place.
func (p *place) enablement(path string) (*core.Enablement, bool) {
	i := slices.IndexFunc(p.status.Enablements, func(e core.Enablement) bool { return e.Skill == path })
	if i < 0 {
		return nil, false
	}
	return &p.status.Enablements[i], true
}

// usedIn returns the places where a library skill is enabled.
func (d *data) usedIn(path string) []*place {
	var out []*place
	for i := range d.places {
		if _, ok := d.places[i].enablement(path); ok {
			out = append(out, &d.places[i])
		}
	}
	return out
}

// attention counts what needs attention in a place: outdated and modified
// copies, and broken links.
func (p *place) attention() int {
	n := len(p.status.Broken)
	for _, e := range p.status.Enablements {
		if slices.ContainsFunc(e.Placements, func(pl core.Placement) bool { return pl.Outdated || pl.Modified }) {
			n++
		}
	}
	return n
}

// agentName returns the display name of an agent id.
func (d *data) agentName(id string) string {
	if a, ok := agent.Find(d.agents, id); ok {
		return a.Name
	}
	return id
}

// agentNames joins the display names of agent ids.
func (d *data) agentNames(ids []string) string {
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = d.agentName(id)
	}
	return strings.Join(names, ", ")
}

// skillName is a skill's name, with its library path when several skills
// share the name (§8).
func (d *data) skillName(sk core.Skill) string {
	if d.dup[sk.Name] {
		return sk.Name + " · " + sk.Path
	}
	return sk.Name
}

// placeName is what a place is called: Global, or the project folder's name.
func placeName(sc core.Scope) string {
	if sc.IsGlobal() {
		return "Global"
	}
	return filepath.Base(sc.Root)
}

// outcome is what a piece of work did, beyond the changes the next read
// shows.
type outcome struct {
	err      error
	warnings []string
	note     string // said when the work changed nothing
}

// op is a change made off the main thread.
type op struct {
	key     string // what it works on: busy meanwhile, and taking no other op
	fail    string // the title of the alert when it fails
	discard string // what discarding local changes loses, when it is known
	run     func(force bool) outcome
	then    func() // on the main thread, once it succeeded and the read after it completed
}

// start runs o in the background, then reads everything again. The key
// stays busy until that read completes, so that the controls never go back
// to the state before the change. When core refuses to discard local
// changes, it asks whether to, and runs o again forced.
func (a *app) start(o op, force bool) {
	if a.busy[o.key] {
		return
	}
	a.busy[o.key] = true
	a.work.Go(func() {
		out := o.run(force)
		a.post(func() {
			for _, w := range out.warnings {
				a.toast(w, "warning")
			}
			if out.note != "" {
				a.toast(out.note, "")
			}
			if errors.Is(out.err, core.ErrModified) && !force {
				delete(a.busy, o.key)
				a.ask("Discard local changes?", cmp.Or(o.discard, out.err.Error()), "Discard", func() { a.start(o, true) })
				a.reload()
				return
			}
			if out.err != nil {
				a.fail(o.fail, out.err)
			}
			a.reload(func() {
				delete(a.busy, o.key)
				if out.err == nil && o.then != nil {
					o.then()
				}
			})
		})
	})
}

// busyDelay is how long work runs before its controls show it: most changes
// are done sooner, and showing them only flickers.
const busyDelay = 300 * time.Millisecond

// working reports whether the work of key shows as running: it is busy, and
// has been for busyDelay.
func (a *app) working(c *ui.Context, key string) bool {
	if !a.busy[key] {
		delete(a.busySince, key)
		return false
	}
	now := c.Now()
	since, ok := a.busySince[key]
	if !ok {
		since = now
		a.busySince[key] = now
	}
	if wait := busyDelay - now.Sub(since); wait > 0 {
		c.After(wait)
		return false
	}
	return true
}

// toggled is the value a switch or a check box shows: what it was set to
// while the change it started runs or waits to be confirmed, else on.
func (a *app) toggled(control, key string, on bool) bool {
	if v, ok := a.intents[control]; ok && (a.busy[key] || a.alertOpen) {
		return v
	}
	delete(a.intents, control)
	return on
}

// chooseFolder asks for a folder, one dialog at a time, and gives fn the
// folder chosen on the main thread.
func (a *app) chooseFolder(title string, fn func(dir string)) {
	if a.busy["folder"] {
		return
	}
	a.busy["folder"] = true
	go func() {
		dir := a.desk.chooseFolder(title)
		a.post(func() {
			delete(a.busy, "folder")
			if dir != "" {
				fn(dir)
			}
		})
	}()
}

// alert is a question or an error over the window. The last button is the
// default; choose gets the index of the one clicked.
type alert struct {
	title, message string
	buttons        []string
	choose         func(int)
}

func (a *app) showAlert(al alert) {
	a.alerts = append(a.alerts, al)
	a.alertOpen = true
}

// ask asks before an action that can't be undone.
func (a *app) ask(title, message, action string, fn func()) {
	a.showAlert(alert{title: title, message: message, buttons: []string{"Cancel", action}, choose: func(i int) {
		if i == 1 {
			fn()
		}
	}})
}

// fail says what failed and why.
func (a *app) fail(title string, err error) {
	a.showAlert(alert{title: title, message: err.Error(), buttons: []string{"OK"}})
}

func (a *app) toast(text, kind string) {
	a.toasts = append(a.toasts, ui.Toast{ID: text, Title: text, Type: kind})
}

func (a *app) view(c *ui.Context) {
	t := c.Theme()
	vibrant := c.Vibrancy()
	if vibrant {
		c.Root().Background(ui.Transparent)
	}
	for key, p := range map[ui.Key]string{ui.Key1: pageSkills, ui.Key2: pageProjects, ui.Key3: pageFind, ui.Key4: pageAgents} {
		if c.Shortcut(ui.Cmd, key) {
			a.page = p
		}
	}
	if c.Shortcut(ui.Cmd, ui.KeyF) {
		a.searchField.Focus(c)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		side := ui.Column(c).Width(sidebarWidth).Shrink(0)
		if !vibrant {
			side.Background(t.Surface)
		}
		side.Children(func() { a.sidebar(c, vibrant) })
		ui.Box(c).Width(1).Background(t.Border)
		ui.Column(c).Grow(1).MinWidth(0).Background(t.Background).Children(func() {
			switch a.page {
			case pageProjects:
				a.projectsView(c)
			case pageFind:
				a.findView(c)
			case pageAgents:
				a.agentsView(c)
			default:
				a.skillsView(c)
			}
		})
	})
	a.alertView(c)
	for _, toast := range a.toasts {
		c.AddToast(toast)
	}
	a.toasts = nil
}

func (a *app) sidebar(c *ui.Context, vibrant bool) {
	ui.Box(c).Height(titleBarHeight).Shrink(0).DragWindow()
	count := func(n int) func() {
		return func() {
			if n > 0 {
				ui.Badge(c, strconv.Itoa(n))
			}
		}
	}
	attention := 0
	for i := range a.data.places {
		if a.data.places[i].attention() > 0 {
			attention++
		}
	}
	list := ui.Sidebar(c, &a.page, func() {
		ui.SidebarItem(c, pageSkills, iconSkills, "Skills").Children(count(a.updates.available()))
		ui.SidebarItem(c, pageProjects, iconProjects, "Projects").Children(count(attention))
		ui.SidebarItem(c, pageFind, iconFind, "Find")
		ui.SidebarItem(c, pageAgents, iconAgents, "Agents")
	}).Grow(1).Label("Pages")
	if vibrant {
		list.Background(ui.Transparent)
	}
}

// header is a page's title bar: its title, and actions at the end. It drags
// the window.
func header(c *ui.Context, title string, actions func()) {
	bar := c.TitleBar()
	ui.Row(c).Height(titleBarHeight).Shrink(0).AlignItems(ui.Center).Gap(8).
		Padding(0, bar.Right+16, 0, 20).DragWindow().Children(func() {
		ui.Text(c, title).FontSize(15).Bold().SingleLine()
		ui.Spacer(c)
		if actions != nil {
			actions()
		}
	})
	ui.Divider(c)
}

func (a *app) alertView(c *ui.Context) {
	if len(a.alerts) == 0 {
		return
	}
	al := a.alerts[0]
	i := ui.AlertDialog(c, &a.alertOpen, al.title, al.message, al.buttons...)
	if a.alertOpen {
		return
	}
	a.alerts = a.alerts[1:]
	a.alertOpen = len(a.alerts) > 0
	if i >= 0 && al.choose != nil {
		al.choose(i)
	}
}

// muted is secondary text.
func muted(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).TextColor(c.Theme().TextMuted)
}

// iconButton is a button showing an icon, named for assistive technology and
// in a tooltip.
func iconButton(c *ui.Context, icon *ui.SVG, label string) ui.Element {
	return ui.Button(c, "").Label(label).Tooltip(label).Padding(4, 6).Children(func() {
		ui.Icon(c, icon)
	})
}

// tag is a small colored label for a state, as "Outdated".
func tag(c *ui.Context, text string, color ui.Color) ui.Element {
	return ui.Badge(c, text).Background(color.Alpha(0.16)).TextColor(color)
}

// section is the small title over a group of rows.
func section(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(12).FontWeight(600).TextColor(c.Theme().TextMuted)
}

// emptyState fills a pane that has nothing to show yet.
func emptyState(c *ui.Context, title, detail string, actions func()) {
	ui.Column(c).Grow(1).Center().Gap(8).Padding(24).Children(func() {
		ui.Text(c, title).FontSize(15).Bold()
		if detail != "" {
			muted(c, detail).TextAlign(ui.Center).MaxWidth(360)
		}
		if actions != nil {
			ui.Row(c).Gap(8).Margin(8, 0, 0, 0).Children(actions)
		}
	})
}

// tilde writes a path under the home directory with "~".
func tilde(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.Join("~", rel)
	}
	return path
}
