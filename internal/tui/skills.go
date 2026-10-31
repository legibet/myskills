package tui

import (
	"cmp"
	"context"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/sanitize"
	"github.com/legibet/myskills/internal/skill"
)

type tab int

const (
	tabAll tab = iota
	tabEnabled
	tabIssues
)

var tabNames = [...]string{"All", "Enabled", "Issues"}

// skillsState is the Skills view: library rows with their state in the
// active scope.
type skillsState struct {
	tab       tab
	filter    textinput.Model
	filtering bool  // the filter input has focus
	rows      []row // shown rows: tab and filter applied
	counts    [len(tabNames)]int
	stateW    int // width of the state column
	list      listCursor
	updates   map[string]core.UpdateCheck // last check results by library path
	checking  bool
	progress  [2]int // sources checked, total
	reveal    string // library path of the skill last added in Find, selected when Skills shows next
}

func newSkillsState() skillsState {
	in := textinput.New()
	in.Prompt = "/ "
	in.Placeholder = "filter by name or path"
	return skillsState{filter: in, updates: map[string]core.UpdateCheck{}}
}

// row is a library skill with its state in the active scope, or an entry of
// an agent skill directory that belongs to no library skill (Issues tab).
type row struct {
	skill  *core.Skill
	entry  *core.Placement
	en     *core.Enablement
	broken []core.Placement // broken links to this skill
	others []core.Placement // unmanaged entries with this skill's name
	global bool             // enabled globally while a project is active
	hl     []int            // fuzzy-matched rune indexes of the name
}

func (r row) key() string {
	if r.skill != nil {
		return r.skill.Path
	}
	return "entry:" + r.entry.Path
}

func (r row) name() string {
	if r.skill != nil {
		return r.skill.Name
	}
	return r.entry.Name
}

// matchText is what the filter matches: the library path, which ends with
// the name.
func (r row) matchText() string {
	if r.skill != nil {
		return r.skill.Path
	}
	return r.entry.Name
}

func (r row) in(t tab) bool {
	switch t {
	case tabAll:
		return r.skill != nil
	case tabEnabled:
		return r.en != nil
	}
	return r.hasIssue()
}

func (r row) hasIssue() bool {
	if r.entry != nil {
		return true
	}
	if r.skill.Issue != "" || len(r.broken) > 0 || len(r.others) > 0 {
		return true
	}
	return r.en != nil && slices.ContainsFunc(r.en.Placements, func(p core.Placement) bool { return p.Modified })
}

// copies returns the copy placements of the row, limited to placements of
// the given agents when agents is not nil.
func (r row) copies(agents []string) []core.Placement {
	if r.en == nil {
		return nil
	}
	var out []core.Placement
	for _, p := range r.en.Placements {
		if p.Kind != core.KindCopy {
			continue
		}
		if agents == nil || slices.ContainsFunc(p.Agents, func(id string) bool { return slices.Contains(agents, id) }) {
			out = append(out, p)
		}
	}
	return out
}

func (a *app) current() (row, bool) {
	if c := a.sk.list.cursor; c >= 0 && c < len(a.sk.rows) {
		return a.sk.rows[c], true
	}
	return row{}, false
}

// rebuildRows derives the rows from the loaded data for the active scope,
// tab and filter, keeping the highlighted row when it is still there.
func (a *app) rebuildRows() {
	keep := ""
	if r, ok := a.current(); ok {
		keep = r.key()
	}
	all := a.allRows()
	var counts [len(tabNames)]int
	var shown []row
	a.sk.stateW = 0
	for _, r := range all {
		for t := range tabNames {
			if r.in(tab(t)) {
				counts[t]++
			}
		}
		if r.in(a.sk.tab) {
			shown = append(shown, r)
			word, _ := a.stateWord(r)
			a.sk.stateW = max(a.sk.stateW, len(word))
		}
	}
	a.sk.counts = counts
	if q := strings.TrimSpace(a.sk.filter.Value()); q != "" {
		shown = filterRows(shown, q)
	}
	a.sk.rows = shown
	for i, r := range shown {
		if r.key() == keep {
			a.sk.list.cursor = i
			break
		}
	}
}

func (a *app) allRows() []row {
	st := a.data.status[a.scope]
	enabled := map[string]*core.Enablement{}
	for i := range st.Enablements {
		enabled[st.Enablements[i].Skill] = &st.Enablements[i]
	}
	global := map[string]bool{}
	if !a.scope.IsGlobal() {
		for _, e := range a.data.status[core.Global].Enablements {
			global[e.Skill] = true
		}
	}
	broken := map[string][]core.Placement{}
	for _, p := range st.Broken {
		broken[p.Skill] = append(broken[p.Skill], p)
	}
	others := map[string][]core.Placement{}
	for _, p := range st.Others {
		others[p.Name] = append(others[p.Name], p)
	}

	paths, names := map[string]bool{}, map[string]bool{}
	rows := make([]row, 0, len(a.data.lib))
	for i := range a.data.lib {
		sk := &a.data.lib[i]
		paths[sk.Path], names[sk.Name] = true, true
		rows = append(rows, row{
			skill:  sk,
			en:     enabled[sk.Path],
			broken: broken[sk.Path],
			others: others[sk.Name],
			global: global[sk.Path],
		})
	}
	var entries []row
	for i := range st.Broken {
		if !paths[st.Broken[i].Skill] {
			entries = append(entries, row{entry: &st.Broken[i]})
		}
	}
	for i := range st.Others {
		if !names[st.Others[i].Name] {
			entries = append(entries, row{entry: &st.Others[i]})
		}
	}
	slices.SortFunc(entries, func(x, y row) int { return cmp.Compare(x.entry.Name, y.entry.Name) })
	return append(rows, entries...)
}

// filterRows keeps the rows matching q, best match first.
func filterRows(rows []row, q string) []row {
	src := make([]string, len(rows))
	for i, r := range rows {
		src[i] = r.matchText()
	}
	var out []row
	for _, m := range fuzzy.Find(q, src) {
		r := rows[m.Index]
		name, text := r.name(), m.Str
		if off := len(text) - len(name); off >= 0 && strings.HasSuffix(text, name) {
			for _, i := range m.MatchedIndexes {
				if i >= off {
					r.hl = append(r.hl, utf8.RuneCountInString(name[:i-off]))
				}
			}
		}
		out = append(out, r)
	}
	return out
}

// Keys.

func (a *app) syncSkillsKeys() {
	k := &a.keys
	r, ok := a.current()
	isSkill := ok && r.skill != nil && !a.busy[r.key()]
	valid := isSkill && r.skill.Valid()

	k.Scope.SetEnabled(a.opts.Project != nil)
	k.CheckAll.SetEnabled(!a.sk.checking && slices.ContainsFunc(a.data.lib, core.Skill.Updatable))
	k.Read.SetEnabled(ok)
	k.Agents.SetEnabled(valid)
	k.Update.SetEnabled(isSkill && (r.skill.Updatable() || len(a.outdatedCopies(r.skill.Path)) > 0))
	k.Remove.SetEnabled(isSkill)
	k.Edit.SetEnabled(isSkill)
	k.Defaults.SetEnabled(a.loaded)

	switch {
	case ok && r.entry != nil:
		k.Toggle.SetEnabled(r.entry.Kind == core.KindBroken && !a.busy[r.key()])
		k.Toggle.SetHelp("space", "remove link")
		k.Copy.SetEnabled(false)
	case isSkill && (r.en != nil || len(r.broken) > 0):
		k.Toggle.SetEnabled(true)
		k.Toggle.SetHelp("space", "disable")
		k.Copy.SetEnabled(valid)
		if r.en != nil && r.en.Mode == core.Copy {
			k.Copy.SetHelp("c", "to link")
		} else {
			k.Copy.SetHelp("c", "to copy")
		}
	default:
		k.Toggle.SetEnabled(valid)
		k.Toggle.SetHelp("space", "link")
		k.Copy.SetEnabled(valid)
		k.Copy.SetHelp("c", "copy")
	}
}

func (a *app) skillsKey(msg tea.KeyPressMsg) tea.Cmd {
	if a.sk.filtering {
		return a.filterKey(msg)
	}
	k := a.keys
	n := len(a.sk.rows)
	page := max(1, a.layout().bodyH-2)
	if a.help.ShowAll && msg.String() == "esc" {
		a.help.ShowAll = false
		return nil
	}
	switch {
	case key.Matches(msg, k.Quit):
		return a.quit()
	case key.Matches(msg, k.Up):
		a.sk.list.move(-1, n)
	case key.Matches(msg, k.Down):
		a.sk.list.move(1, n)
	case key.Matches(msg, k.Top):
		a.sk.list.cursor = 0
	case key.Matches(msg, k.Bottom):
		a.sk.list.cursor = n - 1
	case key.Matches(msg, k.PageUp):
		a.sk.list.move(-page, n)
	case key.Matches(msg, k.PageDown):
		a.sk.list.move(page, n)
	case key.Matches(msg, k.NextTab):
		a.setTab((a.sk.tab + 1) % tab(len(tabNames)))
	case key.Matches(msg, k.PrevTab):
		a.setTab((a.sk.tab + tab(len(tabNames)) - 1) % tab(len(tabNames)))
	case key.Matches(msg, k.Filter):
		a.sk.filtering = true
		return a.sk.filter.Focus()
	case msg.String() == "esc" && a.sk.filter.Value() != "":
		a.sk.filter.SetValue("")
		a.rebuildRows()
	case key.Matches(msg, k.Help):
		a.help.ShowAll = !a.help.ShowAll
	case key.Matches(msg, k.Find):
		return a.enterFind(false)
	case key.Matches(msg, k.Scope):
		if other, ok := a.otherScope(); ok {
			a.scope = other
			a.rebuildRows()
		}
	case key.Matches(msg, k.Defaults):
		a.defaultsModal()
	case key.Matches(msg, k.CheckAll):
		return a.checkUpdates()
	case key.Matches(msg, k.Reload):
		return a.reload()
	case key.Matches(msg, k.ScrollDown):
		a.pv.vp.HalfPageDown()
	case key.Matches(msg, k.ScrollUp):
		a.pv.vp.HalfPageUp()
	case key.Matches(msg, k.Read):
		a.openReader()
	case key.Matches(msg, k.Toggle):
		return a.toggle()
	case key.Matches(msg, k.Copy):
		return a.toggleCopy()
	case key.Matches(msg, k.Agents):
		a.agentsModal()
	case key.Matches(msg, k.Update):
		return a.updateSkill()
	case key.Matches(msg, k.Remove):
		a.removeModal()
	case key.Matches(msg, k.Edit):
		return a.edit()
	}
	return nil
}

func (a *app) filterKey(msg tea.KeyPressMsg) tea.Cmd {
	k := a.fkeys
	switch {
	case key.Matches(msg, k.Clear):
		a.sk.filter.SetValue("")
		a.sk.filter.Blur()
		a.sk.filtering = false
		a.rebuildRows()
		return nil
	case key.Matches(msg, k.Apply):
		a.sk.filter.Blur()
		a.sk.filtering = false
		return nil
	case key.Matches(msg, k.Up):
		a.sk.list.move(-1, len(a.sk.rows))
		return nil
	case key.Matches(msg, k.Down):
		a.sk.list.move(1, len(a.sk.rows))
		return nil
	}
	return a.filterInput(msg)
}

func (a *app) filterInput(msg tea.Msg) tea.Cmd {
	before := a.sk.filter.Value()
	var cmd tea.Cmd
	a.sk.filter, cmd = a.sk.filter.Update(msg)
	if a.sk.filter.Value() != before {
		a.rebuildRows()
		a.sk.list = listCursor{}
	}
	return cmd
}

func (a *app) setTab(t tab) {
	a.sk.tab = t
	a.rebuildRows()
}

// revealAdded selects the skill last added in Find, showing the All tab and
// clearing the filter when they hide it. Until a load brings the skill in, it
// waits (applyLoad calls it again).
func (a *app) revealAdded() {
	p := a.sk.reveal
	if !a.inLibrary(p) {
		return
	}
	a.sk.reveal = ""
	has := func(r row) bool { return r.key() == p }
	if !slices.ContainsFunc(a.sk.rows, has) {
		a.sk.tab = tabAll
		a.sk.filter.SetValue("")
		a.rebuildRows()
	}
	if i := slices.IndexFunc(a.sk.rows, has); i >= 0 {
		a.sk.list.cursor = i
	}
}

// Actions.

// toggle is space: enable an off skill as a link, disable an enabled one,
// remove a broken link.
func (a *app) toggle() tea.Cmd {
	r, _ := a.current()
	if r.entry != nil {
		return a.start(a.removeEntryOp(*r.entry), false)
	}
	sk := *r.skill
	if r.en != nil || len(r.broken) > 0 {
		return a.confirmCopies(r, a.disableOp(r, nil), r.copies(nil), "Disable "+sanitize.Line(sk.Name)+"?", "Disable")
	}
	return a.start(a.enableOp(r, core.EnableOptions{Mode: core.Link}, "Enabled %s (link)"), false)
}

// toggleCopy is c: enable an off skill as a copy for the default agents, or
// convert an enablement's links to copies and copies to links, for the agents
// it is placed for.
func (a *app) toggleCopy() tea.Cmd {
	r, _ := a.current()
	switch {
	case r.en == nil:
		return a.start(a.enableOp(r, core.EnableOptions{Mode: core.Copy}, "Enabled %s (copy)"), false)
	case r.en.Mode == core.Copy:
		m := a.enableOp(r, core.EnableOptions{Mode: core.Link, Agents: placedIDs(r.en)}, "Switched %s to links")
		return a.confirmCopies(r, m, r.copies(nil), "Replace the copies of "+sanitize.Line(r.skill.Name)+" with links?", "Replace")
	}
	return a.start(a.enableOp(r, core.EnableOptions{Mode: core.Copy, Agents: placedIDs(r.en)}, "Switched %s to copies"), false)
}

// confirmCopies runs m after asking, when it deletes copies.
func (a *app) confirmCopies(r row, m mutation, copies []core.Placement, title, action string) tea.Cmd {
	if len(copies) == 0 {
		return a.start(m, false)
	}
	if slices.ContainsFunc(copies, func(p core.Placement) bool { return p.Modified }) {
		a.modal = confirmModal("Discard local changes?", a.lossText(copies), "Discard", true, func() tea.Cmd { return a.start(m, true) })
		return nil
	}
	lines := []string{"This deletes the copied files of " + sanitize.Line(r.name()) + ":"}
	for _, p := range copies {
		lines = append(lines, "  "+a.placeLabel(p))
	}
	a.modal = confirmModal(title, strings.Join(lines, "\n"), action, true, func() tea.Cmd { return a.start(m, false) })
	return nil
}

func (a *app) enableOp(r row, opts core.EnableOptions, verb string) mutation {
	svc, scope, names, sk := a.svc, a.scope, a.agentNames(), *r.skill
	return mutation{key: sk.Path, discard: a.discardText(sk.Path, a.scope), run: func(force bool) outcome {
		opts.Force = force
		res, err := svc.Enable(scope, sk, opts)
		out := describe(res, err, verb, sk.Name, scope, names)
		if err == nil && len(out.lines) == 0 {
			out.note = sanitize.Line(sk.Name) + " is already enabled that way"
		}
		return out
	}}
}

func (a *app) disableOp(r row, agents []string) mutation {
	svc, scope, names, sk := a.svc, a.scope, a.agentNames(), *r.skill
	return mutation{key: sk.Path, discard: a.discardText(sk.Path, a.scope), run: func(force bool) outcome {
		res, err := svc.Disable(scope, sk.Path, core.DisableOptions{Agents: agents, Force: force})
		return describe(res, err, "Disabled %s", sk.Name, scope, names)
	}}
}

// removeEntryOp removes a broken link whose library skill no longer exists.
// It names the link by its missing library path, never by its entry name,
// which may belong to another library skill.
func (a *app) removeEntryOp(p core.Placement) mutation {
	svc, scope, names := a.svc, a.scope, a.agentNames()
	return mutation{key: "entry:" + p.Path, run: func(force bool) outcome {
		res, err := svc.Disable(scope, p.Skill, core.DisableOptions{Agents: p.Agents, Force: force})
		return describe(res, err, "Removed the broken link %s", p.Name, scope, names)
	}}
}

// agentsModal is a: a checklist of the agent skill directories of the active
// scope, checked where the skill is placed. Agents whose directories resolve
// to the same place share its placements, so they form one item.
func (a *app) agentsModal() {
	r, _ := a.current()
	sk := *r.skill
	dirs := a.data.dirs[a.scope]
	if len(dirs) == 0 {
		a.modal = confirmModal("No agents", "No agent has a skill directory for this scope.", "OK", false, func() tea.Cmd { return nil })
		return
	}
	placed := map[string]bool{}
	mode := core.Link
	if r.en != nil {
		for _, p := range r.en.Placements {
			for _, id := range p.Agents {
				placed[id] = true
			}
		}
		if r.en.Mode != core.Mixed {
			mode = r.en.Mode
		}
	}
	before := make([]bool, len(dirs))
	items := make([]checkItem, len(dirs))
	for i, d := range dirs {
		before[i] = slices.ContainsFunc(d.Agents, func(id string) bool { return placed[id] })
		items[i] = checkItem{label: strings.Join(a.namesOf(d.Agents), ", "), detail: pathLabel(a.scope.Root, d.Path), checked: before[i]}
	}
	where := "in this project"
	if a.scope.IsGlobal() {
		where = "globally"
	}
	kind := "links"
	if mode == core.Copy {
		kind = "copies"
	}
	body := "Agents that load " + sanitize.Line(sk.Name) + " " + where + ". New placements are " + kind + "."
	a.modal = checklistModal(sanitize.Line(sk.Name)+" · agents", body, items, func(checked []bool) tea.Cmd {
		var add, remove []string
		for i, d := range dirs {
			switch {
			case checked[i] && !before[i]:
				add = append(add, d.Agents...)
			case !checked[i] && before[i]:
				remove = append(remove, d.Agents...)
			}
		}
		m := a.agentsOp(r, add, remove, mode)
		switch {
		case len(add)+len(remove) == 0:
			return nil
		case len(remove) == 0: // adding agents deletes nothing
			return a.start(m, false)
		}
		return a.confirmCopies(r, m, r.copies(remove), "Disable "+sanitize.Line(sk.Name)+" for these agents?", "Disable")
	})
}

func (a *app) agentsOp(r row, add, remove []string, mode core.Mode) mutation {
	svc, scope, names, sk := a.svc, a.scope, a.agentNames(), *r.skill
	var discard string
	if len(remove) > 0 {
		discard = a.lossText(r.copies(remove))
	}
	return mutation{key: sk.Path, discard: discard, run: func(force bool) outcome {
		var out outcome
		if len(add) > 0 {
			res, err := svc.Enable(scope, sk, core.EnableOptions{Agents: add})
			out.merge(describe(res, err, "Enabled %s ("+string(mode)+")", sk.Name, scope, names))
			if err != nil {
				return out
			}
		}
		if len(remove) > 0 {
			res, err := svc.Disable(scope, sk.Path, core.DisableOptions{Agents: remove, Force: force})
			out.merge(describe(res, err, "Disabled %s", sk.Name, scope, names))
		}
		return out
	}}
}

// defaultsModal is A: the agents used for new enablements.
func (a *app) defaultsModal() {
	current := a.data.config.DefaultAgents()
	items := make([]checkItem, len(a.data.agents))
	for i, ag := range a.data.agents {
		var dirs []string
		if ag.Project != "" {
			dirs = append(dirs, sanitize.Line(ag.Project))
		}
		if d, ok := ag.GlobalDir(); ok {
			dirs = append(dirs, tildePath(d))
		}
		items[i] = checkItem{label: sanitize.Line(ag.Name), detail: strings.Join(dirs, " · "), checked: slices.Contains(current, ag.ID)}
	}
	agents, svc := a.data.agents, a.svc
	a.modal = checklistModal("Default agents",
		"New enablements are placed for these agents. Skills already enabled keep their placements.",
		items, func(checked []bool) tea.Cmd {
			var ids, names []string
			for i, on := range checked {
				if on {
					ids = append(ids, agents[i].ID)
					names = append(names, agents[i].Name)
				}
			}
			if slices.Equal(ids, current) {
				return nil
			}
			return a.start(mutation{key: "config:agents", run: func(bool) outcome {
				if err := svc.SetDefaultAgents(ids); err != nil {
					return outcome{err: err}
				}
				return outcome{lines: []string{"Default agents: " + strings.Join(names, ", ")}}
			}}, false)
		})
	a.modal.validate = func(checked []bool) string {
		if !slices.Contains(checked, true) {
			return "Choose at least one agent."
		}
		return ""
	}
}

// removeModal is x: confirm removing the skill, or its whole linked entry,
// from the library.
func (a *app) removeModal() {
	r, _ := a.current()
	sk := *r.skill
	name := sanitize.Line(sk.Name)
	affected := []string{sk.Path}
	var body []string
	if sk.Linked {
		var others []string
		for _, o := range a.data.lib {
			if o.Entry == sk.Entry && o.Path != sk.Path {
				others = append(others, sanitize.Line(o.Name))
				affected = append(affected, o.Path)
			}
		}
		body = append(body, fmt.Sprintf("%s belongs to the linked entry %s (%s). The whole entry leaves the library: only the link is removed, never the files behind it.",
			name, sanitize.Line(sk.Entry), sanitize.Line(sk.SourceLabel())))
		if len(others) > 0 {
			body = append(body, "Also removed: "+strings.Join(others, ", ")+".")
		}
	} else {
		body = append(body, "Deletes "+tildePath(sk.Dir)+".")
	}
	var scopes []core.Scope
	for _, p := range affected {
		for _, sc := range a.data.usage[p] {
			if !slices.Contains(scopes, sc) {
				scopes = append(scopes, sc)
			}
		}
	}
	if len(scopes) > 0 {
		body = append(body, "Enabled in "+scopeList(scopes)+": links there are removed, copies stay as unmanaged files.")
	}
	svc, docKey := a.svc, dirDoc(sk.Dir).key
	m := mutation{key: sk.Path, run: func(bool) outcome {
		res, err := svc.Remove(sk)
		if err != nil {
			return outcome{err: err}
		}
		line := "Removed " + name + " from the library"
		if res.Entry != "" && res.Entry != sk.Path {
			line = "Removed the linked entry " + res.Entry + " (" + plural(len(res.Skills), "skill", "skills") + ")"
		}
		if n := len(res.Unlinked); n > 0 {
			line += ", " + plural(n, "link", "links") + " removed"
		}
		out := outcome{lines: []string{line}}
		if n := len(res.Released); n > 0 {
			out.warnings = append(out.warnings, plural(n, "copy", "copies")+" left in place as unmanaged files")
		}
		return out
	}, done: func(outcome) tea.Cmd {
		a.pv.invalidate(docKey)
		return nil
	}}
	a.modal = confirmModal("Remove "+name+" from the library?", strings.Join(body, "\n\n"), "Remove", true, func() tea.Cmd {
		return a.start(m, false)
	})
}

// updateSkill is u: install an available upstream update of the skill, then
// refresh its outdated copies in the project and global scopes. The library
// and the copies of each scope are separate mutations, run one after another,
// so that discarding the local changes of one forces only that one.
func (a *app) updateSkill() tea.Cmd {
	r, _ := a.current()
	sk := *r.skill
	svc, ctx, scopes, names, name := a.svc, a.ctx, a.scopes(), a.agentNames(), sanitize.Line(sk.Name)
	// copies refreshes the copies in scopes[i], then in the scopes after it;
	// changed tells whether the steps before it changed or warned about
	// anything.
	var copies func(i int, changed bool) mutation
	copies = func(i int, changed bool) mutation {
		sc := scopes[i]
		return mutation{key: sk.Path, discard: a.discardText(sk.Path, sc), run: func(force bool) outcome {
			var out outcome
			res, err := svc.RefreshCopies(sc, []string{sk.Path}, force)
			for _, r := range res {
				out.merge(describe(r, nil, "Refreshed the copies of %s", sk.Name, sc, names))
			}
			out.err = err
			if err == nil && i == len(scopes)-1 && !changed && len(out.lines)+len(out.warnings) == 0 {
				out.note = name + " is up to date"
			}
			return out
		}, done: func(out outcome) tea.Cmd {
			if out.err != nil || i == len(scopes)-1 {
				return nil
			}
			return a.start(copies(i+1, changed || len(out.lines)+len(out.warnings) > 0), false)
		}}
	}
	if !sk.Updatable() {
		return a.start(copies(0, false), false)
	}
	m := mutation{key: sk.Path, run: func(force bool) outcome {
		return applyUpdate(ctx, svc, sk, force)
	}, done: func(out outcome) tea.Cmd {
		a.pv.invalidate(dirDoc(sk.Dir).key)
		if out.err != nil {
			return nil
		}
		delete(a.sk.updates, sk.Path)
		return a.start(copies(0, len(out.lines)+len(out.warnings) > 0), false)
	}}
	if sk.Modified {
		m.discard = "Your changes to " + name + " in the library are replaced by the update."
	}
	return a.start(m, false)
}

// applyUpdate checks the skill and installs its update, if any.
func applyUpdate(ctx context.Context, svc service, sk core.Skill, force bool) outcome {
	name := sanitize.Line(sk.Name)
	u, err := svc.CheckUpdates(ctx, []core.Skill{sk}, nil)
	if err != nil {
		return outcome{err: err}
	}
	defer func() { _ = u.Close() }()
	var check core.UpdateCheck
	for _, c := range u.Checks {
		if c.Skill.Path == sk.Path {
			check = c
		}
	}
	var out outcome
	switch check.State {
	case core.Available:
		for _, r := range svc.ApplyUpdates(u, []string{sk.Path}, force) {
			if r.Err != nil {
				out.err = r.Err
				return out
			}
			if r.Updated {
				out.lines = append(out.lines, "Updated "+name)
			}
		}
	case core.RemovedUpstream:
		out.warnings = append(out.warnings, name+" was removed upstream; the library keeps its copy")
	case core.CheckFailed:
		out.err = check.Err
	}
	return out
}

// reload is r, and runs when the terminal comes back into focus: read the
// library, the scopes and the shown SKILL.md files again, since they may
// have changed outside the app.
func (a *app) reload() tea.Cmd {
	return tea.Batch(a.load(), a.pv.recheckLocal())
}

type checkProgressMsg struct {
	done, total int
	ch          <-chan checkProgressMsg
	stop        <-chan struct{}
}

type checkDoneMsg struct {
	checks []core.UpdateCheck
	err    error
}

// checkUpdates is r: check every updatable skill in the background.
func (a *app) checkUpdates() tea.Cmd {
	var skills []core.Skill
	for _, sk := range a.data.lib {
		if sk.Updatable() {
			skills = append(skills, sk)
		}
	}
	a.sk.checking = true
	a.sk.progress = [2]int{}
	ch := make(chan checkProgressMsg, 64)
	stop := make(chan struct{})
	svc, ctx := a.svc, a.ctx
	run := func() tea.Msg {
		defer close(stop)
		u, err := svc.CheckUpdates(ctx, skills, func(done, total int) {
			select {
			case ch <- checkProgressMsg{done: done, total: total, ch: ch, stop: stop}:
			default:
			}
		})
		if err != nil {
			return checkDoneMsg{err: err}
		}
		defer func() { _ = u.Close() }()
		return checkDoneMsg{checks: u.Checks}
	}
	return tea.Batch(run, waitProgress(ch, stop))
}

func waitProgress(ch <-chan checkProgressMsg, stop <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		select {
		case m := <-ch:
			return m
		case <-stop:
			return nil
		}
	}
}

type editedMsg struct {
	doc string
	err error
}

// edit is e: open SKILL.md in $VISUAL or $EDITOR, then reload.
func (a *app) edit() tea.Cmd {
	r, _ := a.current()
	editor := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"))
	if strings.TrimSpace(editor) == "" {
		return a.setStatus(stWarn, "Set $VISUAL or $EDITOR to edit skills")
	}
	args := strings.Fields(editor)
	c := exec.Command(args[0], append(args[1:], filepath.Join(r.skill.Dir, skill.FileName))...)
	key := dirDoc(r.skill.Dir).key
	return tea.ExecProcess(c, func(err error) tea.Msg { return editedMsg{doc: key, err: err} })
}

func (a *app) updateSkills(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case checkProgressMsg:
		a.sk.progress = [2]int{msg.done, msg.total}
		return waitProgress(msg.ch, msg.stop), true
	case checkDoneMsg:
		a.sk.checking = false
		if msg.err != nil {
			return a.setStatus(stError, "Update check failed: "+msg.err.Error()), true
		}
		a.sk.updates = map[string]core.UpdateCheck{}
		var avail, gone, failed int
		for _, c := range msg.checks {
			a.sk.updates[c.Skill.Path] = c
			switch c.State {
			case core.Available:
				avail++
			case core.RemovedUpstream:
				gone++
			case core.CheckFailed:
				failed++
			}
		}
		if avail+gone+failed == 0 {
			return a.setStatus(stSuccess, "Everything is up to date"), true
		}
		var parts []string
		if avail > 0 {
			parts = append(parts, plural(avail, "update", "updates")+" available")
		}
		if gone > 0 {
			parts = append(parts, strconv.Itoa(gone)+" removed upstream")
		}
		if failed > 0 {
			parts = append(parts, plural(failed, "check", "checks")+" failed")
		}
		kind := stInfo
		if gone+failed > 0 {
			kind = stWarn
		}
		return a.setStatus(kind, strings.Join(parts, " · ")), true
	case editedMsg:
		a.pv.invalidate(msg.doc)
		cmds := []tea.Cmd{a.load()}
		if msg.err != nil {
			cmds = append(cmds, a.setStatus(stError, "Editor failed: "+msg.err.Error()))
		}
		return tea.Batch(cmds...), true
	}
	return nil, false
}

// Data helpers.

// agentNames maps agent ids to display names.
func (a *app) agentNames() map[string]string {
	names := make(map[string]string, len(a.data.agents))
	for _, ag := range a.data.agents {
		names[ag.ID] = ag.Name
	}
	return names
}

// namesOf returns the display names of agent ids, each on one line.
func (a *app) namesOf(ids []string) []string {
	names := a.agentNames()
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, sanitize.Line(cmp.Or(names[id], id)))
	}
	return out
}

// placedIDs returns the ids of the agents an enablement is placed for.
func placedIDs(e *core.Enablement) []string {
	var ids []string
	for _, p := range e.Placements {
		for _, id := range p.Agents {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// placedAgents returns the display names of the agents of an enablement.
func (a *app) placedAgents(e *core.Enablement) []string {
	return a.namesOf(placedIDs(e))
}

// missingAgents returns the default agents with a directory in the active
// scope that lack a placement of the enablement (why it is partial).
func (a *app) missingAgents(e *core.Enablement) []string {
	have := map[string]bool{}
	for _, p := range e.Placements {
		for _, id := range p.Agents {
			have[id] = true
		}
	}
	var missing []string
	for _, d := range a.data.dirs[a.scope] {
		for _, id := range d.Agents {
			if slices.Contains(a.data.config.DefaultAgents(), id) && !have[id] {
				missing = append(missing, id)
			}
		}
	}
	return a.namesOf(missing)
}

// placeLabel shows a placement relative to the project, or with ~ outside it.
func (a *app) placeLabel(p core.Placement) string {
	if a.opts.Project == nil {
		return tildePath(p.Path)
	}
	return pathLabel(a.opts.Project.Root, p.Path)
}

// pathLabel shows path relative to the project root when it lies inside it,
// and with "~" otherwise, on one line.
func pathLabel(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && root != "" && !strings.HasPrefix(rel, "..") {
		return sanitize.Line(filepath.ToSlash(rel))
	}
	return tildePath(path)
}

// discardText explains which local changes forcing a change to the skill in
// scope loses.
func (a *app) discardText(skillPath string, scope core.Scope) string {
	for _, e := range a.data.status[scope].Enablements {
		if e.Skill == skillPath {
			return a.lossText(e.Placements)
		}
	}
	return ""
}

// lossText lists the modified copies among placements, whose changes a
// forced change loses.
func (a *app) lossText(placements []core.Placement) string {
	var paths []string
	for _, p := range placements {
		if p.Modified {
			paths = append(paths, "  "+a.placeLabel(p))
		}
	}
	if len(paths) == 0 {
		return ""
	}
	return "These copies have local changes that will be lost:\n" + strings.Join(paths, "\n")
}

// outdatedCopies returns the outdated copy placements of a skill in every
// loaded scope.
func (a *app) outdatedCopies(skillPath string) []core.Placement {
	var out []core.Placement
	for _, st := range a.data.status {
		for _, e := range st.Enablements {
			if e.Skill != skillPath {
				continue
			}
			for _, p := range e.Placements {
				if p.Outdated {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

func scopeList(scopes []core.Scope) string {
	labels := make([]string, len(scopes))
	for i, sc := range scopes {
		labels[i] = sanitize.Line(sc.String())
	}
	return strings.Join(labels, ", ")
}

// describe turns the result of enabling or disabling into status lines.
func describe(res core.Result, err error, verb, name string, scope core.Scope, names map[string]string) outcome {
	out := outcome{err: err, warnings: slices.Clone(res.Warnings)}
	var agents []string
	changed := false
	for _, c := range res.Changes {
		if c.Warning != "" {
			out.warnings = append(out.warnings, pathLabel(scope.Root, c.Path)+": "+c.Warning)
		}
		if c.Action == core.Unchanged {
			continue
		}
		changed = true
		for _, id := range c.Agents {
			if n := cmp.Or(names[id], id); !slices.Contains(agents, n) {
				agents = append(agents, n)
			}
		}
	}
	if !changed {
		return out
	}
	line := fmt.Sprintf(verb, sanitize.Line(name))
	if len(agents) > 0 {
		line += " in " + strings.Join(agents, ", ")
	}
	if scope.IsGlobal() {
		line += " (global)"
	}
	out.lines = append(out.lines, line)
	return out
}

// Rendering.

// skillsPlaceholder renders the body when there is no list to show: while
// loading, when the library cannot be read, and when it is empty.
func (a *app) skillsPlaceholder(l layout) (string, bool) {
	t := a.th
	w, h := a.width, l.bodyH
	switch {
	case !a.loaded:
		return centered(w, h, t.dim.Render("Loading your library…")), true
	case a.data.lib == nil && a.data.err != nil:
		return centered(w, h,
			t.bad.Render("Couldn't read the library"),
			"",
			t.dim.Render(truncate(sanitize.Line(a.data.err.Error()), w-4))), true
	case len(a.data.lib) == 0 && a.sk.tab == tabAll && a.sk.filter.Value() == "":
		block := centered(w, h-1,
			t.bold.Render("Your library is empty"),
			"",
			t.dim.Render("Add skills to your library, then enable them here."),
			"",
			t.bold.Render("f")+t.dim.Render("  search skills.sh or browse a repository"),
			t.bold.Render("myskills add <source>")+t.dim.Render("  from the command line"))
		return " " + a.tabsLine(w-2) + "\n" + block, true
	}
	return "", false
}

func (a *app) skillsListView(w, h int) string {
	lines := []string{a.tabsLine(w)}
	if len(a.sk.rows) == 0 {
		lines = append(lines, "", " "+a.emptyTabText())
		return strings.Join(lines, "\n")
	}
	start, end := a.sk.list.window(len(a.sk.rows), h-1)
	for i := start; i < end; i++ {
		lines = append(lines, a.skillRow(a.sk.rows[i], w, i == a.sk.list.cursor))
	}
	return strings.Join(lines, "\n")
}

func (a *app) emptyTabText() string {
	t := a.th
	switch {
	case a.sk.filter.Value() != "":
		return t.dim.Render("No skills match “" + sanitize.Line(a.sk.filter.Value()) + "”")
	case a.sk.tab == tabEnabled && a.scope.IsGlobal():
		return t.dim.Render("Nothing is enabled globally yet")
	case a.sk.tab == tabEnabled:
		return t.dim.Render("Nothing is enabled in this project yet")
	case a.sk.tab == tabIssues:
		return t.good.Render("✓ No issues")
	}
	return t.dim.Render("No skills")
}

// tabsLine is the top line of the list: the tabs with their counts and the
// filter, or the filter input while it has focus.
func (a *app) tabsLine(w int) string {
	t := a.th
	if a.sk.filtering {
		count := t.hint.Render(strconv.Itoa(len(a.sk.rows)) + " ")
		return " " + layoutLine(w-1, a.sk.filter.View(), count)
	}
	var tabs []string
	for i, name := range tabNames {
		n := strconv.Itoa(a.sk.counts[i])
		if tab(i) == a.sk.tab {
			tabs = append(tabs, t.bold.Render(name)+" "+t.accented.Render(n))
		} else {
			tabs = append(tabs, t.dim.Render(name)+" "+t.hint.Render(n))
		}
	}
	right := t.hint.Render("/ filter ")
	if v := a.sk.filter.Value(); v != "" {
		right = t.accented.Render("/ "+truncate(sanitize.Line(v), 16)) + " "
	}
	return " " + layoutLine(w-1, strings.Join(tabs, "   "), right)
}

// layoutLine puts left and right on one line of width w; right is dropped
// when both do not fit.
func layoutLine(w int, left, right string) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return ansi.Truncate(left, w, "…")
	}
	return left + strings.Repeat(" ", gap) + right
}

func (a *app) skillRow(r row, w int, selected bool) string {
	t := a.th
	var bg color.Color
	nameStyle := t.text
	if selected {
		bg, nameStyle = t.selection, t.bold
	}
	glyph, gs := a.glyph(r)
	if a.busy[r.key()] {
		sp := a.spin
		if bg != nil {
			sp.Style = sp.Style.Background(bg)
		}
		glyph, gs = sp.View(), lipgloss.NewStyle()
	}
	left := []seg{
		cursorSeg(t, selected),
		{text: glyph, style: gs},
		{text: " "},
		{text: sanitize.Line(r.name()), style: nameStyle, hl: r.hl, hlStyle: nameStyle.Foreground(t.accent)},
	}
	if r.entry != nil {
		left = append(left, seg{text: "  " + strings.Join(a.namesOf(r.entry.Agents), ", "), style: t.hint})
	} else {
		if dir := path.Dir(r.skill.Path); dir != "." && a.data.dups[r.skill.Name] {
			left = append(left, seg{text: "  " + sanitize.Line(dir), style: t.hint})
		}
		left = append(left, seg{text: "  " + sanitize.Line(r.skill.SourceLabel()), style: t.hint})
	}

	var right []seg
	for _, b := range a.badges(r) {
		right = append(right, b, seg{text: "  "})
	}
	if a.sk.stateW > 0 {
		word, ws := a.stateWord(r)
		right = append(right, seg{text: word + strings.Repeat(" ", a.sk.stateW-len(word)), style: ws})
	} else if len(right) > 0 {
		right = right[:len(right)-1]
	}
	right = append(right, seg{text: " "})
	return layoutRow(w, bg, left, right)
}

// glyph is the row's state in the active scope.
func (a *app) glyph(r row) (string, lipgloss.Style) {
	t := a.th
	switch {
	case r.entry != nil && r.entry.Kind == core.KindBroken, len(r.broken) > 0:
		return "✗", t.bad
	case r.entry != nil:
		return "◇", t.hint
	case r.en != nil && (r.en.Mode == core.Mixed || r.en.Partial):
		return "◐", t.warn
	case r.en != nil && r.en.Mode == core.Copy:
		return "◆", t.accented
	case r.en != nil:
		return "●", t.accented
	case len(r.others) > 0:
		return "✗", t.bad
	}
	return "○", t.hint
}

// stateWord is the right column: how the skill is enabled in the active
// scope (its mode, or "global" when a project uses it only from the global
// scope), or what is wrong.
func (a *app) stateWord(r row) (string, lipgloss.Style) {
	t := a.th
	switch {
	case r.entry != nil && r.entry.Kind == core.KindBroken, len(r.broken) > 0:
		return "broken", t.bad
	case r.entry != nil:
		return "unmanaged", t.dim
	case r.en != nil:
		return string(r.en.Mode), t.dim
	case len(r.others) > 0:
		return "conflict", t.bad
	case r.global:
		return "global", t.dim
	}
	return "", t.dim
}

func (a *app) badges(r row) []seg {
	if r.skill == nil {
		return nil
	}
	t := a.th
	var out []seg
	add := func(text string, st lipgloss.Style) { out = append(out, seg{text: text, style: st}) }
	if r.en != nil && slices.ContainsFunc(r.en.Placements, func(p core.Placement) bool { return p.Outdated }) {
		add("outdated", t.warn)
	}
	if r.skill.Modified || r.en != nil && slices.ContainsFunc(r.en.Placements, func(p core.Placement) bool { return p.Modified }) {
		add("modified", t.warn)
	}
	if a.sk.updates[r.skill.Path].State == core.Available {
		add("update", t.accented)
	}
	if !r.skill.Valid() {
		add("invalid", t.bad)
	}
	return out
}

// skillPreview returns what the preview pane shows for the highlighted row.
func (a *app) skillPreview(w int) (item string, header []string, intro string, d *doc) {
	r, ok := a.current()
	if !ok {
		return "", nil, "", nil
	}
	t := a.th
	if r.entry != nil {
		return a.entryPreview(r, w)
	}
	sk := r.skill
	title := t.bold.Render(sanitize.Line(sk.Name))
	if dir := path.Dir(sk.Path); dir != "." && a.data.dups[sk.Name] {
		title += "  " + t.hint.Render(sanitize.Line(dir))
	}
	from := sanitize.Line(sk.SourceLabel())
	if sk.Record != nil && sk.Record.Source.Type == "local" {
		from = tildePath(sk.Record.Source.Path)
	}
	header = []string{title, t.dim.Render(from), a.stateLine(r)}
	var notes []string
	note := func(st lipgloss.Style, s string) { notes = append(notes, wrapText(st.Render(s), w)) }
	if sk.Issue != "" {
		note(t.bad, "✗ Invalid: "+sanitize.Line(sk.Issue)+".")
	}
	for _, p := range r.broken {
		note(t.bad, "✗ Broken link "+a.placeLabel(p)+": the library folder is missing.")
	}
	for _, p := range r.others {
		note(t.bad, "✗ "+a.placeLabel(p)+" is not managed by myskills; enabling for "+
			strings.Join(a.namesOf(p.Agents), ", ")+" fails until it is moved.")
	}
	if r.en != nil {
		for _, p := range r.en.Placements {
			if p.Modified {
				note(t.warn, "! Local changes in "+a.placeLabel(p))
			}
			if p.Outdated {
				note(t.warn, "! "+a.placeLabel(p)+" is older than the library.")
			}
		}
	}
	if sk.Modified {
		note(t.warn, "! Changed since it was added.")
	}
	switch c := a.sk.updates[sk.Path]; c.State {
	case core.Available:
		note(t.accented, "↑ Update available.")
	case core.RemovedUpstream:
		note(t.warn, "! Removed upstream.")
	case core.CheckFailed:
		note(t.bad, "✗ Update check failed: "+sanitize.Line(fmt.Sprint(c.Err)))
	}
	if scopes := a.data.usage[sk.Path]; len(scopes) > 0 {
		note(t.dim, "Enabled in "+scopeList(scopes))
	}
	intro = strings.Join(notes, "\n")
	dd := dirDoc(sk.Dir)
	return sk.Path, header, intro, &dd
}

func (a *app) stateLine(r row) string {
	t := a.th
	dot := t.hint.Render(" · ")
	switch {
	case r.en != nil:
		glyph, gs := a.glyph(r)
		line := gs.Render(glyph) + " " + t.text.Render(string(r.en.Mode)) + dot +
			t.dim.Render(strings.Join(a.placedAgents(r.en), ", "))
		if missing := a.missingAgents(r.en); len(missing) > 0 {
			line += dot + t.warn.Render("not in "+strings.Join(missing, ", "))
		}
		return line
	case len(r.broken) > 0:
		return t.bad.Render("✗ broken link")
	case len(r.others) > 0:
		return t.bad.Render("✗ conflict")
	case a.scope.IsGlobal():
		return t.hint.Render("○ not enabled globally")
	}
	return t.hint.Render("○ not enabled in this project")
}

func (a *app) entryPreview(r row, w int) (item string, header []string, intro string, d *doc) {
	t := a.th
	p := r.entry
	agents := strings.Join(a.namesOf(p.Agents), ", ")
	header = []string{t.bold.Render(sanitize.Line(p.Name)), t.dim.Render(a.placeLabel(*p))}
	if p.Kind == core.KindBroken {
		header = append(header, t.bad.Render("✗ broken link")+t.hint.Render(" · ")+t.dim.Render(agents))
		intro = wrapText("This link points to "+sanitize.Line(p.Skill)+" in the library, which no longer exists.", w)
		return r.key(), header, intro, nil
	}
	header = append(header, t.hint.Render("◇ unmanaged · ")+t.dim.Render(agents))
	intro = wrapText(t.dim.Render("Not created by myskills: it is listed here but never changed."), w)
	dd := dirDoc(p.Path)
	return r.key(), header, intro, &dd
}

// wrapText wraps styled text to w cells.
func wrapText(s string, w int) string {
	return lipgloss.NewStyle().Width(w).Render(s)
}

// joinBlocks joins non-empty blocks with a blank line.
func joinBlocks(blocks ...string) string {
	var out []string
	for _, b := range blocks {
		if b != "" {
			out = append(out, b)
		}
	}
	return strings.Join(out, "\n\n")
}
