package skill

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

// tempDir returns a symlink-resolved temp dir (macOS temp dirs live behind
// /var -> /private/var).
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// writeSkill creates dir/SKILL.md with a valid frontmatter named after dir.
func writeSkill(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, FileName), "---\nname: "+filepath.Base(dir)+"\ndescription: A skill.\n---\n# Body\n")
}

func TestReadFileValid(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    Metadata
	}{
		{"minimal", "---\nname: tdd\ndescription: Test first.\n---\n# TDD\n", Metadata{Name: "tdd", Description: "Test first."}},
		{"crlf", "---\r\nname: tdd\r\ndescription: Test first.\r\n---\r\n# TDD\r\n", Metadata{Name: "tdd", Description: "Test first."}},
		{"no body", "---\nname: tdd\ndescription: d\n---", Metadata{Name: "tdd", Description: "d"}},
		{"quoted digits", "---\nname: \"123\"\ndescription: \"true\"\n---\n", Metadata{Name: "123", Description: "true"}},
		{"explicit str tag", "---\nname: !!str 42\ndescription: d\n---\n", Metadata{Name: "42", Description: "d"}},
		{"hyphens and digits", "---\nname: a-1-b\ndescription: d\n---\n", Metadata{Name: "a-1-b", Description: "d"}},
		{"64 chars", "---\nname: " + strings.Repeat("a", 64) + "\ndescription: d\n---\n", Metadata{Name: strings.Repeat("a", 64), Description: "d"}},
		{"folded description", "---\nname: tdd\ndescription: >\n  Two\n  lines\n---\n", Metadata{Name: "tdd", Description: "Two lines"}},
		{"description trimmed", "---\nname: tdd\ndescription: \"  d  \"\n---\n", Metadata{Name: "tdd", Description: "d"}},
		{"1024 runes", "---\nname: tdd\ndescription: " + strings.Repeat("é", 1024) + "\n---\n", Metadata{Name: "tdd", Description: strings.Repeat("é", 1024)}},
		{
			"optional fields",
			"---\nname: tdd\ndescription: d\nlicense: MIT\ncompatibility: Requires git\nallowed-tools: Bash(git:*) Read\nmetadata:\n  author: me\n  version: \"1.0\"\n---\n",
			Metadata{
				Name: "tdd", Description: "d", License: "MIT", Compatibility: "Requires git", AllowedTools: "Bash(git:*) Read",
				Metadata: map[string]string{"author": "me", "version": "1.0"},
			},
		},
		{"empty metadata", "---\nname: tdd\ndescription: d\nmetadata: {}\n---\n", Metadata{Name: "tdd", Description: "d"}},
		{"500 char compatibility", "---\nname: tdd\ndescription: d\ncompatibility: " + strings.Repeat("c", 500) + "\n---\n", Metadata{Name: "tdd", Description: "d", Compatibility: strings.Repeat("c", 500)}},
		{"yes is a string", "---\nname: tdd\ndescription: yes\n---\n", Metadata{Name: "tdd", Description: "yes"}},
		{"unknown fields", "---\nname: tdd\ndescription: d\nfoo: [1, 2]\nbar: 3\n---\n", Metadata{Name: "tdd", Description: "d"}},
		{"dashes in body", "---\nname: tdd\ndescription: d\n---\n---\nmore\n", Metadata{Name: "tdd", Description: "d"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			writeFile(t, path, tt.content)
			got, err := ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestReadFileInvalid(t *testing.T) {
	const desc = "description: d\n"
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{"invalid utf-8", "---\nname: tdd\ndescription: \xff\n---\n", "not valid UTF-8"},
		{"no frontmatter", "# TDD\n", `must start with a "---" frontmatter block`},
		{"leading blank line", "\n---\nname: tdd\n" + desc + "---\n", `must start with a "---" frontmatter block`},
		{"trailing space on opener", "--- \nname: tdd\n" + desc + "---\n", `must start with a "---" frontmatter block`},
		{"empty file", "", `must start with a "---" frontmatter block`},
		{"unclosed", "---\nname: tdd\n" + desc, `frontmatter is not closed by a "---" line`},
		{"empty block", "---\n---\n", "frontmatter must be a YAML mapping"},
		{"list", "---\n- a\n- b\n---\n", "frontmatter must be a YAML mapping"},
		{"scalar", "---\nhello\n---\n", "frontmatter must be a YAML mapping"},
		{"bad yaml", "---\nname: [tdd\n---\n", "invalid frontmatter"},
		{"duplicate field", "---\nname: tdd\nname: tdd\n" + desc + "---\n", `duplicate field "name"`},
		{"missing name", "---\n" + desc + "---\n", `missing required field "name"`},
		{"name int", "---\nname: 123\n" + desc + "---\n", "name must be a string, got int"},
		{"name bool", "---\nname: true\n" + desc + "---\n", "name must be a string, got bool"},
		{"name float", "---\nname: 1.5\n" + desc + "---\n", "name must be a string, got float"},
		{"name null", "---\nname:\n" + desc + "---\n", "name must be a string, got null"},
		{"name list", "---\nname: [a]\n" + desc + "---\n", "name must be a string, got list"},
		{"name mapping", "---\nname: {a: b}\n" + desc + "---\n", "name must be a string, got mapping"},
		{"name empty", "---\nname: \"\"\n" + desc + "---\n", "name must not be empty"},
		{"name uppercase", "---\nname: Foo\n" + desc + "---\n", `name "Foo" must be lowercase letters, digits and hyphens`},
		{"name underscore", "---\nname: a_b\n" + desc + "---\n", `name "a_b" must be lowercase letters, digits and hyphens`},
		{"name space", "---\nname: \"a b\"\n" + desc + "---\n", `name "a b" must be lowercase letters, digits and hyphens`},
		{"name non-ascii", "---\nname: café\n" + desc + "---\n", `name "café" must be lowercase letters, digits and hyphens`},
		{"name too long", "---\nname: " + strings.Repeat("a", 65) + "\n" + desc + "---\n", "name must be at most 64 characters, got 65"},
		{"name leading hyphen", "---\nname: -tdd\n" + desc + "---\n", `name "-tdd" must not start or end with a hyphen`},
		{"name trailing hyphen", "---\nname: tdd-\n" + desc + "---\n", `name "tdd-" must not start or end with a hyphen`},
		{"name double hyphen", "---\nname: a--b\n" + desc + "---\n", `name "a--b" must not contain consecutive hyphens`},
		{"missing description", "---\nname: tdd\n---\n", `missing required field "description"`},
		{"description int", "---\nname: tdd\ndescription: 1\n---\n", "description must be a string, got int"},
		{"description empty", "---\nname: tdd\ndescription: \"\"\n---\n", "description must not be empty"},
		{"description blank", "---\nname: tdd\ndescription: \"  \\n \"\n---\n", "description must not be empty"},
		{"description too long", "---\nname: tdd\ndescription: " + strings.Repeat("é", 1025) + "\n---\n", "description must be at most 1024 characters, got 1025"},
		{"license int", "---\nname: tdd\n" + desc + "license: 2\n---\n", "license must be a string, got int"},
		{"license null", "---\nname: tdd\n" + desc + "license:\n---\n", "license must be a string, got null"},
		{"allowed-tools list", "---\nname: tdd\n" + desc + "allowed-tools: [Bash]\n---\n", "allowed-tools must be a string, got list"},
		{"compatibility empty", "---\nname: tdd\n" + desc + "compatibility: \"\"\n---\n", "compatibility must be 1-500 characters, got 0"},
		{"compatibility too long", "---\nname: tdd\n" + desc + "compatibility: " + strings.Repeat("c", 501) + "\n---\n", "compatibility must be 1-500 characters, got 501"},
		{"compatibility bool", "---\nname: tdd\n" + desc + "compatibility: false\n---\n", "compatibility must be a string, got bool"},
		{"metadata list", "---\nname: tdd\n" + desc + "metadata: [a]\n---\n", "metadata must be a mapping, got list"},
		{"metadata string", "---\nname: tdd\n" + desc + "metadata: x\n---\n", "metadata must be a mapping, got str"},
		{"metadata value float", "---\nname: tdd\n" + desc + "metadata:\n  version: 1.0\n---\n", "metadata.version must be a string, got float"},
		{"metadata value mapping", "---\nname: tdd\n" + desc + "metadata:\n  a: {b: c}\n---\n", "metadata.a must be a string, got mapping"},
		{"metadata key int", "---\nname: tdd\n" + desc + "metadata:\n  1: x\n---\n", `metadata key "1" must be a string, got int`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			writeFile(t, path, tt.content)
			_, err := ReadFile(path)
			if err == nil {
				t.Fatalf("no error, want %q", tt.wantErr)
			}
			if want := "SKILL.md: " + tt.wantErr; !strings.HasPrefix(err.Error(), want) {
				t.Errorf("error %q, want prefix %q", err, want)
			}
		})
	}
}

func TestRead(t *testing.T) {
	root := t.TempDir()

	dir := filepath.Join(root, "tdd")
	writeSkill(t, dir)
	got, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "tdd" || got.Description != "A skill." {
		t.Errorf("got %+v", got)
	}

	// The name must match the directory, even through a trailing slash.
	if _, err := Read(dir + "/"); err != nil {
		t.Errorf("trailing slash: %v", err)
	}
	renamed := filepath.Join(root, "other")
	if err := os.Rename(dir, renamed); err != nil {
		t.Fatal(err)
	}
	_, err = Read(renamed)
	if want := `SKILL.md: name "tdd" does not match directory name "other"`; err == nil || err.Error() != want {
		t.Errorf("error %v, want %q", err, want)
	}

	// ReadFile does not check the directory name.
	if _, err := ReadFile(filepath.Join(renamed, FileName)); err != nil {
		t.Errorf("ReadFile: %v", err)
	}

	if _, err := Read(filepath.Join(root, "missing")); !os.IsNotExist(err) {
		t.Errorf("missing SKILL.md: got %v, want not-exist error", err)
	}
}

func TestStripFrontmatter(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"block", "---\nname: x\n---\n# Title\n", "# Title\n"},
		{"leading blank lines", "---\nname: x\n---\n\n  \n\n# Title\n\nText\n", "# Title\n\nText\n"},
		{"keeps indentation", "---\nname: x\n---\n\n    code\n", "    code\n"},
		{"crlf", "---\r\nname: x\r\n---\r\n\r\n# Title\r\n", "# Title\r\n"},
		{"only frontmatter", "---\nname: x\n---\n", ""},
		{"only frontmatter and blanks", "---\nname: x\n---\n\n \n", ""},
		{"no newline after block", "---\nname: x\n---", ""},
		{"empty block", "---\n---\nbody", "body"},
		{"no block", "# Title\n", "# Title\n"},
		{"unclosed block", "---\nname: x\n# Title\n", "---\nname: x\n# Title\n"},
		{"not at start", "\n---\nname: x\n---\nbody\n", "\n---\nname: x\n---\nbody\n"},
		{"later dashes kept", "---\na: b\n---\ntext\n---\nmore\n", "text\n---\nmore\n"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripFrontmatter(tt.in); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBody(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tdd")
	writeSkill(t, dir)
	got, err := Body(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "# Body\n" {
		t.Errorf("got %q", got)
	}
	if _, err := Body(t.TempDir()); err == nil {
		t.Error("no error for a missing SKILL.md")
	}
}
