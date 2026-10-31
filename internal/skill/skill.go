// Package skill reads, validates, hashes, discovers and copies skill directories.
// It works on the filesystem only and knows nothing about the library or agents.
package skill

// FileName is the file that makes a directory a skill.
const FileName = "SKILL.md"

// Metadata is the validated frontmatter of a skill.
type Metadata struct {
	Name        string
	Description string
}

// Read parses and validates dir/SKILL.md (design §4). The frontmatter name must
// equal the directory name.
func Read(dir string) (Metadata, error) {
	panic("not implemented")
}

// ReadFile parses and validates a SKILL.md file without checking the name of the
// directory that contains it. Used for candidates found in a source, whose
// directory may be renamed on install.
func ReadFile(path string) (Metadata, error) {
	panic("not implemented")
}

// Body returns the markdown of dir/SKILL.md without its frontmatter.
func Body(dir string) (string, error) {
	panic("not implemented")
}

// StripFrontmatter removes a leading frontmatter block from markdown.
func StripFrontmatter(markdown string) string {
	panic("not implemented")
}

// Hash returns the content hash of dir as "v1:<hex sha256>" (design §4).
func Hash(dir string) (string, error) {
	panic("not implemented")
}

// Discover returns the absolute paths of the skill directories under root,
// sorted (design §4).
func Discover(root string) ([]string, error) {
	panic("not implemented")
}

// Copy copies the skill directory src to dst, which must not exist (its parent
// must). boundary is the root of the source the skill came from: symlinks that
// leave src but stay inside boundary are dereferenced; other escaping, absolute
// or broken symlinks fail the copy (design §4). dst is removed on failure.
func Copy(src, dst, boundary string) error {
	panic("not implemented")
}
