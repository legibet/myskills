package core

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/legibet/myskills/internal/agent"
)

// setup returns an env with alpha and beta installed from a git repo.
func setup(t *testing.T) *env {
	t.Helper()
	e := newEnv(t)
	_, url := e.repo("src", map[string]string{"alpha": "skills/alpha", "beta": "skills/beta"})
	e.add(url)
	return e
}

func TestEnableLinkInGitProject(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", true)
	userExclude := "# user patterns\n*.log\n"
	writeFile(t, filepath.Join(sc.Root, ".git", "info", "exclude"), userExclude)

	res := e.enable(sc, "alpha", EnableOptions{})
	agentsLink := filepath.Join(sc.Root, ".agents", "skills", "alpha")
	claudeLink := filepath.Join(sc.Root, ".claude", "skills", "alpha")
	if !slices.Equal(changePaths(res.Changes), []string{agentsLink, claudeLink}) || len(res.Warnings) > 0 {
		t.Fatalf("result = %+v", res)
	}
	for i, c := range res.Changes {
		// Every agent using a directory is reported, also the ones not
		// requested: Antigravity shares .agents/skills with Universal.
		wantAgents := [][]string{{"universal", "antigravity"}, {"claude-code"}}[i]
		if c.Action != Linked || c.Mode != Link || !slices.Equal(c.Agents, wantAgents) || c.Warning != "" {
			t.Errorf("change %+v", c)
		}
		if got := readlink(t, c.Path); got != e.libDir("alpha") {
			t.Errorf("%s → %s", c.Path, got)
		}
	}
	wantBlock := userExclude + excludeBegin + "\n/.agents/skills/alpha\n/.claude/skills/alpha\n" + excludeEnd + "\n"
	if got := exclude(t, sc.Root); got != wantBlock {
		t.Errorf("exclude =\n%s\nwant\n%s", got, wantBlock)
	}
	if out := runGit(t, sc.Root, "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Errorf("links show in git status:\n%s", out)
	}
	if !slices.Equal(e.lock().Projects, []string{sc.Root}) {
		t.Errorf("projects = %v", e.lock().Projects)
	}

	st := e.status(sc)
	if len(st.Enablements) != 1 || len(st.Broken)+len(st.Others) != 0 {
		t.Fatalf("status = %+v", st)
	}
	en := st.Enablements[0]
	if en.Skill != "alpha" || en.Name != "alpha" || en.Mode != Link || en.Partial || len(en.Placements) != 2 {
		t.Errorf("enablement = %+v", en)
	}
	if p := en.Placements[0]; p.Kind != KindLink || !slices.Equal(p.Agents, []string{"universal", "antigravity"}) {
		t.Errorf("placement = %+v", p)
	}

	// Enabling again changes nothing.
	again := e.enable(sc, "alpha", EnableOptions{})
	for _, c := range again.Changes {
		if c.Action != Unchanged || c.Mode != Link {
			t.Errorf("second enable: %+v", c)
		}
	}

	dis, err := e.svc.Disable(sc, "alpha", DisableOptions{})
	if err != nil || len(dis.Changes) != 2 || dis.Changes[0].Action != Removed {
		t.Fatalf("disable = %+v, %v", dis, err)
	}
	if exists(agentsLink) || exists(claudeLink) {
		t.Error("links not removed")
	}
	if got := exclude(t, sc.Root); got != userExclude {
		t.Errorf("exclude after disable = %q", got)
	}
	if len(e.lock().Projects) != 0 {
		t.Errorf("project kept: %v", e.lock().Projects)
	}

	// Disabling a skill that is not enabled is a no-op.
	if dis, err := e.svc.Disable(sc, "alpha", DisableOptions{}); err != nil || len(dis.Changes) != 0 {
		t.Errorf("disable again = %+v, %v", dis, err)
	}
}

func TestEnableGlobal(t *testing.T) {
	e := setup(t)
	res := e.enable(Global, "alpha", EnableOptions{})
	want := []string{filepath.Join(e.home, ".agents", "skills", "alpha"), filepath.Join(e.home, ".claude", "skills", "alpha")}
	if !slices.Equal(changePaths(res.Changes), want) {
		t.Fatalf("changes = %v", changePaths(res.Changes))
	}
	if len(e.lock().Projects) != 0 {
		t.Error("global scope recorded as a project")
	}

	claude := e.path("claude-config")
	t.Setenv("CLAUDE_CONFIG_DIR", claude)
	res = e.enable(Global, "beta", EnableOptions{Agents: []string{"claude-code"}})
	if got := changePaths(res.Changes); !slices.Equal(got, []string{filepath.Join(claude, "skills", "beta")}) {
		t.Errorf("CLAUDE_CONFIG_DIR changes = %v", got)
	}

	sc := e.project("proj", false)
	e.enable(sc, "alpha", EnableOptions{})
	usage, err := e.svc.Usage()
	if err != nil {
		t.Fatal(err)
	}
	if got := usage["alpha"]; !slices.Equal(got, []Scope{Global, sc}) {
		t.Errorf("usage[alpha] = %v", got)
	}
	if got := usage["beta"]; !slices.Equal(got, []Scope{Global}) {
		t.Errorf("usage[beta] = %v", got)
	}
}

func TestWorktreeSharedExclude(t *testing.T) {
	e := setup(t)
	main := e.project("main", true)
	wtDir := e.path("wt")
	runGit(t, main.Root, "worktree", "add", "-q", "-b", "wt", wtDir)
	wt, err := e.svc.ProjectScope(wtDir)
	if err != nil || wt.Root != wtDir {
		t.Fatalf("worktree scope = %v, %v", wt, err)
	}

	e.enable(main, "alpha", EnableOptions{})
	e.enable(wt, "beta", EnableOptions{Agents: []string{"universal"}})
	want := excludeBegin + "\n/.agents/skills/alpha\n/.agents/skills/beta\n/.claude/skills/alpha\n" + excludeEnd + "\n"
	if got := managedBlock(exclude(t, wtDir)); got != want {
		t.Errorf("exclude =\n%s\nwant\n%s", got, want)
	}
	if out := runGit(t, wtDir, "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Errorf("worktree status:\n%s", out)
	}

	// Disabling in one worktree keeps the other worktree's entries.
	if _, err := e.svc.Disable(main, "alpha", DisableOptions{}); err != nil {
		t.Fatal(err)
	}
	want = excludeBegin + "\n/.agents/skills/beta\n" + excludeEnd + "\n"
	if got := managedBlock(exclude(t, main.Root)); got != want {
		t.Errorf("exclude after disable =\n%s\nwant\n%s", got, want)
	}
}

func TestEnableLinkRefusedWhenTracked(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", true)
	e.enable(sc, "alpha", EnableOptions{Mode: Copy, Agents: []string{"universal"}})
	commitAll(t, sc.Root, "commit the copy")
	copyDir := filepath.Join(sc.Root, ".agents", "skills", "alpha")
	lockBefore := e.lock()

	_, err := e.svc.Enable(sc, e.skill("alpha"), EnableOptions{Mode: Link, Agents: []string{"universal"}})
	if err == nil || !strings.Contains(err.Error(), "tracked by git") || !strings.Contains(err.Error(), "git rm -r --cached .agents/skills/alpha") {
		t.Fatalf("err = %v", err)
	}
	if !isRealDir(copyDir) || len(e.lock().Copies) != len(lockBefore.Copies) {
		t.Error("the committed copy changed")
	}
	if out := runGit(t, sc.Root, "status", "--porcelain"); out != "" {
		t.Errorf("work tree changed:\n%s", out)
	}
}

func TestEnableConvertLinkAndCopy(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", true)
	e.enable(sc, "alpha", EnableOptions{})
	paths := []string{filepath.Join(sc.Root, ".agents", "skills", "alpha"), filepath.Join(sc.Root, ".claude", "skills", "alpha")}

	res := e.enable(sc, "alpha", EnableOptions{Mode: Copy})
	for _, c := range res.Changes {
		if c.Action != Copied || c.Mode != Copy || c.Warning != "" {
			t.Errorf("to copy: %+v", c)
		}
	}
	lock := e.lock()
	for _, p := range paths {
		if !isRealDir(p) || lock.Copies[p].Skill != "alpha" || lock.Copies[p].Hash == "" {
			t.Errorf("%s: not a recorded copy (%+v)", p, lock.Copies[p])
		}
	}
	if got := readFile(t, filepath.Join(paths[0], "SKILL.md")); !strings.Contains(got, "alpha body") {
		t.Errorf("copied SKILL.md = %q", got)
	}
	if st := e.status(sc); st.Enablements[0].Mode != Copy || st.Enablements[0].Placements[0].Modified {
		t.Errorf("status = %+v", st.Enablements)
	}
	if strings.Contains(exclude(t, sc.Root), "alpha") {
		t.Error("copies still excluded")
	}

	res = e.enable(sc, "alpha", EnableOptions{Mode: Link})
	for _, c := range res.Changes {
		if c.Action != Linked || c.Mode != Link {
			t.Errorf("to link: %+v", c)
		}
	}
	for _, p := range paths {
		if !isSymlink(p) {
			t.Errorf("%s is not a link", p)
		}
	}
	if len(e.lock().Copies) != 0 {
		t.Errorf("copy records left: %v", e.lock().Copies)
	}
	if l := leftovers(t, filepath.Dir(paths[0]), filepath.Dir(paths[1])); len(l) > 0 {
		t.Errorf("staging left behind: %v", l)
	}
}

func TestEnableWithoutModeNeverConverts(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", false)
	e.enable(sc, "alpha", EnableOptions{Mode: Copy, Agents: []string{"universal"}})

	// New targets follow the existing mode.
	res := e.enable(sc, "alpha", EnableOptions{})
	if c := res.Changes; c[0].Action != Unchanged || c[0].Mode != Copy || c[1].Action != Copied {
		t.Fatalf("changes = %+v", c)
	}

	// Mixed placements: no mode keeps both as they are.
	e.enable(sc, "beta", EnableOptions{Agents: []string{"universal"}, Mode: Copy})
	e.enable(sc, "beta", EnableOptions{Agents: []string{"claude-code"}, Mode: Link})
	res = e.enable(sc, "beta", EnableOptions{})
	if c := res.Changes; c[0].Action != Unchanged || c[0].Mode != Copy || c[1].Action != Unchanged || c[1].Mode != Link {
		t.Fatalf("mixed changes = %+v", c)
	}
	st := e.status(sc)
	modes := map[string]Mode{}
	for _, en := range st.Enablements {
		modes[en.Skill] = en.Mode
	}
	if modes["alpha"] != Copy || modes["beta"] != Mixed {
		t.Errorf("modes = %v", modes)
	}

	// A library change makes copies outdated; only --copy refreshes them.
	writeFile(t, filepath.Join(e.libDir("alpha"), "SKILL.md"), "---\nname: alpha\ndescription: New.\n---\nv2\n")
	if p := e.status(sc).Enablements[0].Placements[0]; !p.Outdated || p.Modified {
		t.Fatalf("placement = %+v", p)
	}
	if res := e.enable(sc, "alpha", EnableOptions{}); res.Changes[0].Action != Unchanged {
		t.Errorf("refreshed without --copy: %+v", res.Changes)
	}
	res = e.enable(sc, "alpha", EnableOptions{Mode: Copy})
	for _, c := range res.Changes {
		if c.Action != Refreshed {
			t.Errorf("refresh: %+v", c)
		}
		if got := readFile(t, filepath.Join(c.Path, "SKILL.md")); !strings.Contains(got, "v2") {
			t.Errorf("%s not refreshed", c.Path)
		}
	}
	if p := e.status(sc).Enablements[0].Placements[0]; p.Outdated || p.Modified {
		t.Errorf("after refresh: %+v", p)
	}
}

func TestModifiedCopyProtection(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", true)
	e.enable(sc, "alpha", EnableOptions{Mode: Copy})
	agentsCopy := filepath.Join(sc.Root, ".agents", "skills", "alpha")
	claudeCopy := filepath.Join(sc.Root, ".claude", "skills", "alpha")
	writeFile(t, filepath.Join(claudeCopy, "local.md"), "my notes")

	st := e.status(sc)
	if ps := st.Enablements[0].Placements; ps[0].Modified || !ps[1].Modified {
		t.Fatalf("placements = %+v", ps)
	}

	// Nothing is removed when any copy is modified, not even the clean one.
	_, err := e.svc.Disable(sc, "alpha", DisableOptions{})
	wantErr(t, err, ErrModified, filepath.Join(".claude", "skills", "alpha"))
	if !isRealDir(agentsCopy) || !exists(filepath.Join(claudeCopy, "local.md")) {
		t.Fatal("disable touched the copies")
	}
	_, err = e.svc.Enable(sc, e.skill("alpha"), EnableOptions{Mode: Link})
	wantErr(t, err, ErrModified)
	if !isRealDir(agentsCopy) || !isRealDir(claudeCopy) {
		t.Fatal("enable touched the copies")
	}

	// A modified outdated copy is not refreshed without force.
	writeFile(t, filepath.Join(e.libDir("alpha"), "extra.md"), "new")
	res := e.enable(sc, "alpha", EnableOptions{Mode: Copy})
	if res.Changes[0].Action != Refreshed || res.Changes[1].Action != Unchanged || res.Changes[1].Warning == "" {
		t.Errorf("refresh = %+v", res.Changes)
	}

	res = e.enable(sc, "alpha", EnableOptions{Mode: Link, Force: true})
	if res.Changes[1].Action != Linked || !isSymlink(claudeCopy) {
		t.Errorf("forced conversion = %+v", res.Changes)
	}

	e.enable(sc, "beta", EnableOptions{Mode: Copy, Agents: []string{"universal"}})
	writeFile(t, filepath.Join(sc.Root, ".agents", "skills", "beta", "x"), "x")
	if _, err := e.svc.Disable(sc, "beta", DisableOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	if exists(filepath.Join(sc.Root, ".agents", "skills", "beta")) {
		t.Error("forced disable kept the copy")
	}
}

func TestEnableConflictLeavesFilesystemUntouched(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", true)
	mine := filepath.Join(sc.Root, ".claude", "skills", "alpha")
	writeFile(t, filepath.Join(mine, "SKILL.md"), "mine")

	for _, mode := range []Mode{"", Copy} {
		_, err := e.svc.Enable(sc, e.skill("alpha"), EnableOptions{Mode: mode, Force: true})
		wantErr(t, err, ErrConflict, filepath.Join(".claude", "skills", "alpha"), "not managed by myskills")
	}
	// The first target would have been fine, but nothing was applied.
	if exists(filepath.Join(sc.Root, ".agents")) {
		t.Error("the free target was created")
	}
	if readFile(t, filepath.Join(mine, "SKILL.md")) != "mine" {
		t.Error("the occupant changed")
	}
	if l := e.lock(); len(l.Projects) != 0 || len(l.Copies) != 0 {
		t.Errorf("lock changed: %+v", l)
	}

	// Another library skill's link is a conflict too, naming that skill.
	if err := os.RemoveAll(mine); err != nil {
		t.Fatal(err)
	}
	symlink(t, e.libDir("beta"), mine)
	_, err := e.svc.Enable(sc, e.skill("alpha"), EnableOptions{})
	wantErr(t, err, ErrConflict, "a link to library skill beta")
	if exists(filepath.Join(sc.Root, ".agents")) || readlink(t, mine) != e.libDir("beta") {
		t.Error("filesystem changed")
	}
}

func TestParentSymlinkedAgentDirs(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", true)
	mkdirAll(t, filepath.Join(sc.Root, ".agents"))
	// A junction on Windows, the directory link users can make there without
	// privileges.
	if err := symlinkDir(filepath.Join(sc.Root, ".agents"), filepath.Join(sc.Root, ".claude")); err != nil {
		t.Fatal(err)
	}
	commitAll(t, sc.Root, "share .agents with Claude Code")

	res := e.enable(sc, "alpha", EnableOptions{})
	place := filepath.Join(sc.Root, ".agents", "skills", "alpha")
	if len(res.Changes) != 1 || res.Changes[0].Path != place || !slices.Equal(res.Changes[0].Agents, []string{"universal", "claude-code", "antigravity"}) {
		t.Fatalf("changes = %+v", res.Changes)
	}
	st := e.status(sc)
	if len(st.Enablements) != 1 || st.Enablements[0].Partial || len(st.Enablements[0].Placements) != 1 {
		t.Fatalf("status = %+v", st)
	}
	if got := st.Enablements[0].Placements[0].Agents; !slices.Equal(got, []string{"universal", "claude-code", "antigravity"}) {
		t.Errorf("agents = %v", got)
	}
	dirs, err := e.svc.AgentDirs(sc)
	if err != nil || len(dirs) != 1 || dirs[0].Path != filepath.Join(sc.Root, ".agents", "skills") ||
		!slices.Equal(dirs[0].Agents, []string{"universal", "claude-code", "antigravity"}) {
		t.Errorf("AgentDirs = %+v, %v", dirs, err)
	}
	// Excluded under .claude too: Git for Windows walks into the junction.
	want := excludeBegin + "\n/.agents/skills/alpha\n/.claude/skills/alpha\n" + excludeEnd + "\n"
	if got := managedBlock(exclude(t, sc.Root)); got != want {
		t.Errorf("exclude = %q", got)
	}
	if out := runGit(t, sc.Root, "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Errorf("git status:\n%s", out)
	}

	// A copy recorded through one spelling is found through the other.
	e.enable(sc, "alpha", EnableOptions{Mode: Copy, Agents: []string{"universal"}})
	res = e.enable(sc, "alpha", EnableOptions{Mode: Copy, Agents: []string{"claude-code"}})
	if c := res.Changes[0]; c.Action != Unchanged || c.Mode != Copy {
		t.Errorf("through .claude: %+v", c)
	}
	if st := e.status(sc); st.Enablements[0].Mode != Copy || len(st.Others) != 0 {
		t.Errorf("status = %+v", st)
	}
}

func TestParentSymlinkedAgentDirMissing(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", false)
	// .claude points at .agents, which does not exist yet.
	symlink(t, ".agents", filepath.Join(sc.Root, ".claude"))
	res := e.enable(sc, "alpha", EnableOptions{})
	if len(res.Changes) != 1 || !slices.Equal(res.Changes[0].Agents, []string{"universal", "claude-code", "antigravity"}) ||
		!isSymlink(filepath.Join(sc.Root, ".agents", "skills", "alpha")) {
		t.Fatalf("changes = %+v", res.Changes)
	}
}

func TestDisableBrokenByName(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", true)
	e.enable(sc, "alpha", EnableOptions{})
	if err := os.RemoveAll(e.libDir("alpha")); err != nil {
		t.Fatal(err)
	}
	st := e.status(sc)
	if len(st.Broken) != 2 || st.Broken[0].Skill != "alpha" || len(st.Enablements) != 0 {
		t.Fatalf("status = %+v", st)
	}

	res, err := e.svc.Disable(sc, "alpha", DisableOptions{})
	if err != nil || len(res.Changes) != 2 || res.Skill != "alpha" {
		t.Fatalf("disable = %+v, %v", res, err)
	}
	if st := e.status(sc); len(st.Broken) != 0 {
		t.Errorf("broken left: %+v", st.Broken)
	}
	if strings.Contains(exclude(t, sc.Root), "alpha") {
		t.Error("exclude still lists alpha")
	}
	_, err = e.svc.Disable(sc, "alpha", DisableOptions{})
	wantErr(t, err, ErrNotFound)
}

func TestDisableBrokenByPathOverName(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", false)
	// pack/delta is named delta; the broken link points at the missing "delta".
	writeSkill(t, e.libDir("pack/delta"), "delta", "body")
	e.enable(sc, "pack/delta", EnableOptions{Mode: Copy, Agents: []string{"claude-code"}})
	broken := filepath.Join(sc.Root, ".agents", "skills", "delta")
	symlink(t, e.libDir("delta"), broken)

	res, err := e.svc.Disable(sc, "delta", DisableOptions{})
	if err != nil || len(res.Changes) != 1 || res.Changes[0].Path != broken {
		t.Fatalf("disable = %+v, %v", res, err)
	}
	if !isRealDir(filepath.Join(sc.Root, ".claude", "skills", "delta")) {
		t.Error("the copy of pack/delta was removed")
	}
}

func TestEnableBrokenLinkOfAnotherSkill(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", false)
	place := filepath.Join(sc.Root, ".agents", "skills", "alpha")
	symlink(t, e.libDir("old/alpha"), place)
	_, err := e.svc.Enable(sc, e.skill("alpha"), EnableOptions{Agents: []string{"universal"}, Force: true})
	wantErr(t, err, ErrConflict, "broken link to library skill old/alpha", "disable old/alpha")
	if readlink(t, place) != e.libDir("old/alpha") {
		t.Error("the broken link changed")
	}

	if _, err := e.svc.Disable(sc, "old/alpha", DisableOptions{}); err != nil {
		t.Fatal(err)
	}
	res := e.enable(sc, "alpha", EnableOptions{Agents: []string{"universal"}})
	if res.Changes[0].Action != Linked || readlink(t, place) != e.libDir("alpha") {
		t.Errorf("changes = %+v", res.Changes)
	}
}

func TestEnableRechecksPlacementsBeforeReplacing(t *testing.T) {
	tests := []struct {
		name   string
		before Mode // how alpha is enabled first; "" for not at all
		mode   Mode
		change func(e *env, place string)
		kept   func(e *env, place string) bool
		kind   error
	}{
		{
			"new", "", Link,
			func(e *env, place string) { writeSkill(e.t, place, "alpha", "mine") },
			func(e *env, place string) bool {
				return strings.Contains(readFile(e.t, filepath.Join(place, "SKILL.md")), "mine")
			},
			ErrConflict,
		},
		{
			"link", Link, Copy,
			func(e *env, place string) {
				if err := os.Remove(place); err != nil {
					e.t.Fatal(err)
				}
				symlink(e.t, e.libDir("beta"), place)
			},
			func(e *env, place string) bool { return isSymlink(place) && readlink(e.t, place) == e.libDir("beta") },
			ErrConflict,
		},
		{
			"copy", Copy, Link,
			func(e *env, place string) { writeFile(e.t, filepath.Join(place, "notes.md"), "mine") },
			func(_ *env, place string) bool { return isRealDir(place) && exists(filepath.Join(place, "notes.md")) },
			ErrModified,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := setup(t)
			sc := e.project("proj", false)
			if tt.before != "" {
				e.enable(sc, "alpha", EnableOptions{Mode: tt.before})
			}
			first := filepath.Join(sc.Root, ".agents", "skills", "alpha")
			second := filepath.Join(sc.Root, ".claude", "skills", "alpha")
			// The second placement changes while the first one is swapped in.
			rename = func(from, to string) error {
				if to == first {
					tt.change(e, second)
				}
				return os.Rename(from, to)
			}
			defer func() { rename = os.Rename }()

			res, err := e.svc.Enable(sc, e.skill("alpha"), EnableOptions{Mode: tt.mode})
			wantErr(t, err, tt.kind, filepath.Join(".claude", "skills", "alpha"), "left in place")
			if !slices.Equal(changePaths(res.Changes), []string{first}) {
				t.Errorf("changes = %+v", res.Changes)
			}
			if !tt.kept(e, second) {
				t.Error("the changed placement was replaced")
			}
		})
	}
}

func TestEnableForceKeepsReplacedCopy(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", false)
	e.enable(sc, "alpha", EnableOptions{Mode: Copy})
	first := filepath.Join(sc.Root, ".agents", "skills", "alpha")
	second := filepath.Join(sc.Root, ".claude", "skills", "alpha")
	writeFile(t, filepath.Join(second, "notes.md"), "mine")
	// Force discards the modified copy, but not a directory that replaced it
	// while the first placement was swapped in.
	rename = func(from, to string) error {
		if to == first {
			// Made before the copy is removed, so that it cannot reuse its inode.
			unrelated := e.path("unrelated")
			writeSkill(t, unrelated, "alpha", "unrelated")
			if err := os.RemoveAll(second); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(unrelated, second); err != nil {
				t.Fatal(err)
			}
		}
		return os.Rename(from, to)
	}
	defer func() { rename = os.Rename }()

	_, err := e.svc.Enable(sc, e.skill("alpha"), EnableOptions{Mode: Link, Force: true})
	wantErr(t, err, ErrConflict, filepath.Join(".claude", "skills", "alpha"), "left in place")
	if !strings.Contains(readFile(t, filepath.Join(second, "SKILL.md")), "unrelated") {
		t.Error("the directory that replaced the copy was deleted")
	}
}

func TestDisableRechecksCopiesBeforeRemoving(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", false)
	e.enable(sc, "alpha", EnableOptions{Mode: Copy})
	first := filepath.Join(sc.Root, ".agents", "skills", "alpha")
	second := filepath.Join(sc.Root, ".claude", "skills", "alpha")
	// The second copy is edited while the first one is removed.
	removeAll = func(path string) error {
		if path == first {
			writeFile(t, filepath.Join(second, "notes.md"), "mine")
		}
		return os.RemoveAll(path)
	}
	defer func() { removeAll = os.RemoveAll }()

	res, err := e.svc.Disable(sc, "alpha", DisableOptions{})
	wantErr(t, err, ErrModified, filepath.Join(".claude", "skills", "alpha"), "left in place")
	if !slices.Equal(changePaths(res.Changes), []string{first}) {
		t.Errorf("changes = %+v", res.Changes)
	}
	if !exists(filepath.Join(second, "notes.md")) {
		t.Error("the edit was deleted")
	}
	if _, ok := e.lock().Copies[second]; !ok {
		t.Error("the record of the kept copy was dropped")
	}
}

func TestDisableKeepsNestedProjectCopies(t *testing.T) {
	e := setup(t)
	app := e.project(filepath.Join("parent", "app"), false)
	parent := e.project("parent", false)
	e.enable(app, "alpha", EnableOptions{Mode: Copy})
	e.enable(parent, "alpha", EnableOptions{})
	if _, err := e.svc.Disable(parent, "alpha", DisableOptions{}); err != nil {
		t.Fatal(err)
	}

	lock := e.lock()
	if !slices.Equal(lock.Projects, []string{app.Root}) {
		t.Errorf("projects = %v", lock.Projects)
	}
	for _, key := range []string{
		filepath.Join(app.Root, ".agents", "skills", "alpha"),
		filepath.Join(app.Root, ".claude", "skills", "alpha"),
	} {
		if _, ok := lock.Copies[key]; !ok {
			t.Errorf("the record of %s was dropped", key)
		}
	}
	st := e.status(app)
	if len(st.Enablements) != 1 || st.Enablements[0].Mode != Copy || len(st.Others) != 0 {
		t.Errorf("status = %+v", st)
	}
}

func TestDisableBrokenByPathWithAmbiguousName(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", false)
	writeSkill(t, e.libDir("one/delta"), "delta", "")
	writeSkill(t, e.libDir("two/delta"), "delta", "")
	broken := filepath.Join(sc.Root, ".agents", "skills", "delta")
	symlink(t, e.libDir("delta"), broken)

	res, err := e.svc.Disable(sc, "delta", DisableOptions{})
	if err != nil || !slices.Equal(changePaths(res.Changes), []string{broken}) {
		t.Fatalf("disable = %+v, %v", res, err)
	}
}

func TestEnableCopyInsideItsLinkedSource(t *testing.T) {
	e := newEnv(t)
	// A skill under development at the root of its repository.
	sc := e.project("dev-skill", true)
	writeSkill(t, sc.Root, "dev-skill", "")
	if _, err := e.svc.LinkLocal(sc.Root); err != nil {
		t.Fatal(err)
	}
	_, err := e.svc.Enable(sc, e.skill("dev-skill"), EnableOptions{Mode: Copy})
	if err == nil || !strings.Contains(err.Error(), "into itself") {
		t.Fatalf("err = %v", err)
	}
	dirs := []string{filepath.Join(sc.Root, ".agents", "skills"), filepath.Join(sc.Root, ".claude", "skills")}
	if l := leftovers(t, dirs...); len(l) > 0 {
		t.Errorf("staging left behind: %v", l)
	}
}

func TestCopyOutdatedAgainstLibraryContent(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", false)
	writeFile(t, filepath.Join(e.libDir("alpha"), "x"), "x")
	symlink(t, "./x", filepath.Join(e.libDir("alpha"), "link"))
	e.enable(sc, "alpha", EnableOptions{Mode: Copy, Agents: []string{"universal"}})
	// Copying normalizes the link, so the copy hashes differently from the library.
	if got := readlink(t, filepath.Join(sc.Root, ".agents", "skills", "alpha", "link")); got != "x" {
		t.Fatalf("copied link → %q", got)
	}
	if p := e.status(sc).Enablements[0].Placements[0]; p.Outdated || p.Modified {
		t.Errorf("right after enabling: %+v", p)
	}

	writeFile(t, filepath.Join(e.libDir("alpha"), "x"), "x v2")
	if p := e.status(sc).Enablements[0].Placements[0]; !p.Outdated || p.Modified {
		t.Errorf("after a library change: %+v", p)
	}
}

func TestCopyOutdatedBehindDereferencedLink(t *testing.T) {
	e := newEnv(t)
	sc := e.project("proj", false)
	// A linked collection whose skill links a file shared by the collection;
	// copying dereferences the link.
	coll := e.path("coll")
	writeSkill(t, filepath.Join(coll, "alpha"), "alpha", "")
	writeFile(t, filepath.Join(coll, "shared.md"), "v1")
	symlink(t, "../shared.md", filepath.Join(coll, "alpha", "shared.md"))
	if _, err := e.svc.LinkLocal(coll); err != nil {
		t.Fatal(err)
	}
	e.enable(sc, "coll/alpha", EnableOptions{Mode: Copy, Agents: []string{"universal"}})
	if p := e.status(sc).Enablements[0].Placements[0]; p.Outdated || p.Modified {
		t.Errorf("right after enabling: %+v", p)
	}

	writeFile(t, filepath.Join(coll, "shared.md"), "v2")
	if p := e.status(sc).Enablements[0].Placements[0]; !p.Outdated || p.Modified {
		t.Errorf("after the shared file changed: %+v", p)
	}
	if _, err := e.svc.RefreshCopies(sc, nil, false); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(sc.Root, ".agents", "skills", "alpha", "shared.md")); got != "v2" {
		t.Errorf("refreshed copy: %q", got)
	}
}

func TestDisableLimitedToAgents(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", false)
	e.enable(sc, "alpha", EnableOptions{})
	res, err := e.svc.Disable(sc, "alpha", DisableOptions{Agents: []string{"claude-code"}})
	if err != nil || len(res.Changes) != 1 || !strings.Contains(res.Changes[0].Path, ".claude") {
		t.Fatalf("disable = %+v, %v", res, err)
	}
	st := e.status(sc)
	if len(st.Enablements) != 1 || !st.Enablements[0].Partial {
		t.Errorf("status = %+v", st)
	}
	_, err = e.svc.Disable(sc, "alpha", DisableOptions{Agents: []string{"nope"}})
	wantErr(t, err, ErrNotFound, "nope")
}

func TestEnableAgentResolution(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", false)
	if err := e.svc.AddAgent(agent.Agent{ID: "pi", Global: "~/.pi/skills"}); err != nil {
		t.Fatal(err)
	}

	_, err := e.svc.Enable(sc, e.skill("alpha"), EnableOptions{Agents: []string{"nope"}})
	wantErr(t, err, ErrNotFound, "nope")
	_, err = e.svc.Enable(sc, e.skill("alpha"), EnableOptions{Agents: []string{"pi"}})
	if err == nil || !strings.Contains(err.Error(), "no project skill directory") {
		t.Errorf("explicit agent without a project dir: %v", err)
	}
	_, err = e.svc.Enable(sc, e.skill("alpha"), EnableOptions{Mode: Mixed})
	if err == nil {
		t.Error("mode mixed accepted")
	}

	// Defaults without a dir for the scope are skipped.
	if err := e.svc.SetDefaultAgents([]string{"pi", "universal", "antigravity"}); err != nil {
		t.Fatal(err)
	}
	res := e.enable(sc, "alpha", EnableOptions{})
	if len(res.Changes) != 1 || !slices.Equal(res.Changes[0].Agents, []string{"universal", "antigravity"}) {
		t.Errorf("changes = %+v", res.Changes)
	}
	res = e.enable(Global, "alpha", EnableOptions{})
	if len(res.Changes) != 3 || res.Changes[0].Path != filepath.Join(e.home, ".pi", "skills", "alpha") {
		t.Errorf("global changes = %+v", res.Changes)
	}
	if err := e.svc.SetDefaultAgents([]string{"pi"}); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Enable(sc, e.skill("beta"), EnableOptions{})
	if err == nil || !strings.Contains(err.Error(), "skill directory") {
		t.Errorf("no target: %v", err)
	}
}

func TestEnableInvalidSkill(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", false)
	writeSkill(t, e.libDir("bad"), "not-bad", "")
	_, err := e.svc.Enable(sc, e.skill("bad"), EnableOptions{})
	if err == nil || !strings.Contains(err.Error(), "can't be enabled") {
		t.Errorf("err = %v", err)
	}
	// A skill that became invalid after the caller read it is refused too.
	sk := e.skill("alpha")
	writeFile(t, filepath.Join(sk.Dir, "SKILL.md"), "nope")
	if _, err := e.svc.Enable(sc, sk, EnableOptions{}); err == nil {
		t.Error("stale valid skill enabled")
	}
}

func TestEnableCopyFailureLeavesNothing(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", false)
	e.enable(sc, "alpha", EnableOptions{Agents: []string{"universal"}})
	// A symlink escaping the library entry makes every copy fail.
	symlink(t, e.path("elsewhere"), filepath.Join(e.libDir("alpha"), "escape"))
	writeFile(t, e.path("elsewhere"), "x")

	_, err := e.svc.Enable(sc, e.skill("alpha"), EnableOptions{Mode: Copy})
	if err == nil || !strings.Contains(err.Error(), "outside the source") {
		t.Fatalf("err = %v", err)
	}
	link := filepath.Join(sc.Root, ".agents", "skills", "alpha")
	if !isSymlink(link) || exists(filepath.Join(sc.Root, ".claude", "skills", "alpha")) {
		t.Error("a target changed")
	}
	if l := leftovers(t, filepath.Dir(link), filepath.Join(sc.Root, ".claude", "skills")); len(l) > 0 {
		t.Errorf("staging left behind: %v", l)
	}
}

func TestRefreshCopies(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", false)
	e.enable(sc, "alpha", EnableOptions{Mode: Copy})
	e.enable(sc, "beta", EnableOptions{Mode: Copy, Agents: []string{"universal"}})
	agentsCopy := filepath.Join(sc.Root, ".agents", "skills", "alpha")
	claudeCopy := filepath.Join(sc.Root, ".claude", "skills", "alpha")

	if res, err := e.svc.RefreshCopies(sc, nil, false); err != nil || len(res) != 0 {
		t.Fatalf("nothing outdated: %+v, %v", res, err)
	}

	writeFile(t, filepath.Join(e.libDir("alpha"), "SKILL.md"), "---\nname: alpha\ndescription: New.\n---\nv2\n")
	writeFile(t, filepath.Join(e.libDir("beta"), "new.md"), "new")
	writeFile(t, filepath.Join(claudeCopy, "local.md"), "mine")

	res, err := e.svc.RefreshCopies(sc, []string{"alpha"}, false)
	wantErr(t, err, ErrModified, filepath.Join(".claude", "skills", "alpha"))
	if len(res) != 1 || res[0].Skill != "alpha" || len(res[0].Changes) != 2 {
		t.Fatalf("results = %+v", res)
	}
	actions := map[string]Action{}
	for _, c := range res[0].Changes {
		actions[c.Path] = c.Action
	}
	if actions[agentsCopy] != Refreshed || actions[claudeCopy] != Unchanged {
		t.Errorf("actions = %v", actions)
	}
	if !strings.Contains(readFile(t, filepath.Join(agentsCopy, "SKILL.md")), "v2") {
		t.Error("copy not refreshed")
	}
	if exists(filepath.Join(sc.Root, ".agents", "skills", "beta", "new.md")) {
		t.Error("beta refreshed though not selected")
	}

	res, err = e.svc.RefreshCopies(sc, nil, true)
	if err != nil || len(res) != 2 {
		t.Fatalf("forced = %+v, %v", res, err)
	}
	if exists(filepath.Join(claudeCopy, "local.md")) || !exists(filepath.Join(sc.Root, ".agents", "skills", "beta", "new.md")) {
		t.Error("forced refresh incomplete")
	}
	st := e.status(sc)
	for _, en := range st.Enablements {
		for _, p := range en.Placements {
			if p.Outdated || p.Modified {
				t.Errorf("after refresh: %+v", p)
			}
		}
	}
}

func TestStatusOthersAndHidden(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", false)
	dir := filepath.Join(sc.Root, ".agents", "skills")
	writeSkill(t, filepath.Join(dir, "handmade"), "handmade", "")
	writeFile(t, filepath.Join(dir, "README.md"), "x")
	writeFile(t, filepath.Join(dir, ".DS_Store"), "x")
	symlink(t, e.path("elsewhere"), filepath.Join(dir, "foreign"))
	rel, err := filepath.Rel(dir, e.libDir("alpha"))
	if err != nil {
		t.Fatal(err)
	}
	symlink(t, rel, filepath.Join(dir, "relative"))

	st := e.status(sc)
	var names []string
	for _, p := range st.Others {
		names = append(names, p.Name)
		if p.Kind != KindOther || p.Skill != "" {
			t.Errorf("other %+v", p)
		}
	}
	if !slices.Equal(names, []string{"README.md", "foreign", "handmade", "relative"}) {
		t.Errorf("others = %v", names)
	}
}

func TestParentSymlinkedAgentDirOnlyThroughLink(t *testing.T) {
	e := setup(t)
	sc := e.project("proj", false)
	symlink(t, ".agents", filepath.Join(sc.Root, ".claude"))
	// Claude Code alone: its dir can only be created at the link's target.
	res := e.enable(sc, "alpha", EnableOptions{Agents: []string{"claude-code"}})
	if len(res.Changes) != 1 || !isSymlink(filepath.Join(sc.Root, ".agents", "skills", "alpha")) {
		t.Fatalf("changes = %+v", res.Changes)
	}
}
