package core

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReplaceBlock(t *testing.T) {
	block := func(lines ...string) string {
		return excludeBegin + "\n" + strings.Join(lines, "\n") + "\n" + excludeEnd + "\n"
	}
	tests := []struct {
		name, content string
		patterns      []string
		want          string
	}{
		{"empty file", "", []string{"/a"}, block("/a")},
		{"nothing to do", "x\n", nil, "x\n"},
		{"append", "x\n", []string{"/a"}, "x\n" + block("/a")},
		{"append without final newline", "x", []string{"/a"}, "x\n" + block("/a")},
		{"replace in place", "x\n" + block("/old") + "y\n", []string{"/a", "/b"}, "x\n" + block("/a", "/b") + "y\n"},
		{"remove keeps the rest", "x\n" + block("/old") + "y", nil, "x\ny"},
		{"remove at end", "x\n" + block("/old"), nil, "x\n"},
		{"crlf block", "x\r\n" + excludeBegin + "\r\n/old\r\n" + excludeEnd + "\r\ny\r\n", []string{"/a"}, "x\r\n" + block("/a") + "y\r\n"},
		{"dangling begin is kept", "x\n" + excludeBegin + "\n/old\n", []string{"/a"}, "x\n" + excludeBegin + "\n/old\n" + block("/a")},
		{"dangling begin before a block", "x\n" + excludeBegin + "\n/old\n" + block("/a"), []string{"/b"}, "x\n" + excludeBegin + "\n/old\n" + block("/b")},
		{"stray end", excludeEnd + "\n", []string{"/a"}, excludeEnd + "\n" + block("/a")},
	}
	for _, tt := range tests {
		if got := replaceBlock(tt.content, tt.patterns); got != tt.want {
			t.Errorf("%s: got %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestEscapePattern(t *testing.T) {
	if got, want := escapePattern(`.agents/skills/a*b?c[d]\e f!#`), `.agents/skills/a\*b\?c\[d]\\e\ f!#`; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Hand-made links into the library are excluded too, with their names
// escaped so that git matches them literally.
func TestExcludeEscapesLinkNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip(`Windows file names cannot contain "*"`)
	}
	e := setup(t)
	sc := e.project("proj", true)
	dir := filepath.Join(sc.Root, ".agents", "skills")
	symlink(t, e.libDir("beta"), filepath.Join(dir, "we*rd name"))
	writeFile(t, filepath.Join(dir, "weXrd name", "f"), "unmanaged")
	e.enable(sc, "alpha", EnableOptions{Agents: []string{"universal"}})

	want := excludeBegin + "\n/.agents/skills/alpha\n/.agents/skills/we\\*rd\\ name\n" + excludeEnd + "\n"
	if got := managedBlock(exclude(t, sc.Root)); got != want {
		t.Errorf("block = %q, want %q", got, want)
	}
	// Only the unmanaged folder shows up as untracked.
	if got := runGit(t, sc.Root, "status", "--porcelain", "--untracked-files=all"); got != `?? ".agents/skills/weXrd name/f"` {
		t.Errorf("git status = %q", got)
	}
}

// A project outside git, or global scope, never touches git.
func TestNoExcludeOutsideGit(t *testing.T) {
	e := setup(t)
	sc := e.project("plain", false)
	res := e.enable(sc, "alpha", EnableOptions{})
	if len(res.Warnings) != 0 {
		t.Errorf("warnings = %v", res.Warnings)
	}
	if exists(filepath.Join(sc.Root, ".git")) {
		t.Error("created .git")
	}
}
