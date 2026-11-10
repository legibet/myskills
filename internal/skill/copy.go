package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// Copy copies the skill directory src to dst, which must not exist (its parent
// must) and must not lie inside src. boundary is the root of the source the
// skill came from: symlinks that leave src but stay inside boundary are
// dereferenced; other escaping, absolute or broken symlinks fail the copy
// (docs/library.md) before anything is written. dst is removed on failure.
//
// Symlinks that resolve inside src are recreated as relative symlinks. Regular
// files keep their permission bits; .git and .DS_Store are skipped.
func Copy(src, dst, boundary string) error {
	c, err := newCopier(src, boundary)
	if err != nil {
		return err
	}
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	parentReal, err := filepath.EvalSymlinks(filepath.Dir(dstAbs))
	if err != nil {
		return err
	}
	if within(c.src, parentReal) {
		// The copy would contain itself, recursing until paths get too long.
		return fmt.Errorf("can't copy %s into itself", src)
	}
	if err := c.walk(c.src, ""); err != nil {
		return err
	}

	if err := os.Mkdir(dst, 0o755); err != nil {
		return err
	}
	for _, e := range c.entries {
		if err := write(e, filepath.Join(dst, filepath.FromSlash(e.rel))); err != nil {
			_ = os.RemoveAll(dst)
			return err
		}
	}
	return nil
}

// CopyHash returns the content hash of what Copy makes of src, without
// copying: symlinks it dereferences count with the content they point at.
// Permission bits are the source's, before the umask.
func CopyHash(src, boundary string) (string, error) {
	c, err := newCopier(src, boundary)
	if err != nil {
		return "", err
	}
	if err := c.walk(c.src, ""); err != nil {
		return "", err
	}
	return hashEntries(c.entries)
}

// write creates the entry e at to.
func write(e entry, to string) error {
	switch {
	case e.mode.IsDir():
		return os.Mkdir(to, 0o755)
	case e.mode&fs.ModeSymlink != 0:
		return os.Symlink(e.link, to)
	}
	data, err := os.ReadFile(e.path)
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, e.mode.Perm())
}

type copier struct {
	src, boundary string          // symlink-resolved roots
	name          string          // base name of src, prefixes paths in errors
	active        map[string]bool // real dirs being walked, to detect symlink cycles
	entries       []entry         // every directory before its contents
}

func newCopier(src, boundary string) (*copier, error) {
	abs, err := filepath.Abs(src)
	if err != nil {
		return nil, err
	}
	srcReal, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	boundaryReal, err := filepath.Abs(boundary)
	if err != nil {
		return nil, err
	}
	if boundaryReal, err = filepath.EvalSymlinks(boundaryReal); err != nil {
		return nil, err
	}
	info, err := os.Stat(srcReal)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", src)
	}
	return &copier{src: srcReal, boundary: boundaryReal, name: filepath.Base(abs), active: map[string]bool{}}, nil
}

// walk lists the entries of the real directory from, whose copy lies at rel
// (slash-separated) below the destination root.
func (c *copier) walk(from, rel string) error {
	if c.active[from] {
		return fmt.Errorf("%s: symlink cycle", c.display(rel))
	}
	c.active[from] = true
	defer delete(c.active, from)

	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if skipped(e.Name()) {
			continue
		}
		p := filepath.Join(from, e.Name())
		entryRel := path.Join(rel, e.Name())
		if e.Type()&fs.ModeSymlink != 0 {
			err = c.addLink(p, entryRel)
		} else {
			err = c.add(p, entryRel)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// add lists the file or directory at the real path from.
func (c *copier) add(from, rel string) error {
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	switch {
	case info.IsDir():
		c.entries = append(c.entries, entry{rel: rel, mode: info.Mode()})
		return c.walk(from, rel)
	case info.Mode().IsRegular():
		c.entries = append(c.entries, entry{rel: rel, mode: info.Mode(), path: from})
		return nil
	default:
		return fmt.Errorf("%s: unsupported file type", c.display(rel))
	}
}

// addLink applies the symlink policy to the link at p.
func (c *copier) addLink(p, rel string) error {
	target, err := filepath.EvalSymlinks(p)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s: broken symlink", c.display(rel))
	}
	if err != nil {
		return fmt.Errorf("%s: %w", c.display(rel), err)
	}
	switch {
	case within(c.src, target):
		// The destination mirrors src, so the link's target keeps its position
		// relative to the destination root.
		targetRel, err := filepath.Rel(c.src, target)
		if err != nil {
			return err
		}
		link, err := filepath.Rel(filepath.Dir(filepath.FromSlash(rel)), targetRel)
		if err != nil {
			return err
		}
		c.entries = append(c.entries, entry{rel: rel, mode: fs.ModeSymlink, link: link})
		return nil
	case within(c.boundary, target):
		return c.add(target, rel)
	default:
		return fmt.Errorf("%s: symlink points outside the source", c.display(rel))
	}
}

func (c *copier) display(rel string) string {
	return path.Join(filepath.ToSlash(c.name), rel)
}
