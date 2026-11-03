package main

import (
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
	header(c, "Agents", func() {
		ui.Button(c, "").Gap(6).Disabled(!a.data.read).Children(func() {
			ui.Icon(c, iconAdd)
			ui.Text(c, "Add Agent…")
		}).OnClick(func() { a.agents.add = addAgent{open: true} })
	})
	if !a.data.read {
		ui.Column(c).Grow(1).Center().Children(func() { ui.Spinner(c).Label("Loading") })
		return
	}
	t := c.Theme()
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).Padding(20, 24).Gap(10).MaxWidth(820).Children(func() {
			muted(c, "Skills are placed in the skill directories of these agents. New enablements use the defaults.").FontSize(12)
			ui.Column(c).Radius(10).Border(1, t.Border).Dividers(1, t.Border).Children(func() {
				for _, ag := range a.data.agents {
					a.agentRow(c, ag)
				}
			})
		})
	})
	a.addAgentView(c)
}

// agentRow is an agent: its directories, whether it is a default, and
// removing it when it is custom.
func (a *app) agentRow(c *ui.Context, ag agent.Agent) {
	_, builtin := agent.Find(agent.Builtins, ag.ID)
	isDefault := slices.Contains(a.data.defaults, ag.ID)
	ui.Row(c.Key(ag.ID)).Gap(12).AlignItems(ui.Center).Padding(10, 14).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(4).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, ag.Name).Bold().SingleLine()
				if ag.Name != ag.ID {
					muted(c, ag.ID).FontSize(12).SingleLine()
				}
			})
			dir := func(label, path string) {
				ui.Row(c).Gap(8).Children(func() {
					muted(c, label).FontSize(12).Width(52).Shrink(0)
					if path == "" {
						muted(c, "None").FontSize(12)
						return
					}
					ui.Text(c, path).FontSize(12).SingleLine().Selectable()
				})
			}
			dir("Project", ag.Project)
			global := ""
			if g, ok := ag.GlobalDir(); ok {
				global = tilde(g)
			}
			dir("Global", global)
		})
		control := "default:" + ag.ID
		on := a.toggled(control, "agents", isDefault)
		last := isDefault && len(a.data.defaults) == 1
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			muted(c, "Default").FontSize(12)
			sw := ui.Switch(c, &on).Label("Default for " + ag.Name).Disabled(last || a.working(c, "agents"))
			if last {
				sw.Tooltip("New enablements need at least one agent")
			}
			if sw.Changed() {
				a.intents[control] = on
				a.setDefault(ag.ID, on)
			}
		})
		if !builtin {
			iconButton(c, iconDisable, "Remove "+ag.Name+"…").Disabled(a.working(c, "agents")).OnClick(func() { a.confirmRemoveAgent(ag) })
		}
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
	t := c.Theme()
	ui.Modal(c, &d.open, func() {
		ui.Column(c).Width(440).Gap(14).Children(func() {
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, "Add Agent").FontSize(15).Bold()
				muted(c, "An agent myskills doesn't know: where it reads skills in a project, globally, or both.").FontSize(12)
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
				ui.Text(c, d.err).FontSize(12).TextColor(t.Danger).Selectable()
			}
			ui.Row(c).Gap(8).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				if d.saving {
					ui.Spinner(c).Label("Adding")
				}
				if ui.Button(c, "Cancel").Clicked() {
					d.open = false
				}
				ui.PrimaryButton(c, "Add").Disabled(d.saving || strings.TrimSpace(d.id) == "").OnClick(a.addAgent)
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
