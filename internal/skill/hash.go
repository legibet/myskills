package skill

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Hash returns the content hash of dir as "v1:<hex sha256>" (docs/library.md).
//
// Entries below dir (not dir itself) are hashed in lexical order of their
// slash-separated relative path, skipping .git and .DS_Store at any depth.
// Each entry writes a kind byte ('F', 'D' or 'L') and its length-prefixed path;
// a file then writes an executable flag byte and its length-prefixed content, a
// symlink its length-prefixed target. Lengths are uint64 big-endian. Symlinks
// inside dir are not followed; dir itself may be a symlink.
func Hash(dir string) (string, error) {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	var entries []entry
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			if !d.IsDir() {
				return fmt.Errorf("%s is not a directory", dir)
			}
			return nil
		}
		if skipped(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		e := entry{rel: filepath.ToSlash(rel), mode: info.Mode(), path: path}
		if e.mode&fs.ModeSymlink != 0 {
			if e.link, err = os.Readlink(path); err != nil {
				return err
			}
		}
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		return "", err
	}
	return hashEntries(entries)
}

// entry is a file, directory or symlink below a skill directory.
type entry struct {
	rel  string // slash-separated path below the root
	mode fs.FileMode
	path string // where a file's content is read
	link string // a symlink's target
}

// hashEntries hashes entries as Hash describes, sorting them first.
func hashEntries(entries []entry) (string, error) {
	slices.SortFunc(entries, func(a, b entry) int { return strings.Compare(a.rel, b.rel) })
	h := sha256.New()
	for _, e := range entries {
		switch {
		case e.mode.IsDir():
			h.Write([]byte{'D'})
			writeField(h, []byte(e.rel))
		case e.mode.IsRegular():
			data, err := os.ReadFile(e.path)
			if err != nil {
				return "", err
			}
			var exec byte
			if e.mode&0o111 != 0 {
				exec = 1
			}
			h.Write([]byte{'F'})
			writeField(h, []byte(e.rel))
			h.Write([]byte{exec})
			writeField(h, data)
		case e.mode&fs.ModeSymlink != 0:
			h.Write([]byte{'L'})
			writeField(h, []byte(e.rel))
			writeField(h, []byte(e.link))
		default:
			return "", errors.New(e.rel + ": unsupported file type")
		}
	}
	return "v1:" + hex.EncodeToString(h.Sum(nil)), nil
}

func writeField(h hash.Hash, b []byte) {
	h.Write(binary.BigEndian.AppendUint64(nil, uint64(len(b))))
	h.Write(b)
}

// skipped reports whether an entry is left out of hashes and copies.
func skipped(name string) bool {
	return name == ".git" || name == ".DS_Store"
}
