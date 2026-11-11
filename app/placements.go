package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/legibet/myskills/internal/core"
)

// placementRow shows how a skill is enabled in a place, and changes it: the
// mode, the agents, the copies' state, and disabling. lead builds what the
// row is about: the place on Skills, the skill on Projects.
func (a *app) placementRow(c *ui.Context, g *rowGroup, p *place, sk core.Skill, e *core.Enablement, lead func()) {
	t := c.Theme()
	busy := a.working(c, sk.Path)
	// The controls move under the lead when the pane is narrow.
	g.row(c.Key(p.scope.Root + "\x00" + sk.Path)).Wrap().Justify(ui.End).GapX(10).GapY(8).Children(func() {
		ui.Row(c).Grow(1).Basis(200).MinWidth(0).Gap(10).AlignItems(ui.Center).Children(lead)
		ui.Row(c).MinWidth(0).Shrink(1).Wrap().Justify(ui.End).Gap(6).AlignItems(ui.Center).Children(func() {
			if e.Outdated() {
				tag(c, "Outdated", t.Warning).Tooltip("The library changed since these files were copied")
				button(c, "Refresh", plain).MinHeight(24).Padding(3, 10).FontSize(textSmall(c)).Disabled(busy).OnClick(func() {
					a.start(a.refresh(p.scope, []string{sk.Path}, sk.Path, "Couldn't refresh the copies of "+sk.Name), false)
				})
			}
			if e.Modified() {
				tag(c, "Modified", t.Warning).Tooltip("The copied files were edited in place")
			}
			a.modeChip(c, p, sk, e, busy)
			a.agentsChip(c, p, sk, e, busy)
			if busy {
				ui.Box(c).Size(28, 28).Shrink(0).Center().Children(func() { spinner(c, "Working") })
				return
			}
			toolButton(c, iconDisable, "Disable in "+placeName(p.scope)).OnClick(func() { a.disable(p.scope, sk, e, nil) })
		})
	})
}

// placeLead is a placement row's lead on Skills: the place.
func placeLead(c *ui.Context, sc core.Scope) func() {
	return func() {
		if sc.IsGlobal() {
			rowIcon(c, iconGlobal)
			rowText(c, "Global", "Every project")
			return
		}
		rowIcon(c, iconProject)
		rowText(c, placeName(sc), tilde(sc.Root))
	}
}

// modeChip shows the mode of an enablement, and switches it.
func (a *app) modeChip(c *ui.Context, p *place, sk core.Skill, e *core.Enablement, busy bool) {
	label := map[core.Mode]string{core.Link: "Link", core.Copy: "Copy", core.Mixed: "Mixed"}[e.Mode]
	popUp(c, "Mode", label).Shrink(0).Disabled(busy).Menu(func(m *ui.Menu) {
		if m.Item("Link").Checked(e.Mode == core.Link).Chosen() && e.Mode != core.Link {
			a.convert(p.scope, sk, e, core.Link)
		}
		if m.Item("Copy").Checked(e.Mode == core.Copy).Chosen() && e.Mode != core.Copy {
			a.convert(p.scope, sk, e, core.Copy)
		}
	})
}

// agentsChip shows the agents an enablement is placed for, and opens a
// check box per agent skill directory: agents sharing a directory share its
// placement, so they are one item.
func (a *app) agentsChip(c *ui.Context, p *place, sk core.Skill, e *core.Enablement, busy bool) {
	key := p.scope.Root + "\x00" + sk.Path
	open := a.agentsOpen && a.agentsFor == key
	b := popUp(c, "Agents", a.agentsSummary(e.Agents())).Tooltip(a.data.agentNames(e.Agents())).
		MaxWidth(200).Shrink(0).Expanded(open).Disabled(busy)
	if b.Clicked() {
		a.agentsOpen, a.agentsFor = !open, key
	}
	if a.agentsFor != key {
		return
	}
	pal := colors(c)
	ui.Popover(c, b, &a.agentsOpen, func() {
		ui.Column(c).Gap(10).Padding(4, 2).MinWidth(240).MaxWidth(420).Children(func() {
			ui.Text(c, "Agents").FontSize(textSmall(c)).FontWeight(600).TextColor(pal.text2)
			for _, d := range p.dirs {
				placed := slices.ContainsFunc(e.Placements, func(pl core.Placement) bool {
					return slices.ContainsFunc(pl.Agents, func(id string) bool { return slices.Contains(d.Agents, id) })
				})
				control := key + "\x00" + d.Path
				on := a.toggled(control, sk.Path, placed)
				ui.Column(c.Key(d.Path)).Gap(2).Children(func() {
					if ui.Checkbox(c, &on, a.data.agentNames(d.Agents)).Disabled(busy).Changed() {
						a.intents[control] = on
						if on {
							a.enable(p.scope, sk, core.EnableOptions{Agents: d.Agents})
						} else {
							a.disable(p.scope, sk, e, d.Agents)
						}
					}
					ui.Text(c, tilde(d.Path)).FontSize(textCaption(c)).TextColor(pal.text2).Margin(0, 0, 0, 26).SingleLine()
				})
			}
		})
	})
}

// agentsSummary names agents briefly: the first, and how many more.
func (a *app) agentsSummary(ids []string) string {
	switch len(ids) {
	case 0:
		return "No agents"
	case 1, 2:
		return a.data.agentNames(ids)
	}
	return fmt.Sprintf("%s +%d", a.data.agentName(ids[0]), len(ids)-1)
}

// enable enables a skill in a place: as a link for the default agents when
// it is new, else for the agents and in the mode opts says.
func (a *app) enable(sc core.Scope, sk core.Skill, opts core.EnableOptions) {
	a.start(op{key: sk.Path, fail: "Couldn't enable " + sk.Name + " in " + placeName(sc), run: func(force bool) outcome {
		opts.Force = force
		return resultOutcome(a.svc.Enable(sc, sk, opts))
	}}, false)
}

// disable removes a skill's placements in a place, for the agents given
// (all when nil), after asking when that deletes copies.
func (a *app) disable(sc core.Scope, sk core.Skill, e *core.Enablement, agents []string) {
	var copies []core.Placement
	for _, p := range e.Placements {
		if p.Kind == core.KindCopy && (agents == nil || slices.ContainsFunc(p.Agents, func(id string) bool { return slices.Contains(agents, id) })) {
			copies = append(copies, p)
		}
	}
	run := func(force bool) {
		a.start(op{key: sk.Path, fail: "Couldn't disable " + sk.Name + " in " + placeName(sc), run: func(f bool) outcome {
			return resultOutcome(a.svc.Disable(sc, sk.Path, core.DisableOptions{Agents: agents, Force: f}))
		}}, force)
	}
	a.confirmCopies(copies, "Disable "+sk.Name+" in "+placeName(sc)+"?", "Disable", run)
}

// convert switches an enablement to links or to copies, for the agents it is
// placed for. Switching to links deletes the copies, after asking.
func (a *app) convert(sc core.Scope, sk core.Skill, e *core.Enablement, mode core.Mode) {
	opts := core.EnableOptions{Mode: mode, Agents: e.Agents()}
	run := func(force bool) {
		a.start(op{key: sk.Path, fail: "Couldn't switch " + sk.Name + " in " + placeName(sc), run: func(f bool) outcome {
			opts.Force = f
			return resultOutcome(a.svc.Enable(sc, sk, opts))
		}}, force)
	}
	if mode == core.Copy {
		run(false)
		return
	}
	var copies []core.Placement
	for _, p := range e.Placements {
		if p.Kind == core.KindCopy {
			copies = append(copies, p)
		}
	}
	a.confirmCopies(copies, "Replace the copies of "+sk.Name+" with links?", "Replace", run)
}

// confirmCopies runs fn at once when no copies would be deleted, else after
// asking, naming them; deleting modified copies is forced once confirmed.
func (a *app) confirmCopies(copies []core.Placement, title, action string, fn func(force bool)) {
	if len(copies) == 0 {
		fn(false)
		return
	}
	if lost := lossText(copies); lost != "" {
		a.ask("Discard local changes?", lost, "Discard", func() { fn(true) })
		return
	}
	var lines []string
	for _, p := range copies {
		lines = append(lines, tilde(p.Path))
	}
	a.ask(title, "This deletes the copied files:\n"+strings.Join(lines, "\n"), action, func() { fn(false) })
}

// lossText lists the modified copies among placements, whose changes a
// forced change loses; "" when there are none.
func lossText(placements []core.Placement) string {
	var paths []string
	for _, p := range placements {
		if p.Modified {
			paths = append(paths, tilde(p.Path))
		}
	}
	if len(paths) == 0 {
		return ""
	}
	return "These copies have local changes that will be lost:\n" + strings.Join(paths, "\n")
}

// refresh is the change copying the library's content again into the
// outdated copies of the skills at paths in a place (every skill when nil).
// Modified copies are refreshed only after asking.
func (a *app) refresh(sc core.Scope, paths []string, key, fail string) op {
	var stale []core.Placement
	if p, ok := a.data.place(sc); ok {
		for _, e := range p.status.Enablements {
			if paths == nil || slices.Contains(paths, e.Skill) {
				stale = append(stale, slices.DeleteFunc(slices.Clone(e.Placements), func(pl core.Placement) bool { return !pl.Outdated })...)
			}
		}
	}
	return op{key: key, fail: fail, discard: lossText(stale), run: func(force bool) outcome {
		results, err := a.svc.RefreshCopies(sc, paths, force)
		out := outcome{err: err}
		for _, r := range results {
			out.warnings = append(out.warnings, warnings(r)...)
		}
		return out
	}}
}

// removeBroken removes a broken link, named by its missing library path,
// never by its entry name, which may belong to another library skill.
func (a *app) removeBroken(sc core.Scope, p core.Placement) {
	a.start(op{key: "broken:" + p.Path, fail: "Couldn't remove the broken link " + p.Name, run: func(force bool) outcome {
		return resultOutcome(a.svc.Disable(sc, p.Skill, core.DisableOptions{Agents: p.Agents, Force: force}))
	}}, false)
}

// resultOutcome is the outcome of enabling or disabling.
func resultOutcome(res core.Result, err error) outcome {
	return outcome{err: err, warnings: warnings(res)}
}

// warnings returns what a result warns about.
func warnings(res core.Result) []string {
	out := slices.Clone(res.Warnings)
	for _, ch := range res.Changes {
		if ch.Warning != "" {
			out = append(out, tilde(ch.Path)+": "+ch.Warning)
		}
	}
	return out
}
