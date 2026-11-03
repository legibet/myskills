package main

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/source"
)

// skillsPage is the state of the Skills page.
type skillsPage struct {
	query  string
	filter int // the segment chosen: all, enabled, unused
	list   ui.ListState
	sel    int    // the row chosen
	path   string // the skill chosen, kept as the rows change
	split  float32
	md     markdown.Options
}

const (
	showAll = iota
	showEnabled
	showUnused
)

// show chooses a skill on the Skills page.
func (a *app) show(path string) {
	a.page = pageSkills
	a.skills.path = path
	a.skills.query, a.skills.filter = "", showAll
}

// skillRows returns the library skills the search and the filter leave.
func (a *app) skillRows() []core.Skill {
	q := strings.ToLower(strings.TrimSpace(a.skills.query))
	var rows []core.Skill
	for _, sk := range a.data.lib {
		used := len(a.data.usedIn(sk.Path)) > 0
		if (a.skills.filter == showEnabled && !used) || (a.skills.filter == showUnused && used) {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(sk.Path+"\x00"+sk.Name+"\x00"+sk.Description+"\x00"+sk.SourceLabel()), q) {
			continue
		}
		rows = append(rows, sk)
	}
	return rows
}

func (a *app) skillsView(c *ui.Context) {
	header(c, "Skills", func() { a.updateActions(c) })
	switch {
	case !a.data.read:
		ui.Column(c).Grow(1).Center().Children(func() { ui.Spinner(c).Label("Loading") })
		return
	case len(a.data.lib) == 0:
		emptyState(c, "Your library is empty", "Add skills from skills.sh, a git repository or a folder; then enable them where you need them.", func() {
			ui.PrimaryButton(c, "Find Skills").OnClick(func() { a.page = pageFind })
		})
		return
	}
	rows := a.skillRows()
	a.skills.sel = max(0, slices.IndexFunc(rows, func(sk core.Skill) bool { return sk.Path == a.skills.path }))
	if len(rows) > 0 {
		a.skills.path = rows[a.skills.sel].Path
	}
	ui.Split(c, &a.skills.split, func() {
		ui.Column(c).Fill().Children(func() {
			ui.Column(c).Padding(10, 12).Gap(8).Children(func() {
				ui.SearchField(c, &a.skills.query).Placeholder("Search Skills").Bind(&a.searchField)
				ui.Segmented(c, &a.skills.filter, "All", "Enabled", "Unused").Label("Show")
			})
			ui.Divider(c)
			s := &a.skills.list
			s.Selected = &a.skills.sel
			s.Key = func(i int) any { return rows[i].Path }
			s.Label = func(i int) string { return rows[i].Name }
			list := ui.List(c, s, len(rows)).Grow(1).Padding(6).Gap(2).Label("Skills")
			list.Rows(func(r ui.ListRow) { a.skillRow(r.Context, rows[r.Index], r.Selected()) })
			list.Children(func() {
				if len(rows) == 0 {
					muted(c, "No skills match.").Padding(16).TextAlign(ui.Center)
				}
			})
			if list.Changed() && a.skills.sel >= 0 && a.skills.sel < len(rows) {
				a.skills.path = rows[a.skills.sel].Path
			}
		})
	}, func() {
		if sk, ok := a.data.skill(a.skills.path); ok && len(rows) > 0 {
			a.skillDetail(c, sk)
		}
	}).Grow(1)
}

// skillRow is a skill in the list: its name and badges, its source and where
// it is enabled.
func (a *app) skillRow(c *ui.Context, sk core.Skill, chosen bool) {
	t := c.Theme()
	sub := t.TextMuted
	if chosen {
		sub = t.AccentText.Alpha(0.8)
	}
	ui.Column(c).Padding(7, 10).Gap(3).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, a.data.skillName(sk)).FontWeight(600).SingleLine().Grow(1)
			for _, b := range a.badges(sk) {
				if chosen {
					ui.Badge(c, b.text).Background(t.AccentText.Alpha(0.2)).TextColor(t.AccentText)
				} else {
					tag(c, b.text, b.color(t))
				}
			}
		})
		ui.Text(c, sk.SourceLabel()+" · "+a.where(sk.Path)).FontSize(12).TextColor(sub).SingleLine()
	})
}

// badge is a state a skill row shows.
type badge struct {
	text  string
	color func(*ui.Theme) ui.Color
}

func (a *app) badges(sk core.Skill) []badge {
	var out []badge
	warn := func(t *ui.Theme) ui.Color { return t.Warning }
	switch {
	case !sk.Valid():
		out = append(out, badge{"Invalid", func(t *ui.Theme) ui.Color { return t.Danger }})
	case a.updates.checks[sk.Path].State == core.Available:
		out = append(out, badge{"Update", func(t *ui.Theme) ui.Color { return t.Accent }})
	}
	var copiesOutdated, copiesModified bool
	for _, p := range a.data.usedIn(sk.Path) {
		e, _ := p.enablement(sk.Path)
		copiesOutdated = copiesOutdated || outdated(e)
		copiesModified = copiesModified || modified(e)
	}
	if copiesOutdated {
		out = append(out, badge{"Outdated", warn})
	}
	if copiesModified || sk.Modified {
		out = append(out, badge{"Modified", warn})
	}
	return out
}

// where says where a skill is enabled: "Global, 2 projects".
func (a *app) where(path string) string {
	var global bool
	var projects int
	for _, p := range a.data.usedIn(path) {
		if p.scope.IsGlobal() {
			global = true
		} else {
			projects++
		}
	}
	var parts []string
	if global {
		parts = append(parts, "Global")
	}
	switch {
	case projects == 1:
		parts = append(parts, "1 project")
	case projects > 1:
		parts = append(parts, fmt.Sprintf("%d projects", projects))
	}
	if len(parts) == 0 {
		return "Not enabled"
	}
	return strings.Join(parts, ", ")
}

// skillDetail is the skill chosen: what it is, where it is enabled, and its
// SKILL.md.
func (a *app) skillDetail(c *ui.Context, sk core.Skill) {
	t := c.Theme()
	ui.Scroll(c.Key(sk.Path)).Fill().Children(func() {
		ui.Column(c).Padding(20, 24).Gap(18).MaxWidth(820).Children(func() {
			ui.Column(c).Gap(6).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, sk.Name).FontSize(20).Bold().Grow(1).Selectable()
					a.skillActions(c, sk)
				})
				if sk.Description != "" {
					ui.Text(c, sk.Description).Selectable()
				}
				a.sourceLine(c, sk)
			})
			for _, n := range a.notes(sk) {
				note(c, n)
			}
			if sk.Valid() {
				a.enabledIn(c, sk)
			}
			if d, ok := a.data.docs[sk.Path]; ok {
				docView(c, d.meta.License, d.meta.Compatibility, d.meta.AllowedTools, d.meta.Metadata, d.body, &a.skills.md)
			}
		})
	}).Background(t.Background)
}

// sourceLine is where a skill came from, a link to its repository page when
// there is one, and its library path.
func (a *app) sourceLine(c *ui.Context, sk core.Skill) {
	t := c.Theme()
	ui.Row(c).Gap(6).Wrap().AlignItems(ui.Center).Children(func() {
		label := sk.SourceLabel()
		if url := webURL(sk); url != "" {
			ui.Button(c, "").Padding(0).Background(ui.Transparent).BorderWidth(0).Tooltip(url).Children(func() {
				ui.Text(c, label).FontSize(12).TextColor(t.Accent)
				ui.Icon(c, iconExternal).FontSize(11).TextColor(t.Accent)
			}).OnClick(func() { c.Services().OpenURL(url) })
		} else {
			muted(c, label).FontSize(12)
		}
		muted(c, "·").FontSize(12)
		muted(c, sk.Path).FontSize(12).Selectable()
	})
}

// webURL is the repository page of a git skill, or "".
func webURL(sk core.Skill) string {
	if sk.Linked || sk.Record == nil || sk.Record.Source.Type != "git" {
		return ""
	}
	src := sk.Record.Source
	return source.WebURL(src.URL, src.Ref, src.Path)
}

// skillActions are the title's buttons: Update when the last check found
// one, and the other actions in a menu.
func (a *app) skillActions(c *ui.Context, sk core.Skill) {
	if a.busy[sk.Path] {
		ui.Spinner(c).Label("Working")
	}
	if a.updates.checks[sk.Path].State == core.Available {
		ui.PrimaryButton(c, "Update").Label("Update " + sk.Name).Disabled(a.busy[sk.Path]).OnClick(func() { a.update(sk) })
	}
	iconButton(c, iconMore, "More").Disabled(a.busy[sk.Path]).PopoverMenuButton(func(m *ui.Menu) {
		if m.Item("Update").Disabled(!sk.Updatable() && !hasCopies(a.data.usedIn(sk.Path), sk.Path)).Chosen() {
			a.update(sk)
		}
		m.Separator()
		if m.Item("Reveal in Finder").Chosen() {
			a.desk.reveal(sk.Dir)
		}
		if m.Item("Open SKILL.md").Disabled(!sk.Valid()).Chosen() {
			a.desk.open(sk.Dir + "/SKILL.md")
		}
		m.Separator()
		if m.Item("Remove from Library…").Chosen() {
			a.confirmRemove(sk)
		}
	})
}

// notes say what is wrong with a skill, or what its last check found.
func (a *app) notes(sk core.Skill) []string {
	var out []string
	if !sk.Valid() {
		out = append(out, "This skill can't be enabled: "+sk.Issue)
	}
	if sk.Modified {
		out = append(out, "Edited in the library since it was added; updating replaces the edits.")
	}
	switch c := a.updates.checks[sk.Path]; c.State {
	case core.RemovedUpstream:
		out = append(out, "Removed upstream; the library keeps its copy.")
	case core.CheckFailed:
		out = append(out, "Couldn't check for an update: "+c.Err.Error())
	}
	return out
}

// note is a callout saying something needs attention.
func note(c *ui.Context, text string) {
	t := c.Theme()
	ui.Row(c).Gap(8).Padding(10, 12).Radius(8).Background(t.Warning.Alpha(0.1)).Children(func() {
		ui.Icon(c, iconWarning).TextColor(t.Warning)
		ui.Text(c, text).Grow(1).Selectable()
	})
}

// enabledIn lists the places with the skill, Global always, and enables it
// in a project.
func (a *app) enabledIn(c *ui.Context, sk core.Skill) {
	t := c.Theme()
	ui.Column(c).Gap(8).Children(func() {
		section(c, "Enabled in")
		ui.Column(c).Radius(10).Border(1, t.Border).Dividers(1, t.Border).Children(func() {
			for i := range a.data.places {
				p := &a.data.places[i]
				e, ok := p.enablement(sk.Path)
				switch {
				case ok:
					a.placementRow(c, p, sk, e, placeLead(c, p.scope))
				case p.scope.IsGlobal():
					a.offRow(c, p, sk)
				}
			}
			a.enableInProject(c, sk)
		})
	})
}

// offRow is Global without the skill.
func (a *app) offRow(c *ui.Context, p *place, sk core.Skill) {
	ui.Row(c.Key("off")).Gap(10).AlignItems(ui.Center).Padding(8, 12).Children(func() {
		ui.Row(c).Grow(1).MinWidth(0).Gap(10).AlignItems(ui.Center).Children(placeLead(c, p.scope))
		muted(c, "Not enabled")
		ui.Button(c, "Enable").Padding(3, 10).Disabled(a.busy[sk.Path]).OnClick(func() {
			a.enable(p.scope, sk, core.EnableOptions{Mode: core.Link})
		})
	})
}

// enableInProject enables the skill in a known project that doesn't have it,
// or in a folder chosen.
func (a *app) enableInProject(c *ui.Context, sk core.Skill) {
	t := c.Theme()
	ui.Row(c.Key("add")).Padding(6, 8).Children(func() {
		ui.Button(c, "").Padding(3, 6).Gap(6).Background(ui.Transparent).BorderWidth(0).Disabled(a.busy[sk.Path]).Children(func() {
			ui.Icon(c, iconAdd).TextColor(t.Accent)
			ui.Text(c, "Enable in Project…").TextColor(t.Accent)
		}).PopoverMenuButton(func(m *ui.Menu) {
			for i := range a.data.places {
				p := &a.data.places[i]
				if _, ok := p.enablement(sk.Path); ok || p.scope.IsGlobal() {
					continue
				}
				if m.Item(tilde(p.scope.Root)).Chosen() {
					a.enable(p.scope, sk, core.EnableOptions{Mode: core.Link})
				}
			}
			m.Separator()
			if m.Item("Choose Folder…").Chosen() {
				a.chooseProject(func(sc core.Scope) { a.enable(sc, sk, core.EnableOptions{}) })
			}
		})
	})
}

// chooseProject asks for a folder, and gives fn its project (§6 Scope).
func (a *app) chooseProject(fn func(core.Scope)) {
	go func() {
		dir := a.desk.chooseFolder("Choose a project folder")
		if dir == "" {
			return
		}
		sc, err := a.svc.ProjectScope(dir)
		a.post(func() {
			if err != nil {
				a.fail("Can't enable skills in "+tilde(dir), err)
				return
			}
			fn(sc)
		})
	}()
}

// hasCopies reports whether a skill has copy placements in the places.
func hasCopies(places []*place, path string) bool {
	return slices.ContainsFunc(places, func(p *place) bool {
		e, ok := p.enablement(path)
		return ok && slices.ContainsFunc(e.Placements, func(pl core.Placement) bool { return pl.Kind == core.KindCopy })
	})
}

// docView shows a SKILL.md: the optional frontmatter fields that are set,
// then the body.
func docView(c *ui.Context, license, compatibility, tools string, meta map[string]string, body string, md *markdown.Options) {
	t := c.Theme()
	ui.Column(c).Gap(10).Children(func() {
		section(c, "SKILL.md")
		fields := [][2]string{{"License", license}, {"Compatibility", compatibility}, {"Allowed tools", tools}}
		for _, k := range slices.Sorted(maps.Keys(meta)) {
			fields = append(fields, [2]string{k, meta[k]})
		}
		ui.Column(c).Gap(4).Children(func() {
			for _, f := range fields {
				if f[1] == "" {
					continue
				}
				ui.Row(c).Gap(8).Children(func() {
					muted(c, f[0]).FontSize(12).Width(110).Shrink(0)
					ui.Text(c, f[1]).FontSize(12).Grow(1).Selectable()
				})
			}
		})
		ui.Column(c).Padding(16, 18).Radius(10).Background(t.Surface).Children(func() {
			markdown.View(c, body, md)
		})
	})
}

// updatesState is the last check for updates.
type updatesState struct {
	checking    bool
	done, total int
	u           *core.Updates // the checkouts the check compared, for Update All
	checks      map[string]core.UpdateCheck
}

// available counts the updates the last check found.
func (s *updatesState) available() int {
	n := 0
	for _, c := range s.checks {
		if c.State == core.Available {
			n++
		}
	}
	return n
}

// close removes the checkouts of the last check.
func (s *updatesState) close() {
	if s.u != nil {
		_ = s.u.Close()
		s.u = nil
	}
}

// updateActions are the Skills title bar's: checking for updates, and
// updating all that the check found.
func (a *app) updateActions(c *ui.Context) {
	if a.updates.checking {
		ui.Spinner(c).Label("Checking")
		muted(c, fmt.Sprintf("Checking %d of %d…", a.updates.done, a.updates.total)).FontSize(12)
		return
	}
	if n := a.updates.available(); n > 0 && a.updates.u != nil {
		ui.PrimaryButton(c, fmt.Sprintf("Update All (%d)", n)).Disabled(a.busy["updates"]).OnClick(a.updateAll)
	}
	ui.Button(c, "").Gap(6).Disabled(a.busy["updates"] || !a.data.read).Children(func() {
		ui.Icon(c, iconRefresh)
		ui.Text(c, "Check for Updates").SingleLine()
	}).OnClick(a.checkUpdates)
}

// checkUpdates checks every updatable skill (§7).
func (a *app) checkUpdates() {
	var skills []core.Skill
	for _, sk := range a.data.lib {
		if sk.Updatable() {
			skills = append(skills, sk)
		}
	}
	if len(skills) == 0 {
		a.toast("No skill has a source to update from", "")
		return
	}
	a.updates.checking, a.updates.done, a.updates.total = true, 0, len(skills)
	a.work.Go(func() {
		u, err := a.svc.CheckUpdates(a.ctx, skills, func(done, _ int) {
			a.post(func() { a.updates.done = done })
		})
		if a.ctx.Err() != nil {
			if u != nil {
				_ = u.Close()
			}
			return
		}
		a.post(func() {
			a.updates.checking = false
			if err != nil {
				a.fail("Couldn't check for updates", err)
				return
			}
			a.updates.close()
			a.updates.u = u
			a.updates.checks = map[string]core.UpdateCheck{}
			failed := 0
			for _, ch := range u.Checks {
				a.updates.checks[ch.Skill.Path] = ch
				if ch.State == core.CheckFailed {
					failed++
				}
			}
			switch n := a.updates.available(); {
			case failed > 0:
				a.toast(fmt.Sprintf("Couldn't check %d %s", failed, plural(failed, "skill", "skills")), "warning")
			case n == 0:
				a.toast("Every skill is up to date", "")
			}
		})
	})
}

// update is the TUI's u: check the skill and update the library, then
// refresh its outdated copies in each place. Each step is its own change,
// so that discarding the local changes of one forces only that one.
func (a *app) update(sk core.Skill) {
	var places []core.Scope
	stale := false
	for _, p := range a.data.usedIn(sk.Path) {
		if hasCopies([]*place{p}, sk.Path) {
			places = append(places, p.scope)
			e, _ := p.enablement(sk.Path)
			stale = stale || outdated(e)
		}
	}
	var copies func(i int)
	copies = func(i int) {
		if i == len(places) {
			return
		}
		sc := places[i]
		o := a.refresh(sc, []string{sk.Path}, sk.Path, "Couldn't refresh the copies of "+sk.Name+" in "+placeName(sc))
		if p, ok := a.data.place(sc); ok {
			// The last read may predate the update, which made every copy
			// outdated: name each modified one.
			if e, ok := p.enablement(sk.Path); ok {
				o.discard = lossText(e.Placements)
			}
		}
		o.then = func() { copies(i + 1) }
		a.start(o, false)
	}
	if !sk.Updatable() {
		copies(0)
		return
	}
	a.start(op{key: sk.Path, fail: "Couldn't update " + sk.Name, discard: "Your changes to " + sk.Name + " in the library are replaced by the update.", run: func(force bool) outcome {
		out, updated := a.applyUpdate(sk, force)
		if out.err == nil && len(out.warnings) == 0 && !updated && !stale {
			out.note = sk.Name + " is up to date"
		}
		return out
	}, then: func() {
		delete(a.updates.checks, sk.Path)
		copies(0)
	}}, false)
}

// applyUpdate checks a skill and installs its update, if any. It runs off
// the main thread.
func (a *app) applyUpdate(sk core.Skill, force bool) (outcome, bool) {
	u, err := a.svc.CheckUpdates(a.ctx, []core.Skill{sk}, nil)
	if err != nil {
		return outcome{err: err}, false
	}
	defer func() { _ = u.Close() }()
	switch ch := u.Checks[0]; ch.State {
	case core.Available:
		r := a.svc.ApplyUpdates(u, []string{sk.Path}, force)[0]
		return outcome{err: r.Err}, r.Updated
	case core.RemovedUpstream:
		return outcome{warnings: []string{sk.Name + " was removed upstream; the library keeps its copy"}}, false
	case core.CheckFailed:
		return outcome{err: ch.Err}, false
	}
	return outcome{}, false
}

// updateAll installs every update the last check found, then refreshes the
// outdated copies of the skills updated in every place. Skills and copies
// with local changes are skipped, as the CLI's update does; updating one
// alone asks whether to discard them.
func (a *app) updateAll() {
	// The work takes the check's checkouts, and removes them when done.
	u := a.updates.u
	a.updates.u = nil
	var paths []string
	for path, ch := range a.updates.checks {
		if ch.State == core.Available {
			paths = append(paths, path)
		}
	}
	var scopes []core.Scope
	for _, p := range a.data.places {
		scopes = append(scopes, p.scope)
	}
	a.start(op{key: "updates", fail: "Some updates were skipped", run: func(bool) outcome {
		defer func() { _ = u.Close() }()
		var out outcome
		var updated, skipped []string
		for _, r := range a.svc.ApplyUpdates(u, paths, false) {
			switch {
			case r.Updated:
				updated = append(updated, r.Skill)
			case errors.Is(r.Err, core.ErrModified):
				skipped = append(skipped, r.Skill+" has local changes in the library")
			case r.Err != nil:
				skipped = append(skipped, r.Skill+": "+r.Err.Error())
			}
		}
		a.post(func() {
			for _, path := range updated {
				delete(a.updates.checks, path)
			}
		})
		if len(updated) > 0 {
			for _, sc := range scopes {
				results, err := a.svc.RefreshCopies(sc, updated, false)
				for _, r := range results {
					out.warnings = append(out.warnings, warnings(r)...)
				}
				if err != nil {
					skipped = append(skipped, placeName(sc)+": "+err.Error())
				}
			}
		}
		if len(skipped) > 0 {
			// Not ErrModified: confirming would force updates already applied.
			out.err = errors.New(strings.Join(skipped, "\n"))
		}
		return out
	}}, false)
}

// confirmRemove asks before removing a skill from the library (§8 remove).
func (a *app) confirmRemove(sk core.Skill) {
	affected := []string{sk.Path}
	var body []string
	if sk.Linked {
		var others []string
		for _, o := range a.data.lib {
			if o.Entry == sk.Entry && o.Path != sk.Path {
				others = append(others, o.Name)
				affected = append(affected, o.Path)
			}
		}
		body = append(body, fmt.Sprintf("%s belongs to the linked folder %s. The whole entry leaves the library: only the link is removed, never the files behind it.", sk.Name, sk.SourceLabel()))
		if len(others) > 0 {
			body = append(body, "Also removed: "+strings.Join(others, ", ")+".")
		}
	} else {
		body = append(body, "Deletes "+tilde(sk.Dir)+".")
	}
	var places []string
	for i := range a.data.places {
		p := &a.data.places[i]
		if slices.ContainsFunc(affected, func(path string) bool { _, ok := p.enablement(path); return ok }) {
			places = append(places, placeName(p.scope))
		}
	}
	if len(places) > 0 {
		body = append(body, "Enabled in "+strings.Join(places, ", ")+": links there are removed, copies stay as unmanaged files.")
	}
	a.ask("Remove "+sk.Name+" from the library?", strings.Join(body, "\n\n"), "Remove", func() {
		a.start(op{key: sk.Path, fail: "Couldn't remove " + sk.Name, run: func(bool) outcome {
			res, err := a.svc.Remove(sk)
			out := outcome{err: err, warnings: res.Warnings}
			if n := len(res.Released); n > 0 {
				out.warnings = append(out.warnings, fmt.Sprintf("%d %s left in place as unmanaged files", n, plural(n, "copy", "copies")))
			}
			return out
		}}, false)
	})
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
