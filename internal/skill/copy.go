package skill

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Copy copies the skill directory src to dst, which must not exist (its parent
// must) and must not lie inside src. boundary is the root of the source the
// skill came from: symlinks that leave src but stay inside boundary are
// dereferenced; other escaping, absolute or broken symlinks fail the copy
// (design §4). dst is removed on failure.
//
// Symlinks that resolve inside src are recreated as relative symlinks. Regular
// files keep their permission bits; .git and .DS_Store are skipped.
func Copy(src, dst, boundary string) error {
	abs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	srcReal, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return err
	}
	boundaryReal, err := filepath.Abs(boundary)
	if err != nil {
		return err
	}
	if boundaryReal, err = filepath.EvalSymlinks(boundaryReal); err != nil {
		return err
	}
	info, err := os.Stat(srcReal)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return err
	}
	parentReal, err := filepath.EvalSymlinks(filepath.Dir(dstAbs))
	if err != nil {
		return err
	}
	if within(srcReal, parentReal) {
		// The copy would contain itself, recursing until paths get too long.
		return fmt.Errorf("can't copy %s into itself", src)
	}

	if err := os.Mkdir(dst, 0o755); err != nil {
		return err
	}
	c := copier{src: srcReal, boundary: boundaryReal, name: filepath.Base(abs), active: map[string]bool{}}
	if err := c.copyDir(srcReal, dst, ""); err != nil {
		_ = os.RemoveAll(dst)
		return err
	}
	return nil
}

type copier struct {
	src, boundary string          // symlink-resolved roots
	name          string          // base name of src, prefixes paths in errors
	active        map[string]bool // real dirs being copied, to detect symlink cycles
}

// copyDir copies the entries of the real directory from into to. rel is the
// position of to below the destination root.
func (c *copier) copyDir(from, to, rel string) error {
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
		path := filepath.Join(from, e.Name())
		target := filepath.Join(to, e.Name())
		entryRel := filepath.Join(rel, e.Name())
		if e.Type()&fs.ModeSymlink != 0 {
			err = c.copyLink(path, target, entryRel)
		} else {
			err = c.copyEntry(path, target, entryRel)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// copyEntry copies the file or directory at the real path from to to.
func (c *copier) copyEntry(from, to, rel string) error {
	info, err := os.Lstat(from)
	if err != nil {
		return err
	}
	switch {
	case info.IsDir():
		if err := os.Mkdir(to, 0o755); err != nil {
			return err
		}
		return c.copyDir(from, to, rel)
	case info.Mode().IsRegular():
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		return os.WriteFile(to, data, info.Mode().Perm())
	default:
		return fmt.Errorf("%s: unsupported file type", c.display(rel))
	}
}

// copyLink applies the symlink policy to the link at path.
func (c *copier) copyLink(path, to, rel string) error {
	target, err := filepath.EvalSymlinks(path)
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
		link, err := filepath.Rel(filepath.Dir(rel), targetRel)
		if err != nil {
			return err
		}
		return os.Symlink(link, to)
	case within(c.boundary, target):
		return c.copyEntry(target, to, rel)
	default:
		return fmt.Errorf("%s: symlink points outside the source", c.display(rel))
	}
}

func (c *copier) display(rel string) string {
	return filepath.ToSlash(filepath.Join(c.name, rel))
}
