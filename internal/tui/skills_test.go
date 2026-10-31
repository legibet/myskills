package tui

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/legibet/myskills/internal/agent"
	"github.com/legibet/myskills/internal/core"
	"github.com/legibet/myskills/internal/source"
)

func rowNames(a *app) []string {
	var names []string
	for _, r := range a.sk.rows {
		names = append(names, r.name())
	}
	return names
}

func TestSkillsTabsAndCounts(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.contains("All 4", "Enabled 2", "Issues 4", "▸ project /work/proj", "global: 1")
	if got, want := rowNames(h.a), []string{"broken", "notes", "review", "tdd"}; !slices.Equal(got, want) {
		t.Fatalf("All rows = %v, want %v", got, want)
	}

	h.press("tab")
	if got, want := rowNames(h.a), []string{"notes", "tdd"}; !slices.Equal(got, want) {
		t.Fatalf("Enabled rows = %v, want %v", got, want)
	}

	// Issues: the invalid skill, the modified copy, and entries without a
	// library skill (a broken link and an unmanaged folder).
	h.press("tab")
	if got, want := rowNames(h.a), []string{"broken", "notes", "gone", "mine"}; !slices.Equal(got, want) {
		t.Fatalf("Issues rows = %v, want %v", got, want)
	}
	h.contains("✗ gone", "broken", "◇ mine", "unmanaged")

	h.press("shift+tab", "shift+tab")
	if h.a.sk.tab != tabAll {
		t.Fatalf("tab = %v, want All", h.a.sk.tab)
	}
}

func TestSkillsGlyphsAndBadges(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	lines := strings.Split(h.view(), "\n")
	find := func(name string) string {
		for _, l := range lines {
			list, _, _ := strings.Cut(l, "│")
			if strings.Contains(list, " "+name+" ") && !strings.Contains(list, "All 4") {
				return list
			}
		}
		t.Fatalf("no line for %s", name)
		return ""
	}
	for name, want := range map[string][]string{
		"tdd":    {"● tdd  acme/tdd", "link"},
		"notes":  {"◐ notes  acme/notes", "modified", "copy"},
		"review": {"○ review  /home/agent-skills", "global"}, // enabled only globally
		"broken": {"○ broken  local", "invalid"},
	} {
		line := find(name)
		for _, w := range want {
			if !strings.Contains(line, w) {
				t.Errorf("row %s = %q, want %q", name, line, w)
			}
		}
		// The library path shows only for names that repeat.
		if strings.Contains(line, "agent-skills/") || strings.Contains(line, "linked") {
			t.Errorf("row %s = %q", name, line)
		}
	}
}

func TestSkillsFilter(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.press("/")
	h.typeText("rev")
	if got := rowNames(h.a); !slices.Equal(got, []string{"review"}) {
		t.Fatalf("filtered rows = %v", got)
	}
	// The match on the collection path also counts: "agent" finds review.
	h.press("backspace", "backspace", "backspace")
	h.typeText("agent")
	if got := rowNames(h.a); !slices.Equal(got, []string{"review"}) {
		t.Fatalf("filtered rows = %v", got)
	}
	h.press("enter")
	if h.a.sk.filtering {
		t.Fatal("enter should leave the filter input")
	}
	h.contains("/ agent")
	h.press("esc")
	if got := len(h.a.sk.rows); got != 4 {
		t.Fatalf("esc should clear the filter, rows = %d", got)
	}

	h.press("/")
	h.typeText("zzz")
	h.contains("No skills match")
	h.press("esc")
	if h.a.sk.filtering || h.a.sk.filter.Value() != "" || len(h.a.sk.rows) != 4 {
		t.Fatal("esc in the filter input should clear and close it")
	}
}

func TestToggleEnablesOffSkillAsLink(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.cursorTo("review")
	h.press("space")
	if got := h.svc.called("Enable"); !slices.Equal(got, []string{"Enable /work/proj agent-skills/review mode=link agents=[] force=false"}) {
		t.Fatalf("calls = %v", got)
	}
	h.contains("✓ Enabled review (link) in Universal, Claude Code")
	if !slices.Contains(h.a.summary, "✓ Enabled review (link) in Universal, Claude Code") {
		t.Fatalf("summary = %v", h.a.summary)
	}
}

func TestShutdownSkipsMutationsTheProgramDropped(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.cursorTo("review")
	_, cmd := h.a.Update(keyPress("space"))
	// The program quits before running the command: shutdown must not wait
	// for it, and running it afterwards changes nothing.
	done := make(chan struct{})
	go func() {
		h.a.shutdown()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown waits for a mutation that never started")
	}
	drain(cmd)
	if got := h.svc.called("Enable"); len(got) != 0 {
		t.Fatalf("calls = %v", got)
	}
}

func TestToggleDisablesLinkWithoutAsking(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.cursorTo("tdd")
	h.press("space")
	if h.a.modal != nil {
		t.Fatal("disabling links needs no confirmation")
	}
	if got := h.svc.called("Disable"); !slices.Equal(got, []string{"Disable /work/proj tdd agents=[] force=false"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestDisablingCopyConfirms(t *testing.T) {
	f := libraryFixture()
	f.status[project].Enablements[0].Placements[0].Modified = false
	h := newHarness(t, f, projectOptions(), 120, 30)
	h.cursorTo("notes")
	h.press("space")
	if h.a.modal == nil {
		t.Fatal("disabling a copy should ask first")
	}
	h.contains("Disable notes?", "deletes the copied files", ".claude/skills/notes")
	h.press("n")
	if h.a.modal != nil || len(h.svc.called("Disable")) != 0 {
		t.Fatal("n should cancel without disabling")
	}
	h.press("space", "y")
	if got := h.svc.called("Disable"); !slices.Equal(got, []string{"Disable /work/proj notes agents=[] force=false"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestModifiedCopyAsksToDiscard(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.cursorTo("notes")
	h.press("c") // copy → link deletes the modified copy
	h.contains("Discard local changes?", ".claude/skills/notes")
	h.press("y")
	if got := h.svc.called("Enable"); !slices.Equal(got, []string{"Enable /work/proj notes mode=link agents=[claude-code] force=true"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestErrModifiedRetriesWithForce(t *testing.T) {
	f := libraryFixture()
	f.failNext("Disable", fmt.Errorf("%w: /work/proj/.agents/skills/tdd", core.ErrModified))
	h := newHarness(t, f, projectOptions(), 120, 30)
	h.cursorTo("tdd")
	h.press("space")
	h.contains("Discard local changes?", "locally modified: /work/proj/.agents/skills/tdd")
	h.press("enter")
	want := []string{"Disable /work/proj tdd agents=[] force=false", "Disable /work/proj tdd agents=[] force=true"}
	if got := h.svc.called("Disable"); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	h.contains("✓ Disabled tdd")
}

func TestConflictShowsOccupant(t *testing.T) {
	f := libraryFixture()
	f.failNext("Enable", fmt.Errorf("%w: .claude/skills/review is a folder myskills does not manage", core.ErrConflict))
	h := newHarness(t, f, projectOptions(), 120, 30)
	h.cursorTo("review")
	h.press("space")
	h.contains("✗ conflict: .claude/skills/review is a folder myskills does not manage")
	if h.a.modal != nil {
		t.Fatal("a conflict is not confirmable")
	}
}

func TestCopyKey(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.cursorTo("review") // off → copy
	h.press("c")
	h.cursorTo("tdd") // link → copy
	h.press("c")
	want := []string{
		"Enable /work/proj agent-skills/review mode=copy agents=[] force=false",
		"Enable /work/proj tdd mode=copy agents=[universal antigravity claude-code] force=false", // the agents it is placed for
	}
	if got := h.svc.called("Enable"); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	h.contains("Switched tdd to copies")
}

func TestInvalidSkillCannotBeEnabled(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.cursorTo("broken")
	h.press("space", "c", "a")
	if len(h.svc.calls) != 0 || h.a.modal != nil {
		t.Fatalf("calls = %v", h.svc.calls)
	}
	h.contains("✗ Invalid: SKILL.md: missing description")
}

func TestBusySkillIgnoresKeys(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.cursorTo("review")
	_, first := h.a.Update(keyPress("space"))
	if !h.a.busy["agent-skills/review"] {
		t.Fatal("review should be busy")
	}
	_, second := h.a.Update(keyPress("space"))
	_, third := h.a.Update(keyPress("c"))
	h.exec(second)
	h.exec(third)
	if got := h.svc.called("Enable"); len(got) != 0 {
		t.Fatalf("keys on a busy skill ran %v", got)
	}
	h.exec(first)
	if got := h.svc.called("Enable"); len(got) != 1 || h.a.busy["agent-skills/review"] {
		t.Fatalf("calls = %v busy = %v", got, h.a.busy)
	}
}

func TestBrokenEntryRemoval(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.press("tab", "tab")
	h.cursorTo("gone")
	h.contains("space remove link", "points to old/gone in the library")
	h.press("space")
	if got := h.svc.called("Disable"); !slices.Equal(got, []string{"Disable /work/proj old/gone agents=[claude-code] force=false"}) {
		t.Fatalf("calls = %v", got)
	}
	h.cursorTo("mine")
	h.press("space")
	if len(h.svc.called("Disable")) != 1 {
		t.Fatal("unmanaged entries are never touched")
	}
}

func TestScopeSwitch(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.press("s")
	if !h.a.scope.IsGlobal() {
		t.Fatal("s should switch to global")
	}
	h.contains("▸ global", "project: 2", "Enabled 1")
	h.cursorTo("review")
	h.press("space")
	if got := h.svc.called("Disable"); !slices.Equal(got, []string{"Disable global agent-skills/review agents=[] force=false"}) {
		t.Fatalf("calls = %v", got)
	}
	h.contains("Disabled review in Universal, Claude Code (global)")

	// Without a project the scope cannot change.
	h = newHarness(t, libraryFixture(), Options{}, 120, 30)
	h.press("s")
	if !h.a.scope.IsGlobal() {
		t.Fatal("scope changed without a project")
	}
	h.lacks("s scope")
}

func TestAgentsModal(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.cursorTo("tdd")
	h.press("a")
	// Universal and Antigravity share .agents/skills: one item.
	h.contains("Universal, Antigravity", ".agents/skills", "Claude Code", ".claude/skills")
	h.press("down", "space", "enter")
	if got := h.svc.called("Disable"); !slices.Equal(got, []string{"Disable /work/proj tdd agents=[claude-code] force=false"}) {
		t.Fatalf("calls = %v", got)
	}
	h.contains("✓ Disabled tdd in Claude Code")

	// Adding an agent to an off skill enables it there only.
	h.cursorTo("review")
	h.press("a", "space", "enter")
	if got := h.svc.called("Enable"); !slices.Equal(got, []string{"Enable /work/proj agent-skills/review mode= agents=[universal antigravity] force=false"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestDefaultAgentsModal(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.press("A")
	h.contains("Default agents", "[✓] Universal", "[✓] Claude Code", "[ ] Antigravity")
	h.press("space", "down", "space", "enter")
	h.contains("Choose at least one agent.")
	if len(h.svc.called("SetDefaultAgents")) != 0 {
		t.Fatal("an empty default list must not be saved")
	}
	h.press("down", "space", "enter")
	if got := h.svc.called("SetDefaultAgents"); !slices.Equal(got, []string{"SetDefaultAgents [antigravity]"}) {
		t.Fatalf("calls = %v", got)
	}
	h.contains("✓ Default agents: Antigravity")
}

func TestRemoveLinkedEntry(t *testing.T) {
	f := libraryFixture()
	f.lib = append(f.lib, core.Skill{Path: "agent-skills/lint", Name: "lint", Dir: "/lib/agent-skills/lint", Entry: "agent-skills", Linked: true})
	h := newHarness(t, f, projectOptions(), 120, 30)
	h.cursorTo("review")
	h.press("x")
	h.contains("Remove review from the library?", "linked entry agent-skills", "Also removed: lint", "Enabled in global")
	h.press("y")
	if got := h.svc.called("Remove"); !slices.Equal(got, []string{"Remove agent-skills/review"}) {
		t.Fatalf("calls = %v", got)
	}
	h.contains("Removed the linked entry agent-skills")
}

func TestCheckAndUpdate(t *testing.T) {
	f := libraryFixture()
	f.checks = []core.UpdateCheck{
		{Skill: f.lib[3], State: core.Available},
		{Skill: f.lib[1], State: core.UpToDate},
	}
	h := newHarness(t, f, projectOptions(), 120, 30)
	h.press("U")
	if got := h.svc.called("CheckUpdates"); !slices.Equal(got, []string{"CheckUpdates [notes tdd]"}) {
		t.Fatalf("calls = %v", got)
	}
	if h.a.sk.checking {
		t.Fatal("the check should be done")
	}
	h.contains("1 update available")
	h.cursorTo("tdd")
	h.contains("update", "↑ Update available")

	h.press("u")
	want := []string{
		"CheckUpdates [tdd]",
		"ApplyUpdates [tdd] force=false",
		"RefreshCopies /work/proj [tdd] force=false",
		"RefreshCopies global [tdd] force=false",
	}
	if got := h.svc.calls[1:]; !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	h.contains("✓ Updated tdd")
	if _, ok := h.a.sk.updates["tdd"]; ok {
		t.Fatal("the update badge should clear")
	}
}

func TestUpdateModifiedLibrarySkillAsks(t *testing.T) {
	f := libraryFixture()
	f.lib[3].Modified = true
	f.checks = []core.UpdateCheck{{Skill: f.lib[3], State: core.Available}}
	f.failNext("ApplyUpdates", fmt.Errorf("%w: tdd", core.ErrModified))
	h := newHarness(t, f, projectOptions(), 120, 30)
	h.cursorTo("tdd")
	h.press("u")
	h.contains("Discard local changes?", "Your changes to tdd in the library are replaced")
	h.press("y")
	if got := h.svc.called("ApplyUpdates"); !slices.Equal(got, []string{"ApplyUpdates [tdd] force=false", "ApplyUpdates [tdd] force=true"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestEmptyLibrary(t *testing.T) {
	h := newHarness(t, newFake(), Options{}, 120, 30)
	h.contains("Your library is empty", "search skills.sh", "myskills add <source>", "▸ global")
	h.press("f")
	if h.a.view != Find {
		t.Fatal("f should open Find")
	}
}

func TestNarrowLayoutOpensPreview(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 80, 24)
	h.lacks("│", "About tdd.")
	h.cursorTo("tdd")
	for _, keys := range [][2]string{{"enter", "esc"}, {"l", "h"}, {"right", "left"}} {
		h.press(keys[0])
		if !h.a.reading {
			t.Fatalf("%s should open the preview", keys[0])
		}
		h.contains("About tdd.", "Body of /lib/tdd", "esc back")
		h.press(keys[1])
		if h.a.reading {
			t.Fatalf("%s should close the preview", keys[1])
		}
	}

	// Growing the terminal brings the preview next to the list.
	h.send(windowSize(130, 30))
	h.contains("│", "About tdd.")
}

func TestViewFitsAnySize(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	for _, size := range [][2]int{{120, 30}, {100, 20}, {99, 20}, {60, 15}, {40, 10}, {30, 8}, {200, 50}} {
		h.send(windowSize(size[0], size[1]))
		for _, view := range []string{"skills", "find"} {
			if view == "find" {
				h.press("f")
			}
			lines := strings.Split(h.a.View().Content, "\n")
			if len(lines) > size[1] {
				t.Errorf("%v %s: %d lines", size, view, len(lines))
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > size[0] {
					t.Errorf("%v %s: line %d is %d wide: %q", size, view, i, w, ansi.Strip(l))
				}
			}
			if view == "find" {
				h.press("esc")
			}
		}
	}
}

func TestCursorShowsWithoutColor(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.press("down")
	var marked []string
	for _, l := range strings.Split(stripView(h.a.View().Content), "\n") {
		if strings.Contains(l, "▌") {
			marked = append(marked, l)
		}
	}
	if len(marked) != 1 || !strings.Contains(marked[0], h.a.sk.rows[1].name()) {
		t.Fatalf("marked lines = %q", marked)
	}
}

func TestHeaderKeepsScopeWithLongPath(t *testing.T) {
	long := core.Scope{Root: "/work/" + strings.Repeat("deeply/nested/", 8) + "proj"}
	h := newHarness(t, libraryFixture(), Options{View: Skills, Scope: long, Project: &long}, 120, 30)
	for _, w := range []int{120, 60, 40} {
		h.send(windowSize(w, 20))
		header := ansi.Strip(strings.Split(h.a.View().Content, "\n")[0])
		if ansi.StringWidth(header) > w || !strings.Contains(header, "▸ ") || !strings.HasSuffix(header, "/proj ") {
			t.Errorf("width %d: header = %q", w, header)
		}
	}
}

func TestHelpToggle(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.press("?")
	h.contains("check updates", "default agents", "edit", "preview down")
	h.press("?")
	h.lacks("default agents")
}

func TestQuitWaitsForMutations(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.cursorTo("review")
	_, pending := h.a.Update(keyPress("space"))
	h.press("q")
	if h.quit {
		t.Fatal("quit should wait for the change in flight")
	}
	h.contains("Finishing 1 change")
	h.exec(pending)
	if !h.quit {
		t.Fatal("quit should follow once the change is done")
	}
}

func TestBrokenLinkBesideSkillOfTheSameName(t *testing.T) {
	f := libraryFixture() // .claude/skills/gone → the removed old/gone
	f.lib = append(f.lib, core.Skill{Path: "gone", Name: "gone", Description: "Another gone.", Dir: "/lib/gone", Entry: "gone"})
	h := newHarness(t, f, projectOptions(), 120, 30)
	h.press("tab", "tab")
	h.cursorTo("gone")
	if r, _ := h.a.current(); r.entry == nil {
		t.Fatal("the Issues tab should hold the broken link, not the skill")
	}
	h.press("space")
	if got := f.called("Disable"); !slices.Equal(got, []string{"Disable /work/proj old/gone agents=[claude-code] force=false"}) {
		t.Fatalf("calls = %v", got)
	}

	// A missing path without a slash is passed as is: core prefers the broken
	// link's path over a skill's name.
	f = libraryFixture()
	f.status[project].Broken[0].Skill = "gone"
	f.lib = append(f.lib, core.Skill{Path: "more/gone", Name: "gone", Description: "Another gone.", Dir: "/lib/more/gone", Entry: "more"})
	h = newHarness(t, f, projectOptions(), 120, 30)
	h.press("tab", "tab")
	h.cursorTo("gone")
	h.press("space")
	if got := f.called("Disable"); !slices.Equal(got, []string{"Disable /work/proj gone agents=[claude-code] force=false"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestAgentsGroupByResolvedDir(t *testing.T) {
	f := libraryFixture()
	// .claude → .agents: one directory, and one placement, for all three.
	f.dirs[project] = []core.AgentDir{{Path: "/work/proj/.agents/skills", Agents: []string{"universal", "antigravity", "claude-code"}}}
	st := f.status[project]
	st.Enablements[1].Placements = []core.Placement{
		{Path: "/work/proj/.agents/skills/tdd", Name: "tdd", Agents: []string{"universal", "antigravity", "claude-code"}, Kind: core.KindLink, Skill: "tdd"},
	}
	f.status[project] = st
	h := newHarness(t, f, projectOptions(), 120, 30)
	h.cursorTo("tdd")
	h.press("a")
	if n := len(h.a.modal.items); n != 1 {
		t.Fatalf("%d items, want one shared directory", n)
	}
	h.contains("[✓] Universal, Antigravity, Claude Code", ".agents/skills")
	h.press("space", "enter")
	if got := f.called("Disable"); !slices.Equal(got, []string{"Disable /work/proj tdd agents=[universal antigravity claude-code] force=false"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestAddingAgentsNeverConfirms(t *testing.T) {
	f := libraryFixture() // notes: a modified copy for Claude Code only
	h := newHarness(t, f, projectOptions(), 120, 30)
	h.cursorTo("notes")
	h.press("a", "space", "enter") // check Universal, Antigravity
	if h.a.modal != nil {
		t.Fatalf("adding agents asked: %s", stripView(h.a.View().Content))
	}
	if got := f.called("Enable"); !slices.Equal(got, []string{"Enable /work/proj notes mode= agents=[universal antigravity] force=false"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestRemovingAgentsConfirmsOnlyTheirCopies(t *testing.T) {
	f := libraryFixture()
	st := f.status[project]
	st.Enablements[0].Partial = false
	st.Enablements[0].Placements = []core.Placement{
		{Path: "/work/proj/.agents/skills/notes", Name: "notes", Agents: []string{"universal", "antigravity"}, Kind: core.KindCopy, Skill: "notes", Modified: true},
		{Path: "/work/proj/.claude/skills/notes", Name: "notes", Agents: []string{"claude-code"}, Kind: core.KindCopy, Skill: "notes", Modified: true},
	}
	f.status[project] = st
	h := newHarness(t, f, projectOptions(), 120, 30)
	h.cursorTo("notes")
	h.press("a", "space", "enter") // uncheck Universal, Antigravity
	if m := h.a.modal; m == nil || m.title != "Discard local changes?" || !strings.Contains(m.body, ".agents/skills/notes") || strings.Contains(m.body, ".claude/skills/notes") {
		t.Fatalf("modal = %+v", m)
	}
	h.press("y")
	if got := f.called("Disable"); !slices.Equal(got, []string{"Disable /work/proj notes agents=[universal antigravity] force=true"}) {
		t.Fatalf("calls = %v", got)
	}
}

func TestUpdateRetryRedoesOnlyTheFailedPhase(t *testing.T) {
	f := libraryFixture()
	f.checks = []core.UpdateCheck{{Skill: f.lib[3], State: core.Available}}
	f.failNext("RefreshCopies", fmt.Errorf("%w: .agents/skills/tdd has local changes", core.ErrModified))
	h := newHarness(t, f, projectOptions(), 120, 30)
	h.a.sk.updates["tdd"] = f.checks[0]
	h.cursorTo("tdd")
	h.press("u")
	h.contains("Discard local changes?", ".agents/skills/tdd has local changes")
	h.lacks("in the library are replaced")
	if _, ok := h.a.sk.updates["tdd"]; ok {
		t.Fatal("the update badge should clear once the library is updated")
	}

	// Meanwhile a newer version appears upstream and the library gets local
	// changes: discarding the project copy's changes must not replace them,
	// nor force the global copies.
	f.mu.Lock()
	f.lib[3].Modified = true
	f.checks = []core.UpdateCheck{{Skill: f.lib[3], State: core.Available}}
	f.mu.Unlock()
	h.press("y")
	want := []string{
		"CheckUpdates [tdd]",
		"ApplyUpdates [tdd] force=false",
		"RefreshCopies /work/proj [tdd] force=false",
		"RefreshCopies /work/proj [tdd] force=true",
		"RefreshCopies global [tdd] force=false",
	}
	if got := f.called(""); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	h.contains("✓ Updated tdd")
	h.lacks("is up to date")
	if !slices.Contains(h.a.summary, "✓ Updated tdd") {
		t.Fatalf("summary = %v", h.a.summary)
	}
}

func TestViewKeepsOnlySGR(t *testing.T) {
	sgr := regexp.MustCompile("\x1b\\[[0-9;:]*m")
	check := func(content string, visible ...string) {
		t.Helper()
		if rest := sgr.ReplaceAllString(content, ""); strings.ContainsAny(rest, "\x1b\x07") || strings.Contains(rest, "pwned") {
			t.Errorf("view holds escapes other than SGR: %q", rest)
		}
		for _, v := range visible {
			if !strings.Contains(stripView(content), v) {
				t.Errorf("view lacks %q:\n%s", v, stripView(content))
			}
		}
	}

	// An entry found on disk with a DCS and an OSC in its name and path, for
	// an agent whose configured name holds an OSC.
	f := libraryFixture()
	f.agents = append(f.agents, agent.Agent{ID: "rogue", Name: "Ro\x1b]0;pwned\x07gue"})
	st := f.status[project]
	st.Others = append(st.Others, core.Placement{
		Path:   "/work/proj/.claude/skills/ev\x1bPq#0;2\x1b\\il\x1b]8;;https://pwned\x07",
		Name:   "ev\x1bP1$rpwned\x1b\\il\x1b]0;pwned\x07",
		Agents: []string{"claude-code", "rogue"},
		Kind:   core.KindOther,
	})
	f.status[project] = st
	h := newHarness(t, f, projectOptions(), 120, 30)
	h.press("tab", "tab")
	h.cursorTo(st.Others[1].Name)
	check(h.a.View().Content, "◇ evil", ".claude/skills/evil", "Claude Code, Rogue")

	// The add picker, for a local folder with escapes in its path.
	p := newPicker(&core.Fetched{
		Spec:       source.Spec{Kind: source.Local, Path: "/src/ev\x1bP1$rpwned\x1b\\il\x1b]0;pwned\x07"},
		Candidates: []core.Candidate{{Name: "one", Dir: "/src/one"}},
	})
	drivePicker(p)
	check(p.View().Content, "Add from /src/evil")
}

func TestUpdateReportsUpToDateOnlyWhenNothingChanged(t *testing.T) {
	f := libraryFixture()
	f.checks = []core.UpdateCheck{{Skill: f.lib[3], State: core.UpToDate}}
	h := newHarness(t, f, projectOptions(), 120, 30)
	h.cursorTo("tdd")
	h.press("u")
	h.contains("tdd is up to date")
	if got := f.called("ApplyUpdates"); len(got) != 0 {
		t.Fatalf("calls = %v", got)
	}
}

func TestHelpBarAndFullHelp(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 60, 24)
	lines := strings.Split(h.view(), "\n")
	if bar := lines[len(lines)-1]; !strings.HasSuffix(strings.TrimRight(bar, " "), "? help") || strings.Contains(bar, "…") {
		t.Fatalf("help bar = %q", bar)
	}

	h.send(windowSize(120, 30))
	h.press("?")
	help := strings.Split(h.a.helpView(), "\n")
	lines = strings.Split(h.view(), "\n")
	for _, l := range lines[len(lines)-len(help):] {
		if !strings.HasPrefix(l, " ") || strings.HasPrefix(l, "  ") {
			t.Errorf("full help line not aligned: %q", l)
		}
	}
	h.press("esc")
	if h.a.help.ShowAll || h.quit {
		t.Fatal("esc should close the help")
	}

	// Narrower, the groups fold into fewer columns rather than being cut.
	h.press("?")
	for _, w := range []int{80, 60} {
		h.send(windowSize(w, 30))
		help := h.a.helpView()
		if strings.Contains(help, "…") || !strings.Contains(help, "quit") {
			t.Errorf("width %d: full help =\n%s", w, help)
		}
		for l := range strings.Lines(help) {
			if ansi.StringWidth(strings.TrimSuffix(l, "\n")) > w-2 {
				t.Errorf("width %d: full help line too wide: %q", w, l)
			}
		}
	}
}

func TestReloadReadsChangedFiles(t *testing.T) {
	h := newHarness(t, libraryFixture(), projectOptions(), 120, 30)
	h.cursorTo("tdd")
	h.contains("About tdd.")
	read := readSkill
	t.Cleanup(func() { readSkill = read })
	readSkill = func(dir string) (string, error) {
		if dir == "/lib/tdd" {
			return "---\nname: tdd\ndescription: Changed outside.\n---\n# New body", nil
		}
		return read(dir)
	}
	h.svc.lib[3].Description = "Changed outside."
	h.press("r")
	h.contains("Changed outside.", "New body")

	// Coming back into focus reloads too.
	h.svc.lib = h.svc.lib[:3]
	h.send(tea.FocusMsg{})
	if slices.ContainsFunc(h.a.data.lib, func(sk core.Skill) bool { return sk.Path == "tdd" }) {
		t.Fatal("focus should reload the library")
	}
}

// A load in flight on reload may have read the file before it changed: its
// result is dropped and the doc loads again.
func TestReloadRestartsLoadsInFlight(t *testing.T) {
	ctx, th := context.Background(), newTheme(true)
	p := newPreviewer()
	d := dirDoc("/lib/tdd")
	load := p.request(ctx, d, 40, th)
	p.recheckLocal()
	p.handle(load().(docMsg))
	if _, ready, _ := p.body(d, 40, th); ready {
		t.Fatal("a load started before the reload should not be shown")
	}
	if p.request(ctx, d, 40, th) == nil {
		t.Fatal("the doc should load again")
	}
}
