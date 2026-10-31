package skill

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// maxDepth is how far below the start directory Discover looks for skills.
const maxDepth = 6

// Discover returns the absolute paths of the skill directories under root,
// sorted (design §4).
//
// If root contains SKILL.md it is the only result. Otherwise directories up to
// maxDepth below root are searched, skipping .git and node_modules; a directory
// with SKILL.md is a result and is not descended into. Symlinked directories are
// followed only when they resolve inside root. Results are real paths.
func Discover(root string) ([]string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	top, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, err
	}
	if hasSkillFile(top) {
		return []string{top}, nil
	}

	// Breadth-first, so that a directory reachable along several paths (through
	// symlinks) is first visited at its smallest depth.
	type item struct {
		dir   string
		depth int
	}
	queue := []item{{top, 0}}
	visited := map[string]bool{top: true}
	var found []string
	for len(queue) > 0 {
		it := queue[0]
		queue = queue[1:]
		entries, err := os.ReadDir(it.dir)
		if err != nil {
			if it.depth == 0 {
				return nil, err
			}
			continue
		}
		for _, e := range entries {
			if e.Name() == ".git" || e.Name() == "node_modules" {
				continue
			}
			dir := filepath.Join(it.dir, e.Name())
			switch {
			case e.IsDir():
			case e.Type()&fs.ModeSymlink != 0:
				target, err := filepath.EvalSymlinks(dir)
				if err != nil || !within(top, target) {
					continue
				}
				if info, err := os.Stat(target); err != nil || !info.IsDir() {
					continue
				}
				dir = target
			default:
				continue
			}
			if visited[dir] {
				continue
			}
			visited[dir] = true
			if hasSkillFile(dir) {
				found = append(found, dir)
			} else if it.depth+1 < maxDepth {
				queue = append(queue, item{dir, it.depth + 1})
			}
		}
	}
	slices.Sort(found)
	return found, nil
}

func hasSkillFile(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, FileName))
	return err == nil && info.Mode().IsRegular()
}

// within reports whether path is root or inside it. Both must be clean,
// absolute and symlink-resolved.
func within(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && filepath.IsLocal(rel)
}
