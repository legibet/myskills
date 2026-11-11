package core

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/legibet/myskills/internal/git"
	"github.com/legibet/myskills/internal/skill"
)

// checkout is a clone of a git source whose files are checked out as they
// are needed (docs/library.md): listing its skills reads only their SKILL.md files,
// installing or checking a skill only its folder and what its symlinks point
// to.
type checkout struct {
	root  string
	tree  map[string]git.Kind
	links []string // every symlink in the commit

	mu       sync.Mutex      // one git command at a time: installs from a source can overlap
	done     map[string]bool // paths checked out with what their symlinks point to
	linksOut bool            // the symlinks are checked out
}

// clone clones url at ref into dir and returns the checkout and the commit.
func clone(ctx context.Context, url, ref, dir string) (*checkout, string, error) {
	commit, err := git.Clone(ctx, url, ref, dir)
	if err != nil {
		return nil, "", err
	}
	tree, err := git.Tree(ctx, dir)
	if err != nil {
		return nil, "", err
	}
	c := &checkout{root: dir, tree: tree, done: map[string]bool{}}
	for p, kind := range tree {
		if kind == git.Link {
			c.links = append(c.links, p)
		}
	}
	return c, commit, nil
}

// skillFiles returns the paths of every SKILL.md in the commit.
func (c *checkout) skillFiles() []string {
	var files []string
	for p, kind := range c.tree {
		if kind != git.Dir && path.Base(p) == skill.FileName {
			files = append(files, p)
		}
	}
	return files
}

// add checks out paths, files or folders slash-separated from the root ("."
// for all of it), and what the symlinks among them point to elsewhere in the
// repository, which copying a skill dereferences (the copy rules in docs/library.md).
func (c *checkout) add(ctx context.Context, paths []string) (err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var marked []string
	defer func() {
		// A path counts as checked out only with everything it points to, so
		// that a retry after a failed download checks out what is missing.
		if err != nil {
			for _, p := range marked {
				delete(c.done, p)
			}
		}
	}()
	for {
		paths = slices.DeleteFunc(slices.Clone(paths), c.covered)
		if len(paths) == 0 {
			return nil
		}
		// The first batch brings every symlink too: resolving a path reads
		// them, and listing skills follows symlinked folders.
		batch := paths
		if !c.linksOut {
			batch = append(slices.Clone(c.links), paths...)
		}
		if err := git.Checkout(ctx, c.root, batch); err != nil {
			return err
		}
		c.linksOut = true
		for _, p := range paths {
			c.done[p] = true
		}
		marked = append(marked, paths...)
		var targets []string
		for _, link := range c.links {
			if !slices.ContainsFunc(paths, func(p string) bool { return under(link, p) }) {
				continue
			}
			target, via, ok := c.resolve(link)
			if !ok {
				continue
			}
			targets = append(targets, target)
			// Following the link enters these folders only to leave them by
			// "..": they must exist, but nothing in them is needed.
			for _, dir := range via {
				if err := os.MkdirAll(filepath.Join(c.root, filepath.FromSlash(dir)), 0o755); err != nil {
					return err
				}
			}
		}
		paths = targets
	}
}

// covered reports whether p, or a folder above it, is checked out.
func (c *checkout) covered(p string) bool {
	for {
		if c.done[p] {
			return true
		}
		if p == "." {
			return false
		}
		p = path.Dir(p)
	}
}

// maxLinks bounds the symlinks followed resolving one path, as the OS does.
const maxLinks = 40

// resolve follows the symlinks along p, a clean path slash-separated from the
// root, and returns the path in the commit it leads to, and the folders it
// passes through and leaves by ".." on the way; false when that path is
// missing, outside the repository or a symlink loop.
func (c *checkout) resolve(p string) (target string, via []string, ok bool) {
	// resolved never holds a symlink, so ".." just drops its last name.
	resolved, rest := ".", strings.Split(p, "/")
	for followed := 0; len(rest) > 0; {
		name := rest[0]
		rest = rest[1:]
		switch name {
		case "", ".":
			continue
		case "..":
			if resolved == "." {
				return "", nil, false
			}
			via = append(via, resolved)
			resolved = path.Dir(resolved)
			continue
		}
		next := path.Join(resolved, name)
		kind, exists := c.tree[next]
		if !exists {
			return "", nil, false
		}
		if kind != git.Link {
			resolved = next
			continue
		}
		link, err := os.Readlink(filepath.Join(c.root, filepath.FromSlash(next)))
		if followed++; err != nil || followed > maxLinks || filepath.IsAbs(link) {
			return "", nil, false
		}
		// The target is relative to the folder holding the link: resolved.
		rest = append(strings.Split(filepath.ToSlash(link), "/"), rest...)
	}
	return resolved, via, true
}

// under reports whether p is dir or inside it. Both are slash-separated
// paths relative to the same root, a repository or the library, with "."
// for the root itself.
func under(p, dir string) bool {
	return dir == "." || p == dir || strings.HasPrefix(p, dir+"/")
}
