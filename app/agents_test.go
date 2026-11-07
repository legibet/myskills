package main

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/legibet/myskills/internal/agent"
	"github.com/legibet/myskills/internal/core"
)

func (h *harness) defaults() []string {
	h.t.Helper()
	cfg, err := h.svc.Config()
	if err != nil {
		h.t.Fatal(err)
	}
	return cfg.DefaultAgents()
}

func TestDefaultAgents(t *testing.T) {
	h := newHarness(t).start()
	h.click("Agents")
	h.want("Universal", "Claude Code", "Antigravity", filepath.FromSlash(".gemini/config/skills"))

	h.click("Default for Antigravity")
	if got := h.defaults(); !slices.Equal(got, []string{"universal", "claude-code", "antigravity"}) {
		t.Fatalf("defaults = %v", got)
	}
	h.click("Default for Universal")
	h.click("Default for Claude Code")
	if got := h.defaults(); !slices.Equal(got, []string{"antigravity"}) {
		t.Fatalf("defaults = %v", got)
	}
	// The last default stays on.
	h.click("Default for Antigravity")
	if got := h.defaults(); !slices.Equal(got, []string{"antigravity"}) {
		t.Fatalf("defaults = %v, want the last one kept", got)
	}
}

func TestAddAndRemoveAgent(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review"}))
	h.start()
	h.click("Agents")

	h.click("Add Agent…")
	h.typeText("grok")
	h.tt.Key(0, ui.KeyTab)
	h.typeText("Grok")
	h.tt.Key(0, ui.KeyTab)
	h.typeText("/abs/skills")
	h.click("Add")
	if !h.a.agents.add.open || h.a.agents.add.err == "" {
		t.Fatal("an invalid agent was not refused in the dialog")
	}
	h.want("must be relative to the project root")

	h.a.agents.add.project = ".grok/skills"
	h.click("Add")
	if h.a.agents.add.open {
		t.Fatalf("dialog still open: %s", h.a.agents.add.err)
	}
	agents, _ := h.svc.Agents()
	ag, ok := agent.Find(agents, "grok")
	if !ok || ag.Name != "Grok" || ag.Project != ".grok/skills" {
		t.Fatalf("agent = %+v", ag)
	}
	h.want("Grok", ".grok/skills")

	proj := h.project("web")
	h.enable(proj, "review", core.EnableOptions{Agents: []string{"grok"}})
	h.settle()
	h.click("Remove Grok…")
	h.confirm("Remove Grok?")
	agents, _ = h.svc.Agents()
	if _, ok := agent.Find(agents, "grok"); ok {
		t.Fatal("grok was not removed")
	}
	if got := h.enabled(proj); len(got) != 0 {
		t.Errorf("its link stayed: %v", got)
	}
}
