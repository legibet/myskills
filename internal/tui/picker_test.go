package tui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/source"
)

func pickerFixture() *core.Fetched {
	spec, _ := source.Parse("acme/skills")
	return &core.Fetched{Spec: spec, Candidates: []core.Candidate{
		{Name: "one", Description: "First.", Dir: "/c/one", RelPath: "skills/one"},
		{Name: "two", Dir: "/c/two", RelPath: "skills/two", Installed: "two"},
		{Name: "three", Dir: "/c/three", RelPath: "skills/three", Conflict: "three"},
		{Name: "four", Dir: "/c/four", RelPath: "skills/four", Issue: "SKILL.md: missing description"},
		{Name: "five", Dir: "/c/five", RelPath: "skills/five"},
	}}
}

// drivePicker sends key presses and runs the commands, like the program would.
func drivePicker(p *picker, keys ...string) (quit bool) {
	var run func(tea.Cmd)
	run = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		switch msg := cmd().(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				run(c)
			}
		case tea.QuitMsg:
			quit = true
		case nil:
		default:
			_, next := p.Update(msg)
			run(next)
		}
	}
	_, cmd := p.Update(windowSize(120, 24))
	run(cmd)
	for _, k := range keys {
		_, cmd := p.Update(keyPress(k))
		run(cmd)
	}
	return quit
}

func names(cs []core.Candidate) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

func TestPickSelectAll(t *testing.T) {
	p := newPicker(pickerFixture())
	if !drivePicker(p, "ctrl+a", "enter") {
		t.Fatal("enter should finish the picker")
	}
	if got := names(p.picked); !slices.Equal(got, []string{"one", "five"}) {
		t.Fatalf("picked = %v", got)
	}
}

func TestPickSkipsUnselectable(t *testing.T) {
	p := newPicker(pickerFixture())
	drivePicker(p, "down", "space")
	if p.checked[1] || !strings.Contains(p.problem, "already in your library") {
		t.Fatalf("installed candidate toggled; problem = %q", p.problem)
	}
	drivePicker(p, "down", "space")
	if p.checked[2] || !strings.Contains(p.problem, "already has three") {
		t.Fatalf("conflicting candidate toggled; problem = %q", p.problem)
	}
	drivePicker(p, "down", "space")
	if p.checked[3] || !strings.Contains(p.problem, "invalid") {
		t.Fatalf("invalid candidate toggled; problem = %q", p.problem)
	}
	v := stripView(p.View().Content)
	for _, want := range []string{"Add from acme/skills", "in library", "conflict", "invalid", "SKILL.md: missing description"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
}

func TestPickToggleAndPreview(t *testing.T) {
	p := newPicker(pickerFixture())
	drivePicker(p, "space")
	if !p.checked[0] || p.list.cursor != 1 {
		t.Fatal("space should check the row and move down")
	}
	drivePicker(p, "up")
	v := stripView(p.View().Content)
	for _, want := range []string{"1 selected", "[✓] one", "About one.", "Body of /c/one"} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q:\n%s", want, v)
		}
	}
	drivePicker(p, "space", "ctrl+a", "ctrl+a")
	if slices.Contains(p.checked, true) {
		t.Fatalf("ctrl+a twice should clear: %v", p.checked)
	}
}

func TestPickEnterTakesHighlightedWhenNoneChecked(t *testing.T) {
	p := newPicker(pickerFixture())
	drivePicker(p, "down", "down", "down", "down", "enter")
	if got := names(p.picked); !slices.Equal(got, []string{"five"}) {
		t.Fatalf("picked = %v", got)
	}

	p = newPicker(pickerFixture())
	if drivePicker(p, "down", "enter") {
		t.Fatal("enter on an installed candidate with nothing checked should not finish")
	}
}

func TestPickCancel(t *testing.T) {
	p := newPicker(pickerFixture())
	if !drivePicker(p, "space", "esc") || p.picked != nil {
		t.Fatalf("esc should cancel with nil, got %v", p.picked)
	}
}
