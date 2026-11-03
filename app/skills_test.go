package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/legibet/myskills/internal/core"
)

func TestEmptyLibrary(t *testing.T) {
	h := newHarness(t).start()
	h.want("Your library is empty", "Find Skills")
	h.click("Find Skills")
	if h.a.page != pageFind {
		t.Errorf("page = %q, want find", h.a.page)
	}
}

func TestEnableGloballyAndInProject(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review", "tdd": "skills/tdd"}))
	proj := h.project("web")
	h.start()
	h.want("review", "tdd", "Not enabled", "The body of review.")

	h.click("Enable")
	if got := h.enabled(core.Global); !slices.Equal(got, []string{"review"}) {
		t.Fatalf("global = %v, want [review]", got)
	}

	h.folder = proj.Root
	h.choose("Enable in Project…", "Choose Folder…")
	if got := h.enabled(proj); !slices.Equal(got, []string{"review"}) {
		t.Fatalf("project = %v, want [review]", got)
	}
	h.want("web", "Global, 1 project")

	// A known project is offered by name once it has a skill.
	h.click("tdd")
	h.choose("Enable in Project…", tilde(proj.Root))
	if got := h.enabled(proj); !slices.Equal(got, []string{"review", "tdd"}) {
		t.Fatalf("project = %v, want [review tdd]", got)
	}
}

func TestChooseHomeFolderIsRefused(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review"}))
	h.start()
	h.folder = h.home
	h.choose("Enable in Project…", "Choose Folder…")
	h.wantAlert("Can't enable skills in ~")
	if !strings.Contains(h.a.alerts[0].message, "home directory") {
		t.Errorf("message = %q", h.a.alerts[0].message)
	}
}

func TestFilterAndSearch(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review", "tdd": "skills/tdd"}))
	h.enable(core.Global, "review", core.EnableOptions{})
	h.start()
	h.click("Unused")
	h.want("tdd")
	h.wantNo("review")
	h.click("Enabled")
	h.want("review")
	h.wantNo("tdd")
	h.click("All")
	h.a.skills.query = "td"
	h.settle()
	h.want("tdd")
	h.wantNo("review")
}

func TestDisableCopyAsks(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review"}))
	proj := h.project("web")
	h.enable(proj, "review", core.EnableOptions{Mode: core.Copy})
	h.start()

	h.click("Disable in web")
	h.dismiss("Disable review in web?")
	if got := h.enabled(proj); len(got) != 1 {
		t.Fatalf("disabled without confirming: %v", got)
	}
	h.click("Disable in web")
	h.confirm("Disable review in web?")
	if got := h.enabled(proj); len(got) != 0 {
		t.Fatalf("project = %v, want nothing", got)
	}
}

func TestDisableModifiedCopyDiscards(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review"}))
	proj := h.project("web")
	h.enable(proj, "review", core.EnableOptions{Mode: core.Copy})
	copied := h.status(proj).Enablements[0].Placements[0].Path
	writeFile(t, filepath.Join(copied, "notes.md"), "mine")
	h.start()
	h.want("Modified")

	h.click("Disable in web")
	h.confirm("Discard local changes?")
	if _, err := os.Stat(copied); !os.IsNotExist(err) {
		t.Fatalf("modified copy left: %v", err)
	}
}

func TestSwitchMode(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review"}))
	proj := h.project("web")
	h.enable(proj, "review", core.EnableOptions{})
	h.start()

	h.choose("Link", "Copy")
	if e := h.status(proj).Enablements[0]; e.Mode != core.Copy {
		t.Fatalf("mode = %s, want copy", e.Mode)
	}
	h.choose("Copy", "Link")
	h.confirm("Replace the copies of review with links?")
	if e := h.status(proj).Enablements[0]; e.Mode != core.Link {
		t.Fatalf("mode = %s, want link", e.Mode)
	}
}

func TestAgentsMenu(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review"}))
	proj := h.project("web")
	h.enable(proj, "review", core.EnableOptions{Agents: []string{"universal"}})
	h.start()

	h.click("Universal, Antigravity")
	h.click("Claude Code")
	e := h.status(proj).Enablements[0]
	if len(e.Placements) != 2 {
		t.Fatalf("placements = %+v, want two", e.Placements)
	}
	h.click("Universal, Antigravity")
	if e := h.status(proj).Enablements[0]; len(e.Placements) != 1 || !slices.Contains(e.Placements[0].Agents, "claude-code") {
		t.Fatalf("placements = %+v, want Claude Code's", e.Placements)
	}
}

func TestUpdateRefreshesCopies(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review", "tdd": "skills/tdd"}))
	proj := h.project("web")
	h.enable(proj, "review", core.EnableOptions{Mode: core.Copy})
	writeSkill(t, h.path("pack", "skills", "review"), "review", "Use review.", "The new body.")
	commitAll(t, h.path("pack"))
	h.start()

	h.click("Check for Updates")
	h.want("Update All", "1 update available", "Update review")
	if n := h.a.updates.available(); n != 1 {
		t.Fatalf("available = %d, want 1", n)
	}
	h.click("Update review")
	if body, _ := os.ReadFile(filepath.Join(h.skill("review").Dir, "SKILL.md")); !strings.Contains(string(body), "The new body.") {
		t.Fatalf("library not updated: %s", body)
	}
	pl := h.status(proj).Enablements[0].Placements[0]
	if pl.Outdated {
		t.Error("the copy is still outdated")
	}
	if body, _ := os.ReadFile(filepath.Join(pl.Path, "SKILL.md")); !strings.Contains(string(body), "The new body.") {
		t.Errorf("copy not refreshed: %s", body)
	}
	h.wantNo("Update All")
}

func TestUpdateAll(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review", "tdd": "skills/tdd"}))
	for _, name := range []string{"review", "tdd"} {
		writeSkill(t, h.path("pack", "skills", name), name, "Use "+name+".", "New "+name+".")
	}
	commitAll(t, h.path("pack"))
	h.start()

	h.click("Check for Updates")
	h.click("Update All")
	for _, name := range []string{"review", "tdd"} {
		if body, _ := os.ReadFile(filepath.Join(h.skill(name).Dir, "SKILL.md")); !strings.Contains(string(body), "New "+name+".") {
			t.Errorf("%s not updated: %s", name, body)
		}
	}
	if h.a.updates.u != nil || h.a.updates.available() != 0 {
		t.Error("the check is kept after updating all")
	}
}

func TestRemoveFromLibrary(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review", "tdd": "skills/tdd"}))
	h.enable(core.Global, "review", core.EnableOptions{})
	h.start()

	h.choose("More", "Remove from Library…")
	h.wantAlert("Remove review from the library?")
	if msg := h.a.alerts[0].message; !strings.Contains(msg, "Enabled in Global") {
		t.Errorf("message = %q", msg)
	}
	h.confirm("Remove review from the library?")
	if _, err := h.svc.Resolve("review"); err == nil {
		t.Fatal("review is still in the library")
	}
	if got := h.enabled(core.Global); len(got) != 0 {
		t.Errorf("global = %v, want nothing", got)
	}
	h.want("tdd")
}

func TestUpdateAllSkipsLocalChanges(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review", "tdd": "skills/tdd"}))
	for _, name := range []string{"review", "tdd"} {
		writeSkill(t, h.path("pack", "skills", name), name, "Use "+name+".", "New "+name+".")
	}
	commitAll(t, h.path("pack"))
	writeFile(t, filepath.Join(h.skill("tdd").Dir, "mine.md"), "mine")
	h.start()

	h.click("Check for Updates")
	h.click("Update All")
	h.wantAlert("Some updates were skipped")
	if msg := h.a.alerts[0].message; !strings.Contains(msg, "tdd has local changes in the library") {
		t.Errorf("message = %q", msg)
	}
	h.confirm("Some updates were skipped")
	// review is updated; tdd still offers its update, which asks first.
	if h.a.updates.u != nil {
		t.Error("the check's checkouts are kept")
	}
	if _, ok := h.a.updates.checks[h.skill("review").Path]; ok {
		t.Error("review still has its check")
	}
	h.click("tdd")
	h.click("Update tdd")
	h.wantAlert("Discard local changes?")
	if msg := h.a.alerts[0].message; msg != "Your changes to tdd in the library are replaced by the update." {
		t.Errorf("message = %q", msg)
	}
	h.confirm("Discard local changes?")
	if body, _ := os.ReadFile(filepath.Join(h.skill("tdd").Dir, "SKILL.md")); !strings.Contains(string(body), "New tdd.") {
		t.Errorf("tdd not updated: %s", body)
	}
}

func TestRefreshModifiedCopyAsks(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review"}))
	proj := h.project("web")
	h.enable(proj, "review", core.EnableOptions{Mode: core.Copy})
	copied := h.status(proj).Enablements[0].Placements[0].Path
	writeFile(t, filepath.Join(copied, "notes.md"), "mine")
	writeFile(t, filepath.Join(h.skill("review").Dir, "extra.md"), "library edit")
	h.start()

	h.click("Refresh")
	h.wantAlert("Discard local changes?")
	if msg := h.a.alerts[0].message; msg != "These copies have local changes that will be lost:\n"+tilde(copied) {
		t.Errorf("message = %q", msg)
	}
	h.confirm("Discard local changes?")
	if _, err := os.Stat(filepath.Join(copied, "notes.md")); !os.IsNotExist(err) {
		t.Errorf("the local change stayed: %v", err)
	}
}
