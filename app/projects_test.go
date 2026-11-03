package main

import (
	"os"
	"slices"
	"testing"

	"github.com/legibet/myskills/internal/core"
)

func TestProjectsList(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review", "tdd": "skills/tdd"}))
	web := h.project("web")
	api := h.project("api")
	h.enable(web, "review", core.EnableOptions{})
	h.enable(web, "tdd", core.EnableOptions{})
	h.enable(api, "tdd", core.EnableOptions{})
	h.start()
	h.click("Projects")
	h.want("Global", "Every project · 0 skills", tilde(web.Root)+" · 2 skills", tilde(api.Root)+" · 1 skill")
	// Global first, then the projects by name.
	if names := []string{placeName(h.a.data.places[1].scope), placeName(h.a.data.places[2].scope)}; !slices.Equal(names, []string{"api", "web"}) {
		t.Errorf("projects = %v", names)
	}

	h.click("web")
	h.want("Use review.", "Use tdd.")
	h.click("Reveal in Finder")
	if !slices.Equal(h.revealed, []string{web.Root}) {
		t.Errorf("revealed %v", h.revealed)
	}

	// A skill's name opens it on Skills.
	h.click("tdd")
	if h.a.page != pageSkills || h.a.skills.path != h.skill("tdd").Path {
		t.Errorf("page %q, skill %q", h.a.page, h.a.skills.path)
	}
}

func TestAddSkills(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review", "tdd": "skills/tdd", "pdf": "skills/pdf"}))
	web := h.project("web")
	h.enable(web, "review", core.EnableOptions{})
	h.start()
	h.click("Projects")
	h.click("web")

	h.click("Add Skills…")
	h.want("Add Skills to web", "tdd", "pdf")
	h.click("tdd")
	h.click("pdf")
	h.click("Copy")
	h.click("Enable 2")
	st := h.status(web)
	if len(st.Enablements) != 3 {
		t.Fatalf("enablements = %+v, want three", st.Enablements)
	}
	for _, e := range st.Enablements {
		want := core.Copy
		if e.Name == "review" {
			want = core.Link
		}
		if e.Mode != want {
			t.Errorf("%s mode = %s, want %s", e.Name, e.Mode, want)
		}
	}
	if h.a.projects.add.open {
		t.Error("the dialog is still open")
	}
}

func TestRefreshCopies(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review"}))
	web := h.project("web")
	h.enable(web, "review", core.EnableOptions{Mode: core.Copy})
	writeFile(t, h.skill("review").Dir+"/extra.md", "library edit")
	h.start()
	h.click("Projects")
	h.click("web")
	h.want("Outdated")

	h.click("Refresh Copies")
	if pl := h.status(web).Enablements[0].Placements[0]; pl.Outdated {
		t.Fatal("the copy is still outdated")
	}
	h.wantNo("Refresh Copies")
}

func TestRemoveBrokenLink(t *testing.T) {
	h := newHarness(t)
	h.add(h.repo("pack", map[string]string{"review": "skills/review", "tdd": "skills/tdd"}))
	web := h.project("web")
	h.enable(web, "tdd", core.EnableOptions{Agents: []string{"claude-code"}})
	if err := os.RemoveAll(h.skill("tdd").Dir); err != nil {
		t.Fatal(err)
	}
	if len(h.status(web).Broken) != 1 {
		t.Fatalf("status = %+v, want a broken link", h.status(web))
	}
	writeFile(t, web.Root+"/.claude/skills/mine/SKILL.md", "---\nname: mine\ndescription: Mine.\n---\n")
	h.start()
	h.click("Projects")
	h.click("web")
	h.want("Broken links", "Not managed by myskills", "mine")

	h.click("Remove")
	if st := h.status(web); len(st.Broken) != 0 || len(st.Others) != 1 {
		t.Fatalf("status = %+v, want only the unmanaged entry", st)
	}
}
