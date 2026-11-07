package skill

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func readLink(t *testing.T, path string) string {
	t.Helper()
	target, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return // Windows keeps no permission bits
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s: mode %v, want %v", path, got, want)
	}
}

func assertNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("%s exists (err %v)", path, err)
	}
}

func TestCopyFiles(t *testing.T) {
	src := filepath.Join(tempDir(t), "skill")
	writeSkill(t, src)
	writeFile(t, filepath.Join(src, "scripts", "run.sh"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(src, "scripts", "run.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(src, "private"), "secret")
	if err := os.Chmod(filepath.Join(src, "private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(src, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(src, ".git", "HEAD"), "ref\n")
	writeFile(t, filepath.Join(src, ".DS_Store"), "x")
	writeFile(t, filepath.Join(src, "scripts", ".DS_Store"), "x")

	dst := filepath.Join(tempDir(t), "skill")
	if err := Copy(src, dst, filepath.Dir(src)); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dst, "scripts", "run.sh")); got != "#!/bin/sh\n" {
		t.Errorf("run.sh: %q", got)
	}
	assertMode(t, filepath.Join(dst, "scripts", "run.sh"), 0o755)
	assertMode(t, filepath.Join(dst, "private"), 0o600)
	if info, err := os.Stat(filepath.Join(dst, "empty")); err != nil || !info.IsDir() {
		t.Errorf("empty dir not copied: %v", err)
	}
	assertNotExist(t, filepath.Join(dst, ".git"))
	assertNotExist(t, filepath.Join(dst, ".DS_Store"))
	assertNotExist(t, filepath.Join(dst, "scripts", ".DS_Store"))
	if mustHash(t, src) != mustHash(t, dst) {
		t.Error("copy hashes differently")
	}
}

func TestCopyInternalLinks(t *testing.T) {
	root := tempDir(t)
	realDir := filepath.Join(root, "real-skill")
	writeSkill(t, realDir)
	writeFile(t, filepath.Join(realDir, "docs", "guide.md"), "guide")
	symlink(t, "docs/guide.md", filepath.Join(realDir, "rel"))
	symlink(t, filepath.Join(realDir, "docs", "guide.md"), filepath.Join(realDir, "abs"))
	symlink(t, "../SKILL.md", filepath.Join(realDir, "docs", "up"))
	symlink(t, filepath.Join(realDir, "docs"), filepath.Join(realDir, "nested", "docs"))
	symlink(t, ".", filepath.Join(realDir, "self"))
	symlink(t, "rel", filepath.Join(realDir, "chain")) // link to a link: resolved fully

	// src is reached through a symlink, and an absolute link goes through it too.
	src := filepath.Join(root, "skill")
	symlink(t, realDir, src)
	symlink(t, filepath.Join(src, "SKILL.md"), filepath.Join(realDir, "via-link"))

	dst := filepath.Join(tempDir(t), "out")
	if err := Copy(src, dst, root); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		"rel":         "docs/guide.md",
		"abs":         "docs/guide.md",
		"docs/up":     "../SKILL.md",
		"nested/docs": "../docs",
		"self":        ".",
		"chain":       "docs/guide.md",
		"via-link":    "SKILL.md",
	}
	for rel, want := range links {
		if got := readLink(t, filepath.Join(dst, filepath.FromSlash(rel))); got != filepath.FromSlash(want) {
			t.Errorf("%s -> %q, want %q", rel, got, want)
		}
	}
	if got := readFile(t, filepath.Join(dst, "nested", "docs", "guide.md")); got != "guide" {
		t.Errorf("link does not resolve in the copy: %q", got)
	}
}

func TestCopyDereferencesWithinBoundary(t *testing.T) {
	repo := tempDir(t)
	src := filepath.Join(repo, "skills", "tdd")
	writeSkill(t, src)
	writeFile(t, filepath.Join(repo, "shared", "style.md"), "style")
	writeFile(t, filepath.Join(repo, "shared", "lib", "tool.sh"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(repo, "shared", "lib", "tool.sh"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(repo, "shared", "lib", ".DS_Store"), "x")
	writeFile(t, filepath.Join(repo, "shared", "other.md"), "other")
	// Links inside the dereferenced dir: back into the skill, and elsewhere in the repo.
	symlink(t, "../../skills/tdd/SKILL.md", filepath.Join(repo, "shared", "lib", "skill"))
	symlink(t, "../other.md", filepath.Join(repo, "shared", "lib", "other"))

	symlink(t, "../../shared/style.md", filepath.Join(src, "style.md"))
	symlink(t, filepath.Join(repo, "shared", "lib"), filepath.Join(src, "lib"))

	dst := filepath.Join(tempDir(t), "tdd")
	if err := Copy(src, dst, repo); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"style.md", "lib", "lib/tool.sh", "lib/other"} {
		info, err := os.Lstat(filepath.Join(dst, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Errorf("%s is still a symlink", rel)
		}
	}
	if got := readFile(t, filepath.Join(dst, "style.md")); got != "style" {
		t.Errorf("style.md: %q", got)
	}
	if got := readFile(t, filepath.Join(dst, "lib", "other")); got != "other" {
		t.Errorf("lib/other: %q", got)
	}
	assertMode(t, filepath.Join(dst, "lib", "tool.sh"), 0o755)
	assertNotExist(t, filepath.Join(dst, "lib", ".DS_Store"))
	if got := readLink(t, filepath.Join(dst, "lib", "skill")); got != filepath.FromSlash("../SKILL.md") {
		t.Errorf("lib/skill -> %q, want ../SKILL.md", got)
	}

	// CopyHash is the hash of the copy, and follows the dereferenced content.
	copyHash := func() string {
		t.Helper()
		h, err := CopyHash(src, repo)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	if got := copyHash(); got != mustHash(t, dst) {
		t.Errorf("CopyHash = %s, the copy hashes %s", got, mustHash(t, dst))
	}
	before, srcHash := copyHash(), mustHash(t, src)
	writeFile(t, filepath.Join(repo, "shared", "style.md"), "changed")
	if copyHash() == before {
		t.Error("CopyHash ignores a change behind a dereferenced link")
	}
	if mustHash(t, src) != srcHash {
		t.Error("Hash follows a link it should not")
	}
}

func TestCopyFailures(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, repo, src string)
		wantErr string
	}{
		{"outside boundary relative", func(t *testing.T, repo, src string) {
			symlink(t, "../../../elsewhere", filepath.Join(src, "refs"))
		}, "tdd/refs: symlink points outside the source"},
		{"outside boundary absolute", func(t *testing.T, repo, src string) {
			outside := tempDir(t)
			writeFile(t, filepath.Join(outside, "f"), "x")
			symlink(t, filepath.Join(outside, "f"), filepath.Join(src, "docs", "f"))
		}, "tdd/docs/f: symlink points outside the source"},
		{"broken", func(t *testing.T, repo, src string) {
			symlink(t, "missing", filepath.Join(src, "a", "b"))
		}, "tdd/a/b: broken symlink"},
		{"outside boundary inside dereferenced dir", func(t *testing.T, repo, src string) {
			symlink(t, "/", filepath.Join(repo, "shared", "root"))
			symlink(t, "../../shared", filepath.Join(src, "shared"))
		}, "tdd/shared/root: symlink points outside the source"},
		{"cycle in dereferenced dir", func(t *testing.T, repo, src string) {
			symlink(t, ".", filepath.Join(repo, "shared", "loop"))
			symlink(t, "../../shared", filepath.Join(src, "shared"))
		}, "tdd/shared/loop: symlink cycle"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := tempDir(t)
			writeFile(t, filepath.Join(base, "elsewhere"), "x")
			repo := filepath.Join(base, "repo")
			src := filepath.Join(repo, "skills", "tdd")
			writeSkill(t, src)
			writeFile(t, filepath.Join(src, "docs", "a.md"), "a")
			writeFile(t, filepath.Join(repo, "shared", "x.md"), "x")
			tt.setup(t, repo, src)

			dst := filepath.Join(tempDir(t), "tdd")
			err := Copy(src, dst, repo)
			if err == nil || err.Error() != tt.wantErr {
				t.Errorf("error %v, want %q", err, tt.wantErr)
			}
			assertNotExist(t, dst)
		})
	}
}

func TestCopyDestination(t *testing.T) {
	src := filepath.Join(tempDir(t), "skill")
	writeSkill(t, src)

	// An existing destination is an error and is left alone.
	dst := filepath.Join(tempDir(t), "skill")
	writeFile(t, filepath.Join(dst, "keep"), "keep")
	if err := Copy(src, dst, src); !os.IsExist(err) {
		t.Errorf("existing dst: got %v, want exist error", err)
	}
	if got := readFile(t, filepath.Join(dst, "keep")); got != "keep" {
		t.Errorf("existing dst modified: %q", got)
	}

	// The parent must exist.
	if err := Copy(src, filepath.Join(tempDir(t), "a", "b"), src); !os.IsNotExist(err) {
		t.Errorf("missing parent: got %v, want not-exist error", err)
	}

	// src must be a directory.
	err := Copy(filepath.Join(src, FileName), filepath.Join(tempDir(t), "x"), src)
	if err == nil || !strings.HasSuffix(err.Error(), "is not a directory") {
		t.Errorf("file src: got %v", err)
	}

	// dst must not be inside src, even through a symlink: the copy would
	// contain itself.
	nested := filepath.Join(src, ".agents", "skills", "skill")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(tempDir(t), "link")
	symlink(t, src, link)
	for _, inside := range []string{nested, filepath.Join(link, "copy")} {
		err := Copy(src, inside, src)
		if err == nil || !strings.Contains(err.Error(), "into itself") {
			t.Errorf("dst %s: got %v", inside, err)
		}
		assertNotExist(t, inside)
	}
}
