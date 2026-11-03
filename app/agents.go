package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/legibet/myskills/internal/agent"
)

// agentsPage is the state of the Agents page.
type agentsPage struct {
	add addAgent
}

// addAgent is the Add Agent dialog.
type addAgent struct {
	open                      bool
	id, name, project, global string
	saving                    bool
	err                       string // why core refused the agent
}

func (a *app) agentsView(c *ui.Context) {
	pal := colors(c)
	bar(c).Padding(0, max(14, c.TitleBar().Right+10), 0, 24).Children(func() {
		detail := ""
		if a.data.read {
			detail = fmt.Sprintf("%d agents · %d %s", len(a.data.agents), len(a.data.defaults), plural(len(a.data.defaults), "default", "defaults"))
		}
		paneTitle(c, "Agents", detail)
		iconTextButton(c, iconAdd, "Add Agent…", plain).Disabled(!a.data.read).OnClick(func() { a.agents.add = addAgent{open: true} })
	})
	if !a.data.read {
		loading(c)
		return
	}
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		ui.Column(c).Padding(4, 24, 40, 24).Gap(10).MaxWidth(760).Children(func() {
			ui.Text(c, "Skills are placed in the skill directories of these agents. New enablements use the defaults.").
				FontSize(textSmall).TextColor(pal.text2).Padding(0, 2)
			group(c, "", func(g *rowGroup) {
				for _, ag := range a.data.agents {
					a.agentRow(c, g, ag)
				}
			})
		})
	})
	a.addAgentView(c)
}

// agentRow is an agent: its directories, whether it is a default, and
// removing it when it is custom.
func (a *app) agentRow(c *ui.Context, g *rowGroup, ag agent.Agent) {
	pal := colors(c)
	_, builtin := agent.Find(agent.Builtins, ag.ID)
	isDefault := slices.Contains(a.data.defaults, ag.ID)
	g.row(c.Key(ag.ID)).Padding(12, 14).Gap(16).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(6).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Baseline).Children(func() {
				ui.Text(c, ag.Name).FontWeight(600).SingleLine()
				if ag.Name != ag.ID {
					ui.Text(c, ag.ID).FontSize(textSmall).TextColor(pal.text2).SingleLine()
				}
			})
			dir := func(label, path string) {
				ui.Row(c).Gap(10).Children(func() {
					ui.Text(c, label).FontSize(textSmall).TextColor(pal.text2).Width(48).Shrink(0)
					if path == "" {
						ui.Text(c, "None").FontSize(textSmall).TextColor(pal.text3)
						return
					}
					ui.Text(c, path).FontSize(textSmall).Font("monospace").SingleLine().Selectable()
				})
			}
			dir("Project", ag.Project)
			global := ""
			if gd, ok := ag.GlobalDir(); ok {
				global = tilde(gd)
			}
			dir("Global", global)
		})
		control := "default:" + ag.ID
		on := a.toggled(control, "agents", isDefault)
		last := isDefault && len(a.data.defaults) == 1
		ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(func() {
			ui.Text(c, "Default").FontSize(textSmall).TextColor(pal.text2)
			sw := ui.Switch(c, &on).Label("Default for " + ag.Name).Disabled(last || a.working(c, "agents"))
			if last {
				sw.Tooltip("New enablements need at least one agent")
			}
			if sw.Changed() {
				a.intents[control] = on
				a.setDefault(ag.ID, on)
			}
		})
		ui.Box(c).Size(28, 28).Shrink(0).Children(func() {
			if !builtin {
				toolButton(c, iconDisable, "Remove "+ag.Name+"…").Disabled(a.working(c, "agents")).OnClick(func() { a.confirmRemoveAgent(ag) })
			}
		})
	})
}

// setDefault makes an agent a default, or not, keeping the agent order.
func (a *app) setDefault(id string, on bool) {
	var ids []string
	for _, ag := range a.data.agents {
		if ag.ID == id && on || ag.ID != id && slices.Contains(a.data.defaults, ag.ID) {
			ids = append(ids, ag.ID)
		}
	}
	a.start(op{key: "agents", fail: "Couldn't change the default agents", run: func(bool) outcome {
		return outcome{err: a.svc.SetDefaultAgents(ids)}
	}}, false)
}

// confirmRemoveAgent asks before removing a custom agent (§8 agents remove).
func (a *app) confirmRemoveAgent(ag agent.Agent) {
	msg := "Its links are removed globally and in every known project; its copies stay as unmanaged files."
	a.ask("Remove "+ag.Name+"?", msg, "Remove", func() {
		a.start(op{key: "agents", fail: "Couldn't remove " + ag.Name, run: func(bool) outcome {
			_, err := a.svc.RemoveAgent(ag.ID)
			return outcome{err: err}
		}}, false)
	})
}

// addAgentView is the Add Agent dialog. Core validates the agent; what it
// refuses shows in the dialog, which stays open.
func (a *app) addAgentView(c *ui.Context) {
	d := &a.agents.add
	if !d.open {
		return
	}
	pal := colors(c)
	ui.Modal(c, &d.open, func() {
		ui.Column(c).Width(440).Gap(16).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, "Add Agent").FontSize(15).FontWeight(700)
				ui.Text(c, "An agent myskills doesn't know: where it reads skills in a project, globally, or both.").FontSize(textSmall).TextColor(pal.text2).LineHeight(1.4)
			})
			ui.Form(c, func() {
				ui.Field(c, "ID", func() { ui.TextInput(c, &d.id).Placeholder("grok").AutoFocus() }).
					Description("Lowercase letters, digits and hyphens.")
				ui.Field(c, "Name", func() { ui.TextInput(c, &d.name).Placeholder("Grok") })
				ui.Field(c, "Project directory", func() { ui.TextInput(c, &d.project).Placeholder(".grok/skills") }).
					Description("Relative to a project's root.")
				ui.Field(c, "Global directory", func() { ui.TextInput(c, &d.global).Placeholder("~/.grok/skills") })
			}).OnSubmit(a.addAgent)
			if d.err != "" {
				ui.Text(c, d.err).FontSize(textSmall).TextColor(pal.red).Selectable()
			}
			ui.Row(c).Gap(8).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				if d.saving {
					spinner(c, "Adding")
				}
				if button(c, "Cancel", plain).Clicked() {
					d.open = false
				}
				button(c, "Add", primary).Disabled(d.saving || strings.TrimSpace(d.id) == "").OnClick(a.addAgent)
			})
		})
	})
}

func (a *app) addAgent() {
	d := &a.agents.add
	if d.saving || strings.TrimSpace(d.id) == "" {
		return
	}
	ag := agent.Agent{
		ID:      strings.TrimSpace(d.id),
		Name:    strings.TrimSpace(d.name),
		Project: strings.TrimSpace(d.project),
		Global:  strings.TrimSpace(d.global),
	}
	d.saving, d.err = true, ""
	a.work.Go(func() {
		err := a.svc.AddAgent(ag)
		a.post(func() {
			d.saving = false
			if err != nil {
				d.err = err.Error()
				return
			}
			d.open = false
			a.reload()
		})
	})
}
