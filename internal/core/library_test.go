package core

import (
	"context"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/legibet/myskills/internal/source"
	"github.com/legibet/myskills/internal/store"
)

func TestInstallFromRepoWithSeveralSkills(t *testing.T) {
	e := newEnv(t)
	dir, url := e.repo("src", map[string]string{
		"alpha": "skills/alpha",
		"beta":  "skills/beta",
		"gamma": ".claude/skills/gamma",
	})
	writeFile(t, filepath.Join(dir, "skills", "broken", "SKILL.md"), "---\nname: Not_Valid\ndescription: x\n---\n")
	commit := commitAll(t, dir, "add broken")

	f := e.fetch(url)
	defer func() { _ = f.Close() }()
	if f.Commit != commit {
		t.Errorf("Commit = %s, want %s", f.Commit, commit)
	}
	got := map[string]Candidate{}
	for _, c := range f.Candidates {
		got[c.RelPath] = c
	}
	for rel, name := range map[string]string{"skills/alpha": "alpha", "skills/beta": "beta", ".claude/skills/gamma": "gamma"} {
		if c := got[rel]; c.Name != name || c.Issue != "" || c.Description != "The "+name+" skill." {
			t.Errorf("candidate %s = %+v", rel, c)
		}
	}
	broken := got["skills/broken"]
	if broken.Name != "broken" || broken.Issue == "" {
		t.Errorf("invalid candidate = %+v, want Name broken with an Issue", broken)
	}

	res := e.svc.Install(context.Background(), f, f.Candidates)
	for _, r := range res {
		switch r.Candidate.Name {
		case "broken":
			if r.Err == nil {
				t.Error("installing an invalid candidate succeeded")
			}
		default:
			if r.Err != nil || r.Already || r.Path != r.Candidate.Name {
				t.Errorf("install %s = %+v", r.Candidate.Name, r)
			}
		}
	}

	lib, err := e.svc.Library()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, sk := range lib {
		names = append(names, sk.Name)
		rel := got[sk.Record.Source.Path]
		want := store.Source{Type: "git", URL: url, Path: rel.RelPath}
		if !sk.Valid() || sk.Modified || sk.Linked || sk.Record.Source != want || sk.Record.Commit != commit || sk.Record.Hash == "" {
			t.Errorf("library skill %+v, record %+v", sk, sk.Record)
		}
		if sk.Dir != e.libDir(sk.Name) || sk.Entry != sk.Name {
			t.Errorf("%s: Dir %s Entry %s", sk.Name, sk.Dir, sk.Entry)
		}
		if sk.SourceLabel() != source.Label(url) {
			t.Errorf("SourceLabel = %q", sk.SourceLabel())
		}
	}
	if !slices.Equal(names, []string{"alpha", "beta", "gamma"}) {
		t.Errorf("library = %v", names)
	}
	if got := readFile(t, filepath.Join(e.libDir("alpha"), "SKILL.md")); !strings.Contains(got, "alpha body") {
		t.Errorf("installed SKILL.md = %q", got)
	}
	if l := leftovers(t, e.svc.paths.Skills); len(l) > 0 {
		t.Errorf("staging left behind: %v", l)
	}

	root := f.Root
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if exists(root) {
		t.Error("Close kept the checkout")
	}
}

func TestFetchSkillFilter(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo("src", map[string]string{
		"review": "pack/review",
		"tdd":    "skills/tdd-dir",
	})
	dir := e.path("src")
	writeSkill(t, filepath.Join(dir, "other", "review"), "review", "other")
	commitAll(t, dir, "second review")

	fetch := func(skills ...string) (*Fetched, error) {
		spec, err := source.Parse(url)
		if err != nil {
			t.Fatal(err)
		}
		spec.Skills = skills
		return e.svc.Fetch(context.Background(), spec)
	}

	_, err := fetch("review")
	wantErr(t, err, ErrAmbiguous, "other/review", "pack/review")

	_, err = fetch("nope")
	wantErr(t, err, ErrNotFound, "nope")

	// The filter matches the directory name too, ignoring case.
	f, err := fetch("TDD-DIR")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if len(f.Candidates) != 1 || f.Candidates[0].Name != "tdd" || f.Candidates[0].RelPath != "skills/tdd-dir" {
		t.Fatalf("candidates = %+v", f.Candidates)
	}
	res := e.svc.Install(context.Background(), f, f.Candidates)
	if res[0].Err != nil || res[0].Path != "tdd" {
		t.Fatalf("install = %+v", res[0])
	}
	// Installed under its name, not its source directory name.
	if !exists(filepath.Join(e.libDir("tdd"), "SKILL.md")) {
		t.Error("skill not installed as tdd")
	}

	// Several names, in the order given; a skill named twice is kept once.
	_, err = fetch("tdd", "other/review", "tdd-dir")
	wantErr(t, err, ErrNotFound, "other/review")
	writeSkill(t, filepath.Join(dir, "solo"), "solo", "solo")
	commitAll(t, dir, "solo")
	g, err := fetch("solo", "tdd", "tdd-dir")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Close() }()
	if len(g.Candidates) != 2 || g.Candidates[0].Name != "solo" || g.Candidates[1].Name != "tdd" {
		t.Fatalf("candidates = %+v", g.Candidates)
	}
}

func TestFetchNamedSkillAtAnyDepth(t *testing.T) {
	e := newEnv(t)
	deep := "cmd/app/template/shared/.agents/skills/deep"
	_, url := e.repo("src", map[string]string{"top": "skills/top", "deep": deep})

	// A listing stops at skill.ListDepth.
	f := e.fetch(url)
	_ = f.Close()
	if len(f.Candidates) != 1 || f.Candidates[0].Name != "top" {
		t.Fatalf("listing = %+v", f.Candidates)
	}

	f = e.fetchSkills(url, "deep")
	defer func() { _ = f.Close() }()
	if len(f.Candidates) != 1 || f.Candidates[0].RelPath != deep {
		t.Fatalf("candidates = %+v", f.Candidates)
	}
}

func TestFetchPrefersPlace(t *testing.T) {
	e := newEnv(t)
	dir := e.path("src")
	gitInit(t, dir)
	for name, rels := range map[string][]string{
		"a": {"skills/a", ".agents/skills/a"},
		"b": {".agents/skills/b", ".claude/skills/b", "plugin/skills/b", "tests/w/.agents/skills/b"},
		"c": {".claude/skills/c", "plugin/skills/c"},
		"d": {".claude/skills/d", ".cursor/skills/d", "plugin/skills/d"},
	} {
		for _, rel := range rels {
			writeSkill(t, filepath.Join(dir, filepath.FromSlash(rel)), name, rel)
		}
	}
	commitAll(t, dir, "init")
	url := fileURL(dir)

	for name, want := range map[string]string{"a": "skills/a", "b": ".agents/skills/b", "c": ".claude/skills/c"} {
		f := e.fetchSkills(url, name)
		_ = f.Close()
		if len(f.Candidates) != 1 || f.Candidates[0].RelPath != want {
			t.Errorf("%s: candidates = %+v, want %s", name, f.Candidates, want)
		}
	}

	// Only the matches at the most preferred place are listed.
	spec, err := source.Parse(url)
	if err != nil {
		t.Fatal(err)
	}
	spec.Skills = []string{"d"}
	_, err = e.svc.Fetch(context.Background(), spec)
	wantErr(t, err, ErrAmbiguous, ".claude/skills/d, .cursor/skills/d")
	if strings.Contains(err.Error(), "plugin") {
		t.Errorf("error %q lists a less preferred match", err)
	}

	f := e.fetch(url)
	_ = f.Close()
	var rels []string
	for _, c := range f.Candidates {
		rels = append(rels, c.RelPath)
	}
	if want := []string{".agents/skills/b", ".claude/skills/c", ".claude/skills/d", ".cursor/skills/d", "skills/a"}; !slices.Equal(rels, want) {
		t.Errorf("candidates = %v, want %v", rels, want)
	}
}

func TestFetchInvalidRootSkill(t *testing.T) {
	e := newEnv(t)
	dir := e.path("tool")
	gitInit(t, dir)
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: tool\n---\n")
	commitAll(t, dir, "init")

	// Named after the repository, so a name filter finds it and the install
	// gives the reason.
	f := e.fetchSkills(fileURL(dir), "tool")
	defer func() { _ = f.Close() }()
	if len(f.Candidates) != 1 || f.Candidates[0].Name != "tool" || f.Candidates[0].Issue == "" {
		t.Fatalf("candidates = %+v", f.Candidates)
	}
	res := e.svc.Install(context.Background(), f, f.Candidates)
	if err := res[0].Err; err == nil || !strings.Contains(err.Error(), `missing required field "description"`) {
		t.Fatalf("install err = %v", err)
	}
}

func TestFetchSubdir(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo("src", map[string]string{"a": "one/a", "b": "two/b"})
	f := e.fetch(url)
	_ = f.Close()
	if len(f.Candidates) != 2 {
		t.Fatalf("candidates = %+v", f.Candidates)
	}

	spec, _ := source.Parse(url)
	spec.Subdir = "two"
	f, err := e.svc.Fetch(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if len(f.Candidates) != 1 || f.Candidates[0].RelPath != "two/b" {
		t.Fatalf("candidates = %+v", f.Candidates)
	}

	spec.Subdir = "missing"
	_, err = e.svc.Fetch(context.Background(), spec)
	wantErr(t, err, ErrNotFound, `has no folder "missing"`)

	writeFile(t, filepath.Join(e.path("src"), "docs", "guide.md"), "guide")
	commitAll(t, e.path("src"), "docs")
	spec.Subdir = "docs"
	_, err = e.svc.Fetch(context.Background(), spec)
	wantErr(t, err, ErrNotFound, "no skills found")
}

func TestGitSourceChecksOutWhatItReads(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git checks out symlinks as plain files on Windows")
	}
	e := newEnv(t)
	dir, url := e.repo("src", map[string]string{"a": "skills/a", "b": "skills/b"})
	writeFile(t, filepath.Join(dir, "skills", "a", "assets", "big.txt"), "a asset")
	writeFile(t, filepath.Join(dir, "skills", "b", "big.txt"), "b asset")
	writeFile(t, filepath.Join(dir, "shared", "notes.md"), "notes v1")
	symlink(t, "../../shared", filepath.Join(dir, "skills", "a", "shared"))
	// ".." after a symlink leaves the folder the symlink points to.
	writeFile(t, filepath.Join(dir, "lib", "data.md"), "data")
	writeFile(t, filepath.Join(dir, "lib", "nested", "other.md"), "other")
	symlink(t, "../lib/nested", filepath.Join(dir, "aliases", "up"))
	symlink(t, "../../aliases/up/../data.md", filepath.Join(dir, "skills", "a", "data.md"))
	// A SKILL.md kept elsewhere, and the skills seen through a symlink.
	writeFile(t, filepath.Join(dir, "docs", "c.md"), "---\nname: c\ndescription: The c skill.\n---\n")
	symlink(t, "../../docs/c.md", filepath.Join(dir, "skills", "c", "SKILL.md"))
	symlink(t, "../skills", filepath.Join(dir, ".claude", "skills"))
	commitAll(t, dir, "more")

	f := e.fetch(url)
	defer func() { _ = f.Close() }()
	var rels []string
	for _, c := range f.Candidates {
		rels = append(rels, c.RelPath)
	}
	if !slices.Equal(rels, []string{"skills/a", "skills/b", "skills/c"}) {
		t.Fatalf("candidates = %+v", f.Candidates)
	}
	// Listing reads the SKILL.md files only.
	for _, p := range []string{"skills/a/assets/big.txt", "skills/b/big.txt", "shared/notes.md"} {
		if exists(filepath.Join(f.Root, p)) {
			t.Errorf("%s checked out by the listing", p)
		}
	}

	picks := []Candidate{f.Candidates[0], f.Candidates[2]}
	for _, r := range e.svc.Install(context.Background(), f, picks) {
		if r.Err != nil {
			t.Fatalf("install %s: %v", r.Candidate.Name, r.Err)
		}
	}
	if got := readFile(t, filepath.Join(e.libDir("a"), "assets", "big.txt")); got != "a asset" {
		t.Errorf("a/assets/big.txt = %q", got)
	}
	if got := readFile(t, filepath.Join(e.libDir("a"), "shared", "notes.md")); got != "notes v1" {
		t.Errorf("a/shared/notes.md = %q", got)
	}
	if got := readFile(t, filepath.Join(e.libDir("a"), "data.md")); got != "data" {
		t.Errorf("a/data.md = %q", got)
	}
	if sk := e.skill("c"); sk.Issue != "" || isSymlink(filepath.Join(sk.Dir, "SKILL.md")) {
		t.Errorf("c = %+v", sk)
	}
	for _, p := range []string{"skills/b/big.txt", "lib/nested/other.md"} {
		if exists(filepath.Join(f.Root, p)) {
			t.Errorf("installing a and c checked out %s", p)
		}
	}

	// A folder named through a symlink.
	spec, _ := source.Parse(url)
	spec.Subdir = ".claude/skills"
	g, err := e.svc.Fetch(context.Background(), spec)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = g.Close() }()
	if len(g.Candidates) != 3 || g.Candidates[0].RelPath != "skills/a" {
		t.Errorf("candidates through the symlink = %+v", g.Candidates)
	}

	// A check reads what the skill's symlinks point to.
	writeFile(t, filepath.Join(dir, "shared", "notes.md"), "notes v2")
	commitAll(t, dir, "notes v2")
	if u := e.check("a"); u.Checks[0].State != Available {
		t.Fatalf("check = %+v", u.Checks[0])
	} else if res := e.svc.ApplyUpdates(u, nil, false); res[0].Err != nil {
		t.Fatal(res[0].Err)
	}
	if got := readFile(t, filepath.Join(e.libDir("a"), "shared", "notes.md")); got != "notes v2" {
		t.Errorf("updated a/shared/notes.md = %q", got)
	}
}

func TestInstallRetriesAFailedDownload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("git checks out symlinks as plain files on Windows")
	}
	e := newEnv(t)
	dir, url := e.repo("src", map[string]string{"a": "skills/a"})
	writeFile(t, filepath.Join(dir, "shared", "notes.md"), "notes")
	symlink(t, "../../shared", filepath.Join(dir, "skills", "a", "shared"))
	commitAll(t, dir, "shared")
	f := e.fetch(url)
	defer func() { _ = f.Close() }()

	// The listing downloaded the whole of a; what its symlink points to is
	// downloaded on install, which fails while the source is gone.
	if err := os.Rename(dir, dir+"-gone"); err != nil {
		t.Fatal(err)
	}
	if res := e.svc.Install(context.Background(), f, f.Candidates); res[0].Err == nil {
		t.Fatal("install succeeded without its source")
	}
	if err := os.Rename(dir+"-gone", dir); err != nil {
		t.Fatal(err)
	}
	if res := e.svc.Install(context.Background(), f, f.Candidates); res[0].Err != nil {
		t.Fatal(res[0].Err)
	}
	if got := readFile(t, filepath.Join(e.libDir("a"), "shared", "notes.md")); got != "notes" {
		t.Errorf("a/shared/notes.md = %q", got)
	}
}

func TestInstallSameSourceIsAlready(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo("src", map[string]string{"alpha": "alpha"})
	e.add(url)
	before := e.lock().Skills["alpha"]

	f := e.fetch(url)
	defer func() { _ = f.Close() }()
	if c := f.Candidates[0]; c.Installed != "alpha" || c.Conflict != "" {
		t.Fatalf("candidate = %+v, want Installed alpha", c)
	}
	res := e.svc.Install(context.Background(), f, f.Candidates)
	if r := res[0]; r.Err != nil || !r.Already || r.Path != "alpha" {
		t.Fatalf("install = %+v, want Already", r)
	}
	if after := e.lock().Skills["alpha"]; after != before {
		t.Errorf("record changed: %+v → %+v", before, after)
	}
}

func TestInstallNameConflict(t *testing.T) {
	e := newEnv(t)
	_, url1 := e.repo("one", map[string]string{"alpha": "alpha"})
	_, url2 := e.repo("two", map[string]string{"alpha": "skills/alpha"})
	e.add(url1)
	content := readFile(t, filepath.Join(e.libDir("alpha"), "SKILL.md"))

	f := e.fetch(url2)
	defer func() { _ = f.Close() }()
	if c := f.Candidates[0]; c.Conflict != "alpha" || c.Installed != "" {
		t.Fatalf("candidate = %+v, want Conflict alpha", c)
	}
	res := e.svc.Install(context.Background(), f, f.Candidates)
	wantErr(t, res[0].Err, ErrConflict, "alpha")
	if rec := e.lock().Skills["alpha"]; rec.Source.URL != url1 {
		t.Errorf("record source = %s, want %s", rec.Source.URL, url1)
	}
	if got := readFile(t, filepath.Join(e.libDir("alpha"), "SKILL.md")); got != content {
		t.Error("library skill changed")
	}
}

func TestInstallLocalFolder(t *testing.T) {
	e := newEnv(t)
	dir := e.path("dev", "tools")
	writeSkill(t, filepath.Join(dir, "my-tool"), "my-tool", "v1")

	e.add(dir)
	sk := e.skill("my-tool")
	want := store.Source{Type: "local", Path: filepath.Join(dir, "my-tool")}
	if sk.Record == nil || sk.Record.Source != want || sk.Record.Commit != "" {
		t.Fatalf("record = %+v", sk.Record)
	}
	if got := sk.SourceLabel(); got != "local" {
		t.Errorf("SourceLabel = %q", got)
	}

	f := e.fetch(dir)
	defer func() { _ = f.Close() }()
	if f.Candidates[0].Installed != "my-tool" {
		t.Errorf("candidate = %+v, want Installed", f.Candidates[0])
	}

	// A folder inside the library is refused.
	_, err := e.svc.Fetch(context.Background(), source.Spec{Kind: source.Local, Path: e.libDir("my-tool")})
	if err == nil || !strings.Contains(err.Error(), "inside the library") {
		t.Errorf("fetch inside library: %v", err)
	}
	_, err = e.svc.Fetch(context.Background(), source.Spec{Kind: source.Local, Path: e.path("missing")})
	wantErr(t, err, ErrNotFound, "does not exist")
}

func TestLinkLocalCollection(t *testing.T) {
	e := newEnv(t)
	coll := e.path("dev", "agent-skills")
	writeSkill(t, filepath.Join(coll, "one"), "one", "")
	writeSkill(t, filepath.Join(coll, "group", "two"), "two", "")
	writeSkill(t, filepath.Join(coll, ".hidden", "three"), "three", "")
	writeFile(t, filepath.Join(coll, "notes.txt"), "notes")
	symlink(t, coll, filepath.Join(coll, "group", "loop")) // a cycle

	name, err := e.svc.LinkLocal(coll)
	if err != nil {
		t.Fatal(err)
	}
	if name != "agent-skills" || readlink(t, e.libDir(name)) != coll {
		t.Fatalf("entry %s → %s", name, readlink(t, e.libDir(name)))
	}

	lib, err := e.svc.Library()
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, sk := range lib {
		paths = append(paths, sk.Path)
		if !sk.Linked || sk.Entry != "agent-skills" || sk.LinkTarget != coll || sk.Record != nil || !sk.Valid() || sk.Updatable() {
			t.Errorf("skill %+v", sk)
		}
		if sk.SourceLabel() != coll {
			t.Errorf("SourceLabel = %q", sk.SourceLabel())
		}
	}
	if !slices.Equal(paths, []string{"agent-skills/one", "agent-skills/group/two"}) {
		t.Fatalf("library = %v", paths)
	}

	// Links point through the library entry, not at the resolved folder.
	sc := e.project("proj", true)
	e.enable(sc, "one", EnableOptions{Agents: []string{"universal"}})
	link := filepath.Join(sc.Root, ".agents", "skills", "one")
	if got := readlink(t, link); got != e.libDir("agent-skills/one") {
		t.Errorf("link target = %s", got)
	}

	_, err = e.svc.LinkLocal(coll)
	wantErr(t, err, ErrConflict, "agent-skills")
	_, err = e.svc.LinkLocal(e.libDir("agent-skills/one"))
	if err == nil {
		t.Error("linking a folder inside the library succeeded")
	}
	empty := e.path("empty")
	mkdirAll(t, empty)
	_, err = e.svc.LinkLocal(empty)
	wantErr(t, err, ErrNotFound)

	// Removing a skill of a linked entry removes the entry, never the files.
	res, err := e.svc.Remove(e.skill("two"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry != "agent-skills" || !slices.Equal(res.Skills, []string{"agent-skills/one", "agent-skills/group/two"}) {
		t.Errorf("remove = %+v", res)
	}
	if !slices.Equal(res.Unlinked, []string{link}) {
		t.Errorf("Unlinked = %v, want %v", res.Unlinked, link)
	}
	if exists(e.libDir("agent-skills")) || exists(link) {
		t.Error("entry or placement still there")
	}
	for _, p := range []string{"one/SKILL.md", "group/two/SKILL.md", "notes.txt"} {
		if !exists(filepath.Join(coll, p)) {
			t.Errorf("linked folder lost %s", p)
		}
	}
}

func TestLibraryIssues(t *testing.T) {
	e := newEnv(t)
	writeSkill(t, e.libDir("good"), "good", "")
	writeSkill(t, e.libDir("mismatch"), "other-name", "")
	writeFile(t, filepath.Join(e.libDir("bad-yaml"), "SKILL.md"), "---\nname: [x\n---\n")
	mkdirAll(t, e.libDir("empty"))
	symlink(t, e.path("missing"), e.libDir("gone"))
	writeFile(t, e.libDir("stray.txt"), "x")
	writeSkill(t, filepath.Join(e.libDir(".incoming-123"), "half"), "half", "")

	lib, err := e.svc.Library()
	if err != nil {
		t.Fatal(err)
	}
	issues := map[string]string{}
	for _, sk := range lib {
		issues[sk.Path] = sk.Issue
	}
	want := map[string]string{
		"good":     "",
		"mismatch": "does not match directory name",
		"bad-yaml": "frontmatter",
		"empty":    "no SKILL.md",
		"gone":     "linked folder is missing: " + e.path("missing"),
	}
	if len(issues) != len(want) {
		t.Errorf("library = %v", issues)
	}
	for p, w := range want {
		got, ok := issues[p]
		if !ok || (w == "") != (got == "") || !strings.Contains(got, w) {
			t.Errorf("%s: issue %q, want %q", p, got, w)
		}
	}

	// A missing linked folder can still be removed: only the link goes.
	gone := lib[slices.IndexFunc(lib, func(sk Skill) bool { return sk.Path == "gone" })]
	if !gone.Linked {
		t.Error("gone is not Linked")
	}
	if _, err := e.svc.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if exists(e.libDir("gone")) {
		t.Error("link not removed")
	}
}

func TestResolve(t *testing.T) {
	e := newEnv(t)
	writeSkill(t, e.libDir("kit/review"), "review", "")
	writeSkill(t, e.libDir("pack/review"), "review", "")
	writeSkill(t, e.libDir("pack/tdd"), "tdd", "")
	writeSkill(t, e.libDir("tdd-dir"), "tdd-dir", "")

	_, err := e.svc.Resolve("review")
	wantErr(t, err, ErrAmbiguous, "kit/review, pack/review")
	// An exact library path wins over names.
	if sk := e.skill("kit/review/"); sk.Path != "kit/review" {
		t.Errorf("Resolve(kit/review/) = %+v", sk)
	}
	if sk := e.skill("pack/review"); sk.Path != "pack/review" || sk.Entry != "pack" || sk.Linked {
		t.Errorf("Resolve(pack/review) = %+v", sk)
	}
	if sk := e.skill("tdd"); sk.Path != "pack/tdd" {
		t.Errorf("Resolve(tdd) = %+v", sk)
	}
	_, err = e.svc.Resolve("nope")
	wantErr(t, err, ErrNotFound, "nope")

	// Removing a skill of a real collection removes only that skill, then
	// the collection once it is empty.
	res, err := e.svc.Remove(e.skill("pack/review"))
	if err != nil || res.Entry != "pack/review" || !slices.Equal(res.Skills, []string{"pack/review"}) {
		t.Fatalf("remove = %+v, %v", res, err)
	}
	if !exists(e.libDir("pack/tdd")) {
		t.Fatal("sibling removed")
	}
	if _, err := e.svc.Remove(e.skill("tdd")); err != nil {
		t.Fatal(err)
	}
	if exists(e.libDir("pack")) {
		t.Error("empty collection kept")
	}
	_, err = e.svc.Remove(Skill{Path: "tdd"})
	wantErr(t, err, ErrNotFound)
}

func TestRemoveNeverFollowsLinkInsideCollection(t *testing.T) {
	e := newEnv(t)
	outside := e.path("outside", "shared")
	writeSkill(t, outside, "shared", "")
	symlink(t, outside, e.libDir("pack/shared"))

	res, err := e.svc.Remove(e.skill("shared"))
	if err != nil {
		t.Fatal(err)
	}
	if res.Entry != "pack/shared" || exists(e.libDir("pack/shared")) {
		t.Errorf("remove = %+v", res)
	}
	if !exists(filepath.Join(outside, "SKILL.md")) {
		t.Error("files behind the link were deleted")
	}
}

func TestRemoveCleansPlacementsInAllScopes(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo("src", map[string]string{"alpha": "alpha", "beta": "beta"})
	e.add(url)
	p1 := e.project("p1", true)
	p2 := e.project("p2", false)
	e.enable(Global, "alpha", EnableOptions{})
	e.enable(p1, "alpha", EnableOptions{})
	e.enable(p1, "beta", EnableOptions{})
	e.enable(p2, "alpha", EnableOptions{Mode: Copy})
	if !strings.Contains(exclude(t, p1.Root), "/.agents/skills/alpha") {
		t.Fatal("exclude block missing alpha")
	}

	res, err := e.svc.Remove(e.skill("alpha"))
	if err != nil {
		t.Fatal(err)
	}
	wantUnlinked := []string{
		filepath.Join(e.home, ".agents", "skills", "alpha"),
		filepath.Join(e.home, ".claude", "skills", "alpha"),
		filepath.Join(p1.Root, ".agents", "skills", "alpha"),
		filepath.Join(p1.Root, ".claude", "skills", "alpha"),
	}
	wantReleased := []string{
		filepath.Join(p2.Root, ".agents", "skills", "alpha"),
		filepath.Join(p2.Root, ".claude", "skills", "alpha"),
	}
	if !slices.Equal(res.Unlinked, wantUnlinked) || !slices.Equal(res.Released, wantReleased) {
		t.Errorf("Unlinked = %v\nReleased = %v", res.Unlinked, res.Released)
	}
	for _, p := range wantUnlinked {
		if exists(p) {
			t.Errorf("%s still there", p)
		}
	}
	for _, p := range wantReleased {
		if !isRealDir(p) {
			t.Errorf("copy %s deleted", p)
		}
	}
	lock := e.lock()
	if _, ok := lock.Skills["alpha"]; ok || len(lock.Copies) != 0 {
		t.Errorf("records left: %+v %+v", lock.Skills, lock.Copies)
	}
	// p1 still holds beta; p2 holds only released copies now.
	if !slices.Equal(lock.Projects, []string{p1.Root}) {
		t.Errorf("projects = %v", lock.Projects)
	}
	ex := exclude(t, p1.Root)
	if strings.Contains(ex, "alpha") || !strings.Contains(ex, "/.agents/skills/beta") {
		t.Errorf("exclude = %q", ex)
	}
	if st := e.status(p2); len(st.Others) != 2 || len(st.Enablements) != 0 {
		t.Errorf("p2 status = %+v", st)
	}
}

func TestProjectScope(t *testing.T) {
	e := newEnv(t)
	_, err := e.svc.ProjectScope(e.home)
	wantErr(t, err, ErrNoProject, "home")
	_, err = e.svc.ProjectScope(string(filepath.Separator))
	wantErr(t, err, ErrNoProject)

	// The home directory seen through a symlink is still the home directory.
	symlink(t, e.home, e.path("home-link"))
	_, err = e.svc.ProjectScope(e.path("home-link"))
	wantErr(t, err, ErrNoProject)

	sc := e.project("repo", true)
	sub := filepath.Join(sc.Root, "a", "b")
	mkdirAll(t, sub)
	got, err := e.svc.ProjectScope(sub)
	if err != nil || got.Root != e.path("repo") {
		t.Errorf("ProjectScope(sub) = %v, %v", got, err)
	}

	plain := e.path("plain")
	mkdirAll(t, plain)
	if got, err := e.svc.ProjectScope(plain); err != nil || got.Root != plain {
		t.Errorf("ProjectScope(plain) = %v, %v", got, err)
	}
	if _, err := e.svc.ProjectScope(e.path("missing")); err == nil {
		t.Error("missing dir accepted")
	}

	if Global.String() != "global" || (Scope{Root: filepath.Join(e.home, "p")}).String() != filepath.Join("~", "p") {
		t.Errorf("String: %s %s", Global, Scope{Root: filepath.Join(e.home, "p")})
	}
}

func TestInstalled(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo("src", map[string]string{"alpha": "skills/alpha"})
	e.add(url)
	lib, err := e.svc.Library()
	if err != nil {
		t.Fatal(err)
	}
	if sk, ok := Installed(lib, url, "alpha"); !ok || sk.Path != "alpha" {
		t.Errorf("Installed(%s) = %+v, %v", url, sk, ok)
	}
	if _, ok := Installed(lib, url, "beta"); ok {
		t.Error("Installed found beta")
	}
	upper := url[:len(url)-3] + strings.ToUpper(url[len(url)-3:])
	for _, other := range []string{url + ".git", upper, "file:///elsewhere"} {
		if _, ok := Installed(lib, other, "alpha"); ok {
			t.Errorf("Installed matched %s", other)
		}
	}
}

func TestSameURL(t *testing.T) {
	parsed := func(spec string) string {
		t.Helper()
		sp, err := source.Parse(spec)
		if err != nil {
			t.Fatal(err)
		}
		return sp.URL
	}
	// Records and skills.sh results use this form.
	github := "https://github.com/Owner/Repo.git"
	for _, u := range []string{
		parsed("owner/repo"),
		parsed("OWNER/repo@tdd"),
		parsed("https://github.com/owner/repo/tree/main/skills"),
		parsed("www.github.com/owner/repo.git"),
		"https://github.com/owner/repo/",
		"HTTPS://GitHub.com/owner/repo",
	} {
		if !sameURL(github, u) {
			t.Errorf("sameURL(%s, %s) = false", github, u)
		}
	}
	for _, pair := range [][2]string{
		{"file:///x/source", "file:///x/source.git"},
		{"file:///x/Source", "file:///x/source"},
		{"https://gitlab.com/g/r.git", "https://gitlab.com/G/R.git"},
		{"git@github.com:owner/repo.git", github},
		{"https://github.com/owner/other.git", github},
	} {
		if sameURL(pair[0], pair[1]) {
			t.Errorf("sameURL(%s, %s) = true", pair[0], pair[1])
		}
	}
}

func TestLibraryModified(t *testing.T) {
	e := newEnv(t)
	_, url := e.repo("src", map[string]string{"alpha": "alpha"})
	e.add(url)
	writeFile(t, filepath.Join(e.libDir("alpha"), "notes.md"), "mine")
	if sk := e.skill("alpha"); !sk.Modified {
		t.Error("local change not detected")
	}
	if err := os.Remove(filepath.Join(e.libDir("alpha"), "notes.md")); err != nil {
		t.Fatal(err)
	}
	if sk := e.skill("alpha"); sk.Modified {
		t.Error("reverted change still modified")
	}
}

func TestInstallCopyFailureLeavesNothing(t *testing.T) {
	e := newEnv(t)
	dir := e.path("dev", "tools")
	writeSkill(t, filepath.Join(dir, "leaky"), "leaky", "")
	writeFile(t, e.path("secret.txt"), "secret")
	symlink(t, e.path("secret.txt"), filepath.Join(dir, "leaky", "secret.txt"))

	f := e.fetch(dir)
	defer func() { _ = f.Close() }()
	res := e.svc.Install(context.Background(), f, f.Candidates)
	if res[0].Err == nil || !strings.Contains(res[0].Err.Error(), "outside the source") {
		t.Fatalf("install = %+v", res[0])
	}
	if exists(e.libDir("leaky")) || len(e.lock().Skills) != 0 {
		t.Error("failed install left a skill or record")
	}
	if l := leftovers(t, e.svc.paths.Skills); len(l) > 0 {
		t.Errorf("leftovers: %v", l)
	}
}

func TestInstallSourceWithAgentCopies(t *testing.T) {
	e := newEnv(t)
	dir := e.path("dev", "multi")
	writeSkill(t, filepath.Join(dir, "skills", "foo"), "foo", "")
	writeSkill(t, filepath.Join(dir, ".claude", "skills", "foo"), "foo", "")

	e.add(dir)
	if got := e.skill("foo").Record.Source.Path; got != filepath.Join(dir, "skills", "foo") {
		t.Errorf("installed from %s", got)
	}
}

func TestDuplicateNames(t *testing.T) {
	lib := []Skill{{Name: "review", Path: "a/review"}, {Name: "tdd", Path: "tdd"}, {Name: "review", Path: "b/review"}}
	if got := DuplicateNames(lib); !maps.Equal(got, map[string]bool{"review": true}) {
		t.Errorf("DuplicateNames = %v", got)
	}
}
